package jpegdec

import (
	"bytes"
	"image"
	"image/jpeg"
	"math/rand"
	"runtime"
	"testing"
)

// noisyPicture is a JPEG of w x h with detail everywhere: many coefficients in each block.
func noisyPicture(t testing.TB, w, h int, ratio image.YCbCrSubsampleRatio, seed int64) []byte {
	r := rand.New(rand.NewSource(seed))
	p := image.NewYCbCr(image.Rect(0, 0, w, h), ratio)
	for i := range p.Y {
		p.Y[i] = uint8(i%w/3 + r.Intn(40))
	}
	for i := range p.Cb {
		p.Cb[i], p.Cr[i] = uint8(90+r.Intn(30)), uint8(150+r.Intn(30))
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, p, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func samePlanes(t *testing.T, name string, got, want image.Image) {
	t.Helper()
	gp, wp := planesOf(got), planesOf(want)
	if got.Bounds() != want.Bounds() || len(gp) != len(wp) {
		t.Fatalf("%s: bounds %v, want %v", name, got.Bounds(), want.Bounds())
	}
	for i := range wp {
		for y := range wp[i].h {
			if !bytes.Equal(gp[i].pix[y*gp[i].stride:y*gp[i].stride+wp[i].w], wp[i].pix[y*wp[i].stride:y*wp[i].stride+wp[i].w]) {
				t.Fatalf("%s: plane %d row %d differs", name, i, y)
			}
		}
	}
}

// The parallel scan gives the same picture as image/jpeg at the whole size, and the same picture
// as the scan on one thread at each scale, for each count of threads.
func TestParallelScan(t *testing.T) {
	old, procs := parallelMin, runtime.GOMAXPROCS(0)
	defer func() { parallelMin = old; runtime.GOMAXPROCS(procs) }()
	pics := map[string][]byte{
		"422 1920x1080": noisyPicture(t, 1920, 1080, image.YCbCrSubsampleRatio422, 1),
		"420 1283x721":  noisyPicture(t, 1283, 721, image.YCbCrSubsampleRatio420, 2),
		"444 640x480":   noisyPicture(t, 640, 480, image.YCbCrSubsampleRatio444, 3),
		"smooth":        testPicture(t, 1280, 720, false),
		"gray":          testPicture(t, 1280, 720, true),
	}
	for name, b := range pics {
		want, err := jpeg.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		for shift := range 4 {
			parallelMin = 1 << 30
			runtime.GOMAXPROCS(1)
			one, err := DecodeScaled(bytes.NewReader(b), shift)
			if err != nil {
				t.Fatal(err)
			}
			if shift == 0 {
				samePlanes(t, name+" one thread", one, want)
			}
			parallelMin = 1
			for _, n := range []int{2, 3, 4, 7, 8} {
				runtime.GOMAXPROCS(n)
				got, err := DecodeScaled(bytes.NewReader(b), shift)
				if err != nil {
					t.Fatalf("%s shift %d threads %d: %v", name, shift, n, err)
				}
				samePlanes(t, name, got, one)
			}
		}
	}
}

// A short frame (a USB camera can send part of a frame) gives an error and no picture.
func TestParallelShort(t *testing.T) {
	old := parallelMin
	defer func() { parallelMin = old }()
	parallelMin = 1
	b := noisyPicture(t, 1920, 1080, image.YCbCrSubsampleRatio422, 4)
	for _, cut := range []int{len(b) / 2, len(b) - 5000} {
		if _, err := DecodeScaled(bytes.NewReader(b[:cut]), 0); err == nil {
			t.Errorf("cut at %d: no error", cut)
		}
	}
}

// DecodeRegion gives the pixels of DecodeScaled in the region, on one thread and on many.
func TestDecodeRegion(t *testing.T) {
	old, procs := parallelMin, runtime.GOMAXPROCS(0)
	defer func() { parallelMin = old; runtime.GOMAXPROCS(procs) }()
	b := noisyPicture(t, 1920, 1080, image.YCbCrSubsampleRatio422, 5)
	g := testPicture(t, 1283, 721, true)
	for _, pic := range [][]byte{b, g} {
		for shift := range 4 {
			whole, err := DecodeScaled(bytes.NewReader(pic), shift)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range []image.Rectangle{image.Rect(0, 0, 100, 50), image.Rect(333, 211, 1001, 777), image.Rect(1800, 1000, 1920, 1080), image.Rect(17, 3, 18, 4)} {
				for _, setup := range []func(){
					func() { parallelMin = 1 << 30; runtime.GOMAXPROCS(1) },
					func() { parallelMin = 1; runtime.GOMAXPROCS(4) },
				} {
					setup()
					got, err := DecodeRegion(bytes.NewReader(pic), shift, r)
					if err != nil {
						t.Fatal(err)
					}
					k := 1 << shift
					want := image.Rect(r.Min.X/k, r.Min.Y/k, (r.Max.X+k-1)/k, (r.Max.Y+k-1)/k).Intersect(whole.Bounds())
					if got.Bounds() != want {
						t.Fatalf("shift %d %v: bounds %v, want %v", shift, r, got.Bounds(), want)
					}
					for y := want.Min.Y; y < want.Max.Y; y++ {
						for x := want.Min.X; x < want.Max.X; x++ {
							if got.At(x, y) != whole.At(x, y) {
								t.Fatalf("shift %d %v: pixel %d,%d: %v, want %v", shift, r, x, y, got.At(x, y), whole.At(x, y))
							}
						}
					}
				}
			}
		}
	}
}
