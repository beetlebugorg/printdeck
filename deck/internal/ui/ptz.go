package ui

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"time"

	"printdeck/internal/camera"
	"printdeck/internal/gfx"
	"printdeck/kit"
)

// The digital pan, tilt and zoom (PTZ) of a camera (Camera page > PTZ). The key shows only for a
// camera with a zoom control. The PTZ keys zoom in and out, move the crop left, right, up and
// down, go back to the whole picture (Reset), and measure the crop of the camera (Measure). The
// camera crops its sensor, so a zoom keeps the detail and costs the deck no CPU. The stream of a
// camera is one stream: each viewer sees the zoom.

// ptzSetter is a Camera that sets a PTZ position with no save of the settings for each step
// (camera.Hub).
type ptzSetter interface {
	SetPTZ(id string, pos *camera.PTZPos)
}

// ptzMeasurer is a Camera that measures the PTZ map of a camera (camera.Hub).
type ptzMeasurer interface {
	MeasurePTZ(ctx context.Context, id string, progress func(done, all int)) (*camera.PTZMap, error)
}

// ptzRun is a measurement of the PTZ map that runs.
type ptzRun struct {
	id        string
	done, all int
}

// ptzRecent is how long the position of the last PTZ step is the position of the next step: the
// capture reports a step a moment later, so quick steps add up.
const ptzRecent = 2 * time.Second

// setPTZ sets the PTZ position of the camera id, or the wide position with nil.
func (a *App) setPTZ(id string, pos *camera.PTZPos) {
	if ps, ok := a.Camera.(ptzSetter); ok {
		ps.SetPTZ(id, pos)
	} else {
		a.changeCamera(id, func(cs *camera.CamSettings) { cs.PTZ = pos })
	}
	a.ptzLast, a.ptzLastID, a.ptzLastAt = camera.PTZPos{}, id, a.Now()
	if pos != nil {
		a.ptzLast = *pos
	} else {
		a.ptzLastAt = time.Time{}
	}
	a.Redraw()
}

// ptzNow is the PTZ control of the camera id: the state of the capture p, with the position of the
// last step when it is recent.
func (a *App) ptzNow(id string, p camera.PTZ) camera.PTZ {
	if a.ptzLastID == id && !a.ptzLastAt.IsZero() && a.Now().Sub(a.ptzLastAt) < ptzRecent {
		p.Pos = p.Target(&a.ptzLast)
	}
	return p
}

// ptzMap returns the PTZ map of the camera id when it is valid.
func (a *App) ptzMap(id string) *camera.PTZMap {
	cs := a.Camera.Settings().Camera(id)
	if cs.PTZMap.ValidAt(cs.Rotate) {
		return cs.PTZMap
	}
	return nil
}

// zoomStep zooms the camera id in (n = 1) or out (n = -1): by a factor of 1.25 with a PTZ map, and
// else by a twelfth of the range.
func (a *App) zoomStep(id string, p camera.PTZ, n int) {
	pos := p.Pos
	if m := a.ptzMap(id); m != nil {
		k := m.Factor(pos.Zoom) * math.Pow(1.25, float64(n))
		z := int(math.Round(m.ZoomFor(k)))
		if z == pos.Zoom {
			z += n * p.Zoom.Step
		}
		pos.Zoom = p.Zoom.Clamp(z)
		pos = m.Limit(pos, p)
	} else {
		pos.Zoom = p.Zoom.Clamp(pos.Zoom + n*max(p.Zoom.Step, (p.Zoom.Max-p.Zoom.Min)/12))
	}
	a.setPTZ(id, &pos)
}

// moveStep moves the crop of the camera id by (dx, dy) steps: right and down for positive values.
// A step is an eighth of the crop with a PTZ map. With no map, a step is a fortieth of the range,
// with a positive pan to the right and a positive tilt up, as V4L2 defines them.
func (a *App) moveStep(id string, p camera.PTZ, dx, dy int) {
	pos := p.Pos
	if m := a.ptzMap(id); m != nil {
		cu, cv, k := m.Window(pos)
		d := 0.125 / k
		next := m.Center(pos, cu+float64(dx)*d, cv+float64(dy)*d, p)
		if next == pos {
			// A step of less than one unit: one unit of each control that moves.
			next.Pan = p.Pan.Clamp(pos.Pan + dx*p.Pan.Step*sign(m.Pan[0]))
			next.Tilt = p.Tilt.Clamp(pos.Tilt + dy*p.Tilt.Step*sign(m.Tilt[1]))
		}
		pos = m.Limit(next, p)
	} else {
		if p.Pan.Valid() {
			pos.Pan = p.Pan.Clamp(pos.Pan + dx*max(p.Pan.Step, (p.Pan.Max-p.Pan.Min)/40))
		}
		if p.Tilt.Valid() {
			pos.Tilt = p.Tilt.Clamp(pos.Tilt - dy*max(p.Tilt.Step, (p.Tilt.Max-p.Tilt.Min)/40))
		}
	}
	a.setPTZ(id, &pos)
}

