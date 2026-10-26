package camera

import (
	"context"
	"image"
	"os"
	"time"
)

// TestCameraEnv is the environment variable that adds a test camera: with any value, the deck
// finds a camera that sends a moving test picture, on every system. It is for the tests of the
// apps with no real camera, for example `printdeck app -sim` on a Mac.
const TestCameraEnv = "PRINTDECK_TEST_CAMERA"

// TestCamera is the device of the test camera.
var TestCamera = Device{Path: "test", ID: "test", Name: "Test camera"}

// testFormat is what the test camera sends: the size and the rate of the raw path of the Cam Link.
var testFormat = Format{In: "TEST", InW: 960, InH: 540, InFPS: 10, OutW: 960, OutH: 540, OutFPS: 10}

// withTestCamera adds the test camera to the scan and the capture of a hub when TestCameraEnv is
// set. The test camera is first in the list, so Auto picks it.
func withTestCamera(scan func() []Device, capture captureFunc) (func() []Device, captureFunc) {
	if os.Getenv(TestCameraEnv) == "" {
		return scan, capture
	}
	return func() []Device {
			return append([]Device{TestCamera}, scan()...)
		}, func(ctx context.Context, d Device, s Settings, format func(Format), emit func(*Frame)) error {
			if d.ID == TestCamera.ID {
				return testCapture(ctx, s, format, emit)
			}
			return capture(ctx, d, s, format, emit)
		}
}

// testCapture sends the test picture until ctx ends: color bars, and a white square that goes
// across the picture in 4 seconds, so a viewer can see that the frames move.
func testCapture(ctx context.Context, s Settings, format func(Format), emit func(*Frame)) error {
	f := testFormat
	format(f)
	tick := time.NewTicker(time.Second / time.Duration(f.OutFPS))
	defer tick.Stop()
	for n := 0; ; n++ {
		pic := testPattern(f.OutW, f.OutH, n, f.OutFPS*4)
		if s.Rotate == 180 {
			pic = scaleYCbCr(pic, f.OutW, f.OutH, true)
		}
		emit(picFrame(pic))
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
func testPattern(w, h, n, period int) *image.YCbCr {
	p := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
	for x := 0; x < w; x++ {
		c := testBars[x*len(testBars)/w]
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
	for y := y0; y < y0+side; y++ {
		for x := x0; x < x0+side; x++ {
			p.Y[y*p.YStride+x] = 235
			p.Cb[(y/2)*p.CStride+x/2], p.Cr[(y/2)*p.CStride+x/2] = 128, 128
		}
	}
	return p
}
