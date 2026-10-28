package api

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/draw"
)

// The photos of the owner. A photo from a phone has EXIF data, often with the GPS position of the
// home. CleanPhoto decodes the photo and writes new JPEG files with no EXIF data, upright from the
// EXIF orientation.

// MaxPhotoPixels is the largest photo, from its header, before the decode.
const MaxPhotoPixels = 50_000_000

// The long sides of the photo and of its small copy.
const photoSide, smallSide = 2048, 480

// ErrNotPhoto is a file that is not a JPEG or PNG photo, or a photo that is too large.
var ErrNotPhoto = errors.New("the file is not a JPEG or PNG photo, or it is too large")

// CleanPhoto returns the photo and its small copy as new JPEG files, and the size of the photo.
func CleanPhoto(data []byte) (full, small []byte, w, h int, err error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "jpeg" && format != "png" || cfg.Width*cfg.Height > MaxPhotoPixels || cfg.Width < 1 || cfg.Height < 1 {
		return nil, nil, 0, 0, ErrNotPhoto
	}
	var img image.Image
	if format == "jpeg" {
		img, err = jpeg.Decode(bytes.NewReader(data))
	} else {
		img, err = png.Decode(bytes.NewReader(data))
	}
	if err != nil {
		return nil, nil, 0, 0, ErrNotPhoto
	}
	if format == "jpeg" {
		img = orient(img, exifOrientation(data))
	}
	big := fit(img, photoSide)
	if full, err = encode(big, 85); err != nil {
		return nil, nil, 0, 0, err
	}
	if small, err = encode(fit(big, smallSide), 80); err != nil {
		return nil, nil, 0, 0, err
	}
	b := big.Bounds()
	return full, small, b.Dx(), b.Dy(), nil
}

func encode(img image.Image, q int) ([]byte, error) {
	var buf bytes.Buffer
	err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q})
	return buf.Bytes(), err
}

// fit returns img scaled down so that its long side is at most side.
func fit(img image.Image, side int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if max(w, h) <= side {
		return img
	}
	if w >= h {
		w, h = side, max(1, h*side/w)
	} else {
		w, h = max(1, w*side/h), side
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst
}

// exifOrientation returns the EXIF orientation of a JPEG (1 to 8), or 1.
func exifOrientation(b []byte) int {
	if len(b) < 4 || b[0] != 0xff || b[1] != 0xd8 {
		return 1
	}
	for i := 2; i+4 <= len(b); {
		if b[i] != 0xff {
			return 1
		}
		marker := b[i+1]
		n := int(binary.BigEndian.Uint16(b[i+2:]))
		if marker == 0xda || n < 2 || i+2+n > len(b) {
			return 1
		}
		seg := b[i+4 : i+2+n]
		if marker == 0xe1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			return tiffOrientation(seg[6:])
		}
		i += 2 + n
	}
	return 1
}

// tiffOrientation reads the orientation tag (0x0112) of IFD0 of a TIFF block.
func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	off := int(bo.Uint32(t[4:]))
	if off+2 > len(t) {
		return 1
	}
	count := int(bo.Uint16(t[off:]))
	for i := 0; i < count; i++ {
		e := off + 2 + i*12
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:]) == 0x0112 {
			if v := int(bo.Uint16(t[e+8:])); v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}

// orient turns and flips img from the EXIF orientation o, so that the picture is upright.
func orient(img image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	ow, oh := w, h
	if o >= 5 {
		ow, oh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, ow, oh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			dst.Set(dx, dy, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}