func sign(v float64) int {
	if v < 0 {
		return -1
	}
	return 1
}

// ptzLine is the PTZ position of a camera in words, for example "Zoom 2.0×, pan 3600, tilt 0".
func (a *App) ptzLine(id string, p camera.PTZ) string {
	z := fmt.Sprintf("Zoom %d of %d to %d", p.Pos.Zoom, p.Zoom.Min, p.Zoom.Max)
	if m := a.ptzMap(id); m != nil {
		z = fmt.Sprintf("Zoom %.1f×", m.Factor(p.Pos.Zoom))
	}
	if p.Pan.Valid() {
		z += fmt.Sprintf(", pan %d", p.Pos.Pan)
	}
	if p.Tilt.Valid() {
		z += fmt.Sprintf(", tilt %d", p.Pos.Tilt)
	}
	return z
}

// ptzState is the state of the PTZ map of the camera id for the info line: the measurement that
// runs, or the need of a measurement. long is the text of a row.
func (a *App) ptzState(id string, long bool) string {
	switch {
	case a.ptzRun != nil && a.ptzRun.id == id:
		return fmt.Sprintf("Measuring: picture %d of %d", a.ptzRun.done, max(a.ptzRun.all, 1))
	case a.ptzMap(id) == nil && long:
		return "Once, for the views and the zoom to the part. Aim the camera at the bed first."
	case a.ptzMap(id) == nil:
		return "Zoom not measured"
	case long:
		return "Measured. Measure again after a move of the camera."
	}
	return "Each viewer of the camera sees the zoom"
}

// measurePTZ measures the PTZ map of the camera id.
func (a *App) measurePTZ(id string) {
	pm, ok := a.Camera.(ptzMeasurer)
	if !ok || a.ptzRun != nil {
		return
	}
	if st := a.Printer.State(); st.Busy() {
		a.Toast("Measure the zoom when the printer does not print")
		return
	}
	run := &ptzRun{id: id}
	a.ptzRun = run
	a.Redraw()
	go func() {
		_, err := pm.MeasurePTZ(context.Background(), id, func(done, all int) {
			a.post(func() { run.done, run.all = done, all; a.Redraw() })
		})
		a.post(func() {
			if a.ptzRun == run {
				a.ptzRun = nil
			}
			if err != nil {
				a.Toast("Zoom not measured: " + err.Error())
			} else {
				a.Toast("Zoom measured. The views follow the zoom.")
			}
			a.Redraw()
		})
	}()
}

// namedButton draws a key with the label on the screen and another name for the knob menu, the
// tests and the manual.
func (a *App) namedButton(c *gfx.Canvas, r image.Rectangle, label, name string, fill, fg color.RGBA, tap func()) {
	a.drawAck(c, r)
	key(c, r, label, fill, fg, a.isPressed(r))
	acked := func() {
		a.ackR, a.ackAt = r, a.Now()
		tap()
	}
	a.addHit(kit.Hit{R: r, Tap: acked})
	a.addFocus(name, r, acked)
	a.region(name, r)
}

// ptzKey is a key of the PTZ keys: its label, its name, and its action.
type ptzKey struct {
	label, name string
	tap         func()
}

// ptzKeys are the keys of the zoom and the moves of the camera id. A camera with no pan or no
// tilt has no keys for it.
func (a *App) ptzKeys(id string, p camera.PTZ) (zoom, move []ptzKey) {
	zoom = []ptzKey{{"−", "Zoom out", func() { a.zoomStep(id, p, -1) }}, {"+", "Zoom in", func() { a.zoomStep(id, p, 1) }}}
	if p.Pan.Valid() {
		move = append(move, ptzKey{"←", "Left", func() { a.moveStep(id, p, -1, 0) }})
	}
	if p.Tilt.Valid() {
		move = append(move, ptzKey{"↑", "Up", func() { a.moveStep(id, p, 0, -1) }},
			ptzKey{"↓", "Down", func() { a.moveStep(id, p, 0, 1) }})
	}
	if p.Pan.Valid() {
		move = append(move, ptzKey{"→", "Right", func() { a.moveStep(id, p, 1, 0) }})
	}
	return zoom, move
}

