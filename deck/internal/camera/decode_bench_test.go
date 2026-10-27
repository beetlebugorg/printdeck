package camera

import (
	"bytes"
	"image"
	"image/jpeg"
	"math"
	"math/rand"
	"testing"
)

// photoJPEG is a JPEG of w x h with the detail of a camera picture: smooth light, edges, a grid
// and noise. At 1280 x 720 it is about as large as a frame of a USB camera in MJPEG.
func photoJPEG(tb testing.TB, w, h int) []byte {
	tb.Helper()
	r := rand.New(rand.NewSource(7))
	p := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
	for y := range h {
		for x := range w {
			v := 90 + 60*math.Sin(float64(x)/90)*math.Cos(float64(y)/70)
			if (x/40+y/40)%2 == 0 {
				v += 25
			}
			if x%80 < 2 || y%80 < 2 {
				v = 220
			}
			v += float64(r.Intn(10)) - 5
			p.Y[y*p.YStride+x] = uint8(max(0, min(255, v)))
		}
	}
	for y := range h / 2 {
		for x := range w / 2 {
			p.Cb[y*p.CStride+x] = uint8(110 + x%40 + r.Intn(6))
			p.Cr[y*p.CStride+x] = uint8(140 - y%30 + r.Intn(6))
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, p, &jpeg.Options{Quality: 85}); err != nil {
		tb.Fatal(err)
	}
	return b.Bytes()
}

// benchSmall makes the picture of one viewer of width w from a new frame of 1280 x 720 each
// time, as a stream does (Frame.ForWidth): the decode, the scale and the JPEG, or the frame of
// the camera.
func benchSmall(b *testing.B, w int) {
	jb := photoJPEG(b, 1280, 720)
	b.SetBytes(int64(len(jb)))
	b.ResetTimer()
	for range b.N {
		f, err := FrameOf(jb)
		if err != nil {
			b.Fatal(err)
		}
		f.ForWidth(w).JPEG()
	}
}

// The widths of the viewers: the iPhone (402 points at 3x), the Camera page of the deck, the
// watch, and a small picture.
func BenchmarkViewer1206(b *testing.B) { benchSmall(b, 1206) }
func BenchmarkViewer640(b *testing.B)  { benchSmall(b, 640) }
func BenchmarkViewer400(b *testing.B)  { benchSmall(b, 400) }
func BenchmarkViewer320(b *testing.B)  { benchSmall(b, 320) }

// BenchmarkDecode720 is the decode of a whole frame of 1280 x 720.
func BenchmarkDecode720(b *testing.B) {
	jb := photoJPEG(b, 1280, 720)
	b.SetBytes(int64(len(jb)))
	b.ResetTimer()
	for range b.N {
		f, _ := FrameOf(jb)
		f.Picture()
	}
}

// BenchmarkDeckPage is the picture of the Camera page of the deck: the decode and the scale of a
// frame to 640, drawn with no JPEG.
func BenchmarkDeckPage(b *testing.B) {
	jb := photoJPEG(b, 1280, 720)
	b.ResetTimer()
	for range b.N {
		f, _ := FrameOf(jb)
		f.Small(640).Picture()
	}
}
