package chitu

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
)

// The sliced files of ChiTuBox for ChiTu boards (.cbddlp and .ctb): a header, two preview
// pictures, and the layers. The layout comes from other programs that read these files (UVtools
// and others); no file of the Phenom checked it yet.

// The magic numbers at the start of a file.
const (
	MagicCBDDLP = 0x12FD0019
	MagicCTB    = 0x12FD0086
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
}

// ReadSliced reads the header of the sliced file data.
func ReadSliced(data []byte) (*Sliced, error) {
	if len(data) < 112 {
		return nil, errors.New("chitu: the file is too short for a sliced file")
	}
	u := func(off int) uint32 { return binary.LittleEndian.Uint32(data[off:]) }
	f := func(off int) float32 { return math.Float32frombits(u(off)) }
	switch u(0) {
	case MagicCBDDLP, MagicCTB:
	default:
		return nil, fmt.Errorf("chitu: no sliced file (magic %#x)", u(0))
	}
	return &Sliced{Version: u(4), BedX: f(8), BedY: f(12), BedZ: f(16), Height: f(28), LayerHeight: f(32),
		Exposure: f(36), BottomExposure: f(40), BottomLayers: u(48), ResX: u(52), ResY: u(56),
		previewLarge: u(60), Layers: u(68), previewSmall: u(72), PrintSeconds: u(76)}, nil
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
