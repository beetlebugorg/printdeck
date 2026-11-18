package ui

import (
	"fmt"
	"image"

	"printdeck/internal/touch"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/kit"
)

// ModelViewer is a 3D view of a G-code file that the finger turns (the print view of zslice and
// the GPU), for the last print of the home screen.
type ModelViewer interface {
	// Frame draws the file path of the printer up to the layer (from 1; 0 or more than the
	// layers of the file is the whole file), w x h pixels, in the colour hex ("#rrggbb", or "" for
	// a colour for each feature). It returns the layers of the file.
	Frame(path string, layer, w, h int, hex string) (img image.Image, layers int, ok bool)
	// Live draws the file path of a print that runs: at the file position pos, or up to the
	// layer (from 1) when layer is above 0. It keeps the camera of the view.
	Live(path string, pos int64, layer, w, h int, hex string) (img image.Image, ok bool)
	// Orbit turns the view with one finger: phase is kit.PadDown, PadMove or PadUp, and x, y the
	// place of the finger in the view.
	Orbit(phase int, x, y float32)
	// Finger gives the view another finger of a multi-touch panel (id above 1), with the same
	// phases: two fingers pinch to zoom and move to pan.
	Finger(id int64, phase int, x, y float32)
	// Zoom zooms in (step > 0) or out.
	Zoom(step int)
	// Reset goes back to the start view.
	Reset()
}

// drawLastPrintView draws the last print l in the card r of the home screen: the name, the
// summary and a small Reprint key at the top, and the model under them. With a ModelViewer the
// model turns under the finger, and a scrubber shows it up to a layer; else it is the picture of
// the print.
func (a *App) drawLastPrintView(c *gfx.Canvas, r image.Rectangle, l printer.Record, summary string) {
	in := r.Inset(12)
	head := image.Rect(in.Min.X, in.Min.Y, in.Max.X, in.Min.Y+48)
	reprint := image.Rect(head.Max.X-120, head.Min.Y, head.Max.X, head.Max.Y)
	nf, sf := gfx.Face(true, 20), gfx.Face(false, 15)
	textR := image.Rect(head.Min.X+4, head.Min.Y, reprint.Min.X-12, head.Max.Y)
	c.Text(image.Rect(textR.Min.X, textR.Min.Y, textR.Max.X, textR.Min.Y+26), gfx.Fit(nf, l.Name(), textR.Dx()), nf, text, gfx.Left)
	c.Text(image.Rect(textR.Min.X, textR.Min.Y+26, textR.Max.X, textR.Max.Y), gfx.Fit(sf, summary, textR.Dx()), sf, dim, gfx.Left)
	rec := l
	open := func() {
		a.record = rec
		a.pages = []page{pageMachine, pageStats, pageHistory, pageRecord}
		a.Redraw()
	}
	a.addHit(kit.Hit{R: textR, Tap: open})
	path := l.Path
	a.printerKey(c, reprint, "Reprint", raised, text, func() { a.confirmPrint(path) })

	body := image.Rect(in.Min.X, head.Max.Y+10, in.Max.X, in.Max.Y)
	if a.Viewer == nil || path == "" || !a.drawLastModel(c, body, l) {
		model := body
		a.recordModel(c, model, l)
		a.addHit(kit.Hit{R: model, Tap: open})
	}
}

// viewState is the scrubber of a model view: the layer that it shows (0 for the whole file), and
// the file of that layer.
type viewState struct {
	layer int
	of    string
}

// drawLastModel draws the model of l with the Viewer in r, with the scrubber and the zoom keys
// under it. It reports false when the Viewer cannot draw the file.
func (a *App) drawLastModel(c *gfx.Canvas, r image.Rectangle, l printer.Record) bool {
	return a.turnView(c, r, l.Path, l.SpoolID, &a.homeView)
}

