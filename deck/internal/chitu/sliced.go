package chitu

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"slices"
)

// The sliced files of ChiTuBox for ChiTu boards (.cbddlp and .ctb): a header, two preview
// pictures, and the layers. The layout comes from other programs that read these files (UVtools
// and others). A .ctb of the Phenom of 2026-10-30 (magic 0x12FD0106, version 5) has this header.

// The magic numbers at the start of a file.
const (
	MagicCBDDLP = 0x12FD0019
	MagicCTB    = 0x12FD0086
	MagicCTBv5  = 0x12FD0106 // ChiTuBox of 2026: the same header, with the layers encrypted
)

// Sliced is the header of a sliced file.
type Sliced struct {
	Version                    uint32
	BedX, BedY, BedZ           float32 // mm
	Height, LayerHeight        float32 // mm
	Exposure, BottomExposure   float32 // s
	BottomLayers, Layers       uint32
	ResX, ResY                 uint32 // the pixels of the screen
	PrintSeconds               uint32
	previewLarge, previewSmall uint32 // the offsets of the headers of the pictures
	Magic                      uint32
	Mirror                     bool   // the screen shows the mirror image of the slicer (projector 1)
	layerTable                 uint32 // the offset of the table of the layers
}

// ReadSliced reads the header of the sliced file data.
func ReadSliced(data []byte) (*Sliced, error) {
	if len(data) < 112 {
		return nil, errors.New("chitu: the file is too short for a sliced file")
	}
	u := func(off int) uint32 { return binary.LittleEndian.Uint32(data[off:]) }
	f := func(off int) float32 { return math.Float32frombits(u(off)) }
	switch u(0) {
	case MagicCBDDLP, MagicCTB, MagicCTBv5:
	default:
		return nil, fmt.Errorf("chitu: no sliced file (magic %#x)", u(0))
	}
	return &Sliced{Version: u(4), BedX: f(8), BedY: f(12), BedZ: f(16), Height: f(28), LayerHeight: f(32),
		Exposure: f(36), BottomExposure: f(40), BottomLayers: u(48), ResX: u(52), ResY: u(56),
		previewLarge: u(60), layerTable: u(64), Layers: u(68), previewSmall: u(72), PrintSeconds: u(76),
		Magic: u(0), Mirror: u(80) == 1}, nil
}

// Preview returns the large preview picture of the sliced file data, or the small one when large
// is false.
func Preview(data []byte, large bool) (image.Image, error) {
	s, err := ReadSliced(data)
	if err != nil {
		return nil, err
	}
	off := s.previewSmall
	if large {
		off = s.previewLarge
	}
	if off == 0 || int(off)+16 > len(data) {
		return nil, errors.New("chitu: the file has no preview")
	}
	u := func(o uint32) uint32 { return binary.LittleEndian.Uint32(data[o:]) }
	w, h, imgOff, imgLen := int(u(off)), int(u(off+4)), u(off+8), u(off+12)
	if w <= 0 || h <= 0 || w > 4096 || h > 4096 || int(imgOff)+int(imgLen) > len(data) {
		return nil, errors.New("chitu: a bad preview")
	}
	return decodePreview(data[imgOff:imgOff+imgLen], w, h)
}

// decodePreview decodes the run-length colors of a preview: each pixel is a 16-bit color (5 bits
// of red, 5 of green, a flag, 5 of blue). With the flag, the next 16 bits are 0x3000 with the
// count of the run minus one in the 12 low bits.
func decodePreview(p []byte, w, h int) (image.Image, error) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	n := 0
	for i := 0; i+1 < len(p) && n < w*h; {
		dot := binary.LittleEndian.Uint16(p[i:])
		i += 2
		run := 1
		if dot&0x0020 != 0 {
			if i+1 >= len(p) {
				break
			}
			run = int(binary.LittleEndian.Uint16(p[i:])&0x0FFF) + 1
			i += 2
		}
		c := color.RGBA{R: uint8(dot>>11) << 3, G: uint8(dot>>6&0x1F) << 3, B: uint8(dot&0x1F) << 3, A: 255}
		for ; run > 0 && n < w*h; run-- {
			img.SetRGBA(n%w, n/w, c)
			n++
		}
	}
	if n < w*h {
		return nil, errors.New("chitu: the preview is short")
	}
	return img, nil
}

// layerDefSize is the size of a layer in the table of the layers: its height, its exposure, its
// time with no light, the offset and the size of its data, and four more words.
const layerDefSize = 36

