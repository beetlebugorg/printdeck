package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"path"
	"strings"
	"time"

	"printdeck/internal/camera"
	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/kit"
)

// The calibration of the camera (Camera page > Calibrate). The page offers two ways:
//
//	With a print   the deck prints 5 towers (printer.CalibTowers). After the print, the toolhead
//	               goes to the left side at the top, and the bed to the middle of the print area.
//	               The user taps the bottom and the top of each tower on a still picture. With a
//	               bed that moves in Y, the bed then goes a third of its travel to the front, and
//	               the user taps the top of each tower again (camera.TowerMarks).
//	With the nozzle the deck moves the nozzle to points over the bed (camera.CalibrationPoints),
//	               and the user taps the nozzle tip on a still picture at each point.
//
// The first tap zooms in around the tap, and the next taps (or a drag) place the mark exactly.
// Skip goes to the next mark, for a mark that the picture does not show. The deck then fits the
// projection from the places of the printer to the picture (camera.Fit). The views of the camera
// use it (camera/views.go). The setting "The bed moves front to back" is for the print: the fit is
// then in the frame of the printer (camera.Calibration.BedY). Its default is from the kinematics
// of printer.cfg. The nozzle stays at y 0 of that frame, so the nozzle way fits the toolhead
// places. The nozzle way does not change the heaters.

// calibPhase is the step of the calibration.
type calibPhase int

const (
	calibMoving   calibPhase = iota // the nozzle or the bed moves
	calibTap                        // the user taps the mark on the picture
	calibDone                       // the fit is good: Save or Discard
	calibFailed                     // the calibration has an error: Again or Close
	calibChoose                     // the two ways, and the setting of the bed
	calibPrinting                   // the towers print, or the print starts
	calibPrinted                    // the towers are on the bed: Go on
	calibRemove                     // the end of a calibration with towers: remove them
)

// calibFlow is a calibration that runs.
type calibFlow struct {
	cam    camera.Source // the camera of the calibration
	rotate int           // the turn of the camera at the start
	kind   string        // camera.KindTowers or camera.KindNozzle
	bedY   bool          // the bed moves in Y (the setting of the page)
	points [][3]float64  // the places of the plan (the toolhead coordinates)
	i      int           // the point now
	phase  calibPhase

	// The calibration with towers: the places of the towers, the marks of the plan (one for each
	// point), the toolhead Y of the bed for each place of the bed, and the picture at each.
	towers  [][2]float64
	marks   []camera.TowerMark
	beds    [2]float64
	bedAt   [2]float64
	shots   [2]*camera.Frame
	parkX   float64 // the X of the toolhead out of the view
	topZ    float64 // the Z of the toolhead out of the view
	started bool    // the print of the towers started

	frame   *camera.Frame // the still picture of the point
	place   [3]float64    // the place of the nozzle at the point
	tap     [2]float64    // the mark, in pixels of the frame
	tapped  bool
	zoom    *camera.Frame // the crop around the mark, or nil for the whole picture
	zoomBox [4]float64    // the crop: x0, y0, x1, y1 in pixels of the frame

	places [][3]float64  // the places of the points that have a mark, in the coordinates of the fit
	pix    [][2]float64  // the marks
	pixBed []int         // the place of the bed of each mark of a calibration with towers
	size   image.Point   // the size of the frames
	status camera.Status // the camera when it took the picture of the point now
	first  camera.Status // the camera at the first mark

	result *camera.Calibration
	msg    string
}

// calibZoom is the share of the width of the picture that the zoom shows.
const calibZoom = 4

// The times of the calibration. The tests make them short.
var (
	// calibSettle is the time after a move before the deck gets the picture: the camera and the
	// state of the printer are a moment late.
	calibSettle = 600 * time.Millisecond
	// calibFrameWait is the longest time that the deck waits for a new picture after a move.
	calibFrameWait = 5 * time.Second
)

// calibLine is the line of the Calibration row of the Camera page.
func calibLine(c *camera.Calibration, valid bool) string {
	switch {
	case c != nil && valid:
		way := "nozzle"
		if c.KindName() == camera.KindTowers {
			way = "print"
		}
		return fmt.Sprintf("Calibrated: %.1f px, %s, %s", c.Error, way, c.Time.Local().Format("Jan 2"))
	case c != nil:
		return "Not calibrated for this camera"
	}
	return "Not calibrated"
}

// calibReady returns why the deck cannot calibrate now, or "".
func (a *App) calibReady() string {
	st := a.Printer.State()
	cam, _ := a.shownCam()
	cs := cam.Status()
	switch {
	case cs.State == "off" || cs.State == "none" || cs.State == "error" || cs.State == "nosignal":
		return "Calibrate needs a camera with a picture"
	case !st.Connected || st.Err != "":
		return "Connect the printer first"
	case st.Busy() || st.Preparing || st.Finishing || st.Action != "":
		return waitText
	}
	return ""
}

