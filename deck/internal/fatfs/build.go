package fatfs

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
)

// BuildFile is a file of an image of Build. Path has "/" between the folders.
type BuildFile struct {
	Path     string
	Data     []byte
	Modified time.Time
}

// partStart is the first block of the partition of an image: 1 MiB, as on a new SD card.
const partStart = 2048

// Build makes the image of a card with a partition table and one FAT16 or FAT32 partition that
// holds files. The simulated printer board has it as its SD card, and the tests read it.
func Build(files []BuildFile, fat32 bool) ([]byte, error) {
	root := &node{dir: true}
	for _, f := range files {
		parts := strings.Split(strings.Trim(f.Path, "/"), "/")
		n := root
		for i, p := range parts {
			last := i == len(parts)-1
			c := n.child(p)
			if c == nil {
				c = &node{name: p, dir: !last, modified: f.Modified}
				n.kids = append(n.kids, c)
			}
			if last {
				if c.dir {
					return nil, fmt.Errorf("%s is a folder", f.Path)
				}
				c.data = f.Data
			}
			n = c
		}
	}

	spc, ent, rsvd := uint32(4), uint32(2), uint32(1)
	if fat32 {
		spc, ent, rsvd = 1, 4, 32
	}
	clusBytes := spc * BlockSize
	need := uint32(0)
	var count func(n *node, isRoot bool)
	count = func(n *node, isRoot bool) {
		if n.dir {
			if !isRoot || fat32 {
				need += clusFor(uint32(n.dirBytes(isRoot)), clusBytes)
			}
			for _, k := range n.kids {
				count(k, false)
			}
			return
		}
		need += clusFor(uint32(len(n.data)), clusBytes)
	}
	count(root, true)
	if !fat32 && root.dirBytes(true) > 512*32 {
		return nil, fmt.Errorf("too many files in the root folder for FAT16")
	}
	clusters := need + 16
	if fat32 {
		clusters = max(clusters, 65525+16)
	} else {
		clusters = max(clusters, 4085+16)
		if clusters >= 65525 {
			return nil, fmt.Errorf("the files are too large for FAT16")
		}
	}
	fatSize := ((clusters+2)*ent + BlockSize - 1) / BlockSize
	rootSecs := uint32(0)
	if !fat32 {
		rootSecs = 512 * 32 / BlockSize
	}
	dataStart := rsvd + 2*fatSize + rootSecs
	total := dataStart + clusters*spc
	img := make([]byte, int(partStart+total)*BlockSize)

	// The partition table.
	mbr := img[:BlockSize]
	p := mbr[446:]
	p[4] = 0x06
	if fat32 {
		p[4] = 0x0c
	}
	binary.LittleEndian.PutUint32(p[8:], partStart)
	binary.LittleEndian.PutUint32(p[12:], total)
	mbr[510], mbr[511] = 0x55, 0xaa

	// The boot sector.
	vol := img[partStart*BlockSize:]
	bs := vol[:BlockSize]
	copy(bs, []byte{0xeb, 0x3c, 0x90})
	copy(bs[3:], "PRINTDCK")
	put16 := func(o int, v uint32) { binary.LittleEndian.PutUint16(bs[o:], uint16(v)) }
	put32 := func(o int, v uint32) { binary.LittleEndian.PutUint32(bs[o:], v) }
	put16(11, BlockSize)
	bs[13] = byte(spc)
	put16(14, rsvd)
	bs[16] = 2
	bs[21] = 0xf8
	put16(24, 63)
	put16(26, 255)
	put32(28, partStart)
	put32(32, total)
	if fat32 {
		put32(36, fatSize)
		put32(44, 2)
		put16(48, 1)
		put16(50, 6)
		bs[64], bs[66] = 0x80, 0x29
		copy(bs[71:], "NO NAME    FAT32   ")
	} else {
		put16(17, 512)
		put16(22, fatSize)
		bs[36], bs[38] = 0x80, 0x29
		copy(bs[43:], "NO NAME    FAT16   ")
	}
	bs[510], bs[511] = 0x55, 0xaa

	fat := func(c, v uint32) {
		for i := uint32(0); i < 2; i++ {
			at := (rsvd+i*fatSize)*BlockSize + c*ent
			if fat32 {
				binary.LittleEndian.PutUint32(vol[at:], v)
			} else {
				binary.LittleEndian.PutUint16(vol[at:], uint16(v))
			}
		}
	}
	eoc := uint32(0xffff)
	if fat32 {
		eoc = 0x0fffffff
	}
	fat(0, 0x0ffffff8&eoc)
	fat(1, eoc)
	nextFree := uint32(2)
	// alloc gives a chain of clusters for size bytes (at least one), and returns its first cluster.
	alloc := func(size uint32) uint32 {
		n := max(1, clusFor(size, clusBytes))
		first := nextFree
		for i := uint32(0); i < n; i++ {
			c := nextFree + i
			if i == n-1 {
				fat(c, eoc)
			} else {
				fat(c, c+1)
			}
		}
		nextFree += n
		return first
	}
	write := func(first uint32, data []byte) {
		copy(vol[(dataStart+(first-2)*spc)*BlockSize:], data)
	}

	// The clusters of the folders and the files, then the entries of each folder.
	var place func(n *node, isRoot bool)
	place = func(n *node, isRoot bool) {
		if n.dir {
			if !isRoot || fat32 {
				n.cluster = alloc(uint32(n.dirBytes(isRoot)))
			}
			for _, k := range n.kids {
				place(k, false)
			}
			return
		}
		if len(n.data) > 0 {
			n.cluster = alloc(uint32(len(n.data)))
			write(n.cluster, n.data)
		}
	}
	place(root, true)
	var fill func(n *node, parent uint32, isRoot bool)
	fill = func(n *node, parent uint32, isRoot bool) {
		if !n.dir {
			return
		}
		b := n.entries(parent, isRoot)
		if isRoot && !fat32 {
			copy(vol[(rsvd+2*fatSize)*BlockSize:], b)
		} else {
			write(n.cluster, b)
		}
		self := n.cluster
		if isRoot {
			self = 0
		}
		for _, k := range n.kids {
			fill(k, self, false)
		}
	}
	fill(root, 0, true)
	return img, nil
}

