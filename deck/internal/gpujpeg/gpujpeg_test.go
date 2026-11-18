//go:build vulkan

package gpujpeg

import (
	"bytes"
	"image"
	"image/jpeg"
	"math/rand"
	"testing"
	"time"

	"printdeck/internal/jpegdec"
)

func noisy(t testing.TB, w, h int, ratio image.YCbCrSubsampleRatio) []byte {
	r := rand.New(rand.NewSource(1))
	p := image.NewYCbCr(image.Rect(0, 0, w, h), ratio)
	for i := range p.Y {
		p.Y[i] = uint8(i%w/3 + r.Intn(40))
	}
	for i := range p.Cb {
		p.Cb[i], p.Cr[i] = uint8(90+r.Intn(30)), uint8(150+r.Intn(30))
	}
	var b bytes.Buffer
	jpeg.Encode(&b, p, &jpeg.Options{Quality: 90})
	return b.Bytes()
}

// The pixels of the GPU are the pixels of jpegdec on the CPU.
func TestRender(t *testing.T) {
	g, err := Open()
	if err != nil {
		t.Skip(err)
	}
	defer g.Close()
	t.Logf("GPU: %s", g.Name)
	for _, size := range []image.Point{{1283, 721}, {3840, 2160}} {
		pic := noisy(t, size.X, size.Y, image.YCbCrSubsampleRatio422)
		s, err := jpegdec.DecodeSparse(pic)
		if err != nil {
			t.Fatal(err)
		}
		in, err := s.GPUInput()
		if err != nil {
			t.Fatal(err)
		}
		st := time.Now()
		got, err := g.Render(in)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%v: %.1f ms", size, time.Since(st).Seconds()*1000)
		want, err := jpegdec.DecodeScaled(bytes.NewReader(pic), 0)
		if err != nil {
			t.Fatal(err)
		}
		gp, wp := got.(*image.YCbCr), want.(*image.YCbCr)
		for y := 0; y < size.Y; y++ {
			if !bytes.Equal(gp.Y[gp.YOffset(0, y):gp.YOffset(0, y)+size.X], wp.Y[wp.YOffset(0, y):wp.YOffset(0, y)+size.X]) {
				t.Fatalf("%v: Y row %d differs", size, y)
			}
			cw := (size.X + 1) / 2
			if !bytes.Equal(gp.Cb[gp.COffset(0, y):gp.COffset(0, y)+cw], wp.Cb[wp.COffset(0, y):wp.COffset(0, y)+cw]) ||
				!bytes.Equal(gp.Cr[gp.COffset(0, y):gp.COffset(0, y)+cw], wp.Cr[wp.COffset(0, y):wp.COffset(0, y)+cw]) {
				t.Fatalf("%v: C row %d differs", size, y)
			}
		}
		s.Release()
	}
}
