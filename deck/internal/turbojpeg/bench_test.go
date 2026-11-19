//go:build turbojpeg

package turbojpeg

import (
	"bytes"
	"image"
	"os"
	"runtime"
	"testing"
	"time"

	"printdeck/internal/jpegdec"
)

// TestCompare times libjpeg-turbo and jpegdec on the JPEG of $JPEG_COMPARE (a camera frame).
func TestCompare(t *testing.T) {
	path := os.Getenv("JPEG_COMPARE")
	if path == "" {
		t.Skip("JPEG_COMPARE is not set")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	best := func(n int, f func()) float64 {
		m := time.Hour
		for range n {
			s := time.Now()
			f()
			m = min(m, time.Since(s))
		}
		return m.Seconds() * 1000
	}
	t.Logf("%d KB, GOMAXPROCS %d", len(b)/1024, runtime.GOMAXPROCS(0))
	for shift := range 4 {
		tj := best(30, func() { Decode(b, shift) })
		j1 := best(30, func() { jpegdec.DecodeThreads(bytes.NewReader(b), shift, image.Rectangle{}, 1) })
		jn := best(30, func() { jpegdec.DecodeThreads(bytes.NewReader(b), shift, image.Rectangle{}, 0) })
		t.Logf("decode 1/%d: libjpeg-turbo %6.1f ms   jpegdec 1 thread %6.1f ms   jpegdec all %6.1f ms", 1<<shift, tj, j1, jn)
	}
	p, _ := jpegdec.DecodeScaled(bytes.NewReader(b), 1)
	src := p.(*image.YCbCr)
	tj := best(20, func() { Encode(src, 90) })
	j1 := best(20, func() { jpegdec.EncodeThreads(src, 90, 1) })
	jn := best(20, func() { jpegdec.EncodeThreads(src, 90, 0) })
	t.Logf("encode %dx%d q90: libjpeg-turbo %6.1f ms   jpegdec 1 thread %6.1f ms   jpegdec all %6.1f ms", src.Rect.Dx(), src.Rect.Dy(), tj, j1, jn)
}
