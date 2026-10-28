package ui

import (
	"bytes"
	"image"
	"image/jpeg"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/timelapse"
)

// benchVideo writes a video of n frames of 1280 x 720, as the camera of the deck makes them, to
// dir. The frames have smooth parts and noise: a JPEG has about 120 kB, as a frame of a camera.
func benchVideo(b *testing.B, dir string, n int) string {
	rng := rand.New(rand.NewSource(1))
	r := timelapse.NewRecorder()
	for i := range n {
		img := image.NewYCbCr(image.Rect(0, 0, 1280, 720), image.YCbCrSubsampleRatio420)
		for y := range 720 {
			for x := range 1280 {
				img.Y[y*img.YStride+x] = uint8((x/4+y/3+i*7)%200) + uint8(rng.Intn(14))
			}
		}
		for k := range img.Cb {
			img.Cb[k], img.Cr[k] = uint8(100+k%40), uint8(140-k%30)
		}
		var buf bytes.Buffer
		jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80})
		if i == 0 {
			b.ReportMetric(float64(buf.Len())/1000, "kB/frame")
		}
		r.Add(buf.Bytes(), image.Pt(1280, 720), i+1)
	}
	var buf bytes.Buffer
	if err := r.WriteVideo(&buf); err != nil {
		b.Fatal(err)
	}
	name := "bench-20261027-1432.mov"
	if err := os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0o644); err != nil {
		b.Fatal(err)
	}
	return name
}

// The metric %cpu@30fps is the CPU use of one core at 30 frames a second, the highest rate of a
// video (timelapse.Rate).

// benchPlayer draws one frame of the player for each new frame of the video, as the deck does:
// the UI is recorded, and the recording is drawn at the size of the screen.
func benchPlayer(b *testing.B, wide bool, scale [2]int) {
	dir := b.TempDir()
	name := benchVideo(b, dir, 30)
	a, _, _ := newPrintApp(idle)
	a.Wide, a.Scale = wide, scale
	now := time.Unix(5000, 0)
	a.Now = func() time.Time { return now }
	a.TimeLapse = &fakeTimeLapse{dir: dir, in: timelapse.Info{Videos: []timelapse.Video{{Name: name}}}}
	a.openPlayer(name, "bench.gcode")
	s := a.Screen()
	big := gfx.New(s.W*scale[0]/scale[1], s.H*scale[0]/scale[1])
	draw := func() {
		cv, rec := gfx.NewRecording(s.W, s.H)
		a.Draw(cv)
		rec.DrawScaled(big, scale[0], scale[1])
	}
	draw()
	b.ResetTimer()
	for range b.N {
		a.player.seek(float64((a.player.pos+1)%a.player.n)/float64(a.player.n-1), now)
		a.player.playing = false
		draw()
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)*30/1e7, "%cpu@30fps")
}

// The Touch Display 2 of the deck: 720 x 1200, the UI at 1.5 times.
func BenchmarkPlayerTall(b *testing.B) { benchPlayer(b, false, [2]int{720, 480}) }
func BenchmarkPlayerWide(b *testing.B) { benchPlayer(b, true, [2]int{1200, 800}) }

// A remote screen: the recording only, with the JPEG of the file for the browser.
func BenchmarkPlayerRemote(b *testing.B) {
	dir := b.TempDir()
	name := benchVideo(b, dir, 30)
	a, _, _ := newPrintApp(idle)
	a.Record = true
	now := time.Unix(5000, 0)
	a.Now = func() time.Time { return now }
	a.TimeLapse = &fakeTimeLapse{dir: dir, in: timelapse.Info{Videos: []timelapse.Video{{Name: name}}}}
	a.openPlayer(name, "bench.gcode")
	s := a.Screen()
	cv, _ := gfx.NewRecording(s.W, s.H)
	a.Draw(cv)
	b.ResetTimer()
	for range b.N {
		a.player.seek(float64((a.player.pos+1)%a.player.n)/float64(a.player.n-1), now)
		a.player.playing = false
		cv, _ := gfx.NewRecording(s.W, s.H)
		a.Draw(cv)
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)*30/1e7, "%cpu@30fps")
}