func clusFor(size, clusBytes uint32) uint32 { return (size + clusBytes - 1) / clusBytes }

type node struct {
	name     string
	dir      bool
	data     []byte
	modified time.Time
	kids     []*node
	cluster  uint32
}

func (n *node) child(name string) *node {
	for _, k := range n.kids {
		if k.name == name {
			return k
		}
	}
	return nil
}

// lfnCount is the count of long name entries of a name: 0 for a name that is a short name.
func lfnCount(name string) int {
	if isShort(name) {
		return 0
	}
	return (len(utf16.Encode([]rune(name))) + 12) / 13
}

func isShort(name string) bool {
	base, ext, _ := strings.Cut(name, ".")
	if base == "" || len(base) > 8 || len(ext) > 3 || strings.Count(name, ".") > 1 {
		return false
	}
	for _, r := range name {
		if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func (n *node) dirBytes(isRoot bool) int {
	e := 0
	if !isRoot {
		e = 2 // . and ..
	}
	for _, k := range n.kids {
		e += 1 + lfnCount(k.name)
	}
	return e * 32
}

// entries is the content of the folder n.
func (n *node) entries(parent uint32, isRoot bool) []byte {
	var out []byte
	entry := func(name [11]byte, attr byte, cluster, size uint32, t time.Time) []byte {
		e := make([]byte, 32)
		copy(e, name[:])
		e[11] = attr
		binary.LittleEndian.PutUint16(e[20:], uint16(cluster>>16))
		binary.LittleEndian.PutUint16(e[26:], uint16(cluster))
		binary.LittleEndian.PutUint32(e[28:], size)
		if !t.IsZero() {
			d := uint16(t.Year()-1980)<<9 | uint16(t.Month())<<5 | uint16(t.Day())
			tm := uint16(t.Hour())<<11 | uint16(t.Minute())<<5 | uint16(t.Second()/2)
			binary.LittleEndian.PutUint16(e[22:], tm)
			binary.LittleEndian.PutUint16(e[24:], d)
		}
		return e
	}
	if !isRoot {
		var dot, dotdot [11]byte
		copy(dot[:], ".          ")
		copy(dotdot[:], "..         ")
		out = append(out, entry(dot, 0x10, n.cluster, 0, n.modified)...)
		out = append(out, entry(dotdot, 0x10, parent, 0, n.modified)...)
	}
	kids := append([]*node(nil), n.kids...)
	sort.SliceStable(kids, func(i, j int) bool { return kids[i].name < kids[j].name })
	used := map[string]bool{}
	for _, k := range kids {
		short := makeShort(k.name, used)
		if c := lfnCount(k.name); c > 0 {
			u := utf16.Encode([]rune(k.name))
			sum := checksum(short[:])
			for ord := c; ord >= 1; ord-- {
				e := make([]byte, 32)
				e[0] = byte(ord)
				if ord == c {
					e[0] |= 0x40
				}
				e[11], e[13] = 0x0f, sum
				for i, at := range []int{1, 3, 5, 7, 9, 14, 16, 18, 20, 22, 24, 28, 30} {
					idx := (ord-1)*13 + i
					v := uint16(0xffff)
					switch {
					case idx < len(u):
						v = u[idx]
					case idx == len(u):
						v = 0
					}
					binary.LittleEndian.PutUint16(e[at:], v)
				}
				out = append(out, e...)
			}
		}
		attr := byte(0x20)
		size := uint32(len(k.data))
		if k.dir {
			attr, size = 0x10, 0
		}
		out = append(out, entry(short, attr, k.cluster, size, k.modified)...)
	}
	return out
}

// makeShort makes the short name of a name: the name itself when it is one, else the first six
// letters, "~" and a number, and the first three letters of the extension.
func makeShort(name string, used map[string]bool) [11]byte {
	var s [11]byte
	for i := range s {
		s[i] = ' '
	}
	clean := func(x string, n int) string {
		var b strings.Builder
		for _, r := range strings.ToUpper(x) {
			if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
				b.WriteRune(r)
			}
			if b.Len() == n {
				break
			}
		}
		return b.String()
	}
	if isShort(name) {
		base, ext, _ := strings.Cut(name, ".")
		copy(s[:8], base)
		copy(s[8:], ext)
		used[string(s[:])] = true
		return s
	}
	base, ext := name, ""
	if i := strings.LastIndex(name, "."); i > 0 {
		base, ext = name[:i], name[i+1:]
	}
	b, e := clean(base, 6), clean(ext, 3)
	if b == "" {
		b = "FILE"
	}
	for n := 1; ; n++ {
		tail := fmt.Sprintf("~%d", n)
		head := b
		if len(head)+len(tail) > 8 {
			head = head[:8-len(tail)]
		}
		for i := range s {
			s[i] = ' '
		}
		copy(s[:8], head+tail)
		copy(s[8:], e)
		if !used[string(s[:])] {
			used[string(s[:])] = true
			return s
		}
	}
}