// drawPTZKeys draws the PTZ keys of the camera id in r, in one row: the zoom, the moves, Reset,
// Measure and Done.
func (a *App) drawPTZKeys(c *gfx.Canvas, r image.Rectangle, id string, p camera.PTZ) {
	zoom, move := a.ptzKeys(id, p)
	keys := append(zoom, move...)
	var sizes []kit.Size
	for range keys {
		sizes = append(sizes, kit.Fr(2))
	}
	_, measure := a.Camera.(ptzMeasurer)
	sizes = append(sizes, kit.Fr(3))
	if measure {
		sizes = append(sizes, kit.Fr(3))
	}
	sizes = append(sizes, kit.Fr(3))
	k := kit.Cols(r, 8, sizes...)
	for i, kk := range keys {
		a.namedButton(c, k[i], kk.label, kk.name, raised, text, kk.tap)
	}
	i := len(keys)
	a.button(c, k[i], "Reset", raised, text, func() { a.setPTZ(id, nil) })
	if measure {
		i++
		fill, ink := raised, text
		if a.ptzRun != nil {
			fill, ink = keyOn, keyOnInk
		}
		a.button(c, k[i], "Measure", fill, ink, func() { a.measurePTZ(id) })
	}
	a.button(c, k[len(k)-1], "Done", raised, text, func() { a.camPTZ = false })
}

// ptzRowH is the height of the rows of the PTZ keys on a tall screen: the zoom row and the row of
// the moves.
const ptzRowH = 72

// drawPTZRows draws the PTZ keys of the camera id in r on a tall screen: the zoom keys, and under
// them the moves. It returns the space under the keys.
func (a *App) drawPTZRows(c *gfx.Canvas, r image.Rectangle, id string, p camera.PTZ, gap int) image.Rectangle {
	zoom, move := a.ptzKeys(id, p)
	sizes := []kit.Size{kit.Px(ptzRowH)}
	if len(move) > 0 {
		sizes = append(sizes, kit.Px(ptzRowH))
	}
	sizes = append(sizes, kit.Fr(1))
	rows := kit.Rows(r, gap, sizes...)
	row := func(r image.Rectangle, keys []ptzKey) {
		s := make([]kit.Size, len(keys))
		for i := range s {
			s[i] = kit.Fr(1)
		}
		k := kit.Cols(r, 10, s...)
		for i, kk := range keys {
			a.namedButton(c, k[i], kk.label, kk.name, raised, text, kk.tap)
		}
	}
	// The zoom keys are wide here: their names fit.
	for i := range zoom {
		zoom[i].label = zoom[i].name
	}
	row(rows[0], zoom)
	if len(move) > 0 {
		row(rows[1], move)
	}
	return rows[len(rows)-1]
}

// ptzItems are the rows of the PTZ mode on a tall screen: Reset, Measure the zoom and Done.
func (a *App) ptzItems(id string, p camera.PTZ) []menuItem {
	items := []menuItem{{"Reset", "Back to the whole picture", dim, func() { a.setPTZ(id, nil) }}}
	if _, ok := a.Camera.(ptzMeasurer); ok {
		items = append(items, menuItem{"Measure the zoom", a.ptzState(id, true), dim, func() { a.measurePTZ(id) }})
	}
	return append(items, menuItem{"Done", "Back to the camera rows. Each viewer sees the zoom.", dim, func() { a.camPTZ = false }})
}

// ptzRowsHeight is the height of the PTZ keys on a tall screen, with the gap under them.
func ptzRowsHeight(p camera.PTZ, gap int) int {
	if p.Pan.Valid() || p.Tilt.Valid() {
		return 2*(ptzRowH+gap) + gap
	}
	return ptzRowH + gap
}

// calibPTZLine is the line of the calibration of a camera with PTZ, when the calibration is not
// valid at the PTZ position now and the camera has no PTZ map, or "".
func calibPTZLine(cal *camera.Calibration, valid bool, st camera.Status) string {
	if cal == nil || valid || st.PTZ == nil || st.PTZ.Mapped || st.PTZ.Target(cal.PTZ) == st.PTZ.Pos {
		return ""
	}
	return "Zoom changed: measure the zoom, or calibrate again"
}

// followLine is the line of the "Zoom to the part" row of the page of a camera.
func followLine(st camera.Status, calibrated bool) string {
	switch {
	case st.PTZ == nil:
		return "Turn the camera on to see if it zooms"
	case !st.PTZ.Mapped:
		return "Measure the zoom on the Camera page first"
	case !calibrated:
		return "Calibrate the camera first"
	}
	return "During a print. Each viewer sees the zoom."
}

// toggleFollowPart turns the zoom to the part of the camera id on or off.
func (a *App) toggleFollowPart(id string) {
	cs := a.changeCamera(id, func(cs *camera.CamSettings) { cs.FollowPart = !cs.FollowPart })
	if cs.FollowPart {
		a.Toast("The camera zooms to the part during a print")
	} else {
		a.Toast("The camera keeps its zoom during a print")
	}
}