// askCalibrate opens the calibration page with the two ways.
func (a *App) askCalibrate() {
	if why := a.calibReady(); why != "" {
		a.Toast(why)
		return
	}
	cam, in := a.shownCam()
	a.calib = &calibFlow{cam: cam, rotate: in.Rotate, phase: calibChoose, bedY: a.Printer.Machine().BedMovesY()}
	a.open(pageCalibrate)
}

// askNozzle asks to move the nozzle, and starts the calibration with the nozzle.
func (a *App) askNozzle() {
	fl := a.calib
	if fl == nil {
		return
	}
	if why := a.calibReady(); why != "" {
		a.Toast(why)
		return
	}
	n := len(camera.CalibrationPoints([3]float64{0, 0, 0}, [3]float64{200, 200, 200}))
	q := fmt.Sprintf("The nozzle moves to %d points over the bed, 10 mm and higher. Clear the bed. Then tap the nozzle tip on the picture at each point.", n)
	yes := "Start"
	home := a.Printer.State().Homed != "xyz"
	if home {
		q = "The printer homes first. " + q
		yes = "Home and start"
	}
	a.confirm = &confirmBox{question: q, yes: yes, onYes: func() { a.startCalibration(fl, home) }}
}

// askTowers asks to print the towers, with the temperatures of the loaded material.
func (a *App) askTowers() {
	fl := a.calib
	if fl == nil {
		return
	}
	if why := a.calibReady(); why != "" {
		a.Toast(why)
		return
	}
	m := a.loaded()
	name := m.Name
	if name == "" {
		name = "the loaded filament"
	}
	tw := printer.CalibTowers(a.Printer.Machine(), m.Tool, m.Bed, m.Name)
	q := fmt.Sprintf("Clear the bed. The deck prints 5 towers of 20 mm in %s at %d° and %d° on the bed: about %d minutes after the heating.",
		name, m.Tool, m.Bed, int(math.Ceil(tw.Seconds/60)))
	a.confirm = &confirmBox{question: q, yes: "Print", onYes: func() {
		fl.kind, fl.phase, fl.started = camera.KindTowers, calibPrinting, false
		go func() {
			if err := a.Printer.PrintCalibTowers(m.Tool, m.Bed, m.Name); err != nil {
				a.post(func() { a.calibFail(fl, "The towers did not print: "+err.Error()) })
			}
		}()
	}}
}

// followCalib follows the print of the towers. When it starts, the screen of the print shows it.
// When it ends, the calibration page opens again: Go on, or a message.
func (a *App) followCalib(st printer.State) {
	fl := a.calib
	if fl == nil || fl.phase != calibPrinting {
		return
	}
	busy := st.Reachable && st.Busy()
	if busy {
		fl.started = fl.started || path.Base(st.FilePath) == printer.TowersFile
		return
	}
	if !fl.started || st.Finishing {
		return
	}
	if st.Finished == "complete" && path.Base(st.FinishedPath) == printer.TowersFile {
		fl.phase = calibPrinted
	} else {
		fl.phase, fl.msg = calibFailed, "The print of the towers did not end. Calibrate again."
	}
	if a.topPage() != pageCalibrate {
		a.pages = []page{pageMachine, pageSettings, pageCamera, pageCalibrate}
	}
	a.Redraw()
}

// startCalibration starts the calibration with the nozzle: it homes when home is true, and moves
// to the first point.
func (a *App) startCalibration(fl *calibFlow, home bool) {
	fl.kind, fl.phase, fl.bedY = camera.KindNozzle, calibMoving, false
	go func() {
		if home {
			if err := a.Printer.Home(); err != nil {
				a.post(func() { a.calibFail(fl, "The printer did not home: "+err.Error()) })
				return
			}
		}
		st := a.Printer.State()
		lo, hi := st.Axis[0], st.Axis[1]
		if hi[0] <= lo[0] || hi[1] <= lo[1] || hi[2] < lo[2]+20 {
			a.post(func() { a.calibFail(fl, "The deck does not know the limits of the axes.") })
			return
		}
		pts := camera.CalibrationPoints(lo, hi)
		a.post(func() {
			if a.calib != fl {
				return
			}
			fl.points = pts
			a.calibMove(fl)
		})
	}()
}

