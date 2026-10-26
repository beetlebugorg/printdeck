package ui

import (
	"fmt"
	"image"
	"math"
	"strings"
	"time"

	"printdeck/internal/camera"
	"printdeck/internal/gfx"
	"printdeck/kit"
)

// The calibration of the camera (Camera page > Calibrate): the deck moves the nozzle to points
// over the bed (camera.CalibrationPoints), one at a time. At each point the page shows a still
// picture, and the user taps the nozzle tip on it. The first tap zooms in around the tap, and the
// next taps (or a drag) place the mark exactly. The deck then fits the projection from the places
// of the printer to the picture (camera.Fit). The views of the camera use it (camera/views.go).
// The heaters stay as they are.

// calibPhase is the step of the calibration.
type calibPhase int

const (
	calibMoving calibPhase = iota // the nozzle moves to the point
	calibTap                      // the user taps the nozzle tip on the picture
	calibDone                     // the fit is good: Save or Discard
	calibFailed                   // the calibration has an error: Again or Close
)

// calibFlow is a calibration that runs.
type calibFlow struct {
	points [][3]float64 // the places of the plan
	i      int          // the point now
	phase  calibPhase

	frame   *camera.Frame // the still picture of the point
	place   [3]float64    // the place of the nozzle at the point
	tap     [2]float64    // the mark, in pixels of the frame
	tapped  bool
	zoom    *camera.Frame // the crop around the mark, or nil for the whole picture
	zoomBox [4]float64    // the crop: x0, y0, x1, y1 in pixels of the frame

	places [][3]float64  // the places of the points that have a mark
	pix    [][2]float64  // the marks
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
		return fmt.Sprintf("Calibrated: %.1f px, %s", c.Error, c.Time.Local().Format("Jan 2"))
	case c != nil:
		return "Not calibrated for this camera"
	}
	return "Not calibrated"
}

