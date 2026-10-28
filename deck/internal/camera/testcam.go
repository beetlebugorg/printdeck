package camera

import (
	"context"
	"image"
	"os"
	"time"
)

// TestCameraEnv is the environment variable that adds a test camera: with any value, the deck
// finds a camera that sends a moving test picture, on every system. With the value 2, the deck
// finds two test cameras of the same model. It is for the tests of the apps with no real camera,
// for example `printdeck app -sim` on a Mac.
const TestCameraEnv = "PRINTDECK_TEST_CAMERA"

// TestCamera is the device of the test camera, and TestCamera2 the device of the second test
// camera.
var (
	TestCamera  = Device{Path: "test", ID: "test", Name: "Test camera"}
	TestCamera2 = Device{Path: "test2", ID: "test2", Name: "Test camera"}
)

// testFormat is what the test camera sends: the size and the rate of the raw path of the Cam Link.
var testFormat = Format{In: "TEST", InW: 960, InH: 540, InFPS: 10, OutW: 960, OutH: 540, OutFPS: 10}

// withTestCamera adds the test cameras to the scan and the capture of a hub when TestCameraEnv is
// set. The test cameras are first in the list.
func withTestCamera(scan func() []Device, capture captureFunc) (func() []Device, captureFunc) {
	env := os.Getenv(TestCameraEnv)
	if env == "" {
		return scan, capture
	}
	tests := []Device{TestCamera}
	if env == "2" {
		tests = append(tests, TestCamera2)
	}
	return func() []Device {
			return append(append([]Device(nil), tests...), scan()...)
		}, func(ctx context.Context, d Device, l link) error {
			switch d.ID {
			case TestCamera.ID:
				return testCapture(ctx, l, false)
			case TestCamera2.ID:
				return testCapture(ctx, l, true)
			}
			return capture(ctx, d, l)
		}
}

// testCapture sends the test picture until ctx ends: color bars, and a white square that goes
// across the picture in 4 seconds, so a viewer can see that the frames move. The second camera
// (second) has the bars in the other order, and its square goes down the picture. The first
// camera has a focus control: a fixed focus away from testFocus.Pos blurs the picture. The turn of
// the settings turns the picture, and a turn of 90° or 270° makes the frames tall.
func testCapture(ctx context.Context, l link, second bool) error {
	s := l.settings()
	f := testFormat
	f.OutW, f.OutH = TurnedSize(f.OutW, f.OutH, s.Rotate)
	l.format(f)
	var z testFocuser
	if !second {
		z.apply(s, l.focus)
	}
	tick := time.NewTicker(time.Second / time.Duration(f.OutFPS))
	defer tick.Stop()
	for n := 0; ; n++ {
		pic := testPattern(testFormat.OutW, testFormat.OutH, n, f.OutFPS*4, second)
		if s.Rotate != 0 {
			pic = scaleYCbCr(pic, f.OutW, f.OutH, s.Rotate)
		}
		if !second {
			z.apply(l.settings(), l.focus)
			pic = z.blur(pic)
		}
		l.emit(picFrame(pic))
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

// testBars are the colors of the bars as Y, Cb, Cr: white, yellow, cyan, green, magenta, red and
// blue at 75 %.
var testBars = [][3]uint8{{180, 128, 128}, {162, 44, 142}, {131, 156, 44}, {112, 72, 58},
	{84, 184, 198}, {65, 100, 212}, {35, 212, 114}}

// testPattern is frame n of the test picture of w x h pixels: the square is at n/period of its way.
// The second picture has the bars in the other order, and its square goes down.
func testPattern(w, h, n, period int, second bool) *image.YCbCr {
	p := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
	for x := 0; x < w; x++ {
		i := x * len(testBars) / w
		if second {
			i = len(testBars) - 1 - i
		}
		c := testBars[i]
		for y := 0; y < h; y++ {
			p.Y[y*p.YStride+x] = c[0]
		}
		if x%2 == 0 {
			for y := 0; y < h/2; y++ {
				p.Cb[y*p.CStride+x/2], p.Cr[y*p.CStride+x/2] = c[1], c[2]
			}
		}
	}
	side := h / 4
	x0 := (w - side) * (n % period) / period
	y0 := (h - side) / 2
	if second {
		x0, y0 = (w-side)/2, (h-side)*(n%period)/period
	}
	for y := y0; y < y0+side; y++ {
		for x := x0; x < x0+side; x++ {
			p.Y[y*p.YStride+x] = 235
			p.Cb[(y/2)*p.CStride+x/2], p.Cr[(y/2)*p.CStride+x/2] = 128, 128
		}
	}
	return p
}

// testFocus is the focus control of the first test camera: Pos is the sharp position.
var testFocus = Focus{Min: 0, Max: 1023, Step: 1, Auto: true, Pos: 300}

// testFocuser is the focus of the first test camera.
type testFocuser struct {
	f   Focus
	set bool // f has a state
}

// apply sets the focus of the settings s, and gives a change to report.
func (z *testFocuser) apply(s CamSettings, report func(Focus)) {
	f := testFocus
	if s.FixedFocus {
		f.Auto, f.Pos = false, f.Clamp(s.Focus)
	}
	if !z.set || f != z.f {
		z.f, z.set = f, true
		report(f)
	}
}

// blur blurs the picture by the distance of the lens from the sharp position: a box average of
// the Y plane, one pixel of radius for each 40 steps.
func (z *testFocuser) blur(p *image.YCbCr) *image.YCbCr {
	r := z.f.Pos - testFocus.Pos
	if r < 0 {
		r = -r
	}
	r /= 40
	if r == 0 {
		return p
	}
	w, h := p.Rect.Dx(), p.Rect.Dy()
	row := make([]int, w+1)
	tmp := make([]uint8, len(p.Y))
	for y := range h {
		line := p.Y[y*p.YStride:]
		for x := range w {
			row[x+1] = row[x] + int(line[x])
		}
		for x := range w {
			a, b := max(0, x-r), min(w, x+r+1)
			tmp[y*p.YStride+x] = uint8((row[b] - row[a]) / (b - a))
		}
	}
	col := make([]int, h+1)
	for x := range w {
		for y := range h {
			col[y+1] = col[y] + int(tmp[y*p.YStride+x])
		}
		for y := range h {
			a, b := max(0, y-r), min(h, y+r+1)
			p.Y[y*p.YStride+x] = uint8((col[b] - col[a]) / (b - a))
		}
	}
	return p
}
