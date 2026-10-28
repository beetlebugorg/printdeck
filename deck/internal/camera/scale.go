package camera

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"

	"printdeck/internal/jpegdec"
)

// The conversion of the pictures: a picture of the camera (YUYV, NV12, YU12, or a decoded JPEG)
// goes to a YCbCr 4:2:0 picture at a smaller size, and then to JPEG. Each plane (Y, Cb, Cr) is
// scaled on its own, with tables of the source positions: the nearest sample, or the mean of 2 x 2
// samples when the picture becomes half as large or smaller. This is fast, and the mean keeps
// the thin lines of a half size.

// quality is the JPEG quality of the frames that the deck makes.
const quality = 70

// plane is one plane of a picture in memory: the sample (x, y) is at pix[off + y*stride + x*step].
type plane struct {
	pix         []byte
	off, stride int
	step        int
	w, h        int
}

// valid reports that each sample of the plane is in pix.
func (p plane) valid() bool {
	if p.w <= 0 || p.h <= 0 || p.step <= 0 || p.off < 0 {
		return false
	}
	return p.off+(p.h-1)*p.stride+(p.w-1)*p.step < len(p.pix)
}

// positions are the source samples of each place of the scaled plane: a and b are the two samples
// of the mean (the same sample for the nearest).
func positions(n, src int, flip bool) (a, b []int) {
	a, b = make([]int, n), make([]int, n)
	box := src >= 2*n
	for i := range n {
		j := i
		if flip {
			j = n - 1 - i
		}
		c := ((2*j + 1) * src) / (2 * n) // the middle of the place in the source
		c = min(c, src-1)
		if box {
			a[i], b[i] = max(0, c-1), c
		} else {
			a[i], b[i] = c, c
		}
	}
	return a, b
}

// scalePlane scales src into dst, a plane of dw x dh bytes with dstStride bytes a row. flip turns
// the picture 180°.
func scalePlane(dst []byte, dstStride, dw, dh int, src plane, flip bool) {
	xa, xb := positions(dw, src.w, flip)
	ya, yb := positions(dh, src.h, flip)
	for i := range xa {
		xa[i] *= src.step
		xb[i] *= src.step
	}
	p := src.pix
	box := src.w >= 2*dw || src.h >= 2*dh
	for y := range dh {
		row := dst[y*dstStride : y*dstStride+dw]
		r0 := src.off + ya[y]*src.stride
		if !box {
			for x := range row {
				row[x] = p[r0+xa[x]]
			}
			continue
		}
		r1 := src.off + yb[y]*src.stride
		for x := range row {
			s := uint(p[r0+xa[x]]) + uint(p[r0+xb[x]]) + uint(p[r1+xa[x]]) + uint(p[r1+xb[x]])
			row[x] = byte((s + 2) >> 2)
		}
	}
}

// toYCbCr scales the planes of a picture to a YCbCr 4:2:0 picture of w x h (even numbers), and
// turns it clockwise by rotate degrees: 0, 90, 180 or 270. For 90 and 270, w x h is the size
// after the turn.
func toYCbCr(w, h int, y, cb, cr plane, rotate int) *image.YCbCr {
	out := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
	if rotate == 90 || rotate == 270 {
		turnPlane(out.Y, out.YStride, w, h, y, rotate)
		turnPlane(out.Cb, out.CStride, w/2, h/2, cb, rotate)
		turnPlane(out.Cr, out.CStride, w/2, h/2, cr, rotate)
		return out
	}
	flip := rotate == 180
	scalePlane(out.Y, out.YStride, w, h, y, flip)
	scalePlane(out.Cb, out.CStride, w/2, h/2, cb, flip)
	scalePlane(out.Cr, out.CStride, w/2, h/2, cr, flip)
	return out
}

// turnTile is the side of the square of dst samples that turnPlane writes in one pass. The
// samples of a tile come from turnTile rows of src, so that the reads stay in the cache.
const turnTile = 32

// turnPlane scales src and turns it 90° (rotate 90) or 270° clockwise into dst, a plane of dw x dh
// bytes with dstStride bytes a row. Before the turn, the scaled plane is dh x dw. A turn of 90°
// puts the sample (u, v) of the scaled plane at (dw-1-v, u), and a turn of 270° at (v, dh-1-u).
func turnPlane(dst []byte, dstStride, dw, dh int, src plane, rotate int) {
	// The source column of each dst row, and the source row of each dst column.
	ua, ub := positions(dh, src.w, rotate == 270)
	va, vb := positions(dw, src.h, rotate == 90)
	for i := range ua {
		ua[i] = src.off + ua[i]*src.step
		ub[i] = src.off + ub[i]*src.step
	}
	for i := range va {
		va[i] *= src.stride
		vb[i] *= src.stride
	}
	p := src.pix
	box := src.w >= 2*dh || src.h >= 2*dw
	for ty := 0; ty < dh; ty += turnTile {
		for tx := 0; tx < dw; tx += turnTile {
			xs, xe := tx, min(tx+turnTile, dw)
			for y := ty; y < min(ty+turnTile, dh); y++ {
				row := dst[y*dstStride+xs : y*dstStride+xe]
				c0 := ua[y]
				if !box {
					for i := range row {
						row[i] = p[c0+va[xs+i]]
					}
					continue
				}
				c1 := ub[y]
				for i := range row {
					r0, r1 := va[xs+i], vb[xs+i]
					s := uint(p[r0+c0]) + uint(p[r0+c1]) + uint(p[r1+c0]) + uint(p[r1+c1])
					row[i] = byte((s + 2) >> 2)
				}
			}
		}
	}
}

