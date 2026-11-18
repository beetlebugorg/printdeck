package jpegdec

import (
	"bytes"
	"image"
	"runtime"
	"testing"
)

// The pictures of a Sparse are the pictures of DecodeRegion, for each scale and region, and for
// one thread and many.
func TestSparseRender(t *testing.T) {
	procs := runtime.GOMAXPROCS(0)
	defer runtime.GOMAXPROCS(procs)
	for _, pic := range [][]byte{
		noisyPicture(t, 1920, 1080, image.YCbCrSubsampleRatio422, 9),
		noisyPicture(t, 1283, 721, image.YCbCrSubsampleRatio420, 10),
		testPicture(t, 640, 480, true),
	} {
		for _, n := range []int{1, 4} {
			runtime.GOMAXPROCS(n)
			s, err := DecodeSparse(pic)
			if err != nil {
				t.Fatal(err)
			}
			for shift := range 4 {
				for _, r := range []image.Rectangle{{}, image.Rect(333, 211, 1001, 777), image.Rect(0, 0, 64, 64)} {
					var want image.Image
					if r.Empty() {
						want, err = DecodeScaled(bytes.NewReader(pic), shift)
					} else {
						want, err = DecodeRegion(bytes.NewReader(pic), shift, r)
					}
					if err != nil {
						t.Fatal(err)
					}
					got, err := s.Render(shift, r)
					if err != nil {
						t.Fatal(err)
					}
					if got.Bounds() != want.Bounds() {
						t.Fatalf("shift %d %v: bounds %v, want %v", shift, r, got.Bounds(), want.Bounds())
					}
					b := want.Bounds()
					for y := b.Min.Y; y < b.Max.Y; y++ {
						for x := b.Min.X; x < b.Max.X; x++ {
							if got.At(x, y) != want.At(x, y) {
								t.Fatalf("threads %d shift %d %v: pixel %d,%d differs", n, shift, r, x, y)
							}
						}
					}
				}
			}
			s.Release()
		}
	}
}

// The output of GPUInput on the CPU, block by block, is the picture of DecodeScaled.
func TestGPUInputCPU(t *testing.T) {
	for _, pic := range [][]byte{
		noisyPicture(t, 1920, 1080, image.YCbCrSubsampleRatio422, 11),
		noisyPicture(t, 1283, 721, image.YCbCrSubsampleRatio420, 12),
		testPicture(t, 640, 480, true),
	} {
		s, err := DecodeSparse(pic)
		if err != nil {
			t.Fatal(err)
		}
		in, err := s.GPUInput()
		if err != nil {
			t.Fatal(err)
		}
		got := in.Picture(in.RenderCPU())
		want, _ := DecodeScaled(bytes.NewReader(pic), 0)
		samePlanes(t, "GPUInput", got, want)
		s.Release()
	}
}