// startMarks starts the marks of the towers: the plan of the marks, and the move of the bed to
// the first place.
func (a *App) startMarks(fl *calibFlow) {
	st := a.Printer.State()
	lo, hi := st.Axis[0], st.Axis[1]
	if hi[0] <= lo[0] || hi[1] <= lo[1] || hi[2] < lo[2]+20 {
		a.calibFail(fl, "The deck does not know the limits of the axes.")
		return
	}
	m := a.Printer.Machine()
	alo, ahi := camera.PrintArea([2]float64{lo[0], lo[1]}, [2]float64{hi[0], hi[1]})
	if m.MaxX > m.MinX && m.MaxY > m.MinY {
		// The places of the G-code of the towers.
		alo, ahi = camera.PrintArea([2]float64{m.MinX, m.MinY}, [2]float64{m.MaxX, m.MaxY})
	}
	fl.towers = camera.TowerPlaces(alo, ahi)
	fl.marks = camera.TowerMarks(fl.bedY)
	fl.points = fl.points[:0]
	for _, mk := range fl.marks {
		fl.points = append(fl.points, mk.Place(fl.towers))
	}
	fl.beds = camera.TowerBedPlaces(lo[1], hi[1], alo[1], ahi[1])
	fl.parkX = lo[0] + 5
	if m.HasParkX {
		fl.parkX = max(lo[0], min(hi[0], m.ParkX))
	}
	fl.topZ = hi[2] - 10
	fl.i, fl.places, fl.pix, fl.pixBed, fl.shots = 0, nil, nil, nil, [2]*camera.Frame{}
	a.calibBed(fl, 0)
}

// calibBed moves the toolhead out of the view and the bed to the place bed, and then shows the
// picture. Before the first place, the printer homes: the end of a print can turn the motors off,
// and the state of the deck can be a moment late. With bed tilt or a bed mesh, a move in X and Y
// also moves Z, so Z must be homed too. A printer with the Z endstop at the top (a TAZ) homes all
// the axes: the nozzle goes up, away from the towers. Another printer homes X and Y only, because
// a probe at the middle of the bed can hit the middle tower. The towers G-code ends with the
// nozzle 30 mm over the towers, so the homing of X and Y is clear of them. Then the toolhead goes
// to the top. Klipper refuses that move when Z is not homed, and the toolhead stays at its height.
func (a *App) calibBed(fl *calibFlow, bed int) {
	fl.phase, fl.frame, fl.zoom, fl.tapped = calibMoving, nil, nil, false
	y := fl.beds[bed]
	zMax := a.Printer.Machine().ZHomesMax
	go func() {
		var lines []string
		switch {
		case bed == 0 && zMax:
			lines = append(lines, "G28")
		case bed == 0:
			lines = append(lines, "G28 X Y")
		}
		lines = append(lines, "G90", fmt.Sprintf("G1 X%.2f Y%.2f F3000", fl.parkX, y), "M400")
		if err := a.Printer.Gcode(lines...); err != nil {
			msg := "The bed did not move: " + err.Error()
			if !zMax {
				msg += " Home Z with the nozzle clear of the towers, then tap Again."
			}
			a.post(func() { a.calibFail(fl, msg) })
			return
		}
		if bed == 0 {
			if st := a.Printer.State(); !st.HasZ || st.Z < fl.topZ {
				a.Printer.Gcode(fmt.Sprintf("G1 Z%.2f F1200", fl.topZ), "M400")
			}
		}
		a.calibShot(fl, func(fr *camera.Frame, cs camera.Status, st printer.State) {
			at := y
			if math.Abs(st.Y-y) < 1 {
				at = st.Y
			}
			fl.bedAt[bed], fl.shots[bed] = at, fr
			fl.phase, fl.frame, fl.status = calibTap, fr, cs
			fl.place = fl.points[fl.i]
		})
	}()
}

// calibMove moves the nozzle to the point now, and then shows its picture.
func (a *App) calibMove(fl *calibFlow) {
	fl.phase, fl.frame, fl.zoom, fl.tapped = calibMoving, nil, nil, false
	target := fl.points[fl.i]
	go func() {
		st := a.Printer.State()
		safe := target[2]
		if st.HasZ {
			safe = max(safe, st.Z)
		}
		err := a.Printer.Gcode("G90", fmt.Sprintf("G1 Z%.2f F1200", safe),
			fmt.Sprintf("G1 X%.2f Y%.2f F3000", target[0], target[1]),
			fmt.Sprintf("G1 Z%.2f F1200", target[2]), "M400")
		if err != nil {
			a.post(func() { a.calibFail(fl, "The nozzle did not move: "+err.Error()) })
			return
		}
		a.calibShot(fl, func(fr *camera.Frame, cs camera.Status, st printer.State) {
			// The place of the toolhead, when the state has it near the target: the state has the
			// G-code offsets and the bed tilt.
			place := target
			if st.HasZ && math.Abs(st.X-target[0]) < 1 && math.Abs(st.Y-target[1]) < 1 && math.Abs(st.Z-target[2]) < 2 {
				place = [3]float64{st.X, st.Y, st.Z}
			}
			fl.phase, fl.frame, fl.place, fl.status = calibTap, fr, place, cs
		})
	}()
}

