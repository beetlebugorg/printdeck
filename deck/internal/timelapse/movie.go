package timelapse

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"os"
	"sync"
	"time"

	"printdeck/internal/jpegdec"
)

// Movie is a time lapse video that is open for the player. It keeps the index of the frames (the
// sample table of the file) in memory, and reads one frame at a time from the file by its offset.
// Movie reads files of WriteMOV, and other QuickTime files with one track of JPEG frames.
type Movie struct {
	W, H int // the size of the frames
	FPS  int // the frames a second

	f       io.ReaderAt
	closer  io.Closer
	offsets []int64 // the offset of each frame in the file
	sizes   []uint32
}

// The largest frame and the largest index that OpenMovie accepts.
const (
	maxFrameBytes = 16 << 20
	maxIndexBytes = 8 << 20
)

// OpenMovie opens the video at path. Close it after use.
func OpenMovie(path string) (*Movie, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	m, err := ReadMovie(f, fi.Size())
	if err != nil {
		f.Close()
		return nil, err
	}
	m.closer = f
	return m, nil
}

// ReadMovie reads the index of a video of size bytes from r.
func ReadMovie(r io.ReaderAt, size int64) (*Movie, error) {
	moov, err := findAtom(r, 0, size, "moov")
	if err != nil {
		return nil, err
	}
	if moov.size > maxIndexBytes {
		return nil, errors.New("timelapse: the index of the video is too large")
	}
	b := make([]byte, moov.size)
	if _, err := r.ReadAt(b, moov.at); err != nil {
		return nil, err
	}
	trak, ok := videoTrack(b)
	if !ok {
		return nil, errors.New("timelapse: the video has no JPEG track")
	}
	m := &Movie{f: r}
	if err := m.index(trak, size); err != nil {
		return nil, err
	}
	return m, nil
}

// Close closes the file of the video.
func (m *Movie) Close() error {
	if m.closer == nil {
		return nil
	}
	return m.closer.Close()
}

// Len is the count of frames.
func (m *Movie) Len() int { return len(m.offsets) }

// Length is the play time of the video.
func (m *Movie) Length() time.Duration {
	return time.Duration(len(m.offsets)) * time.Second / time.Duration(m.FPS)
}

// JPEG reads the JPEG of frame i from the file.
func (m *Movie) JPEG(i int) ([]byte, error) {
	if i < 0 || i >= len(m.offsets) {
		return nil, fmt.Errorf("timelapse: no frame %d", i)
	}
	b := make([]byte, m.sizes[i])
	if _, err := m.f.ReadAt(b, m.offsets[i]); err != nil {
		return nil, err
	}
	return b, nil
}

// Frame reads frame i for a picture of width w pixels on the screen.
func (m *Movie) Frame(i, w int) (*Picture, error) {
	b, err := m.JPEG(i)
	if err != nil {
		return nil, err
	}
	return NewPicture(b, m.W, m.H, w), nil
}

// Picture is a frame of a video for the screen (gfx.Photo): a browser loads the JPEG of the file
// as it is, and the deck decodes it at 1/2, 1/4 or 1/8 of its size when that is enough for the
// screen. The decode is done once, when the deck draws the picture first.
type Picture struct {
	jpeg  []byte
	w, h  int
	shift int

	once sync.Once
	pic  image.Image
}

// NewPicture returns the picture of the JPEG b of w x h pixels, for a width of screen pixels.
func NewPicture(b []byte, w, h, screen int) *Picture {
	return &Picture{jpeg: b, w: w, h: h, shift: decodeShift(w, screen)}
}

// decodeShift is the smallest decode of a picture of width w for screen pixels: 3 for 1/8 of the
// size, 2 for 1/4, 1 for 1/2 and 0 for the whole picture. The decode can be up to 1/5 narrower
// than the screen: the player then shows the picture at the size of the decode, with no scale.
// That costs less than the decode of the whole picture.
func decodeShift(w, screen int) int {
	for s := 3; s >= 1; s-- {
		if ((w+1<<s-1)>>s)*5 >= screen*4 {
			return s
		}
	}
	return 0
}

// DecodeWidth is the width of the decode of a picture of width w for screen pixels.
func DecodeWidth(w, screen int) int {
	s := decodeShift(w, screen)
	return (w + 1<<s - 1) >> s
}

// JPEG returns the JPEG of the file.
func (p *Picture) JPEG() []byte { return p.jpeg }

// Picture returns the decoded picture. A bad JPEG gives a gray picture.
func (p *Picture) Picture() image.Image {
	p.once.Do(func() {
		img, err := jpegdec.DecodeScaled(bytes.NewReader(p.jpeg), p.shift)
		if err != nil {
			img = image.NewGray(image.Rect(0, 0, max(1, p.w>>p.shift), max(1, p.h>>p.shift)))
		}
		p.pic = img
	})
	return p.pic
}

// The image.Image of the picture, at the size of the video. At is slow: draw the Picture.
func (p *Picture) ColorModel() color.Model { return color.YCbCrModel }
func (p *Picture) Bounds() image.Rectangle { return image.Rect(0, 0, p.w, p.h) }
func (p *Picture) At(x, y int) color.Color {
	pic := p.Picture()
	b := pic.Bounds()
	return pic.At(b.Min.X+x*b.Dx()/p.w, b.Min.Y+y*b.Dy()/p.h)
}

// span is the place of an atom in a file: the data after its header.
type span struct{ at, size int64 }

