// Package fatfs reads a FAT16 or FAT32 file system from a device of 512-byte blocks, such as the SD
// card of a printer. It reads the partition table of the card, the folders with their long file
// names, and the files. It does not write.
package fatfs

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf16"
)

// BlockSize is the size of a block of the device, and of a sector of the file system.
const BlockSize = 512

// Device is a device of 512-byte blocks. ReadBlock reads block lba into b (BlockSize bytes).
type Device interface {
	ReadBlock(lba uint32, b []byte) error
}

// FS is an open file system.
type FS struct {
	dev        Device
	fat32      bool
	start      uint32 // the first block of the volume
	secPerClus uint32
	fatStart   uint32 // the first block of the first FAT
	rootStart  uint32 // FAT16: the first block of the root folder
	rootSecs   uint32 // FAT16: the blocks of the root folder
	rootClus   uint32 // FAT32: the first cluster of the root folder
	dataStart  uint32 // the first block of cluster 2
	clusters   uint32 // the count of clusters

	cacheLBA uint32
	cache    []byte
}

// Entry is a file or a folder in a folder.
type Entry struct {
	Name     string
	Size     int64
	Dir      bool
	Modified time.Time
	cluster  uint32
}

// ErrNotFound is the error of a path that is not on the card.
var ErrNotFound = errors.New("not on the card")

// partitionTypes are the partition types of a FAT16 or FAT32 volume.
var partitionTypes = map[byte]bool{0x04: true, 0x06: true, 0x0b: true, 0x0c: true, 0x0e: true}

// Open reads the boot sector of the volume: at block 0 (a card with no partition table), or at the
// first FAT partition of the partition table.
func Open(dev Device) (*FS, error) {
	f := &FS{dev: dev, cache: make([]byte, BlockSize), cacheLBA: ^uint32(0)}
	b := make([]byte, BlockSize)
	if err := dev.ReadBlock(0, b); err != nil {
		return nil, err
	}
	if b[510] != 0x55 || b[511] != 0xaa {
		return nil, errors.New("the card has no FAT file system")
	}
	if !isBootSector(b) {
		found := false
		for i := 0; i < 4; i++ {
			p := b[446+16*i:]
			if partitionTypes[p[4]] {
				f.start = binary.LittleEndian.Uint32(p[8:])
				found = true
				break
			}
		}
		if !found {
			return nil, errors.New("the card has no FAT partition")
		}
		if err := dev.ReadBlock(f.start, b); err != nil {
			return nil, err
		}
		if !isBootSector(b) {
			return nil, errors.New("the FAT partition has no boot sector")
		}
	}
	le16 := func(o int) uint32 { return uint32(binary.LittleEndian.Uint16(b[o:])) }
	le32 := func(o int) uint32 { return binary.LittleEndian.Uint32(b[o:]) }
	if le16(11) != BlockSize {
		return nil, fmt.Errorf("sectors of %d bytes are not supported", le16(11))
	}
	f.secPerClus = uint32(b[13])
	rsvd, nFATs, rootEnts := le16(14), uint32(b[16]), le16(17)
	fatSize := le16(22)
	if fatSize == 0 {
		fatSize = le32(36)
	}
	total := le16(19)
	if total == 0 {
		total = le32(32)
	}
	if f.secPerClus == 0 || nFATs == 0 || fatSize == 0 {
		return nil, errors.New("the boot sector is not valid")
	}
	f.rootSecs = (rootEnts*32 + BlockSize - 1) / BlockSize
	f.fatStart = f.start + rsvd
	f.rootStart = f.fatStart + nFATs*fatSize
	f.dataStart = f.rootStart + f.rootSecs
	f.clusters = (total - rsvd - nFATs*fatSize - f.rootSecs) / f.secPerClus
	switch {
	case f.clusters < 4085:
		return nil, errors.New("FAT12 is not supported")
	case f.clusters >= 65525:
		f.fat32 = true
		f.rootClus = le32(44)
	}
	return f, nil
}

// isBootSector is true for a boot sector and false for a partition table: a boot sector starts
// with a jump.
func isBootSector(b []byte) bool {
	return (b[0] == 0xeb || b[0] == 0xe9) && binary.LittleEndian.Uint16(b[11:]) == BlockSize
}

