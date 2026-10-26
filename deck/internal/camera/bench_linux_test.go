package camera

import (
	"bytes"
	"image/jpeg"
	"math/rand"
	"testing"
)

// benchFrame is a raw frame of w x h with gradients and noise, like a picture of a camera.
func benchFrame(format string, w, h int) []byte {
	r := rand.New(rand.NewSource(1))
	bpl := w
	if format == "YUYV" {
		bpl = 2 * w
	}
	b := rawFrame(format, w, h, bpl)
	if len(b) == 0 {
		b = make([]byte, bpl*h*3/2)
	}
	for i := range b {
		b[i] = byte(int(b[i])/2 + (i/7)%64 + r.Intn(6))
	}
	return b
}

// benchRaw converts a raw frame, and encodes the JPEG when encode is true.
func benchRaw(b *testing.B, format string, encode bool) {
	const w, h = 1920, 1080
	data := benchFrame(format, w, h)
	bpl := w
	if format == "YUYV" {
		bpl = 2 * w
	}
	ow, oh := outSize(w, h, rawMax.X, rawMax.Y)
	f := Format{OutW: ow, OutH: oh}
	b.ResetTimer()
	for range b.N {
		fr := makeFrame(format, data, w, h, bpl, f, false)
		if fr == nil {
			b.Fatal("no frame")
		}
		if encode {
			fr.JPEG()
		}
	}
}

// The raw pictures of an HDMI capture stick: 1920 x 1080 to a JPEG of 960 x 540. The capture
// converts each frame of the rate; the JPEG is made only when a viewer takes the frame.
func BenchmarkYUYV(b *testing.B)        { benchRaw(b, "YUYV", true) }
func BenchmarkNV12(b *testing.B)        { benchRaw(b, "NV12", true) }
func BenchmarkYU12(b *testing.B)        { benchRaw(b, "YU12", true) }
func BenchmarkYUYVConvert(b *testing.B) { benchRaw(b, "YUYV", false) }
func BenchmarkNV12Convert(b *testing.B) { benchRaw(b, "NV12", false) }
func BenchmarkYU12Convert(b *testing.B) { benchRaw(b, "YU12", false) }

// benchJPEG is a JPEG of 1280 x 720, as a webcam sends.
func benchJPEG(b *testing.B) []byte {
	y, cb, cr, _ := rawPlanes("YU12", benchFrame("YU12", 1280, 720), 1280, 720, 1280)
	var buf bytes.Buffer
	jpeg.Encode(&buf, toYCbCr(1280, 720, y, cb, cr, false), &jpeg.Options{Quality: 80})
	return stripDHT(buf.Bytes())
}

// An MJPEG webcam: the frame goes through with the Huffman tables.
func BenchmarkMJPEG(b *testing.B) {
	data := benchJPEG(b)
	b.ResetTimer()
	for range b.N {
		if makeFrame("MJPG", data, 1280, 720, 0, Format{OutW: 1280, OutH: 720}, false) == nil {
			b.Fatal("no frame")
		}
	}
}

// An MJPEG webcam turned 180°: decode, turn, and encode at 960 x 540.
func BenchmarkMJPEGTurned(b *testing.B) {
	data := benchJPEG(b)
	b.ResetTimer()
	for range b.N {
		fr := makeFrame("MJPG", data, 1280, 720, 0, Format{OutW: 960, OutH: 540}, true)
		if fr == nil {
			b.Fatal("no frame")
		}
		fr.JPEG()
	}
}

// A small frame for a watch, from a frame of an MJPEG webcam: decode, scale and encode.
func BenchmarkSmall320(b *testing.B) {
	data, w, h, _ := fixJPEG(benchJPEG(b))
	b.ResetTimer()
	for range b.N {
		newFrame(data, w, h, nil).Small(320).JPEG()
	}
}

// The picture of the camera page of the deck UI, from a frame of 960 x 540.
func BenchmarkSmall448FromRaw(b *testing.B) {
	data := benchFrame("YU12", 1920, 1080)
	y, cb, cr, _ := rawPlanes("YU12", data, 1920, 1080, 1920)
	pic := toYCbCr(960, 540, y, cb, cr, false)
	b.ResetTimer()
	for range b.N {
		picFrame(pic).Small(448).JPEG()
	}

}
