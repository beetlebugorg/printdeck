package ui

import (
	"image"
	"math"
	"strings"
	"testing"
	"time"

	"printdeck/internal/camera"
	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/internal/touch"
)

// homedIdle is an idle printer that is homed, with the axes of a TAZ Workhorse.
var homedIdle = func() (st printer.State) {
	st = idle
	st.Homed = "xyz"
	st.Axis = [2][3]float64{{-50, -17, -2}, {295, 308, 299}}
	return st
}()

// tallCamera projects a place of the printer to the demo picture of 1280 x 720: a pinhole camera
// in front of the bed, up and looking down at it.
func tallCamera(p [3]float64) (float64, float64) {
	eye := [3]float64{140, -300, 300}
	fwd := norm3([3]float64{0, 440, -300})
	right := norm3([3]float64{fwd[1], -fwd[0], 0})
	down := [3]float64{fwd[1]*right[2] - fwd[2]*right[1], fwd[2]*right[0] - fwd[0]*right[2], fwd[0]*right[1] - fwd[1]*right[0]}
	d := [3]float64{p[0] - eye[0], p[1] - eye[1], p[2] - eye[2]}
	dot := func(a, b [3]float64) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
	z := dot(fwd, d)
	return 640 + 900*dot(right, d)/z, 360 + 900*dot(down, d)/z
}

func norm3(a [3]float64) [3]float64 {
	l := math.Sqrt(a[0]*a[0] + a[1]*a[1] + a[2]*a[2])
	return [3]float64{a[0] / l, a[1] / l, a[2] / l}
}

// calibPosts runs the posts of the moves until the calibration waits for a tap or ends.
func calibPosts(t *testing.T, a *App) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for a.calib != nil && a.calib.phase == calibMoving {
		select {
		case f := <-a.posted:
			f()
		case <-deadline:
			t.Fatal("the calibration does not go on")
		}
	}
}

// pictureHit returns the picture of the calibration page that gets the taps.
func pictureHit(t *testing.T, a *App, cv *gfx.Canvas) image.Rectangle {
	t.Helper()
	a.Draw(cv)
	for _, h := range a.in.Hits() {
		if h.Pad != nil {
			return h.R
		}
	}
	t.Fatal("no picture to tap")
	return image.Rectangle{}
}

// tapFrame taps the picture of the calibration page at the pixel (u, v) of the frame.
func tapFrame(t *testing.T, a *App, cv *gfx.Canvas, u, v float64) {
	t.Helper()
	r := pictureHit(t, a, cv)
	fl := a.calib
	sz := fl.frame.Size()
	box := [4]float64{0, 0, float64(sz.X), float64(sz.Y)}
	if fl.zoom != nil {
		box = fl.zoomBox
	}
	x := r.Min.X + int((u-box[0])/(box[2]-box[0])*float64(r.Dx())+0.5)
	y := r.Min.Y + int((v-box[1])/(box[3]-box[1])*float64(r.Dy())+0.5)
	ev(a, touch.Down, x, y)
	ev(a, touch.Up, x, y)
}

// The calibration moves the nozzle to each point, gets a tap and a zoomed tap on the nozzle tip
// at each point, fits the camera, and saves the calibration.
func TestCameraCalibrate(t *testing.T) {
	calibSettle, calibFrameWait = 0, 10*time.Millisecond
	a, pr, cv := newPrintApp(homedIdle)
	cam := NewDemoCamera()
	a.Camera = cam
	a.pages = []page{pageMachine, pageSettings, pageCamera}
	if f := simFrame(t, a, "camera-not-calibrated"); !strings.Contains(f, "NOT CALIBRATED") && !strings.Contains(f, "Not calibrated") {
		t.Error("no calibration state on the page")
	}
	tapLabel(t, a, cv, "Calibrate")
	if a.confirm == nil || !strings.Contains(a.confirm.question, "9 points") {
		t.Fatalf("no question before the moves: %+v", a.confirm)
	}
	simFrame(t, a, "camera-calibrate-ask")
	tapLabel(t, a, cv, "Start")
	calibPosts(t, a)
	if a.topPage() != pageCalibrate || a.calib.phase != calibTap {
		t.Fatalf("page %v, phase %v", a.topPage(), a.calib.phase)
	}
	if f := simFrame(t, a, "camera-calibrate-point"); !strings.Contains(f, "POINT 1 OF 9") {
		t.Error("no step on the page")
	}
	for i := 0; a.calib.phase == calibTap; i++ {
		fl := a.calib
		for _, p := range fl.points {
			if p[2] < 5 || p[0] < -50 || p[0] > 295 || p[1] < -17 || p[1] > 308 {
				t.Fatalf("point %v is outside the limits or lower than 5 mm", p)
			}
		}
		u, v := tallCamera(fl.place)
		// The first tap is 20 px off: it zooms in. The second tap places the mark.
		tapFrame(t, a, cv, u+20, v-12)
		if fl.zoom == nil {
			t.Fatal("no zoom after the first tap")
		}
		if i == 0 {
			simFrame(t, a, "camera-calibrate-zoom")
		}
		tapFrame(t, a, cv, u, v)
		if math.Hypot(fl.tap[0]-u, fl.tap[1]-v) > 1.5 {
			t.Errorf("point %d: mark %.1f,%.1f, want %.1f,%.1f", i, fl.tap[0], fl.tap[1], u, v)
		}
		tapLabel(t, a, cv, "Use point")
		calibPosts(t, a)
	}
	if a.calib.phase != calibDone {
		t.Fatalf("phase %v: %s", a.calib.phase, a.calib.msg)
	}
	res := a.calib.result
	t.Logf("calibration: %d points, error %.2f px", res.Points, res.Error)
	if res.Points != 9 || res.Error > 2 || res.Width != 1280 || res.Height != 720 || res.InW != 1280 {
		t.Errorf("result %+v", res)
	}
	if f := simFrame(t, a, "camera-calibrate-done"); !strings.Contains(f, "SAVE THE CALIBRATION") {
		t.Error("no result on the page")
	}
	tapLabel(t, a, cv, "Save")
	if cam.Cal == nil || a.topPage() != pageCamera {
		t.Fatalf("not saved: page %v", a.topPage())
	}
	moves := 0
	for _, c := range pr.Calls() {
		if strings.HasPrefix(c, "gcode G90; G1 Z") {
			moves++
		}
		if strings.Contains(c, "M104") || strings.Contains(c, "M140") {
			t.Errorf("a heater changed: %s", c)
		}
	}
	if moves != 9 {
		t.Errorf("%d moves, want 9", moves)
	}
	// The Camera page shows the calibration, the views and Clear.
	if f := simFrame(t, a, "camera-calibrated"); !strings.Contains(f, "Calibrated: ") {
		t.Error("no calibration on the page")
	}
	tapLabel(t, a, cv, "View")
	if a.camView != camera.ViewToolhead {
		t.Errorf("view %q", a.camView)
	}
	simFrame(t, a, "camera-view-toolhead")
	tapLabel(t, a, cv, "Clear calibration")
	tapLabel(t, a, cv, "Clear")
	if cam.Cal != nil {
		t.Error("not cleared")
	}
}