// turnView draws the file path with the Viewer in r: the model that one finger turns, and under
// it the zoom keys, Reset, and the scrubber of the layers, in the state vs. It reports false when
// the Viewer cannot draw the file.
func (a *App) turnView(c *gfx.Canvas, r image.Rectangle, path, spoolID string, vs *viewState) bool {
	if a.Viewer == nil || path == "" {
		return false
	}
	if vs.of != path {
		vs.of, vs.layer = path, 0 // another file shows whole
	}
	ctlH := 48
	if gfx.FinePrint() {
		ctlH = 56
	}
	rows := kit.Rows(r, 10, kit.Fr(1), kit.Px(ctlH))
	model, ctl := rows[0], rows[1]
	hex := ""
	if rolls := a.Printer.SpoolStore(); rolls != nil {
		if sp, ok := rolls.Get(spoolID); ok {
			hex = sp.Color
		}
	}
	inner := model.Inset(2)
	img, layers, ok := a.Viewer.Frame(path, vs.layer, inner.Dx(), inner.Dy(), hex)
	if !ok {
		return false
	}
	window(c, model)
	c.Image(inner, img)
	a.region("Model", model)
	a.addHit(kit.Hit{R: model, Pad: func(p image.Point, ph kit.Phase) {
		a.Viewer.Orbit(int(ph), float32(p.X-inner.Min.X), float32(p.Y-inner.Min.Y))
		a.Redraw()
	}})
	a.pinchArea = inner
	// The zoom keys and Reset at the left, and the scrubber of the layers at the right.
	k := kit.Cols(ctl, 8, kit.Px(ctlH), kit.Px(ctlH), kit.Px(ctlH*16/10), kit.Fr(1))
	a.namedButton(c, k[0], "−", "Zoom out", raised, text, func() { a.Viewer.Zoom(-1); a.Redraw() })
	a.namedButton(c, k[1], "+", "Zoom in", raised, text, func() { a.Viewer.Zoom(1); a.Redraw() })
	a.namedButton(c, k[2], "Reset", "Reset the view", raised, text, func() {
		a.Viewer.Reset()
		vs.layer = 0
		a.Redraw()
	})
	shown := vs.layer
	if shown <= 0 || shown > layers {
		shown = layers
	}
	bar := k[3]
	track := image.Rect(bar.Min.X+8, bar.Max.Y-16, bar.Max.X-8, bar.Max.Y-10)
	c.RoundRect(track, 3, edge)
	if layers > 0 {
		w := track.Dx() * shown / layers
		c.RoundRect(image.Rect(track.Min.X, track.Min.Y, track.Min.X+w, track.Max.Y), 3, orange)
		knob := image.Rect(track.Min.X+w-7, track.Min.Y-8, track.Min.X+w+7, track.Max.Y+8)
		c.RoundRect(knob, 5, text)
	}
	lf := gfx.Face(false, 15)
	c.Text(image.Rect(bar.Min.X+8, bar.Min.Y, bar.Max.X-8, track.Min.Y-8), fmt.Sprintf("Layer %d of %d", shown, layers), lf, dim, gfx.Left)
	set := func(f float64) {
		if layers > 0 {
			vs.layer = max(1, min(layers, int(f*float64(layers)+0.5)))
			a.Redraw()
		}
	}
	a.addHit(kit.Hit{R: bar, Drag: set, End: set})
	a.region("Layers", bar)
	return true
}

// otherFinger gives a second finger of a multi-touch panel to the 3D view under it: two fingers
// pinch to zoom and pan. A finger that comes down out of the view is ignored until it lifts.
func (a *App) otherFinger(ev touch.Event, p image.Point) {
	if a.Viewer == nil {
		return
	}
	if a.pinch == nil {
		a.pinch = map[int]bool{}
	}
	switch ev.Kind {
	case touch.Down:
		if !p.In(a.pinchArea) {
			return
		}
		a.pinch[ev.Finger] = true
	default:
		if !a.pinch[ev.Finger] {
			return
		}
		if ev.Kind == touch.Up {
			delete(a.pinch, ev.Finger)
		}
	}
	phase := map[touch.Kind]int{touch.Down: int(kit.PadDown), touch.Move: int(kit.PadMove), touch.Up: int(kit.PadUp)}[ev.Kind]
	r := a.pinchArea
	a.Viewer.Finger(int64(ev.Finger)+1, phase, float32(p.X-r.Min.X), float32(p.Y-r.Min.Y))
}
