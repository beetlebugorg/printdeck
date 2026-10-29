package camera

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"syscall"
	"testing"
	"time"
)

// blurredJPEGs returns JPEGs of 1280 x 720 with the detail of a camera picture (photoJPEG), with
// a box blur of radius 0 to n-1.
func blurredJPEGs(tb testing.TB, n int) [][]byte {
	img, err := jpeg.Decode(bytes.NewReader(photoJPEG(tb, 1280, 720)))
	if err != nil {
		tb.Fatal(err)
	}
	src := img.(*image.YCbCr)
	var out [][]byte
	for r := range n {
		p := *src
		p.Y = append([]uint8(nil), src.Y...)
		if r > 0 {
			p.Y = boxBlur(&p, r)
		}
		var b bytes.Buffer
		if err := jpeg.Encode(&b, &p, &jpeg.Options{Quality: 85}); err != nil {
			tb.Fatal(err)
		}
		out = append(out, b.Bytes())
	}
	return out
}

// The sharpness of one frame of 1280 x 720 of an MJPEG camera: a new frame each time, so each
// measure decodes its JPEG. The middle half of the picture is the box with no calibration.
func BenchmarkFocusMeasure(b *testing.B) {
	jb := photoJPEG(b, 1280, 720)
	for _, tc := range []struct {
		name string
		r    image.Rectangle
	}{
		{"middle", image.Rect(320, 180, 960, 540)},
		{"part", image.Rect(500, 250, 800, 500)},
		{"whole", image.Rect(0, 0, 1280, 720)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			for range b.N {
				f, err := FrameOf(jb)
				if err != nil {
					b.Fatal(err)
				}
				f.sharpness(tc.r)
			}
		})
	}
}

// cpuTime is the CPU time of the process: user and system.
func cpuTime() time.Duration {
	var ru syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

// A whole search on a camera of 1280 x 720 in MJPEG with a focus range of 0 to 255: the
// sharpness of a picture goes down with the distance of the lens from 90. The fake camera sends
// 200 frames a second, so the search is quick. The CPU per search is the CPU of the deck less the
// CPU of the fake camera with no search.
func BenchmarkFocusSearch(b *testing.B) {
	jpegs := blurredJPEGs(b, 7)
	h := NewHub(Settings{})
	h.scan = func() []Device { return []Device{camA} }
	h.capture = func(ctx context.Context, d Device, l link) error {
		l.format(Format{In: "MJPEG", InW: 1280, InH: 720, OutW: 1280, OutH: 720, OutFPS: 200})
		z := Focus{Min: 0, Max: 255, Step: 1, Auto: true, Pos: 128}
		l.focus(z)
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for {
			if cs := l.settings(); cs.FixedFocus && (z.Auto || z.Pos != cs.Focus) {
				z.Auto, z.Pos = false, z.Clamp(cs.Focus)
				l.focus(z)
			}
			d := z.Pos - 90
			if d < 0 {
				d = -d
			}
			l.emit(newFrame(jpegs[min(len(jpegs)-1, d/15)], 1280, 720, nil))
			select {
			case <-ctx.Done():
				return nil
			case <-tick.C:
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.Run(ctx)
	stop := h.Cam(camA.ID).Watch()
	defer stop()
	if _, err := h.Cam(camA.ID).Snapshot(ctx); err != nil {
		b.Fatal(err)
	}
	// The CPU of the fake camera with no search.
	idleStart, idleCPU := time.Now(), cpuTime()
	time.Sleep(500 * time.Millisecond)
	idle := float64(cpuTime()-idleCPU) / float64(time.Since(idleStart))
	b.ResetTimer()
	start, cpu := time.Now(), cpuTime()
	for range b.N {
		pos, err := h.FocusOnPart(ctx, camA.ID, nil)
		if err != nil {
			b.Fatal(err)
		}
		if pos < 75 || pos > 105 {
			b.Fatalf("best %d, want about 90", pos)
		}
	}
	wall := time.Since(start)
	used := cpuTime() - cpu - time.Duration(idle*float64(wall))
	b.ReportMetric(float64(used.Milliseconds())/float64(b.N), "cpu-ms/search")
}
