package ui

import (
	"fmt"
	"image"
	"image/color"
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
	if a.topPage() != pageCalibrate || a.calib.phase != calibChoose {
		t.Fatalf("no choice of the way: page %v", a.topPage())
	}
	tapLabel(t, a, cv, "With the nozzle")
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
	if res.Points != 9 || res.Error > 2 || res.Width != 1280 || res.Height != 720 || res.InW != 1280 || res.Kind != camera.KindNozzle || res.BedY {
		t.Errorf("result %+v", res)
	}
	if f := simFrame(t, a, "camera-calibrate-done"); !strings.Contains(f, "SAVE THE CALIBRATION") {
		t.Error("no result on the page")
	}
	tapLabel(t, a, cv, "Save")
	if cam.Cams[0].Cal == nil || a.topPage() != pageCamera {
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
	if cam.Cams[0].Cal != nil {
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
	tapLabel(t, a, cv, "With the nozzle")
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
	tapLabel(t, a, cv, "With the nozzle")
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
	tapLabel(t, a, cv, "With the nozzle")
	tapLabel(t, a, cv, "Start")
	calibPosts(t, a)
	u, v := tallCamera(a.calib.place)
	tapFrame(t, a, cv, u, v)
	if f := simFrame(t, a, "camera-calibrate-wide"); !strings.Contains(f, "USE POINT") {
		t.Error("no Use point key")
	}
	tapLabel(t, a, cv, "Cancel")
	cam := a.Camera.(*DemoCamera)
	cam.Cams[0].Cal = &camera.Calibration{Error: 3.2, Points: 9, Time: time.Date(2026, 10, 25, 12, 0, 0, 0, time.Local)}
	a.camView = camera.ViewPart
	if f := simFrame(t, a, "camera-wide-calibrated"); !strings.Contains(f, `"CLEAR"`) {
		t.Error("no Clear key")
	}
}

// lowCamera projects a place of the frame of a printer with the bed on Y to the demo picture of
// 1280 x 720: a pinhole camera low and in front of the bed, looking at the line of the nozzle.
func lowCamera(p [3]float64) (float64, float64) {
	eye := [3]float64{146, -480, 90}
	fwd := norm3([3]float64{0, 480, -75})
	right := norm3([3]float64{fwd[1], -fwd[0], 0})
	down := [3]float64{fwd[1]*right[2] - fwd[2]*right[1], fwd[2]*right[0] - fwd[0]*right[2], fwd[0]*right[1] - fwd[1]*right[0]}
	d := [3]float64{p[0] - eye[0], p[1] - eye[1], p[2] - eye[2]}
	dot := func(a, b [3]float64) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
	z := dot(fwd, d)
	return 640 + 1000*dot(right, d)/z, 360 + 1000*dot(down, d)/z
}

// tazMachine is a TAZ Workhorse: cartesian, with the bed on Y.
var tazMachine = printer.Machine{Kinematics: "cartesian", NozzleDiameter: 0.5, FilamentDiameter: 2.85,
	MinX: -50, MaxX: 293, MinY: -17, MaxY: 308, Macros: []string{"PRINT_START", "PRINT_END"}}

// startTowers opens the calibration with a print, prints the towers, and ends the print, so the
// page shows Go on.
func startTowers(t *testing.T, a *App, pr *recPrinter, cv *gfx.Canvas, shots bool) {
	t.Helper()
	tapLabel(t, a, cv, "Calibrate")
	if a.topPage() != pageCalibrate || a.calib.phase != calibChoose || !a.calib.bedY {
		t.Fatalf("page %v, flow %+v", a.topPage(), a.calib)
	}
	if shots {
		if f := simFrame(t, a, "camera-calibrate-choose"); !strings.Contains(f, "WITH A PRINT") || !strings.Contains(f, "THE BED MOVES FRONT TO BACK") {
			t.Error("no ways on the page")
		}
	}
	// The setting of the bed: off and on again.
	tapLabel(t, a, cv, "The bed moves front to back")
	if a.calib.bedY {
		t.Error("the bed setting does not change")
	}
	tapLabel(t, a, cv, "The bed moves front to back")
	tapLabel(t, a, cv, "With a print")
	if a.confirm == nil || !strings.Contains(a.confirm.question, "5 towers") || !strings.Contains(a.confirm.question, "PLA at 210°") {
		t.Fatalf("question %+v", a.confirm)
	}
	if shots {
		simFrame(t, a, "camera-calibrate-print-ask")
	}
	tapLabel(t, a, cv, "Print")
	if c := pr.wait(1); len(c) == 0 || c[len(c)-1] != "towers 210 60 PLA" {
		t.Fatalf("calls %v", c)
	}
	// The print: the screen of the print shows it.
	printing := homedIdle
	printing.Printing, printing.FilePath, printing.File = true, printer.TowersFile, printer.TowersFile
	pr.State_ = printing
	a.Draw(cv)
	if a.topPage() == pageCalibrate || a.calib == nil || !a.calib.started {
		t.Fatalf("page %v during the print", a.topPage())
	}
	// The end of the print turns the motors off.
	done := homedIdle
	done.Homed, done.Finished, done.FinishedPath = "", "complete", printer.TowersFile
	pr.State_ = done
	a.Draw(cv)
	if a.topPage() != pageCalibrate || a.calib.phase != calibPrinted {
		t.Fatalf("after the print: page %v, phase %v", a.topPage(), a.calib.phase)
	}
}

// The calibration with a print: the towers print, the toolhead goes out of the view, the user
// taps the bottom and the top of each tower and the tops again at a second place of the bed (one
// mark skipped), and the fit of the bed slinger is saved. Then the page asks to remove the towers.
func TestCameraCalibrateTowers(t *testing.T) {
	calibSettle, calibFrameWait = 0, 10*time.Millisecond
	a, pr, cv := newPrintApp(homedIdle)
	pr.Machine_ = tazMachine
	a.Material = "PLA"
	cam := NewDemoCamera()
	a.Camera = cam
	a.pages = []page{pageMachine, pageSettings, pageCamera}
	startTowers(t, a, pr, cv, true)
	if f := simFrame(t, a, "camera-calibrate-printed"); !strings.Contains(f, "GO ON") {
		t.Error("no Go on key")
	}
	n := len(pr.Calls())
	tapLabel(t, a, cv, "Go on")
	calibPosts(t, a)
	c := pr.Calls()
	if len(c) <= n+1 || c[n] != "gcode G28 X Y; G90; G1 X-45.00 Y146.50 F3000; M400" || c[n+1] != "gcode G1 Z289.00 F1200; M400" {
		t.Fatalf("moves %v", c[n:])
	}
	fl := a.calib
	if fl.phase != calibTap || len(fl.points) != 15 {
		t.Fatalf("phase %v, %d points", fl.phase, len(fl.points))
	}
	if f := simFrame(t, a, "camera-calibrate-tower"); !strings.Contains(f, "MARK 1 OF 15: TOWER 1, BOTTOM") {
		t.Error("no mark on the page")
	}
	for i := 0; fl.phase == calibTap; i++ {
		if i == 3 {
			tapLabel(t, a, cv, "Skip")
			calibPosts(t, a)
			continue
		}
		p := fl.points[fl.i]
		bedY := fl.bedAt[fl.marks[fl.i].Bed]
		u, v := lowCamera([3]float64{p[0], p[1] - bedY, p[2]})
		tapFrame(t, a, cv, u+15, v+10)
		if i == 0 {
			simFrame(t, a, "camera-calibrate-tower-zoom")
		}
		tapFrame(t, a, cv, u, v)
		if i == 10 {
			if f := simFrame(t, a, "camera-calibrate-tower-second"); !strings.Contains(f, "TOWER 1, TOP") {
				t.Error("no second place of the bed")
			}
		}
		tapLabel(t, a, cv, "Use mark")
		calibPosts(t, a)
	}
	if fl.phase != calibDone {
		t.Fatalf("phase %v: %s", fl.phase, fl.msg)
	}
	if moves := len(pr.Calls()) - n; moves != 3 {
		t.Errorf("%d moves, want 3: the first place, the top, the second place", moves)
	}
	if fl.beds[1] <= fl.beds[0] {
		t.Errorf("bed places %v", fl.beds)
	}
	res := fl.result
	t.Logf("calibration with towers: %d marks, error %.2f px", res.Points, res.Error)
	if res.Points != 14 || res.Error > 2 || res.Kind != camera.KindTowers || !res.BedY {
		t.Errorf("result %+v", res)
	}
	f := simFrame(t, a, "camera-calibrate-towers-done")
	if !strings.Contains(f, "GREEN LINES") || !strings.Contains(f, fmt.Sprint(rgbInt(calibFitInk))) {
		t.Error("no fit over the picture")
	}
	// The fit projects a place at another place of the bed as the camera.
	u, v, _ := res.ProjectAt([3]float64{200, 100, 40}, 60, 1280, 720, 0)
	wu, wv := lowCamera([3]float64{200, 40, 40})
	if math.Hypot(u-wu, v-wv) > 3 {
		t.Errorf("projection %.1f,%.1f, want %.1f,%.1f", u, v, wu, wv)
	}
	tapLabel(t, a, cv, "Save")
	if fl.phase != calibRemove || cam.Cams[0].Cal == nil || cam.Cams[0].Cal.Kind != camera.KindTowers {
		t.Fatalf("not saved: phase %v", fl.phase)
	}
	if f := simFrame(t, a, "camera-calibrate-remove"); !strings.Contains(f, "REMOVE THE TOWERS") {
		t.Error("no request to remove the towers")
	}
	tapLabel(t, a, cv, "Done")
	if a.calib != nil || a.topPage() != pageCamera {
		t.Fatalf("page %v", a.topPage())
	}
	if f := simFrame(t, a, "camera-calibrated-print"); !strings.Contains(f, "print") {
		t.Error("no kind on the Camera page")
	}
}

func rgbInt(c color.RGBA) int { return int(c.R)<<16 | int(c.G)<<8 | int(c.B) }

// A print of the towers that does not end, too few marks, Again with the towers on the bed, and
// the wide page with the plan of the towers.
func TestCameraCalibrateTowersFails(t *testing.T) {
	calibSettle, calibFrameWait = 0, 10*time.Millisecond
	a, pr, cv := newPrintApp(homedIdle)
	pr.Machine_ = tazMachine
	a.Material = "PLA"
	a.Camera = NewDemoCamera()
	a.pages = []page{pageMachine, pageSettings, pageCamera}

	// A cancelled print.
	tapLabel(t, a, cv, "Calibrate")
	tapLabel(t, a, cv, "With a print")
	tapLabel(t, a, cv, "Print")
	printing := homedIdle
	printing.Printing, printing.FilePath = true, printer.TowersFile
	pr.State_ = printing
	a.Draw(cv)
	end := homedIdle
	end.Finished, end.FinishedPath = "cancelled", printer.TowersFile
	pr.State_ = end
	a.Draw(cv)
	if a.topPage() != pageCalibrate || a.calib.phase != calibFailed {
		t.Fatalf("cancelled: page %v, phase %v", a.topPage(), a.calib.phase)
	}
	tapLabel(t, a, cv, "Close")
	if a.calib != nil || a.topPage() != pageCamera {
		t.Fatal("Close does not end the calibration")
	}

	// Each mark skipped: too few marks. Again goes back to the marks, with no new print.
	pr.State_ = homedIdle
	startTowers(t, a, pr, cv, false)
	pr.State_.Homed = "xyz"
	tapLabel(t, a, cv, "Go on")
	calibPosts(t, a)
	if c := pr.Calls(); !strings.Contains(c[len(c)-2], "G28 X Y") || c[len(c)-1] != "gcode G1 Z289.00 F1200; M400" {
		t.Errorf("moves %v", c[len(c)-2:])
	}
	for a.calib.phase == calibTap {
		tapLabel(t, a, cv, "Skip")
		calibPosts(t, a)
	}
	if a.calib.phase != calibFailed || !strings.Contains(a.calib.msg, "0 marks") {
		t.Fatalf("phase %v: %q", a.calib.phase, a.calib.msg)
	}
	tapLabel(t, a, cv, "Again")
	if a.calib.phase != calibPrinted {
		t.Fatalf("Again: phase %v", a.calib.phase)
	}
	tapLabel(t, a, cv, "Go on")
	calibPosts(t, a)
	if a.calib.phase != calibTap || a.calib.i != 0 || len(a.calib.places) != 0 {
		t.Fatalf("Again: phase %v, mark %d", a.calib.phase, a.calib.i)
	}

	// The wide page: the keys and the plan of the towers in the left column.
	a.Wide = true
	cv = gfx.New(a.Screen().W, a.Screen().H)
	f := simFrame(t, a, "camera-calibrate-tower-wide")
	if r, ok := a.KeyRect("Plan"); !ok || r.Dx() < 80 || !strings.Contains(f, "SKIP") {
		t.Errorf("no plan (%v) or keys on the wide page", r)
	}
	tapLabel(t, a, cv, "Cancel")
	if a.calib != nil {
		t.Error("Cancel does not end the calibration")
	}

	// A printer with the bed on Z: the setting is off, and the marks have one place of the bed.
	pr.Machine_.Kinematics = "corexy"
	a.Wide = false
	cv = gfx.New(W, BaseH)
	startTowersNoBed := func() {
		a.askCalibrate()
		if a.calib.bedY {
			t.Fatal("corexy: the bed moves in Y")
		}
	}
	startTowersNoBed()
	a.calib.kind, a.calib.towers = camera.KindTowers, nil
	a.startMarks(a.calib)
	calibPosts(t, a)
	if len(a.calib.points) != 10 {
		t.Errorf("corexy: %d marks", len(a.calib.points))
	}
}
