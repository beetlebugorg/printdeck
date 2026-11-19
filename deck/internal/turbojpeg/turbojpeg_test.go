//go:build turbojpeg

package turbojpeg

import (
	"bytes"
	"image"
	"image/jpeg"
	"math/rand"
	"testing"

	"printdeck/internal/jpegdec"
)

func picture(t testing.TB) []byte {
	r := rand.New(rand.NewSource(1))
	p := image.NewYCbCr(image.Rect(0, 0, 1283, 721), image.YCbCrSubsampleRatio422)
	for i := range p.Y {
		p.Y[i] = uint8(i%1283/5 + r.Intn(30))
	}
	for i := range p.Cb {
		p.Cb[i], p.Cr[i] = uint8(100+r.Intn(20)), uint8(150+r.Intn(20))
	}
	var b bytes.Buffer
	jpeg.Encode(&b, p, &jpeg.Options{Quality: 90})
	return b.Bytes()
}

// The pictures of libjpeg-turbo and jpegdec have the same size, and about the same pixels: the
// reduced inverse DCTs of the two are not the same, so a pixel of a noisy picture can differ.
func TestDecode(t *testing.T) {
	b := picture(t)
	for shift := range 4 {
		got, err := Decode(b, shift)
		if err != nil {
			t.Fatal(err)
		}
		img, _ := jpegdec.DecodeScaled(bytes.NewReader(b), shift)
		want := img.(*image.YCbCr)
		if got.Rect != want.Rect {
			t.Fatalf("shift %d: %v, want %v", shift, got.Rect, want.Rect)
		}
		var sum, n int
		for y := 0; y < want.Rect.Dy(); y++ {
			for x := 0; x < want.Rect.Dx(); x++ {
				d := int(got.Y[got.YOffset(x, y)]) - int(want.Y[want.YOffset(x, y)])
				sum += max(d, -d)
				n++
			}
		}
		t.Logf("shift %d: mean difference %.2f", shift, float64(sum)/float64(n))
		if float64(sum)/float64(n) > 6 {
			t.Errorf("shift %d: the mean difference is %.2f", shift, float64(sum)/float64(n))
		}
	}
	p, _ := Decode(b, 1)
	out, err := Encode(p, 90)
	if err != nil || len(out) < 1000 {
		t.Fatalf("encode: %d bytes, %v", len(out), err)
	}
}
