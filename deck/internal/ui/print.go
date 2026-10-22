package ui

import (
	"fmt"
	"image"
	"image/color"
	lcdscreen "printdeck/internal/lcd"
	"printdeck/kit"
	"strings"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

func duration(d time.Duration) string {
	m := int(d.Minutes())
	if m >= 60 {
		return fmt.Sprintf("%dh %02dm", m/60, m%60)
	}
	return fmt.Sprintf("%d min", max(m, 1))
}

func (a *App) drawPrint(c *gfx.Canvas) {
	if a.picker != nil {
		a.drawCuraPicker(c)
		return
	}
	if a.form != nil {
		a.drawRollForm(c)
		return
	}
	if a.swap != nil {
		a.drawSwap(c)
		return
	}
	if a.flash != nil {
		a.drawFlash(c)
		return
	}
	if a.actionScreen(c, a.Printer.State()) {
		return
	}
	st := a.Printer.State()
	// A manual Z probe (the paper test) runs: its screen comes first.
	if st.Reachable && st.ManualProbe {
		a.drawManualProbe(c, st)
		return
	}
	switch a.topPage() {
	case pageFiles:
		a.drawFiles(c)
		return
	case pageFile:
		a.drawFile(c, st)
		return
	case pageMove:
		a.drawMove(c)
		return
	case pageFilament:
		a.drawFilament(c, st)
		return
	case pageMaintenance:
		a.drawMaintenance(c, st)
		return
	case pageMachine:
		a.drawMachine(c, st)
		return
	case pageMacros:
		a.drawMacros(c, st)
		return
	case pageHistory:
		a.drawHistory(c)
		return
	case pageRecord:
		a.drawRecord(c, st)
		return
	case pageSpools:
		a.drawSpools(c)
		return
	case pageSpool:
		a.drawSpool(c)
		return
	case pageStats:
		a.drawStats(c)
		return
	case pageUpdate:
		a.drawUpdate(c, st)
		return
	case pageConfigs:
		a.drawConfigs(c)
		return
	case pageConsole:
		a.drawConsole(c)
		return
	}
	if a.tune {
		a.drawTune(c)
		return
	}
	if !st.Reachable {
		switch {
		case st.NeedsSetup:
			a.drawPrinterSetup(c, st)
		case st.Starting:
			a.drawStarting(c, st)
		default:
			a.drawKlipperError(c, st)
		}
		return
	}
	if !st.Busy() {
		a.finishLeft = false
	}
	if st.Busy() {
		switch {
		case st.Finishing && a.finishLeft && a.finishFile == st.FilePath:
			// The user left the end of the print: the home screen, while the end macro runs.
			a.drawHome(c, st)
		case st.Paused:
			a.drawPaused(c, st)
		case st.Preparing:
			a.drawPreparing(c, st)
		case st.Finishing:
			a.drawFinishing(c, st)
		case st.Layer == 1 && st.Layers > 1:
			a.drawFirstLayer(c, st)
		default:
			a.drawJob(c, st)
		}
		return
	}
	if st.Finished != "" && finishedKey(st) != a.doneSeen {
		a.drawDone(c, st)
		return
	}
	a.drawHome(c, st)
}

// stateAreas are the areas of a state screen under its display. A tall screen has one body under
// the display, so keys and view are both the body. A wide screen has its keys in the left column,
// under the display, and the view in the right column.
func (a *App) stateAreas() (f frame, keys, view image.Rectangle) {
	f = a.stateFrame()
	if f.wide {
		return f, kit.Inset(f.side, wideOut, 0, wideIn, lcdTop), kit.Inset(f.main, wideIn, lcdTop, wideOut, lcdTop)
	}
	body := kit.Inset(f.main, margin, 0, margin, 8)
	return f, body, body
}

// jobAreas are the keys and the model view of the job screen. The view has the layer slider at its
// bottom edge.
func (a *App) jobAreas() (keys, box image.Rectangle) {
	f, keys, view := a.stateAreas()
	if f.wide {
		return kit.Rows(keys, 0, kit.Px(keyRowH))[0], view
	}
	r := kit.Rows(view, 12, kit.Px(keyRowH), kit.Fr(1))
	return r[0], r[1]
}

// drawJob is the screen of a running print: the status display, the keys, and the model view.
func (a *App) drawJob(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	a.statusPanel(c, st)
	keys, box := a.jobAreas()
	if f, _, _ := a.stateAreas(); f.wide {
		// Under the keys in the left column: the file, the layer and the end of the print.
		_, side, _ := a.stateAreas()
		a.jobCard(c, kit.Rows(side, 12, kit.Px(keyRowH), kit.Fr(1))[1], st)
	}
	k := kit.Cols(keys, 8, kit.Fr(1), kit.Fr(1), kit.Fr(1))
	a.button(c, k[0], "Pause", orange, onAcc, pr.Pause)
	a.button(c, k[1], "Tune", raised, text, func() { a.tune = true })
	a.button(c, k[2], "Cancel", raised, red, a.askCancel)
	a.modelView(c, box, st)
	a.drawScrubber(c, box, st)
}

// jobCard draws the file of the print, its layer, and the time of its end, in a plate.
func (a *App) jobCard(c *gfx.Canvas, r image.Rectangle, st printer.State) {
	plate(c, r)
	in := kit.Inset(r, 18, 10, 18, 10)
	s := kit.NewStack(in, 4)
	c.Text(s.Next(30), gfx.Fit(gfx.Face(true, 20), st.File, in.Dx()), gfx.Face(true, 20), text, gfx.Left)
	if st.Layers > 0 {
		c.Text(s.Next(26), fmt.Sprintf("Layer %d of %d", st.Layer, st.Layers), gfx.Face(false, 17), dim, gfx.Left)
	}
	if st.HasTimeLeft {
		end := clockText(a.Now().Add(st.TimeLeft), a.Clock24)
		c.Text(s.Next(26), fmt.Sprintf("%s left · done at %s", duration(st.TimeLeft), end), gfx.Face(false, 17), dim, gfx.Left)
	}
}

// askCancel asks before it cancels the print.
func (a *App) askCancel() {
	a.confirm = &confirmBox{question: "Cancel this print?", yes: "Cancel print", danger: true, onYes: a.Printer.Cancel}
}

// modelView draws the model in a window, with the move now and its speed at the top right. A
// picture that is larger than the window becomes smaller to fit.
func (a *App) modelView(c *gfx.Canvas, box image.Rectangle, st printer.State) {
	window(c, box)
	a.region("Model", box)
	if img := a.Printer.Preview(); img != nil {
		picture(c, box, img)
	} else {
		c.Text(box, "LOADING MODEL…", gfx.Face(true, 18), lcdDim, gfx.Center)
	}
	if st.HasNow {
		now := fmt.Sprintf("%s %.0f MM/S", strings.ToUpper(st.NowKind), st.NowSpeed*float64(a.speedFactor(st))/100)
		c.Text(image.Rect(box.Min.X+200, box.Min.Y+8, box.Max.X-14, box.Min.Y+30), now, gfx.Face(true, 13), lcdDim, gfx.Right)
	}
}

// picture draws a picture of the model in the middle of a window box. A picture that is larger
// than the window becomes smaller to fit.
func picture(c *gfx.Canvas, box image.Rectangle, img image.Image) {
	b, in := img.Bounds(), box.Inset(2)
	if b.Dx() > in.Dx() || b.Dy() > in.Dy() {
		s := min(float64(in.Dx())/float64(b.Dx()), float64(in.Dy())/float64(b.Dy()))
		c.Cover(kit.Center(in, int(float64(b.Dx())*s), int(float64(b.Dy())*s)), img)
		return
	}
	c.Clipped(in, func(in *gfx.Canvas) {
		in.Image(image.Rect(box.Min.X+(box.Dx()-b.Dx())/2, box.Min.Y+(box.Dy()-b.Dy())/2, box.Max.X, box.Max.Y), img)
	})
}

// speedFactor is the speed in percent of the print (M220).
func (a *App) speedFactor(st printer.State) int {
	if st.SpeedPct > 0 {
		return st.SpeedPct
	}
	return 100
}

// lcdLines are the two lines of 20 characters of the character display.
func (a *App) lcdLines(st printer.State) []string {
	lr := func(l, r string) string { // l at the left, r at the right, 20 characters
		n := 20 - len([]rune(r))
		ls := []rune(l)
		if len(ls) > n-1 {
			ls = ls[:max(0, n-1)]
		}
		return string(ls) + strings.Repeat(" ", n-len(ls)) + r
	}
	right := fmt.Sprintf("%.0f%%", st.Completion)
	if st.HasTimeLeft {
		m := int(st.TimeLeft.Minutes())
		right += fmt.Sprintf(" %d:%02d", m/60, m%60)
	}
	// The second line: the move now and its speed, and every 4 seconds the time of the finish on
	// the clock, as the display of a printer alternates its lines.
	now := "--"
	if st.HasNow {
		now = lr(strings.ToUpper(st.NowKind), fmt.Sprintf("%.0fMM/S", st.NowSpeed*float64(a.speedFactor(st))/100))
	}
	if st.HasTimeLeft && (!st.HasNow || a.Now().Unix()/4%2 == 1) {
		now = lr("DONE AT", lcdscreen.Clock(a.Now().Add(st.TimeLeft), a.Clock24))
	}
	return []string{lr(strings.ToUpper(st.Activity), right), now}
}

// temps draws the nozzle and the bed side by side in r: the temperature, and the target while the
// heater is on. A tap opens the keypad for the target.
func (a *App) temps(c *gfx.Canvas, area image.Rectangle, tool, toolT, bed, bedT float64, ok bool) {
	a.region("Temperatures", area)
	h := area.Dy()
	card := func(r image.Rectangle, icon func(cx, cy, r int, col color.RGBA), name string, v, target float64) {
		plate(c, r)
		ic := dim
		if ok && target > 0 {
			ic = orange // heater on
		}
		icon(r.Min.X+34, r.Min.Y+h/2, 15, ic)
		label(c, image.Rect(r.Min.X+62, r.Min.Y+8, r.Max.X-14, r.Min.Y+28), name, gfx.Left)
		val := "—"
		if ok {
			val = fmt.Sprintf("%.0f°", v)
		}
		c.Text(image.Rect(r.Min.X+62, r.Min.Y+26, r.Max.X-10, r.Max.Y-4), val, gfx.Face(true, 30), text, gfx.Left)
		if ok && target > 0 {
			c.Text(image.Rect(r.Min.X+62, r.Min.Y+6, r.Max.X-10, r.Min.Y+28), fmt.Sprintf("SET %.0f°", target), gfx.Face(true, 13), orange, gfx.Right)
		}
	}
	cards := kit.Cols(area, 16, kit.Fr(1), kit.Fr(1))
	nozzle, bedR := cards[0], cards[1]
	card(nozzle, func(cx, cy, r int, col color.RGBA) { drawNozzle(c, cx, cy, r, col) }, "Nozzle", tool, toolT)
	card(bedR, func(cx, cy, r int, col color.RGBA) { drawBed(c, cx, cy, r, col) }, "Bed", bed, bedT)
	if ok { // a tap opens the keypad for the target
		pr := a.Printer
		a.addHit(kit.Hit{R: nozzle, Tap: func() { a.editTemp("Nozzle temperature", int(toolT), 300, toolPresets, pr.SetToolTemp) }})
		a.addHit(kit.Hit{R: bedR, Tap: func() { a.editTemp("Bed temperature", int(bedT), 120, bedPresets, pr.SetBedTemp) }})
	}
}

// drawScrubber is the strip under the model view: a slider through the layers, the layer, and a button to
// go back to the layer that prints now.
func (a *App) drawScrubber(c *gfx.Canvas, box image.Rectangle, st printer.State) {
	if st.Layers == 0 {
		return
	}
	pr := a.Printer
	view := pr.ViewLayer()
	layerOf := func(f float64) int { return int(f*float64(st.Layers) + 0.5) }

	slider := image.Rect(box.Min.X+18, box.Max.Y-36, box.Max.X-18, box.Max.Y-4)
	a.region("Layer slider", slider)
	f := float64(st.Layer) / float64(st.Layers)
	shown := st.Layer
	drag, dragging := a.in.Dragging(slider)
	switch {
	case dragging:
		f = drag
		shown = layerOf(f)
	case view >= 0:
		f, shown = float64(view)/float64(st.Layers), view
	}
	col, label := dim, fmt.Sprintf("Layer %d of %d", shown, st.Layers)
	if view >= 0 || dragging {
		col = orange
	}
	if col == dim {
		col = lcdDim
	}
	c.Text(image.Rect(box.Min.X+16, box.Min.Y+8, box.Min.X+240, box.Min.Y+34), strings.ToUpper(label), gfx.Face(true, 15), col, gfx.Left)
	if view >= 0 {
		live := image.Rect(box.Max.X-100, box.Min.Y+8, box.Max.X-14, box.Min.Y+40)
		a.button(c, live, "Live", orange, onAcc, func() { pr.SetViewLayer(-1) })
	}
	set := func(f float64) { pr.SetViewLayer(layerOf(f)) }
	faderWith(c, slider, f, orange, dayPalette.raised) // a light cap on the dark window
	a.addHit(kit.Hit{R: slider, Drag: set, End: set})
}
