package jpegdec

import (
	"bytes"
	"image"
	"image/jpeg"
	"testing"
)

// The benchmarks decode and encode a 4K picture with detail everywhere, as a camera sends
// (4:2:2): run them with -cpu 1,4 for the scan on one core and the parallel scan.

func bench4K(b *testing.B, shift int, region image.Rectangle) {
	j := noisyPicture(b, 3840, 2160, image.YCbCrSubsampleRatio422, 7)
	b.SetBytes(int64(len(j)))
	for b.Loop() {
		var err error
		if region.Empty() {
			_, err = DecodeScaled(bytes.NewReader(j), shift)
		} else {
			_, err = DecodeRegion(bytes.NewReader(j), shift, region)
		}
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecode4K(b *testing.B)       { bench4K(b, 0, image.Rectangle{}) }
func BenchmarkDecode4KHalf(b *testing.B)   { bench4K(b, 1, image.Rectangle{}) }
func BenchmarkDecode4KEighth(b *testing.B) { bench4K(b, 3, image.Rectangle{}) }
func BenchmarkDecode4KCrop(b *testing.B)   { bench4K(b, 0, image.Rect(1400, 700, 2600, 1500)) }

func BenchmarkDecode4KImageJPEG(b *testing.B) {
	j := noisyPicture(b, 3840, 2160, image.YCbCrSubsampleRatio422, 7)
	b.SetBytes(int64(len(j)))
	for b.Loop() {
		if _, err := jpeg.Decode(bytes.NewReader(j)); err != nil {
			b.Fatal(err)
		}
	}
}

func benchPicture(b *testing.B) *image.YCbCr {
	src, err := jpeg.Decode(bytes.NewReader(noisyPicture(b, 1920, 1080, image.YCbCrSubsampleRatio420, 8)))
	if err != nil {
		b.Fatal(err)
	}
	return src.(*image.YCbCr)
}

func BenchmarkEncode1080(b *testing.B) {
	p := benchPicture(b)
	for b.Loop() {
		if _, err := Encode(p, 90); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncode1080ImageJPEG(b *testing.B) {
	p := benchPicture(b)
	for b.Loop() {
		var w bytes.Buffer
		if err := jpeg.Encode(&w, p, &jpeg.Options{Quality: 90}); err != nil {
			b.Fatal(err)
		}
	}
}