// LayerImage returns the layer n (from 0) of the .cbddlp file r with the header h, as a gray
// picture about w pixels wide, as the slicer shows it (the mirror of the screen of a printer with
// projector 1). A pixel of the picture with a lit pixel of the screen is at least a third lit, so
// a thin wall shows.
// A layer of a .cbddlp is a run-length code of 1 bit: in each byte, bit 7 is the colour and the 7
// low bits are the length of the run (at most 125 from ChiTuBox). The runs go on past the end of a
// row. A file of the Phenom of 2026-10-30 checked this layout. The layers of a .ctb have another code and a key, and
// LayerImage does not read them.
func LayerImage(r io.ReaderAt, h *Sliced, n, w int) (*image.Gray, error) {
	if h.Magic != MagicCBDDLP {
		return nil, errors.New("chitu: only a .cbddlp has layers that the deck reads")
	}
	if n < 0 || uint32(n) >= h.Layers || h.ResX == 0 || h.ResY == 0 || w <= 0 {
		return nil, errors.New("chitu: no such layer")
	}
	def := make([]byte, layerDefSize)
	if _, err := r.ReadAt(def, int64(h.layerTable)+int64(n)*layerDefSize); err != nil {
		return nil, err
	}
	addr, size := binary.LittleEndian.Uint32(def[12:]), binary.LittleEndian.Uint32(def[16:])
	if size == 0 || size > 64<<20 {
		return nil, fmt.Errorf("chitu: a bad layer size %d", size)
	}
	data := make([]byte, size)
	if _, err := r.ReadAt(data, int64(addr)); err != nil {
		return nil, err
	}
	img := decodeLayer(data, int(h.ResX), int(h.ResY), w)
	if h.Mirror {
		b := img.Bounds()
		for y := 0; y < b.Dy(); y++ {
			row := img.Pix[y*img.Stride : y*img.Stride+b.Dx()]
			slices.Reverse(row)
		}
	}
	return img, nil
}

// decodeLayer decodes the 1-bit run-length code of a layer of resX x resY pixels into a gray
// picture w pixels wide.
func decodeLayer(data []byte, resX, resY, w int) *image.Gray {
	scale := max(1, (resX+w-1)/w)
	gw, gh := (resX+scale-1)/scale, (resY+scale-1)/scale
	lit := make([]int, gw*gh)
	pos, total := 0, resX*resY
	for _, b := range data {
		run := int(b & 0x7F)
		if b&0x80 != 0 {
			for end := min(pos+run, total); pos < end; {
				x, y := pos%resX, pos/resX
				// The rest of the run in this row.
				n := min(end-pos, resX-x)
				for i := 0; i < n; {
					cell := (x + i) / scale
					next := min(n, (cell+1)*scale-x)
					lit[(y/scale)*gw+cell] += next - i
					i = next
				}
				pos += n
			}
		} else {
			pos += run
		}
		if pos >= total {
			break
		}
	}
	img := image.NewGray(image.Rect(0, 0, gw, gh))
	full := scale * scale
	for i, v := range lit {
		if v > 0 {
			img.Pix[i] = uint8(min(255, 85+v*170/full))
		}
	}
	return img
}

// LayerPlan returns the seconds of light of each layer of the file r with the header h (its
// exposure and its time with no light, from the table of the layers), and the seconds of the moves
// of the plate for each layer: the rest of the print time of the header, the same for each layer.
func LayerPlan(r io.ReaderAt, h *Sliced) (light []float64, moves float64, err error) {
	n := int(h.Layers)
	if n <= 0 || n > 100000 {
		return nil, 0, errors.New("chitu: no layers")
	}
	table := make([]byte, n*layerDefSize)
	if _, err := r.ReadAt(table, int64(h.layerTable)); err != nil {
		return nil, 0, err
	}
	f := func(off int) float64 { return float64(math.Float32frombits(binary.LittleEndian.Uint32(table[off:]))) }
	light = make([]float64, n)
	sum := 0.0
	for i := range light {
		exp, off := f(i*layerDefSize+4), f(i*layerDefSize+8)
		if exp < 0 || exp > 600 || off < 0 || off > 600 {
			return nil, 0, fmt.Errorf("chitu: a bad time in layer %d", i)
		}
		light[i] = exp + off
		sum += light[i]
	}
	return light, max(0, float64(h.PrintSeconds)-sum) / float64(n), nil
}