// calibShot waits for a picture of the camera after a move, and then calls done in the UI loop
// with the picture, the camera, and the state of the printer. It ends the calibration when the
// camera sends no picture. It runs in the goroutine of the move.
func (a *App) calibShot(fl *calibFlow, done func(fr *camera.Frame, cs camera.Status, st printer.State)) {
	moved := time.Now()
	var before uint64
	if f := fl.cam.Peek(); f != nil {
		before = f.Seq
	}
	time.Sleep(calibSettle)
	// A picture of the camera after the move.
	var fr *camera.Frame
	for end := time.Now().Add(calibFrameWait); ; {
		f := fl.cam.Peek()
		if f != nil && (f.Seq > before && f.Time.After(moved) || time.Now().After(end)) {
			fr = f
			break
		}
		if time.Now().After(end) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	cs := fl.cam.Status()
	st := a.Printer.State()
	a.post(func() {
		if a.calib != fl {
			return
		}
		if fr == nil {
			a.calibFail(fl, "The camera sends no picture.")
			return
		}
		done(fr, cs, st)
		a.Redraw()
	})
}

// calibFail ends the calibration with a message.
func (a *App) calibFail(fl *calibFlow, msg string) {
	if a.calib != fl {
		return
	}
	fl.phase, fl.msg = calibFailed, msg
	a.Redraw()
}

// calibNext keeps the mark of the point now (when keep is true), and goes to the next point, or
// fits the projection after the last point. A mark of a tower at the same place of the bed uses
// the same picture.
func (a *App) calibNext(keep bool) {
	fl := a.calib
	if fl == nil || fl.phase != calibTap {
		return
	}
	if keep && fl.tapped {
		sz := fl.frame.Size()
		if len(fl.places) > 0 && (sz != fl.size || fl.status.Device.ID != fl.first.Device.ID) {
			a.calibFail(fl, "The camera or the size of the picture changed. Calibrate again.")
			return
		}
		if len(fl.places) == 0 {
			fl.first = fl.status
		}
		fl.size = sz
		fl.places = append(fl.places, fl.fitPlace(fl.i))
		fl.pix = append(fl.pix, fl.tap)
		if fl.kind == camera.KindTowers {
			fl.pixBed = append(fl.pixBed, fl.marks[fl.i].Bed)
		}
	}
	fl.i++
	switch {
	case fl.i >= len(fl.points):
		a.calibFit(fl)
	case fl.kind != camera.KindTowers:
		a.calibMove(fl)
	case fl.marks[fl.i].Bed != fl.marks[fl.i-1].Bed:
		a.calibBed(fl, fl.marks[fl.i].Bed)
	default:
		fl.place, fl.zoom, fl.tapped = fl.points[fl.i], nil, false
		a.Redraw()
	}
}

// fitPlace is the place of the point i in the coordinates of the fit: the toolhead coordinates,
// or with a bed in Y, the frame of the printer with the bed at its place for the mark.
func (fl *calibFlow) fitPlace(i int) [3]float64 {
	if fl.kind != camera.KindTowers {
		return fl.place
	}
	c := camera.Calibration{BedY: fl.bedY}
	return c.Frame(fl.points[i], fl.bedAt[fl.marks[i].Bed])
}

// calibFit fits the projection to the marks.
func (a *App) calibFit(fl *calibFlow) {
	what := "points"
	tip := "the nozzle tip at each point"
	if fl.kind == camera.KindTowers {
		what, tip = "marks", "the bottom and the top of each tower"
	}
	if len(fl.places) < camera.MinCalibrationPoints {
		a.calibFail(fl, fmt.Sprintf("%d %s have a mark. The calibration needs %d or more.", len(fl.places), what, camera.MinCalibrationPoints))
		return
	}
	m, rms, err := camera.Fit(fl.places, fl.pix, fl.size.X, fl.size.Y)
	if err != nil {
		a.calibFail(fl, "The marks do not fit a camera. Calibrate again, and tap "+tip+".")
		return
	}
	if rms > camera.MaxCalibrationError {
		a.calibFail(fl, fmt.Sprintf("The error is %.1f px: it must be %.0f px or less. Calibrate again, and tap %s exactly.", rms, camera.MaxCalibrationError, tip))
		return
	}
	st := fl.first
	fl.result = &camera.Calibration{Device: st.Device.ID, InW: st.Format.InW, InH: st.Format.InH,
		Width: fl.size.X, Height: fl.size.Y, Rotate: fl.rotate, Kind: fl.kind, BedY: fl.bedY, P: m, Error: rms,
		Points: len(fl.places), Time: a.Now()}
	fl.phase = calibDone
	a.Redraw()
}

// cancelCalibration ends the calibration. The nozzle stays where it is.
func (a *App) cancelCalibration() {
	a.calib = nil
	if a.topPage() == pageCalibrate {
		a.back()
	}
}

// saveCalibration keeps the calibration. A calibration with towers then asks to remove them, and
// the other goes back to the Camera page.
func (a *App) saveCalibration() {
	fl := a.calib
	if fl != nil && fl.result != nil {
		fl.cam.SetCalibration(fl.result)
		a.Toast(fmt.Sprintf("Camera calibrated: %.1f px", fl.result.Error))
	}
	a.endCalibration()
}

// endCalibration ends the calibration with no save: the end of a calibration with towers asks to
// remove them.
func (a *App) endCalibration() {
	if fl := a.calib; fl != nil && fl.kind == camera.KindTowers && len(fl.towers) > 0 {
		fl.phase = calibRemove
		a.Redraw()
		return
	}
	a.cancelCalibration()
}

// calibAgain starts the calibration again after an error: the marks again with the towers on the
// bed, or else the page of the two ways.
func (a *App) calibAgain() {
	fl := a.calib
	if fl != nil && fl.kind == camera.KindTowers && len(fl.towers) > 0 {
		fl.phase, fl.msg, fl.result = calibPrinted, "", nil
		a.Redraw()
		return
	}
	a.calib = nil
	a.back()
	a.askCalibrate()
}

// askClearCalibration asks to remove the calibration.
func (a *App) askClearCalibration() {
	a.confirm = &confirmBox{question: "Clear the calibration of the camera? The views show the whole picture until you calibrate again.",
		yes: "Clear", danger: true, onYes: func() {
			cam, _ := a.shownCam()
			cam.SetCalibration(nil)
			a.camView = camera.ViewFull
			a.Toast("Calibration cleared")
		}}
}

// calibTapAt places the mark at the point p of the screen on the picture r.
func (a *App) calibTapAt(r image.Rectangle, p image.Point, phase kit.Phase) {
	fl := a.calib
	if fl == nil || fl.phase != calibTap || fl.frame == nil || r.Dx() <= 0 || r.Dy() <= 0 {
		return
	}
	sz := fl.frame.Size()
	box := [4]float64{0, 0, float64(sz.X), float64(sz.Y)}
	if fl.zoom != nil {
		box = fl.zoomBox
	}
	fx := float64(p.X-r.Min.X) / float64(r.Dx())
	fy := float64(p.Y-r.Min.Y) / float64(r.Dy())
	fx, fy = max(0, min(1, fx)), max(0, min(1, fy))
	fl.tap = [2]float64{box[0] + fx*(box[2]-box[0]), box[1] + fy*(box[3]-box[1])}
	fl.tapped = true
	if fl.zoom == nil && phase == kit.PadUp {
		// The first tap zooms in around the mark.
		w := r.Dx()
		if a.Scale[1] > 0 {
			w = w * a.Scale[0] / a.Scale[1]
		}
		fl.zoom, fl.zoomBox = fl.frame.Crop(fl.tap[0], fl.tap[1], float64(sz.X)/calibZoom, w)
	}
	a.Redraw()
}

// calibFitInk is the color of the projected towers over the picture of the result.
var calibFitInk = color.RGBA{70, 220, 110, 255}

// calibLines are the lines of the step: the first line, and the lines under it.
func (a *App) calibLines(fl *calibFlow) []string {
	towers := fl.kind == camera.KindTowers
	switch fl.phase {
	case calibChoose:
		return []string{"Choose a way", "A print is best when the nozzle tip is hard to see."}
	case calibPrinting:
		return []string{"Printing the towers", "Leave them on the bed. This page opens again after the print."}
	case calibPrinted:
		return []string{"The towers are printed", "Leave them on the bed. The printer homes. The toolhead moves to the left side at the top, and the bed moves."}
	case calibRemove:
		return []string{"Remove the towers", "Take the 5 towers off the bed."}
	case calibMoving:
		switch {
		case len(fl.points) == 0:
			return []string{"Starting…"}
		case towers:
			return []string{fmt.Sprintf("Bed place %d of %d", fl.marks[fl.i].Bed+1, fl.marks[len(fl.marks)-1].Bed+1), "The toolhead and the bed move…"}
		}
		return []string{fmt.Sprintf("Point %d of %d", fl.i+1, len(fl.points)), "The nozzle moves…"}
	case calibTap:
		var first, what string
		if towers {
			mk := fl.marks[fl.i]
			end, where := "bottom", "the middle of the foot of the tower"
			if mk.Top {
				end, where = "top", "the middle of the top of the tower"
			}
			first = fmt.Sprintf("Mark %d of %d: tower %d, %s", fl.i+1, len(fl.points), mk.Tower+1, end)
			what = where
		} else {
			p := fl.place
			first = fmt.Sprintf("Point %d of %d: X%.0f Y%.0f Z%.0f", fl.i+1, len(fl.points), p[0], p[1], p[2])
			what = "the nozzle tip"
		}
		switch {
		case !fl.tapped:
			return []string{first, "Tap " + what + " on the picture."}
		case fl.zoom != nil:
			return []string{first, "Tap or drag to put the mark on " + what + "."}
		}
		return []string{first, "Tap the same place again to zoom in."}
	case calibDone:
		what := "points"
		if towers {
			what = "marks"
		}
		l := []string{fmt.Sprintf("Error %.1f px on %d %s", fl.result.Error, fl.result.Points, what), "Save the calibration?"}
		if towers {
			l[1] = "The green lines show the fit. Save the calibration?"
		}
		return l
	case calibFailed:
		return []string{"Not calibrated", fl.msg}
	}
	return nil
}

// drawCalibrate is the calibration page.
func (a *App) drawCalibrate(c *gfx.Canvas) {
	fl := a.calib
	f := a.pageBack(c, "Calibrate camera", "Cancel", a.cancelCalibration)
	if fl == nil {
		return
	}
	var pic, info, keys image.Rectangle
	if f.wide {
		r := kit.Rows(f.body(), 8, kit.Fr(1), kit.Px(56))
		pic, info = r[0], r[1]
		keys = f.aside()
	} else {
		body := f.body()
		h := body.Dx() * 9 / 16
		if fl.frame != nil && fl.frame.Size().X > 0 {
			h = body.Dx() * fl.frame.Size().Y / fl.frame.Size().X
		}
		r := kit.Rows(kit.Inset(body, 0, 10, 0, 0), 12, kit.Px(h), kit.Px(110), kit.Fr(1))
		pic, info, keys = r[0], r[1], r[2]
	}
	plan := fl.kind == camera.KindTowers && len(fl.marks) > 0 && (fl.phase == calibTap || fl.phase == calibMoving)

	// The step, in the window under the picture. A tall page has the plan of the towers at the
	// right of the window.
	window(c, info)
	a.region("Step", info)
	in := kit.Inset(info, 14, 6, 14, 6)
	if plan && !f.wide {
		side := info.Dy() - 12
		a.drawTowerPlan(c, image.Rect(info.Max.X-6-side, info.Min.Y+6, info.Max.X-6, info.Max.Y-6), fl)
		in.Max.X = info.Max.X - side - 18
	}
	face := gfx.Face(true, 14)
	var all []string
	for i, l := range a.calibLines(fl) {
		l = strings.ToUpper(l)
		if i == 0 {
			all = append(all, gfx.Fit(face, l, in.Dx()))
			continue
		}
		all = append(all, wrapText(face, l, calibWrap(in.Dx()))...)
	}
	lineH := min(26, in.Dy()/max(1, len(all)))
	for i, l := range all {
		col := lcdInk
		if i > 0 {
			col = lcdDim
		}
		c.Text(image.Rect(in.Min.X, in.Min.Y+i*lineH, in.Max.X, in.Min.Y+(i+1)*lineH), l, face, col, gfx.Left)
	}

	// The picture: the two ways, the still picture of the point or its zoom with the mark, or the
	// picture of the result with the fit.
	if fl.phase == calibChoose {
		a.drawCalibChoose(c, pic, fl)
	} else {
		a.drawCalibPicture(c, pic, fl)
	}

	// The keys.
	var ks []func(image.Rectangle)
	switch fl.phase {
	case calibChoose:
		ks = append(ks, func(r image.Rectangle) { a.button(c, r, "With a print", green, onAcc, a.askTowers) })
		ks = append(ks, func(r image.Rectangle) { a.button(c, r, "With the nozzle", raised, text, a.askNozzle) })
	case calibPrinted:
		ks = append(ks, func(r image.Rectangle) { a.button(c, r, "Go on", green, onAcc, func() { a.startMarks(fl) }) })
	case calibRemove:
		ks = append(ks, func(r image.Rectangle) { a.button(c, r, "Done", green, onAcc, a.cancelCalibration) })
	case calibTap:
		use := "Use point"
		if fl.kind == camera.KindTowers {
			use = "Use mark"
		}
		ks = append(ks, func(r image.Rectangle) {
			if fl.tapped {
				a.button(c, r, use, green, onAcc, func() { a.calibNext(true) })
			} else {
				key(c, r, use, raised, dim, false)
			}
		})
		ks = append(ks, func(r image.Rectangle) { a.button(c, r, "Skip", raised, text, func() { a.calibNext(false) }) })
		if fl.zoom != nil {
			ks = append(ks, func(r image.Rectangle) {
				a.button(c, r, "Whole picture", raised, text, func() { fl.zoom = nil })
			})
		}
	case calibDone:
		ks = append(ks, func(r image.Rectangle) { a.button(c, r, "Save", green, onAcc, a.saveCalibration) })
		ks = append(ks, func(r image.Rectangle) { a.button(c, r, "Discard", raised, text, a.endCalibration) })
	case calibFailed:
		ks = append(ks, func(r image.Rectangle) { a.button(c, r, "Again", raised, text, a.calibAgain) })
		ks = append(ks, func(r image.Rectangle) { a.button(c, r, "Close", raised, text, a.endCalibration) })
	}
	if f.wide {
		sizes := make([]kit.Size, 4)
		for i := range sizes {
			sizes[i] = kit.Px(64)
		}
		rows := kit.Rows(keys, 12, sizes...)
		for i, r := range rows {
			if i < len(ks) {
				ks[i](r)
			}
		}
		// The plan of the towers under the keys.
		if plan {
			below := image.Rect(keys.Min.X, rows[len(ks)-1].Max.Y+12, keys.Max.X, keys.Max.Y)
			if side := min(below.Dx(), below.Dy()); side >= 80 {
				a.drawTowerPlan(c, kit.Center(below, side, side), fl)
			}
		}
		return
	}
	if len(ks) == 0 {
		return
	}
	row := image.Rect(keys.Min.X, keys.Min.Y, keys.Max.X, keys.Min.Y+72)
	sizes := make([]kit.Size, len(ks))
	for i := range sizes {
		sizes[i] = kit.Fr(1)
	}
	for i, r := range kit.Cols(row, 12, sizes...) {
		ks[i](r)
	}
}

// calibWrap is the width for the lines of a text in an area of width w. The text of a picture at
// a larger size (gfx.ReplayScaled, for the manual) is a little wider, so the lines leave space.
func calibWrap(w int) int { return w - w/16 }

// drawCalibChoose draws the two ways in the window of the picture, and the row of the setting of
// the bed under them.
func (a *App) drawCalibChoose(c *gfx.Canvas, pic image.Rectangle, fl *calibFlow) {
	window(c, pic)
	a.region("Ways", pic)
	r := kit.Rows(pic.Inset(8), 8, kit.Fr(1), kit.Px(64))
	in := kit.Inset(r[0], 8, 4, 8, 0)
	n := len(camera.CalibrationPoints([3]float64{0, 0, 0}, [3]float64{200, 200, 200}))
	paras := []string{
		"With a print: the deck prints 5 towers. Then you tap the bottom and the top of each tower on a picture.",
		fmt.Sprintf("With the nozzle: you tap the nozzle tip at %d points.", n),
	}
	face := gfx.Face(true, 14)
	var lines []string
	for i, p := range paras {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, wrapText(face, strings.ToUpper(p), calibWrap(in.Dx()))...)
	}
	lineH := min(22, in.Dy()/max(1, len(lines)))
	for i, l := range lines {
		c.Text(image.Rect(in.Min.X, in.Min.Y+i*lineH, in.Max.X, in.Min.Y+(i+1)*lineH), l, face, lcdDim, gfx.Left)
	}
	sub := "No: the bed stays, the toolhead moves in Y"
	if fl.bedY {
		sub = "Yes: the bed moves in Y, as on a TAZ"
	}
	a.menuRow(c, r[1], "The bed moves front to back", sub, dim, func() {
		fl.bedY = !fl.bedY
		a.Redraw()
	})
}