// decodeTurned decodes the JPEG b of jw x jh, and returns its picture at ow x oh turned clockwise
// by rotate degrees, or nil for a broken JPEG. ow x oh is the size after the turn. The decode is
// at 1/2, 1/4 or 1/8 of the size (jpegdec) when that size is not smaller than the output: the
// reduced decode costs less than the whole decode, and leaves less to scale and turn.
func decodeTurned(b []byte, jw, jh, ow, oh, rotate int) *image.YCbCr {
	uw, uh := TurnedSize(ow, oh, rotate) // the size before the turn
	shift := 0
	for s := 3; s >= 1; s-- {
		if (jw+1<<s-1)>>s >= uw && (jh+1<<s-1)>>s >= uh {
			shift = s
			break
		}
	}
	img, err := jpegdec.DecodeScaled(bytes.NewReader(b), shift)
	if err != nil {
		return nil
	}
	p := toYCbCrPicture(img)
	if p == nil {
		return nil
	}
	return scaleYCbCr(p, ow, oh, rotate)
}

// TurnedSize returns the size w x h after a turn of rotate degrees: the sides change places for
// 90 and 270.
func TurnedSize(w, h, rotate int) (int, int) {
	if rotate == 90 || rotate == 270 {
		return h, w
	}
	return w, h
}

var errShort = errors.New("camera: the frame is too short")

// rawPlanes are the planes of a raw picture of the camera: a frame of w x h pixels with bpl bytes
// a row (of the Y plane).
func rawPlanes(format string, b []byte, w, h, bpl int) (y, cb, cr plane, err error) {
	switch format {
	case "YUYV": // Y0 U Y1 V: U and V for each two pixels of a row
		y = plane{b, 0, bpl, 2, w, h}
		cb = plane{b, 1, bpl, 4, w / 2, h}
		cr = plane{b, 3, bpl, 4, w / 2, h}
	case "NV12": // the Y plane, then a plane of U V pairs at half the size
		y = plane{b, 0, bpl, 1, w, h}
		cb = plane{b, bpl * h, bpl, 2, w / 2, h / 2}
		cr = plane{b, bpl*h + 1, bpl, 2, w / 2, h / 2}
	case "YU12": // the Y plane, the U plane and the V plane at half the size
		y = plane{b, 0, bpl, 1, w, h}
		cb = plane{b, bpl * h, bpl / 2, 1, w / 2, h / 2}
		cr = plane{b, bpl*h + (bpl/2)*(h/2), bpl / 2, 1, w / 2, h / 2}
	default:
		return y, cb, cr, errors.New("camera: no conversion for " + format)
	}
	if !y.valid() || !cb.valid() || !cr.valid() {
		return y, cb, cr, errShort
	}
	return y, cb, cr, nil
}

// ycbcrPlanes are the planes of a YCbCr picture.
func ycbcrPlanes(p *image.YCbCr) (y, cb, cr plane) {
	w, h := p.Rect.Dx(), p.Rect.Dy()
	cw, ch := w, h
	switch p.SubsampleRatio {
	case image.YCbCrSubsampleRatio422:
		cw = (w + 1) / 2
	case image.YCbCrSubsampleRatio420:
		cw, ch = (w+1)/2, (h+1)/2
	case image.YCbCrSubsampleRatio440:
		ch = (h + 1) / 2
	case image.YCbCrSubsampleRatio411:
		cw = (w + 3) / 4
	case image.YCbCrSubsampleRatio410:
		cw, ch = (w+3)/4, (h+1)/2
	}
	y = plane{p.Y, p.YOffset(p.Rect.Min.X, p.Rect.Min.Y), p.YStride, 1, w, h}
	co := p.COffset(p.Rect.Min.X, p.Rect.Min.Y)
	cb = plane{p.Cb, co, p.CStride, 1, cw, ch}
	cr = plane{p.Cr, co, p.CStride, 1, cw, ch}
	return y, cb, cr
}

// scaleYCbCr scales a YCbCr picture to w x h, and turns it clockwise by rotate degrees (see
// toYCbCr).
func scaleYCbCr(p *image.YCbCr, w, h, rotate int) *image.YCbCr {
	y, cb, cr := ycbcrPlanes(p)
	return toYCbCr(w, h, y, cb, cr, rotate)
}

// encodeJPEG encodes a picture as a JPEG.
func encodeJPEG(p *image.YCbCr) ([]byte, error) {
	var b bytes.Buffer
	b.Grow(p.Rect.Dx() * p.Rect.Dy() / 8)
	err := jpeg.Encode(&b, p, &jpeg.Options{Quality: quality})
	return b.Bytes(), err
}

// outSize is the size of the frames for a picture of w x h: the size that fits in maxW x maxH
// with the same shape, in even numbers.
func outSize(w, h, maxW, maxH int) (int, int) {
	if w <= maxW && h <= maxH {
		return w &^ 1, h &^ 1
	}
	ow, oh := maxW, h*maxW/w
	if oh > maxH {
		ow, oh = w*maxH/h, maxH
	}
	return max(2, ow&^1), max(2, oh&^1)
}

// blank reports that a plane has one value, give or take 3: the picture of an HDMI capture with
// no signal. It looks at a grid of 16 x 9 samples.
func blank(p plane) bool {
	lo, hi := 255, 0
	for i := range 9 {
		row := p.off + (p.h*(2*i+1)/18)*p.stride
		for j := range 16 {
			v := int(p.pix[row+(p.w*(2*j+1)/32)*p.step])
			lo, hi = min(lo, v), max(hi, v)
		}
	}
	return hi-lo <= 3
}
