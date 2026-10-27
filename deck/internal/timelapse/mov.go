package timelapse

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
)

// WriteMOV writes a QuickTime movie of JPEG frames (the codec "jpeg", Photo-JPEG): each frame is a
// sample of the video track, fps frames a second. All frames have the size w x h. The file has the
// atoms ftyp, mdat (the frames) and moov (the index), in that order.
func WriteMOV(out io.Writer, frames [][]byte, w, h, fps int) error {
	if len(frames) == 0 {
		return errors.New("timelapse: no frames")
	}
	if fps < 1 || w < 1 || h < 1 || w > 0xffff || h > 0xffff {
		return errors.New("timelapse: a bad size or rate")
	}
	var data int64
	for _, f := range frames {
		data += int64(len(f))
	}
	if data+8 > 0xffffffff {
		return errors.New("timelapse: the frames are too large for one file")
	}
	bw := bufio.NewWriterSize(out, 64<<10)

	ftyp := atom("ftyp", []byte("qt  "), u32(0x200), []byte("qt  "))
	bw.Write(ftyp)
	bw.Write(u32(uint32(data + 8)))
	bw.WriteString("mdat")
	for _, f := range frames {
		bw.Write(f)
	}
	first := uint32(len(ftyp) + 8) // the offset of the first frame in the file
	bw.Write(moov(frames, w, h, fps, first))
	return bw.Flush()
}

// moov is the index of the movie: one video track with all frames in one chunk.
func moov(frames [][]byte, w, h, fps int, offset uint32) []byte {
	n := uint32(len(frames))
	matrix := cat(u32(0x10000), u32(0), u32(0), u32(0), u32(0x10000), u32(0), u32(0), u32(0), u32(0x40000000))
	mvhd := full("mvhd", 0, 0, u32(0), u32(0), u32(uint32(fps)), u32(n), u32(0x10000), u16(0x100), make([]byte, 10),
		matrix, u32(0), u32(0), u32(0), u32(0), u32(0), u32(0), u32(2))
	tkhd := full("tkhd", 0, 3, u32(0), u32(0), u32(1), u32(0), u32(n), make([]byte, 8), u16(0), u16(0), u16(0), u16(0),
		matrix, u32(uint32(w)<<16), u32(uint32(h)<<16))
	mdhd := full("mdhd", 0, 0, u32(0), u32(0), u32(uint32(fps)), u32(n), u16(0), u16(0))
	hdlr := full("hdlr", 0, 0, []byte("mhlr"), []byte("vide"), u32(0), u32(0), u32(0), pascal("VideoHandler"))
	vmhd := full("vmhd", 0, 1, u16(0x40), u16(0x8000), u16(0x8000), u16(0x8000))
	dhlr := full("hdlr", 0, 0, []byte("dhlr"), []byte("alis"), u32(0), u32(0), u32(0), pascal("DataHandler"))
	dref := full("dref", 0, 0, u32(1), full("alis", 0, 1))
	dinf := atom("dinf", dref)

	name := make([]byte, 32)
	copy(name, pascal("Photo - JPEG"))
	entry := atom("jpeg", make([]byte, 6), u16(1), // the data reference
		u16(0), u16(0), u32(0), u32(0), u32(512), // the version, the vendor, and the quality
		u16(uint16(w)), u16(uint16(h)), u32(72<<16), u32(72<<16), u32(0), u16(1), name, u16(24), u16(0xffff))
	stsd := full("stsd", 0, 0, u32(1), entry)
	stts := full("stts", 0, 0, u32(1), u32(n), u32(1))
	stsc := full("stsc", 0, 0, u32(1), u32(1), u32(n), u32(1))
	sizes := make([]byte, 0, 4*len(frames))
	for _, f := range frames {
		sizes = append(sizes, u32(uint32(len(f)))...)
	}
	stsz := full("stsz", 0, 0, u32(0), u32(n), sizes)
	stco := full("stco", 0, 0, u32(1), u32(offset))
	stbl := atom("stbl", stsd, stts, stsc, stsz, stco)
	minf := atom("minf", vmhd, dhlr, dinf, stbl)
	mdia := atom("mdia", mdhd, hdlr, minf)
	trak := atom("trak", tkhd, mdia)
	return atom("moov", mvhd, trak)
}

// atom is a QuickTime atom: its size, its type, and the parts of its data.
func atom(kind string, parts ...[]byte) []byte {
	body := cat(parts...)
	return cat(u32(uint32(8+len(body))), []byte(kind), body)
}

// full is an atom with a version and flags before its data.
func full(kind string, version byte, flags uint32, parts ...[]byte) []byte {
	vf := u32(flags & 0xffffff)
	vf[0] = version
	return atom(kind, append([][]byte{vf}, parts...)...)
}

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func u32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }
func u16(v uint16) []byte { return binary.BigEndian.AppendUint16(nil, v) }

// pascal is a string with its length in the first byte.
func pascal(s string) []byte { return append([]byte{byte(len(s))}, s...) }