// calibReady returns why the deck cannot calibrate now, or "".
func (a *App) calibReady() string {
	st := a.Printer.State()
	cs := a.Camera.Status()
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

// askCalibrate asks to move the nozzle, and starts the calibration.
func (a *App) askCalibrate() {
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
	a.confirm = &confirmBox{question: q, yes: yes, onYes: func() { a.startCalibration(home) }}
}

// startCalibration opens the calibration page, homes when home is true, and moves to the first
// point.
func (a *App) startCalibration(home bool) {
	fl := &calibFlow{}
	a.calib = fl
	a.open(pageCalibrate)
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
		moved := time.Now()
		var before uint64
		if f := a.Camera.Peek(); f != nil {
			before = f.Seq
		}
		time.Sleep(calibSettle)
		// A picture of the camera after the move.
		var fr *camera.Frame
		for end := time.Now().Add(calibFrameWait); ; {
			f := a.Camera.Peek()
			if f != nil && (f.Seq > before && f.Time.After(moved) || time.Now().After(end)) {
				fr = f
				break
			}
			if time.Now().After(end) {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		cs := a.Camera.Status()
		// The place of the toolhead, when the state has it near the target: the state has the
		// G-code offsets and the bed tilt.
		place := target
		st = a.Printer.State()
		if st.HasZ && math.Abs(st.X-target[0]) < 1 && math.Abs(st.Y-target[1]) < 1 && math.Abs(st.Z-target[2]) < 2 {
			place = [3]float64{st.X, st.Y, st.Z}
		}
		a.post(func() {
			if a.calib != fl {
				return
			}
			if fr == nil {
				a.calibFail(fl, "The camera sends no picture.")
				return
			}
			fl.phase, fl.frame, fl.place, fl.status = calibTap, fr, place, cs
			a.Redraw()
		})
	}()
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
// fits the projection after the last point.
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
		fl.places = append(fl.places, fl.place)
		fl.pix = append(fl.pix, fl.tap)
	}
	fl.i++
	if fl.i < len(fl.points) {
		a.calibMove(fl)
		return
	}
	a.calibFit(fl)
}

// calibFit fits the projection to the marks.
func (a *App) calibFit(fl *calibFlow) {
	if len(fl.places) < camera.MinCalibrationPoints {
		a.calibFail(fl, fmt.Sprintf("%d points have a mark. The calibration needs %d or more.", len(fl.places), camera.MinCalibrationPoints))
		return
	}
	m, rms, err := camera.Fit(fl.places, fl.pix, fl.size.X, fl.size.Y)
	if err != nil {
		a.calibFail(fl, "The marks do not fit a camera. Calibrate again, and tap the nozzle tip at each point.")
		return
	}
	if rms > camera.MaxCalibrationError {
		a.calibFail(fl, fmt.Sprintf("The error is %.1f px: it must be %.0f px or less. Calibrate again, and tap the nozzle tip exactly.", rms, camera.MaxCalibrationError))
		return
	}
	st := fl.first
	fl.result = &camera.Calibration{Device: st.Device.ID, InW: st.Format.InW, InH: st.Format.InH,
		Width: fl.size.X, Height: fl.size.Y, Rotate: a.Camera.Settings().Rotate, P: m, Error: rms,
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

// saveCalibration keeps the calibration, and goes back to the Camera page.
func (a *App) saveCalibration() {
	if fl := a.calib; fl != nil && fl.result != nil {
		a.Camera.SetCalibration(fl.result)
		a.Toast(fmt.Sprintf("Camera calibrated: %.1f px", fl.result.Error))
	}
	a.cancelCalibration()
}

// askClearCalibration asks to remove the calibration.
func (a *App) askClearCalibration() {
	a.confirm = &confirmBox{question: "Clear the calibration of the camera? The views show the whole picture until you calibrate again.",
		yes: "Clear", danger: true, onYes: func() {
			a.Camera.SetCalibration(nil)
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

	// The step, in the window under the picture.
	window(c, info)
	a.region("Step", info)
	var lines []string
	switch fl.phase {
	case calibMoving:
		if len(fl.points) == 0 {
			lines = []string{"Starting…"}
		} else {
			lines = []string{fmt.Sprintf("Point %d of %d", fl.i+1, len(fl.points)), "The nozzle moves…"}
		}
	case calibTap:
		p := fl.place
		lines = []string{fmt.Sprintf("Point %d of %d: X%.0f Y%.0f Z%.0f", fl.i+1, len(fl.points), p[0], p[1], p[2])}
		switch {
		case !fl.tapped:
			lines = append(lines, "Tap the nozzle tip on the picture.")
		case fl.zoom != nil:
			lines = append(lines, "Tap or drag to put the mark on the tip.")
		default:
			lines = append(lines, "Tap the tip again to zoom in.")
		}
	case calibDone:
		lines = []string{fmt.Sprintf("Error %.1f px on %d points", fl.result.Error, fl.result.Points), "Save the calibration?"}
	case calibFailed:
		lines = []string{"Not calibrated", fl.msg}
	}
	in := kit.Inset(info, 14, 6, 14, 6)
	face := gfx.Face(true, 14)
	var all []string
	for i, l := range lines {
		if i == 0 {
			all = append(all, l)
			continue
		}
		all = append(all, wrapText(face, l, in.Dx())...)
	}
	lineH := min(26, in.Dy()/max(1, len(all)))
	for i, l := range all {
		col := lcdInk
		if i > 0 {
			col = lcdDim
		}
		c.Text(image.Rect(in.Min.X, in.Min.Y+i*lineH, in.Max.X, in.Min.Y+(i+1)*lineH), strings.ToUpper(l), face, col, gfx.Left)
	}

	// The picture: the still picture of the point, or the zoom, with the mark.
	window(c, pic)
	a.region("Picture", pic)
	area := pic.Inset(4)
	show, box := fl.frame, [4]float64{}
	if fl.frame != nil {
		sz := fl.frame.Size()
		box = [4]float64{0, 0, float64(sz.X), float64(sz.Y)}
		if fl.zoom != nil {
			show, box = fl.zoom, fl.zoomBox
		}
	}
	if show == nil || fl.phase != calibTap {
		msg := "…"
		if fl.phase == calibDone {
			msg = "Calibrated"
		} else if fl.phase == calibFailed {
			msg = "Not calibrated"
		}
		c.Text(area, strings.ToUpper(msg), gfx.Face(true, 18), lcdDim, gfx.Center)
	} else {
		sz := show.Size()
		k := min(float64(area.Dx())/float64(sz.X), float64(area.Dy())/float64(sz.Y))
		r := kit.Center(area, int(float64(sz.X)*k), int(float64(sz.Y)*k))
		w := r.Dx()
		if a.Scale[1] > 0 {
			w = w * a.Scale[0] / a.Scale[1]
		}
		if fl.zoom != nil {
			c.Cover(r, show)
		} else {
			c.Cover(r, show.Small(w))
		}
		if fl.tapped {
			x := r.Min.X + int((fl.tap[0]-box[0])/(box[2]-box[0])*float64(r.Dx())+0.5)
			y := r.Min.Y + int((fl.tap[1]-box[1])/(box[3]-box[1])*float64(r.Dy())+0.5)
			drawMark(c, x, y, r)
		}
		a.addHit(kit.Hit{R: r, Pad: func(p image.Point, ph kit.Phase) { a.calibTapAt(r, p, ph) }})
	}

	// The keys.
	var ks []func(image.Rectangle)
	switch fl.phase {
	case calibTap:
		ks = append(ks, func(r image.Rectangle) {
			if fl.tapped {
				a.button(c, r, "Use point", green, onAcc, func() { a.calibNext(true) })
			} else {
				key(c, r, "Use point", raised, dim, false)
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
		ks = append(ks, func(r image.Rectangle) { a.button(c, r, "Discard", raised, text, a.cancelCalibration) })
	case calibFailed:
		ks = append(ks, func(r image.Rectangle) {
			a.button(c, r, "Again", raised, text, func() {
				a.calib = nil
				a.back()
				a.askCalibrate()
			})
		})
		ks = append(ks, func(r image.Rectangle) { a.button(c, r, "Close", raised, text, a.cancelCalibration) })
	}
	if len(ks) == 0 {
		return
	}
	if f.wide {
		sizes := make([]kit.Size, 4)
		for i := range sizes {
			sizes[i] = kit.Px(64)
		}
		for i, r := range kit.Rows(keys, 12, sizes...) {
			if i < len(ks) {
				ks[i](r)
			}
		}
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
