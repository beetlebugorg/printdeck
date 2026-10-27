package jpegdec

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"math/rand"
	"testing"
)

// testPicture is a JPEG of w x h with smooth light, edges and noise, in color or in gray.
func testPicture(t testing.TB, w, h int, gray bool) []byte {
	r := rand.New(rand.NewSource(3))
	var img image.Image
	if gray {
		g := image.NewGray(image.Rect(0, 0, w, h))
		for y := range h {
			for x := range w {
				g.SetGray(x, y, color.Gray{uint8(100 + 80*math.Sin(float64(x)/30) + float64(r.Intn(10)))})
			}
		}
		img = g
	} else {
		p := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
		for y := range h {
			for x := range w {
				v := 100 + 70*math.Sin(float64(x)/40)*math.Cos(float64(y)/25) + float64(r.Intn(10))
				if x%50 < 3 {
					v = 230
				}
				p.Y[y*p.YStride+x] = uint8(max(0, min(255, v)))
			}
		}
		for y := range (h + 1) / 2 {
			for x := range (w + 1) / 2 {
				p.Cb[y*p.CStride+x], p.Cr[y*p.CStride+x] = uint8(80+x%90), uint8(170-y%60)
			}
		}
		img = p
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// planes returns the planes of a picture: Y, or Y, Cb and Cr, with their sizes.
type plane struct {
	pix          []byte
	stride, w, h int
}

func planesOf(img image.Image) []plane {
	switch p := img.(type) {
	case *image.Gray:
		return []plane{{p.Pix, p.Stride, p.Rect.Dx(), p.Rect.Dy()}}
	case *image.YCbCr:
		cw, ch := (p.Rect.Dx()+1)/2, (p.Rect.Dy()+1)/2
		return []plane{{p.Y, p.YStride, p.Rect.Dx(), p.Rect.Dy()}, {p.Cb, p.CStride, cw, ch}, {p.Cr, p.CStride, cw, ch}}
	}
	return nil
}

// With shift 0, the picture is the picture of image/jpeg.
func TestWholeSize(t *testing.T) {
	for _, gray := range []bool{false, true} {
		b := testPicture(t, 333, 201, gray)
		want, err := jpeg.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		got, err := DecodeScaled(bytes.NewReader(b), 0)
		if err != nil {
			t.Fatal(err)
		}
		gp, wp := planesOf(got), planesOf(want)
		for i := range wp {
			for y := range wp[i].h {
				if !bytes.Equal(gp[i].pix[y*gp[i].stride:y*gp[i].stride+wp[i].w], wp[i].pix[y*wp[i].stride:y*wp[i].stride+wp[i].w]) {
					t.Fatalf("gray %v: plane %d row %d differs", gray, i, y)
				}
			}
		}
	}
}

// At 1/2, 1/4 and 1/8, each pixel is near the mean of its pixels in the whole picture: the mean
// error is below 2.5 levels with no bias, and the largest error below 40 levels (at the sharp
// lines). The reduced DCT is a low pass filter, a little sharper than the mean.
func TestScaled(t *testing.T) {
	for _, gray := range []bool{false, true} {
		for _, size := range []image.Point{{1280, 720}, {333, 201}} {
			b := testPicture(t, size.X, size.Y, gray)
			whole, err := jpeg.Decode(bytes.NewReader(b))
			if err != nil {
				t.Fatal(err)
			}
			for shift := 1; shift <= 3; shift++ {
				got, err := DecodeScaled(bytes.NewReader(b), shift)
				if err != nil {
					t.Fatal(err)
				}
				k := 1 << shift
				if want := image.Rect(0, 0, (size.X+k-1)/k, (size.Y+k-1)/k); got.Bounds() != want {
					t.Fatalf("shift %d: bounds %v, want %v", shift, got.Bounds(), want)
				}
				gp, wp := planesOf(got), planesOf(whole)
				for i := range wp {
					var sum, signed, worst float64
					n := 0
					// The whole blocks: the last block of an odd size has padding.
					for y := 0; y < wp[i].h/k; y++ {
						for x := 0; x < wp[i].w/k; x++ {
							var m float64
							for dy := range k {
								for dx := range k {
									m += float64(wp[i].pix[(y*k+dy)*wp[i].stride+x*k+dx])
								}
							}
							m /= float64(k * k)
							e := float64(gp[i].pix[y*gp[i].stride+x]) - m
							d := math.Abs(e)
							sum += d
							signed += e
							worst = max(worst, d)
							n++
						}
					}
					mean, bias := sum/float64(n), signed/float64(n)
					if mean > 2.5 || math.Abs(bias) > 0.5 || worst > 40 {
						t.Errorf("gray %v %v shift %d plane %d: mean error %.2f, bias %.2f, largest %.0f", gray, size, shift, i, mean, bias, worst)
					}
				}
			}
		}
	}
}

func benchDecode(b *testing.B, shift int) {
	jb := testPicture(b, 1280, 720, false)
	b.SetBytes(int64(len(jb)))
	b.ResetTimer()
	for range b.N {
		if _, err := DecodeScaled(bytes.NewReader(jb), shift); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeWhole(b *testing.B)   { benchDecode(b, 0) }
func BenchmarkDecodeHalf(b *testing.B)    { benchDecode(b, 1) }
func BenchmarkDecodeQuarter(b *testing.B) { benchDecode(b, 2) }
func BenchmarkDecodeEighth(b *testing.B)  { benchDecode(b, 3) }
