package camera

import (
	"context"
	"errors"
	"image"
	"math/rand"
	"os"
	"sync"
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
// camera has a focus control: a fixed focus away from testFocus.Pos blurs the picture. It also has
// a digital PTZ: its picture is the crop of a scene of twice the size (testScenePicture). The turn
// of the settings turns the picture, and a turn of 90° or 270° makes the frames tall.
func testCapture(ctx context.Context, l link, second bool) error {
	s := l.settings()
	f := testFormat
	f.OutW, f.OutH = TurnedSize(f.OutW, f.OutH, s.Rotate)
	l.format(f)
	var z testFocuser
	var pz *ptzCtl
	if !second {
		z.apply(s, l.focus)
		pz = newPTZCtl(newTestPTZDev())
		pz.apply(s)
		l.reportPTZ(pz.p)
	}
	tick := time.NewTicker(time.Second / time.Duration(f.OutFPS))
	defer tick.Stop()
	for n := 0; ; n++ {
		var pic *image.YCbCr
		if second {
			pic = testPattern(testFormat.OutW, testFormat.OutH, n, f.OutFPS*4, second)
		} else {
			cs := l.settings()
			z.apply(cs, l.focus)
			if pz.apply(cs) {
				l.reportPTZ(pz.p)
			}
			pic = testScenePicture(testFormat.OutW, testFormat.OutH, n, f.OutFPS*4, pz.p.Pos)
		}
		if s.Rotate != 0 {
			pic = scaleYCbCr(pic, f.OutW, f.OutH, s.Rotate)
		}
		if !second {
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

// The digital PTZ of the first test camera. The zoom factor is the zoom value / 100, and the
// center of the crop moves by testPanShift of the width of the scene for each unit of pan, and by
// testTiltShift of its height for each unit of tilt (up for a positive tilt). The crop stays in
// the scene. The deck measures this map (MeasurePTZ) as on a real camera.
var (
	testZoom = PTZRange{Min: 100, Max: 400, Step: 1, Default: 100}
	testPan  = PTZRange{Min: -36000, Max: 36000, Step: 100}
	testTilt = PTZRange{Min: -36000, Max: 36000, Step: 100}
)

const (
	testPanShift  = 1.0 / 96000
	testTiltShift = -1.0 / 72000
)

// testWindow is the crop of the scene at the position p: its center (cu, cv) and its zoom factor
// k, in normalized coordinates of the scene.
func testWindow(p PTZPos) (cu, cv, k float64) {
	k = float64(p.Zoom) / 100
	half := 0.5 / k
	cu = max(half, min(1-half, 0.5+float64(p.Pan)*testPanShift))
	cv = max(half, min(1-half, 0.5+float64(p.Tilt)*testTiltShift))
	return cu, cv, k
}

// testPTZDev is the PTZ controls of the first test camera (ctrlDev).
type testPTZDev struct {
	v map[uint32]int
}

func newTestPTZDev() *testPTZDev {
	return &testPTZDev{v: map[uint32]int{cidZoomAbsolute: testZoom.Default, cidPanAbsolute: 0, cidTiltAbsolute: 0}}
}

func testRange(id uint32) (PTZRange, bool) {
	switch id {
	case cidZoomAbsolute:
		return testZoom, true
	case cidPanAbsolute:
		return testPan, true
	case cidTiltAbsolute:
		return testTilt, true
	}
	return PTZRange{}, false
}

func (d *testPTZDev) query(id uint32) (PTZRange, bool) { return testRange(id) }

func (d *testPTZDev) get(id uint32) (int, error) {
	v, ok := d.v[id]
	if !ok {
		return 0, errors.New("no control")
	}
	return v, nil
}

func (d *testPTZDev) set(id uint32, v int) error {
	r, ok := testRange(id)
	if !ok {
		return errors.New("no control")
	}
	d.v[id] = r.Clamp(v)
	return nil
}

// testScene is the scene of the first test camera: color bars, with a grid and boxes of other
// brightness, so that each part of the scene looks different. It is twice the size of the frames.
var testScene = sync.OnceValue(func() *image.YCbCr {
	w, h := 2*testFormat.OutW, 2*testFormat.OutH
	p := testPattern(w, h, 0, 1, false)
	// testPattern draws the square at the left: the bar color goes over it again.
	for x := 0; x < w; x++ {
		c := testBars[x*len(testBars)/w]
		for y := 0; y < h; y++ {
			p.Y[y*p.YStride+x] = c[0]
		}
	}
	rng := rand.New(rand.NewSource(7))
	for range 160 {
		bw, bh := 20+rng.Intn(140), 20+rng.Intn(140)
		x0, y0 := rng.Intn(w-bw), rng.Intn(h-bh)
		yv := uint8(16 + rng.Intn(220))
		for y := y0; y < y0+bh; y++ {
			for x := x0; x < x0+bw; x++ {
				p.Y[y*p.YStride+x] = yv
			}
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x%160 < 6 || y%160 < 6 {
				p.Y[y*p.YStride+x] = 20
			}
		}
	}
	return p
})

// testScenePicture is frame n of the first test camera at the PTZ position p, at w x h: the crop of
// the scene, and a white square that goes across the scene in period frames.
func testScenePicture(w, h, n, period int, p PTZPos) *image.YCbCr {
	sc := testScene()
	sw, sh := float64(sc.Rect.Dx()), float64(sc.Rect.Dy())
	cu, cv, k := testWindow(p)
	c := [4]float64{(cu - 0.5/k) * sw, (cv - 0.5/k) * sh, (cu + 0.5/k) * sw, (cv + 0.5/k) * sh}
	out := cropScale(sc, c, w, h)
	// The square, a tenth of the height of the scene, in pixels of the scene and then of the
	// frame.
	side := sh / 10
	sx := (sw - side) * float64(n%period) / float64(period)
	sy := (sh - side) / 2
	fx, fy := float64(w)/(c[2]-c[0]), float64(h)/(c[3]-c[1])
	x0, x1 := max(0, int((sx-c[0])*fx)), min(w, int((sx+side-c[0])*fx))
	y0, y1 := max(0, int((sy-c[1])*fy)), min(h, int((sy+side-c[1])*fy))
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			out.Y[y*out.YStride+x] = 235
		}
	}
	return out
}
