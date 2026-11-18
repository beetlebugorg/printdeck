package jpegdec

import (
	"bytes"
	"image"
	"image/jpeg"
	"math"
	"runtime"
	"testing"
)

// psnr returns the peak signal to noise ratio of the Y planes of a and b, in dB.
func psnr(a, b *image.YCbCr) float64 {
	var se float64
	w, h := a.Rect.Dx(), a.Rect.Dy()
	for y := range h {
		for x := range w {
			d := float64(a.Y[a.YOffset(a.Rect.Min.X+x, a.Rect.Min.Y+y)]) - float64(b.Y[b.YOffset(b.Rect.Min.X+x, b.Rect.Min.Y+y)])
			se += d * d
		}
	}
	return 10 * math.Log10(255*255/(se/float64(w*h)))
}

// Encode gives a JPEG that image/jpeg reads, as good as the JPEG of image/jpeg at the same
// quality and about as large, with one strip and with many. The decoder of the deck reads it to
// the same pixels as image/jpeg.
func TestEncode(t *testing.T) {
	procs := runtime.GOMAXPROCS(0)
	defer runtime.GOMAXPROCS(procs)
	for _, ratio := range []image.YCbCrSubsampleRatio{image.YCbCrSubsampleRatio420, image.YCbCrSubsampleRatio422, image.YCbCrSubsampleRatio444} {
		src, err := jpeg.Decode(bytes.NewReader(noisyPicture(t, 1283, 721, ratio, 6)))
		if err != nil {
			t.Fatal(err)
		}
		p := src.(*image.YCbCr)
		for _, sub := range []image.Rectangle{p.Rect, image.Rect(101, 37, 900, 600)} {
			pic := p.SubImage(sub).(*image.YCbCr)
			var std bytes.Buffer
			jpeg.Encode(&std, pic, &jpeg.Options{Quality: 90})
			stdLen := std.Len()
			// image/jpeg always writes 4:2:0 and makes its chroma with the mean of 2 x 2
			// samples: at an odd origin, that blurs the chroma, and the JPEG is smaller.
			slack := 1.05
			if sub != p.Rect || ratio != image.YCbCrSubsampleRatio420 {
				slack = 1.2
			}
			stdPic, _ := jpeg.Decode(&std)
			for _, procs := range []int{1, 4} {
				runtime.GOMAXPROCS(procs)
				b, err := Encode(pic, 90)
				if err != nil {
					t.Fatal(err)
				}
				got, err := jpeg.Decode(bytes.NewReader(b))
				if err != nil {
					t.Fatalf("%v %v procs %d: %v", ratio, sub, procs, err)
				}
				g := got.(*image.YCbCr)
				if g.Rect.Dx() != sub.Dx() || g.Rect.Dy() != sub.Dy() {
					t.Fatalf("size %v, want %v", g.Rect, sub)
				}
				q, qs := psnr(g, pic), psnr(stdPic.(*image.YCbCr), pic)
				if q < qs-0.2 || float64(len(b)) > slack*float64(stdLen)+2048 {
					t.Errorf("%v %v procs %d: %.2f dB %d bytes, image/jpeg %.2f dB %d bytes", ratio, sub, procs, q, len(b), qs, stdLen)
				}
				mine, err := DecodeScaled(bytes.NewReader(b), 0)
				if err != nil {
					t.Fatal(err)
				}
				samePlanes(t, "decode of Encode", mine, got)
			}
		}
	}
}

// The quantizer by multiplication gives the quotients of the division, for each table value and
// each output of fdct.
func TestEncQuant(t *testing.T) {
	var q [blockSize]uint8
	for v := 1; v <= 255; v++ {
		for i := range q {
			q[i] = uint8(v)
		}
		e := makeEncQ(&q)
		for a := int32(-20000); a <= 20000; a++ {
			if got, want := e.quant(a, 0), encDiv(a, 8*int32(v)); got != want {
				t.Fatalf("q %d a %d: %d, want %d", v, a, got, want)
			}
		}
	}
}