// A calibration with too few marks fails with a message, Cancel ends it, a printer that is not
// homed homes first, and a print stops the start.
func TestCameraCalibrateFails(t *testing.T) {
	calibSettle, calibFrameWait = 0, 10*time.Millisecond
	st := homedIdle
	st.Homed = ""
	a, pr, cv := newPrintApp(st)
	a.Camera = NewDemoCamera()
	a.pages = []page{pageMachine, pageSettings, pageCamera}
	tapLabel(t, a, cv, "Calibrate")
	if a.confirm == nil || !strings.Contains(a.confirm.question, "homes first") {
		t.Fatalf("question %+v", a.confirm)
	}
	tapLabel(t, a, cv, "Home and start")
	calibPosts(t, a)
	if c := pr.Calls(); len(c) == 0 || c[0] != "home []" {
		t.Errorf("calls %v, want a homing first", c)
	}
	for a.calib.phase == calibTap {
		tapLabel(t, a, cv, "Skip")
		calibPosts(t, a)
	}
	if a.calib.phase != calibFailed || !strings.Contains(a.calib.msg, "0 points") {
		t.Fatalf("phase %v: %q", a.calib.phase, a.calib.msg)
	}
	simFrame(t, a, "camera-calibrate-failed")
	tapLabel(t, a, cv, "Close")
	if a.calib != nil || a.topPage() != pageCamera {
		t.Error("Close does not end the calibration")
	}

	// Cancel during a point.
	pr.State_ = homedIdle
	tapLabel(t, a, cv, "Calibrate")
	tapLabel(t, a, cv, "Start")
	calibPosts(t, a)
	tapLabel(t, a, cv, "Cancel")
	if a.calib != nil || a.topPage() != pageCamera {
		t.Error("Cancel does not end the calibration")
	}

	// A print: no calibration.
	pr.State_ = paused
	a.askCalibrate()
	if a.confirm != nil || a.calibReady() != waitText {
		t.Error("a calibration during a print")
	}
}

// The wide calibration page has the keys in the left column.
func TestCameraCalibrateWide(t *testing.T) {
	calibSettle, calibFrameWait = 0, 10*time.Millisecond
	a, _, _ := newPrintApp(homedIdle)
	a.Wide = true
	cv := gfx.New(a.Screen().W, a.Screen().H)
	a.Camera = NewDemoCamera()
	a.pages = []page{pageMachine, pageSettings, pageCamera}
	simFrame(t, a, "camera-wide-not-calibrated")
	tapLabel(t, a, cv, "Calibrate")
	tapLabel(t, a, cv, "Start")
	calibPosts(t, a)
	u, v := tallCamera(a.calib.place)
	tapFrame(t, a, cv, u, v)
	if f := simFrame(t, a, "camera-calibrate-wide"); !strings.Contains(f, "USE POINT") {
		t.Error("no Use point key")
	}
	tapLabel(t, a, cv, "Cancel")
	cam := a.Camera.(*DemoCamera)
	cam.Cal = &camera.Calibration{Error: 3.2, Points: 9, Time: time.Date(2026, 10, 25, 12, 0, 0, 0, time.Local)}
	a.camView = camera.ViewPart
	if f := simFrame(t, a, "camera-wide-calibrated"); !strings.Contains(f, `"CLEAR"`) {
		t.Error("no Clear key")
	}
}