func (f *FS) block(lba uint32) ([]byte, error) {
	if lba != f.cacheLBA {
		if err := f.dev.ReadBlock(lba, f.cache); err != nil {
			f.cacheLBA = ^uint32(0)
			return nil, err
		}
		f.cacheLBA = lba
	}
	return f.cache, nil
}

// next returns the cluster after c in its chain, and false at the end of the chain.
func (f *FS) next(c uint32) (uint32, bool, error) {
	size := uint32(2)
	if f.fat32 {
		size = 4
	}
	off := c * size
	b, err := f.block(f.fatStart + off/BlockSize)
	if err != nil {
		return 0, false, err
	}
	var n uint32
	if f.fat32 {
		n = binary.LittleEndian.Uint32(b[off%BlockSize:]) & 0x0fffffff
		if n >= 0x0ffffff8 {
			return 0, false, nil
		}
	} else {
		n = uint32(binary.LittleEndian.Uint16(b[off%BlockSize:]))
		if n >= 0xfff8 {
			return 0, false, nil
		}
	}
	if n < 2 || n >= f.clusters+2 {
		return 0, false, fmt.Errorf("the chain of cluster %d is broken", c)
	}
	return n, true, nil
}

func (f *FS) clusterLBA(c uint32) uint32 { return f.dataStart + (c-2)*f.secPerClus }

// dirBlocks calls each with each block of the folder that starts at cluster c (0: the root
// folder), until each returns false.
func (f *FS) dirBlocks(c uint32, each func(b []byte) bool) error {
	if c == 0 && !f.fat32 {
		for i := uint32(0); i < f.rootSecs; i++ {
			b, err := f.block(f.rootStart + i)
			if err != nil {
				return err
			}
			if !each(b) {
				return nil
			}
		}
		return nil
	}
	if c == 0 {
		c = f.rootClus
	}
	for n := 0; ; n++ {
		if n > int(f.clusters) {
			return errors.New("a folder has a loop in its chain")
		}
		for i := uint32(0); i < f.secPerClus; i++ {
			b, err := f.block(f.clusterLBA(c) + i)
			if err != nil {
				return err
			}
			if !each(b) {
				return nil
			}
		}
		nc, ok, err := f.next(c)
		if err != nil || !ok {
			return err
		}
		c = nc
	}
}

// readDir reads the entries of the folder that starts at cluster c (0: the root folder). It
// leaves out ".", "..", deleted entries and the volume label.
func (f *FS) readDir(c uint32) ([]Entry, error) {
	var out []Entry
	var lfn []uint16
	var lfnSum byte
	lfnNext := 0 // the order number of the next long name entry, 0 for none
	err := f.dirBlocks(c, func(b []byte) bool {
		for o := 0; o < BlockSize; o += 32 {
			e := b[o : o+32]
			switch {
			case e[0] == 0:
				return false
			case e[0] == 0xe5:
				lfn, lfnNext = nil, 0
				continue
			case e[11]&0x3f == 0x0f: // a part of a long name
				ord := int(e[0] & 0x1f)
				if e[0]&0x40 != 0 {
					lfn, lfnSum, lfnNext = make([]uint16, 13*ord), e[13], ord
				}
				if ord != lfnNext || ord == 0 || e[13] != lfnSum {
					lfn, lfnNext = nil, 0
					continue
				}
				part := lfn[13*(ord-1):]
				for i, at := range []int{1, 3, 5, 7, 9, 14, 16, 18, 20, 22, 24, 28, 30} {
					part[i] = binary.LittleEndian.Uint16(e[at:])
				}
				lfnNext--
				continue
			case e[11]&0x08 != 0: // the volume label
				lfn, lfnNext = nil, 0
				continue
			}
			short := shortName(e)
			name := short
			if lfnNext == 0 && lfn != nil && checksum(e[:11]) == lfnSum {
				name = decodeLFN(lfn)
			}
			lfn, lfnNext = nil, 0
			if short == "." || short == ".." {
				continue
			}
			out = append(out, Entry{
				Name:     name,
				Size:     int64(binary.LittleEndian.Uint32(e[28:])),
				Dir:      e[11]&0x10 != 0,
				Modified: fatTime(binary.LittleEndian.Uint16(e[24:]), binary.LittleEndian.Uint16(e[22:])),
				cluster:  uint32(binary.LittleEndian.Uint16(e[20:]))<<16 | uint32(binary.LittleEndian.Uint16(e[26:])),
			})
		}
		return true
	})
	return out, err
}