// drawCalibPicture draws the window of the picture: the still picture of the point, or its zoom,
// with the mark; the first picture of a calibration with towers, with the projected towers and the
// marks; or a word for the step.
func (a *App) drawCalibPicture(c *gfx.Canvas, pic image.Rectangle, fl *calibFlow) {
	window(c, pic)
	a.region("Picture", pic)
	area := pic.Inset(4)
	show, box := fl.frame, [4]float64{}
	bed := -1 // the place of the bed of the picture of the result
	if fl.phase == calibDone && fl.kind == camera.KindTowers {
		// The picture of the first place of the bed: it has all the towers.
		show = nil
		for b := range fl.shots {
			if show == nil && fl.shots[b] != nil {
				show, bed = fl.shots[b], b
			}
		}
	} else if fl.phase != calibTap {
		show = nil
	}
	if show != nil {
		sz := show.Size()
		box = [4]float64{0, 0, float64(sz.X), float64(sz.Y)}
		if fl.zoom != nil && bed < 0 {
			show, box = fl.zoom, fl.zoomBox
		}
	}
	if show == nil {
		msg := "…"
		switch fl.phase {
		case calibDone:
			msg = "Calibrated"
		case calibFailed:
			msg = "Not calibrated"
		case calibPrinting:
			msg = "Printing"
		case calibPrinted:
			msg = "Towers on the bed"
		case calibRemove:
			msg = "Remove the towers"
		}
		c.Text(area, strings.ToUpper(msg), gfx.Face(true, 18), lcdDim, gfx.Center)
		return
	}
	sz := show.Size()
	k := min(float64(area.Dx())/float64(sz.X), float64(area.Dy())/float64(sz.Y))
	r := kit.Center(area, int(float64(sz.X)*k), int(float64(sz.Y)*k))
	w := r.Dx()
	if a.Scale[1] > 0 {
		w = w * a.Scale[0] / a.Scale[1]
	}
	if fl.zoom != nil && bed < 0 {
		c.Cover(r, show)
	} else {
		c.Cover(r, show.Small(w))
	}
	at := func(u, v float64) (int, int) {
		return r.Min.X + int((u-box[0])/(box[2]-box[0])*float64(r.Dx())+0.5),
			r.Min.Y + int((v-box[1])/(box[3]-box[1])*float64(r.Dy())+0.5)
	}
	if bed >= 0 {
		a.drawTowerFit(c, r, fl, bed, at)
		return
	}
	if fl.tapped {
		x, y := at(fl.tap[0], fl.tap[1])
		drawMark(c, x, y, r)
	}
	a.addHit(kit.Hit{R: r, Pad: func(p image.Point, ph kit.Phase) { a.calibTapAt(r, p, ph) }})
}

