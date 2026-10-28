package camera

import (
	"bytes"
	"image"
	"image/jpeg"
	"testing"
)

// The crop of a time lapse: the projected box of the part, a margin of 15 % at each side, edges
// on the grid of 16 pixels, inside the picture.
func TestPartCrop(t *testing.T) {
	// u = x/200 and v = y/100: a box of x 50..100 and y 20..40 is x 320..640 and y 144..288 in a
	// picture of 1280 x 720.
	c := &Calibration{P: [12]float64{1.0 / 200, 0, 0, 0, 0, 1.0 / 100, 0, 0, 0, 0, 0, 1}}
	sz := image.Pt(1280, 720)
	r, ok := c.PartCrop(Box{Min: [3]float64{50, 20, 0}, Max: [3]float64{100, 40, 10}}, 0, sz, 0)
	if want := image.Rect(272, 112, 688, 320); !ok || r != want {
		t.Errorf("crop %v, want %v", r, want)
	}
	// A box at the edge: the crop stays in the picture.
	r, ok = c.PartCrop(Box{Min: [3]float64{0, 0, 0}, Max: [3]float64{190, 95, 10}}, 0, sz, 0)
	if want := image.Rect(0, 0, 1280, 720); !ok || r != want {
		t.Errorf("crop at the edge %v, want %v", r, want)
	}
	// The turn of 180°.
	r, _ = c.PartCrop(Box{Min: [3]float64{50, 20, 0}, Max: [3]float64{100, 40, 10}}, 0, sz, 180)
	if want := image.Rect(592, 400, 1008, 608); r != want {
		t.Errorf("turned crop %v, want %v", r, want)
	}
	// A box behind the camera, and a box outside the picture.
	behind := &Calibration{P: [12]float64{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0}}
	if _, ok := behind.PartCrop(Box{Min: [3]float64{0, 0, -5}, Max: [3]float64{1, 1, 5}}, 0, sz, 0); ok {
		t.Error("a crop of a box behind the camera")
	}
	if _, ok := c.PartCrop(Box{Min: [3]float64{300, 300, 0}, Max: [3]float64{400, 400, 10}}, 0, sz, 0); ok {
		t.Error("a crop of a box outside the picture")
	}
}

// CropTo makes a frame of the crop at the size of the video.
func TestCropTo(t *testing.T) {
	f := picFrame(texturePicture(1280, 720))
	v := f.CropTo(image.Rect(272, 112, 688, 320), 832, 416)
	if v.Size() != image.Pt(832, 416) {
		t.Fatalf("size %v", v.Size())
	}
	c, err := jpeg.DecodeConfig(bytes.NewReader(v.JPEG()))
	if err != nil || c.Width != 832 || c.Height != 416 {
		t.Errorf("JPEG %dx%d, %v", c.Width, c.Height, err)
	}
}

// BenchmarkCropFrame is the cost of one frame of a time lapse with a crop: the decode of a JPEG
// of the camera (1280 x 720), the crop of about half its width at the width of the video, and the
// encode of the JPEG of the video.
func BenchmarkCropFrame(b *testing.B) {
	var buf bytes.Buffer
	jpeg.Encode(&buf, texturePicture(1280, 720), &jpeg.Options{Quality: 80})
	data := buf.Bytes()
	b.ResetTimer()
	for range b.N {
		f, err := FrameOf(data)
		if err != nil {
			b.Fatal(err)
		}
		if len(f.CropTo(image.Rect(320, 176, 960, 544), 1280, 736).JPEG()) == 0 {
			b.Fatal("no JPEG")
		}
	}
}

// BenchmarkWholeFrame is the cost of a frame with no crop: the JPEG of the camera as it is.
func BenchmarkWholeFrame(b *testing.B) {
	var buf bytes.Buffer
	jpeg.Encode(&buf, texturePicture(1280, 720), &jpeg.Options{Quality: 80})
	data := buf.Bytes()
	b.ResetTimer()
	for range b.N {
		f, _ := FrameOf(data)
		if len(f.JPEG()) == 0 {
			b.Fatal("no JPEG")
		}
	}
}