func shortName(e []byte) string {
	base := strings.TrimRight(string(e[0:8]), " ")
	ext := strings.TrimRight(string(e[8:11]), " ")
	if e[0] == 0x05 {
		base = "\xe5" + base[1:]
	}
	if e[12]&0x08 != 0 {
		base = strings.ToLower(base)
	}
	if e[12]&0x10 != 0 {
		ext = strings.ToLower(ext)
	}
	if ext == "" {
		return base
	}
	return base + "." + ext
}

func decodeLFN(u []uint16) string {
	for i, c := range u {
		if c == 0 {
			u = u[:i]
			break
		}
	}
	return string(utf16.Decode(u))
}

// checksum is the checksum of a short name, in its long name entries.
func checksum(name []byte) byte {
	var s byte
	for _, c := range name {
		s = (s&1)<<7 + s>>1 + c
	}
	return s
}

func fatTime(d, t uint16) time.Time {
	if d == 0 {
		return time.Time{}
	}
	return time.Date(1980+int(d>>9), time.Month(d>>5&0xf), int(d&0x1f), int(t>>11), int(t>>5&0x3f), int(t&0x1f)*2, 0, time.Local)
}

// lookup finds the entry of path ("" or "/" is the root folder). The names match with no regard
// to case, as on the card.
func (f *FS) lookup(path string) (Entry, error) {
	e := Entry{Dir: true}
	for _, part := range strings.Split(strings.Trim(path, "/"), "/") {
		if part == "" {
			continue
		}
		if !e.Dir {
			return Entry{}, ErrNotFound
		}
		list, err := f.readDir(e.cluster)
		if err != nil {
			return Entry{}, err
		}
		found := false
		for _, c := range list {
			if strings.EqualFold(c.Name, part) {
				e, found = c, true
				break
			}
		}
		if !found {
			return Entry{}, ErrNotFound
		}
	}
	return e, nil
}

// ReadDir returns the files and the folders in the folder path.
func (f *FS) ReadDir(path string) ([]Entry, error) {
	e, err := f.lookup(path)
	if err != nil {
		return nil, err
	}
	if !e.Dir {
		return nil, fmt.Errorf("%s is not a folder", path)
	}
	return f.readDir(e.cluster)
}

// Open opens the file path for reading.
func (f *FS) Open(path string) (*File, error) {
	e, err := f.lookup(path)
	if err != nil {
		return nil, err
	}
	if e.Dir {
		return nil, fmt.Errorf("%s is a folder", path)
	}
	return &File{fs: f, Entry: e, cluster: e.cluster}, nil
}

// File is an open file. It reads from the start to the end.
type File struct {
	Entry
	fs      *FS
	cluster uint32 // the cluster of the next block
	sector  uint32 // the next block in the cluster
	pos     int64
}

// Read reads the next bytes of the file.
func (r *File) Read(p []byte) (int, error) {
	if r.pos >= r.Size {
		return 0, io.EOF
	}
	if r.sector == r.fs.secPerClus {
		c, ok, err := r.fs.next(r.cluster)
		if err != nil {
			return 0, err
		}
		if !ok {
			return 0, io.ErrUnexpectedEOF
		}
		r.cluster, r.sector = c, 0
	}
	if r.cluster < 2 {
		return 0, io.ErrUnexpectedEOF
	}
	b, err := r.fs.block(r.fs.clusterLBA(r.cluster) + r.sector)
	if err != nil {
		return 0, err
	}
	off := int(r.pos % BlockSize)
	n := copy(p, b[off:min(BlockSize, off+int(r.Size-r.pos))])
	r.pos += int64(n)
	if r.pos%BlockSize == 0 {
		r.sector++
	}
	return n, nil
}