// drawTowerFit draws the towers of the result over the picture r of the place bed of the bed:
// the edges of each projected tower in green, and the marks of that picture in orange. at is the
// place on the screen of a pixel of the frame.
func (a *App) drawTowerFit(c *gfx.Canvas, r image.Rectangle, fl *calibFlow, bed int, at func(u, v float64) (int, int)) {
	res := fl.result
	sz := fl.shots[bed].Size()
	h := camera.TowerSide / 2
	for _, p := range fl.towers {
		var q [8][2]int
		ok := true
		for i := range 8 {
			pt := [3]float64{p[0] - h, p[1] - h, 0}
			if i&1 != 0 {
				pt[0] = p[0] + h
			}
			if i&2 != 0 {
				pt[1] = p[1] + h
			}
			if i&4 != 0 {
				pt[2] = camera.TowerTop
			}
			u, v, in := res.ProjectAt(pt, fl.bedAt[bed], sz.X, sz.Y, fl.rotate)
			if !in {
				ok = false
				break
			}
			q[i][0], q[i][1] = at(u, v)
		}
		if !ok {
			continue
		}
		for _, e := range [][2]int{{0, 1}, {1, 3}, {3, 2}, {2, 0}, {4, 5}, {5, 7}, {7, 6}, {6, 4}, {0, 4}, {1, 5}, {2, 6}, {3, 7}} {
			drawSeg(c, q[e[0]], q[e[1]], calibFitInk, r)
		}
	}
	for i, m := range fl.pixBed {
		if m == bed {
			x, y := at(fl.pix[i][0], fl.pix[i][1])
			c.Fill(image.Rect(x-2, y-2, x+3, y+3).Intersect(r), orange)
		}
	}
}

