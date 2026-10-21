package ui

import (
	"fmt"
	"image"
	"image/color"
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
		if st.NeedsSetup {
			a.drawPrinterSetup(c, st)
		} else {
			a.drawKlipperError(c, st)
		}
		return
	}
	if st.Busy() {
		switch {
		case st.Paused:
			a.drawPaused(c, st)
		case st.Preparing:
			a.drawPreparing(c, st)
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

// jobBox is the model view of the job screen, with the layer slider at its bottom edge, down to
// the tabs.
func jobBox() image.Rectangle { return image.Rect(24, belowPanel+keyRowH+12, W-24, contentY1-8) }

// drawJob is the screen of a running print: the status display, the keys, and the model view.
func (a *App) drawJob(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	a.statusPanel(c, st)

	third := (W - 48 - 16) / 3
	y := belowPanel
	a.button(c, image.Rect(24, y, 24+third, y+keyRowH), "Pause", orange, onAcc, pr.Pause)
	a.button(c, image.Rect(24+third+8, y, 24+2*third+8, y+keyRowH), "Tune", raised, text, func() { a.tune = true })
	a.button(c, image.Rect(W-24-third, y, W-24, y+keyRowH), "Cancel", raised, red, func() {
		a.confirm = &confirmBox{question: "Cancel this print?", yes: "Cancel print", danger: true, onYes: pr.Cancel}
	})

	box := jobBox()
	a.modelView(c, box, st)
	a.drawScrubber(c, box, st)
}

// modelView draws the model in a window, with the move now and its speed at the top right.
func (a *App) modelView(c *gfx.Canvas, box image.Rectangle, st printer.State) {
	window(c, box)
	if img := a.Printer.Preview(); img != nil {
		b := img.Bounds()
		c.Clipped(box.Inset(2), func(in *gfx.Canvas) {
			in.Image(image.Rect(box.Min.X+(box.Dx()-b.Dx())/2, box.Min.Y+(box.Dy()-b.Dy())/2, box.Max.X, box.Max.Y), img)
		})
	} else {
		c.Text(box, "LOADING MODEL…", gfx.Face(true, 18), lcdDim, gfx.Center)
	}
	if st.HasNow {
		now := fmt.Sprintf("%s %.0f MM/S", strings.ToUpper(st.NowKind), st.NowSpeed*float64(a.speedFactor(st))/100)
		c.Text(image.Rect(box.Min.X+200, box.Min.Y+8, box.Max.X-14, box.Min.Y+30), now, gfx.Face(true, 13), lcdDim, gfx.Right)
	}
}

// speedFactor is the speed in percent that the tune sheet set for this print.
func (a *App) speedFactor(st printer.State) int {
	if a.tuneFile == st.FilePath && a.tuneSpeed > 0 {
		return a.tuneSpeed
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
		now = lr("DONE AT", a.Now().Add(st.TimeLeft).Format("15:04"))
	}
	return []string{lr(strings.ToUpper(st.Activity), right), now}
}

func (a *App) temps(c *gfx.Canvas, y, h int, tool, toolT, bed, bedT float64, ok bool) {
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
	half := (W - 80 - 16) / 2
	nozzle, bedR := image.Rect(40, y, 40+half, y+h), image.Rect(40+half+16, y, W-40, y+h)
	card(nozzle, func(cx, cy, r int, col color.RGBA) { drawNozzle(c, cx, cy, r, col) }, "Nozzle", tool, toolT)
	card(bedR, func(cx, cy, r int, col color.RGBA) { drawBed(c, cx, cy, r, col) }, "Bed", bed, bedT)
	if ok { // a tap opens the keypad for the target
		pr := a.Printer
		a.addHit(hit{r: nozzle, tap: func() { a.editTemp("Nozzle temperature", int(toolT), 300, toolPresets, pr.SetToolTemp) }})
		a.addHit(hit{r: bedR, tap: func() { a.editTemp("Bed temperature", int(bedT), 120, bedPresets, pr.SetBedTemp) }})
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
	f := float64(st.Layer) / float64(st.Layers)
	shown := st.Layer
	switch {
	case a.dragging != nil && a.dragging.r == slider:
		f = a.dragFrac
		shown = layerOf(f)
	case view >= 0:
		f, shown = float64(view)/float64(st.Layers), view
	}
	col, label := dim, fmt.Sprintf("Layer %d of %d", shown, st.Layers)
	if view >= 0 || (a.dragging != nil && a.dragging.r == slider) {
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
	a.addHit(hit{r: slider, drag: set, end: set})
}