// findAtom returns the data of the first atom of the kind in [at, end) of r, at the top level.
func findAtom(r io.ReaderAt, at, end int64, kind string) (span, error) {
	var h [16]byte
	for at+8 <= end {
		if _, err := r.ReadAt(h[:8], at); err != nil {
			return span{}, err
		}
		size, head := int64(binary.BigEndian.Uint32(h[:4])), int64(8)
		switch size {
		case 0: // to the end of the file
			size = end - at
		case 1: // a 64 bit size after the type
			if _, err := r.ReadAt(h[8:16], at+8); err != nil {
				return span{}, err
			}
			size, head = int64(binary.BigEndian.Uint64(h[8:16])), 16
		}
		if size < head || at+size > end {
			return span{}, errors.New("timelapse: a bad atom in the video")
		}
		if string(h[4:8]) == kind {
			return span{at + head, size - head}, nil
		}
		at += size
	}
	return span{}, fmt.Errorf("timelapse: the video has no %s", kind)
}

// child returns the data of the first atom of the kind in b, or false.
func child(b []byte, kind string) ([]byte, bool) {
	for len(b) >= 8 {
		size := int(binary.BigEndian.Uint32(b))
		if size < 8 || size > len(b) {
			return nil, false
		}
		if string(b[4:8]) == kind {
			return b[8:size], true
		}
		b = b[size:]
	}
	return nil, false
}

// atomPath returns the data of the atom at the path of kinds under b, or false.
func atomPath(b []byte, kinds ...string) ([]byte, bool) {
	for _, k := range kinds {
		var ok bool
		if b, ok = child(b, k); !ok {
			return nil, false
		}
	}
	return b, true
}

// videoTrack returns the first track of the moov data b with JPEG frames.
func videoTrack(b []byte) ([]byte, bool) {
	for len(b) >= 8 {
		size := int(binary.BigEndian.Uint32(b))
		if size < 8 || size > len(b) {
			return nil, false
		}
		if string(b[4:8]) == "trak" {
			t := b[8:size]
			if stsd, ok := atomPath(t, "mdia", "minf", "stbl", "stsd"); ok && len(stsd) >= 16 {
				if c := string(stsd[12:16]); c == "jpeg" || c == "mjpa" {
					return t, true
				}
			}
		}
		b = b[size:]
	}
	return nil, false
}

// index reads the size, the rate and the place of each frame from the track trak, in a file of
// size bytes.
func (m *Movie) index(trak []byte, size int64) error {
	bad := errors.New("timelapse: a bad index in the video")
	stbl, ok := atomPath(trak, "mdia", "minf", "stbl")
	mdhd, ok2 := atomPath(trak, "mdia", "mdhd")
	if !ok || !ok2 || len(mdhd) < 24 || mdhd[0] != 0 {
		return bad
	}
	scale := binary.BigEndian.Uint32(mdhd[12:])
	stsd, _ := child(stbl, "stsd")
	stts, _ := child(stbl, "stts")
	stsc, _ := child(stbl, "stsc")
	stsz, _ := child(stbl, "stsz")
	stco, _ := child(stbl, "stco")
	co64, is64 := child(stbl, "co64")
	if is64 {
		stco = co64
	}
	if len(stsd) < 16+28 || len(stts) < 16 || len(stsc) < 8 || len(stsz) < 12 || len(stco) < 8 {
		return bad
	}
	// The sample entry: 8 bytes of its header, then the width and the height at 24 and 26.
	m.W, m.H = int(binary.BigEndian.Uint16(stsd[16+24:])), int(binary.BigEndian.Uint16(stsd[16+26:]))
	if delta := binary.BigEndian.Uint32(stts[12:]); delta > 0 && scale > 0 {
		m.FPS = int((scale + delta/2) / delta)
	}
	if m.W < 1 || m.H < 1 || m.FPS < 1 {
		return bad
	}

	n := int(binary.BigEndian.Uint32(stsz[8:]))
	if n < 1 || n > maxIndexBytes/4 {
		return bad
	}
	m.sizes = make([]uint32, n)
	if same := binary.BigEndian.Uint32(stsz[4:]); same > 0 {
		for i := range m.sizes {
			m.sizes[i] = same
		}
	} else {
		if len(stsz) < 12+4*n {
			return bad
		}
		for i := range m.sizes {
			m.sizes[i] = binary.BigEndian.Uint32(stsz[12+4*i:])
		}
	}

	chunks := int(binary.BigEndian.Uint32(stco[4:]))
	width := 4
	if is64 {
		width = 8
	}
	if chunks < 1 || len(stco) < 8+width*chunks {
		return bad
	}
	chunkAt := func(c int) int64 {
		if is64 {
			return int64(binary.BigEndian.Uint64(stco[8+8*c:]))
		}
		return int64(binary.BigEndian.Uint32(stco[8+4*c:]))
	}
	// The sample to chunk table: from its first chunk on, each chunk has the same count of frames.
	runs := int(binary.BigEndian.Uint32(stsc[4:]))
	if runs < 1 || len(stsc) < 8+12*runs {
		return bad
	}
	m.offsets = make([]int64, 0, n)
	for r := 0; r < runs && len(m.offsets) < n; r++ {
		first := int(binary.BigEndian.Uint32(stsc[8+12*r:])) - 1
		per := int(binary.BigEndian.Uint32(stsc[8+12*r+4:]))
		last := chunks
		if r+1 < runs {
			last = int(binary.BigEndian.Uint32(stsc[8+12*(r+1):])) - 1
		}
		if first < 0 || last > chunks || per < 1 {
			return bad
		}
		for c := first; c < last && len(m.offsets) < n; c++ {
			at := chunkAt(c)
			for k := 0; k < per && len(m.offsets) < n; k++ {
				i := len(m.offsets)
				if m.sizes[i] > maxFrameBytes || at+int64(m.sizes[i]) > size {
					return bad
				}
				m.offsets = append(m.offsets, at)
				at += int64(m.sizes[i])
			}
		}
	}
	if len(m.offsets) != n {
		return bad
	}
	return nil
}
