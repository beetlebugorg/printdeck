package camera

import (
	"image"
	"math/rand"
	"testing"
)

// randomYCbCr is a picture of w x h with the subsample ratio sr and random samples.
func randomYCbCr(w, h int, sr image.YCbCrSubsampleRatio, r *rand.Rand) *image.YCbCr {
	p := image.NewYCbCr(image.Rect(0, 0, w, h), sr)
	for _, s := range [][]byte{p.Y, p.Cb, p.Cr} {
		for i := range s {
			s[i] = byte(r.Intn(256))
		}
	}
	return p
}

// turnedRef turns the samples of a plane of w x h (stride w) clockwise by rot degrees, one sample
// at a time.
func turnedRef(pix []byte, stride, w, h, rot int) []byte {
	ow, oh := TurnedSize(w, h, rot)
	out := make([]byte, ow*oh)
	for y := range h {
		for x := range w {
			ox, oy := x, y
			switch rot {
			case 90:
				ox, oy = h-1-y, x
			case 180:
				ox, oy = w-1-x, h-1-y
			case 270:
				ox, oy = y, w-1-x
			}
			out[oy*ow+ox] = pix[y*stride+x]
		}
	}
	return out
}

// samePlane reports that the plane of w x h with stride in pix has the samples of ref.
func samePlane(pix []byte, stride, w, h int, ref []byte) bool {
	for y := range h {
		for x := range w {
			if pix[y*stride+x] != ref[y*w+x] {
				return false
			}
		}
	}
	return true
}

// A turn of a picture with each subsample ratio is the same as the scale with no turn and then a
// turn of each sample: at the same size, at half the size (the mean of 2 x 2 samples), and at a
// size between.
func TestTurnYCbCr(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	ratios := []image.YCbCrSubsampleRatio{image.YCbCrSubsampleRatio444, image.YCbCrSubsampleRatio422,
		image.YCbCrSubsampleRatio420, image.YCbCrSubsampleRatio440, image.YCbCrSubsampleRatio411,
		image.YCbCrSubsampleRatio410}
	const w, h = 64, 48
	for _, sr := range ratios {
		p := randomYCbCr(w, h, sr, r)
		for _, size := range []image.Point{{64, 48}, {32, 24}, {48, 36}} {
			flat := scaleYCbCr(p, size.X, size.Y, 0)
			for _, rot := range []int{0, 90, 180, 270} {
				ow, oh := TurnedSize(size.X, size.Y, rot)
				out := scaleYCbCr(p, ow, oh, rot)
				if out.Rect != image.Rect(0, 0, ow, oh) || out.SubsampleRatio != image.YCbCrSubsampleRatio420 {
					t.Fatalf("%v %v turn %d: %v %v", sr, size, rot, out.Rect, out.SubsampleRatio)
				}
				if !samePlane(out.Y, out.YStride, ow, oh, turnedRef(flat.Y, flat.YStride, size.X, size.Y, rot)) {
					t.Errorf("%v %v turn %d: the Y plane is not turned", sr, size, rot)
				}
				if !samePlane(out.Cb, out.CStride, ow/2, oh/2, turnedRef(flat.Cb, flat.CStride, size.X/2, size.Y/2, rot)) ||
					!samePlane(out.Cr, out.CStride, ow/2, oh/2, turnedRef(flat.Cr, flat.CStride, size.X/2, size.Y/2, rot)) {
					t.Errorf("%v %v turn %d: the color planes are not turned", sr, size, rot)
				}
			}
		}
	}
}

// At the same size, a turn of 90° moves each Y sample with no change, and a turn of 90° and then
// 270° gives the picture back.
func TestTurnExact(t *testing.T) {
	r := rand.New(rand.NewSource(4))
	// A size that is larger than turnTile, and not a multiple of it.
	const w, h = 100, 70
	p := randomYCbCr(w, h, image.YCbCrSubsampleRatio420, r)
	a := scaleYCbCr(p, h, w, 90)
	for y := range h {
		for x := range w {
			if a.Y[x*a.YStride+h-1-y] != p.Y[y*p.YStride+x] {
				t.Fatalf("turn 90: the sample %d,%d moved to the wrong place", x, y)
			}
		}
	}
	b := scaleYCbCr(a, w, h, 270)
	if !samePlane(b.Y, b.YStride, w, h, p.Y) || !samePlane(b.Cb, b.CStride, w/2, h/2, p.Cb) {
		t.Error("a turn of 90° and then 270° does not give the picture back")
	}
}

// decodeTurned makes a tall frame from a wide JPEG, with the reduced decode for a large JPEG.
func TestDecodeTurned(t *testing.T) {
	for _, c := range []struct{ w, h int }{{1280, 720}, {1920, 1080}, {640, 480}} {
		jb := photoJPEG(t, c.w, c.h)
		for _, rot := range []int{90, 180, 270} {
			ow, oh := outSize(c.w, c.h, 960, 540)
			ow, oh = TurnedSize(ow, oh, rot)
			p := decodeTurned(jb, c.w, c.h, ow, oh, rot)
			if p == nil || p.Rect.Dx() != ow || p.Rect.Dy() != oh {
				t.Fatalf("%dx%d turn %d: %v, want %dx%d", c.w, c.h, rot, p, ow, oh)
			}
			if rot != 180 && oh <= ow {
				t.Errorf("%dx%d turn %d: the frame of %dx%d is not tall", c.w, c.h, rot, ow, oh)
			}
		}
	}
	if decodeTurned([]byte{0xFF, 0xD8, 0, 0}, 8, 8, 8, 8, 90) != nil {
		t.Error("a broken JPEG gives a picture")
	}
}
