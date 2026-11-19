package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"time"

	"printdeck/internal/camera"
	"printdeck/internal/gfx"
	"printdeck/kit"
)

// The digital pan, tilt and zoom (PTZ) of a camera (Camera page > PTZ). Each camera that runs has
// the key. The PTZ keys zoom in and out, move the crop left, right, up and down, and go back to the
// whole picture (Reset). The deck crops the frames of the camera (camera.DeckPTZ): a 4K camera
// keeps its detail up to a zoom of 2. The stream of a camera is one stream: each viewer sees the
// zoom.

// ptzSetter is a Camera that sets a PTZ position with no save of the settings for each step
// (camera.Hub).
type ptzSetter interface {
	SetPTZ(id string, pos *camera.PTZPos)
}

// focusRun is a focus search on the part that runs.
type focusRun struct {
	id        string
	done, all int
}

// ptzRecent is how long the position of the last PTZ step is the position of the next step: the
// state of the camera reports a step a moment later, so quick steps add up.
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

// ptzNow is the PTZ control of the camera id: the state of the camera p, with the position of the
// last step when it is recent.
func (a *App) ptzNow(id string, p camera.PTZ) camera.PTZ {
	if a.ptzLastID == id && !a.ptzLastAt.IsZero() && a.Now().Sub(a.ptzLastAt) < ptzRecent {
		p.Pos = p.Target(&a.ptzLast)
	}
	return p
}

// ptzMap returns the PTZ map of the camera id.
func (a *App) ptzMap(id string) *camera.PTZMap {
	return a.Camera.Settings().Camera(id).Map()
}

// zoomStep zooms the camera id in (n = 1) or out (n = -1) by a factor of 1.25.
func (a *App) zoomStep(id string, p camera.PTZ, n int) {
	pos := p.Pos
	m := a.ptzMap(id)
	k := m.Factor(pos.Zoom) * math.Pow(1.25, float64(n))
	z := int(math.Round(m.ZoomFor(k)))
	if z == pos.Zoom {
		z += n * p.Zoom.Step
	}
	pos.Zoom = p.Zoom.Clamp(z)
	pos = m.Limit(pos, p)
	a.setPTZ(id, &pos)
}

// moveStep moves the crop of the camera id by (dx, dy) steps: right and down for positive values.
// A step is an eighth of the crop.
func (a *App) moveStep(id string, p camera.PTZ, dx, dy int) {
	pos := p.Pos
	m := a.ptzMap(id)
	cu, cv, k := m.Window(pos)
	d := 0.125 / k
	next := m.Center(pos, cu+float64(dx)*d, cv+float64(dy)*d, p)
	if next == pos {
		// A step of less than one unit: one unit of each control that moves.
		next.Pan = p.Pan.Clamp(pos.Pan + dx*p.Pan.Step*sign(m.Pan[0]))
		next.Tilt = p.Tilt.Clamp(pos.Tilt + dy*p.Tilt.Step*sign(m.Tilt[1]))
	}
	a.setPTZ(id, ptr(m.Limit(next, p)))
}

func ptr(p camera.PTZPos) *camera.PTZPos { return &p }

func sign(v float64) int {
	if v < 0 {
		return -1
	}
	return 1
}

// ptzLine is the PTZ position of a camera in words, for example "Zoom 2.0×".
func (a *App) ptzLine(p camera.PTZ) string {
	return fmt.Sprintf("Zoom %.1f×", float64(p.Pos.Zoom)/100)
}

// namedButton draws a key with the label on the screen and another name for the knob menu, the
// tests and the manual.
func (a *App) namedButton(c *gfx.Canvas, r image.Rectangle, label, name string, fill, fg color.RGBA, tap func()) {
	a.namedButtonAt(c, r, label, name, fill, fg, rowSize{}, tap)
}

// namedButtonAt is namedButton with the label size of a row, as keyAt.
func (a *App) namedButtonAt(c *gfx.Canvas, r image.Rectangle, label, name string, fill, fg color.RGBA, size rowSize, tap func()) {
	a.drawAck(c, r)
	keyAt(c, r, label, fill, fg, a.isPressed(r), 0, size)
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

// drawPTZKeys draws the PTZ keys of the camera id in r on a wide screen, in two rows of equal
// keys, so that each key is large enough for a finger: the zoom and the moves, then Reset and
// Done.
func (a *App) drawPTZKeys(c *gfx.Canvas, r image.Rectangle, id string, p camera.PTZ) {
	zoom, move := a.ptzKeys(id, p)
	keys := append(zoom, move...)
	rows := kit.Rows(r, 8, kit.Fr(1), kit.Fr(1))
	top := kit.Grid(rows[0], len(keys), 1, 8)
	for i, kk := range keys {
		a.namedButton(c, top[i], kk.label, kk.name, raised, text, kk.tap)
	}
	bottom := kit.Grid(rows[1], 2, 1, 8)
	a.button(c, bottom[0], "Reset", raised, text, func() { a.setPTZ(id, nil) })
	a.button(c, bottom[1], "Done", raised, text, func() { a.camPTZ = false })
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

// ptzItems are the rows of the PTZ mode on a tall screen: Reset and Done.
func (a *App) ptzItems(id string) []menuItem {
	return []menuItem{{"Reset", "Back to the whole picture", dim, func() { a.setPTZ(id, nil) }},
		{"Done", "Back to the camera rows", dim, func() { a.camPTZ = false }}}
}

// ptzRowsHeight is the height of the PTZ keys on a tall screen, with the gap under them.
func ptzRowsHeight(p camera.PTZ, gap int) int {
	if p.Pan.Valid() || p.Tilt.Valid() {
		return 2*(ptzRowH+gap) + gap
	}
	return ptzRowH + gap
}

// followLine is the line of the "Zoom to the part" row of the page of a camera.
func followLine(calibrated bool) string {
	if !calibrated {
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