// drawSeg draws a line of 2 pixels from a to b, inside r.
func drawSeg(c *gfx.Canvas, a, b [2]int, col color.RGBA, r image.Rectangle) {
	n := max(abs(b[0]-a[0]), abs(b[1]-a[1]), 1)
	for i := 0; i <= n; i += 2 {
		x := a[0] + (b[0]-a[0])*i/n
		y := a[1] + (b[1]-a[1])*i/n
		c.Fill(image.Rect(x-1, y-1, x+2, y+2).Intersect(r), col)
	}
}

// drawTowerPlan draws the plan of the towers in the square r: the bed seen from the top, with the
// largest Y at the top, and a square for each tower. The tower of the mark now is orange.
func (a *App) drawTowerPlan(c *gfx.Canvas, r image.Rectangle, fl *calibFlow) {
	a.region("Plan", r)
	plate(c, r)
	bed := r.Inset(6)
	lo, hi := fl.towers[0], fl.towers[0]
	for _, p := range fl.towers {
		lo = [2]float64{min(lo[0], p[0]), min(lo[1], p[1])}
		hi = [2]float64{max(hi[0], p[0]), max(hi[1], p[1])}
	}
	sq := max(10, bed.Dx()/6)
	inner := bed.Inset(sq/2 + 2)
	now := -1
	if fl.i < len(fl.marks) {
		now = fl.marks[fl.i].Tower
	}
	face := gfx.Face(true, max(10, sq-4))
	for i, p := range fl.towers {
		fx := (p[0] - lo[0]) / max(1e-9, hi[0]-lo[0])
		fy := (p[1] - lo[1]) / max(1e-9, hi[1]-lo[1])
		x := inner.Min.X + int(fx*float64(inner.Dx())+0.5)
		y := inner.Max.Y - int(fy*float64(inner.Dy())+0.5)
		t := image.Rect(x-sq/2, y-sq/2, x-sq/2+sq, y-sq/2+sq)
		fill, ink := raised, dim
		if i == now {
			fill, ink = orange, onAcc
		}
		c.Fill(t, fill)
		c.Text(t, fmt.Sprint(i+1), face, ink, gfx.Center)
	}
}

// drawMark draws the mark of a tap at (x, y) in r: a cross with a gap in the middle, so the tip
// under it stays in view.
func drawMark(c *gfx.Canvas, x, y int, r image.Rectangle) {
	const arm, gap, th = 14, 5, 2
	for _, s := range []image.Rectangle{
		image.Rect(x-arm-gap, y-th/2, x-gap, y+th/2+th%2),
		image.Rect(x+gap, y-th/2, x+gap+arm, y+th/2+th%2),
		image.Rect(x-th/2, y-arm-gap, x+th/2+th%2, y-gap),
		image.Rect(x-th/2, y+gap, x+th/2+th%2, y+gap+arm),
	} {
		c.Fill(s.Inset(-1).Intersect(r), bg)
		c.Fill(s.Intersect(r), orange)
	}
}
