package ui

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
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
	switch {
	case a.swap != nil:
		a.drawSwap(c)
		return
	case a.control:
		a.drawControl(c)
		return
	case a.tune:
		a.drawTune(c)
		return
	}
	if a.files {
		a.drawFiles(c)
		return
	}
	st := a.Printer.State()
	pr := a.Printer

	if !st.Reachable {
		if st.NeedsSetup {
			a.drawPrinterSetup(c, st)
		} else {
			a.drawKlipperError(c, st)
		}
		return
	}

	if st.Busy() {
		a.drawJob(c, st)
		return
	}

	// State pill
	col := dim
	if strings.Contains(strings.ToLower(st.Text), "error") {
		col = red
	}
	pill := image.Rect(W/2-100, 76, W/2+100, 120)
	window(c, pill)
	if col == dim {
		col = lcdInk
	}
	c.Text(pill, strings.ToUpper(st.Text), gfx.Face(true, 20), col, gfx.Center)
	if st.FirmwareOld {
		c.Text(image.Rect(24, 124, W-24, 152), "The board firmware is old", gfx.Face(true, 17), orange, gfx.Center)
	}

	a.temps(c, 160, 100, st.Tool, st.ToolTarget, st.Bed, st.BedTarget, st.Connected)

	y := 300
	if !st.Connected {
		a.button(c, image.Rect(40, y, W-40, y+84), "Connect printer", orange, onAcc, pr.Connect)
		// After an emergency stop or an error, Klipper must restart before it does anything.
		a.button(c, image.Rect(40, y+104, W-40, y+176), "Reset printer", redCap, onRed, func() {
			a.confirm = &confirmBox{question: "Restart the printer firmware?", yes: "Reset", danger: true, onYes: func() {
				a.Toast("Restarting the printer…")
				a.do(pr.FirmwareRestart)
			}}
		})
		return
	}
	a.button(c, image.Rect(40, y, W-40, y+84), "Print a file", orange, onAcc, func() {
		go func() { pr.LoadFiles(); a.Redraw() }()
		a.files = true
	})
	half := (W - 80 - 16) / 2
	a.button(c, image.Rect(40, y+104, 40+half, y+176), fmt.Sprintf("Preheat %d/%d", a.PreheatTool, a.PreheatBed), raised, text, func() { pr.Heat(a.PreheatTool, a.PreheatBed) })
	a.button(c, image.Rect(40+half+16, y+104, W-40, y+176), "Cool down", raised, text, func() { pr.Heat(0, 0) })
	a.button(c, image.Rect(40, y+196, 40+half, y+268), "Control", raised, text, func() { a.control = true })
	// After Klipper stops itself (a heater fault, an emergency stop), OctoPrint can still say "Operational",
	// so the reset is always here. It restarts Klipper (FIRMWARE_RESTART); the heaters stay off.
	a.button(c, image.Rect(40, y+288, W-40, y+356), "Reset printer", redCap, onRed, func() {
		a.confirm = &confirmBox{question: "Restart the printer firmware?", yes: "Reset", danger: true, onYes: func() {
			a.Toast("Restarting the printer…")
			a.do(pr.FirmwareRestart)
		}}
	})
	a.button(c, image.Rect(40+half+16, y+196, W-40, y+268), "Filament", raised, text, func() { a.swap = &swapState{} })
}

// drawJob is the screen of a running or paused print: the model, the progress, the temperatures, and the buttons.
// The places on the job screen.
var (
	jobLCD = image.Rect(16, 62, W-16, 154)
	jobBox = image.Rect(40, 164, W-40, 164+350) // the model view, with the scrubber at the bottom
)

// drawJob is the screen of a running or paused print: a character display with the status, the model,
// the progress, the temperatures, and the keys.
func (a *App) drawJob(c *gfx.Canvas, st printer.State) {
	pr := a.Printer

	// The character display: what it does, and the move now with its speed.
	charLCD(c, jobLCD, a.lcdLines(st), 20)

	box := jobBox
	window(c, box)
	if img := pr.Preview(); img != nil {
		draw.Draw(c.RGBA, image.Rectangle{box.Min, box.Min.Add(img.Bounds().Size())}.Intersect(box), img, img.Bounds().Min, draw.Over)
	} else {
		c.Text(image.Rect(box.Min.X, box.Min.Y, box.Max.X, box.Min.Y+260), "LOADING MODEL…", gfx.Face(true, 18), lcdDim, gfx.Center)
	}
	c.Text(image.Rect(box.Min.X+200, box.Min.Y+8, box.Max.X-14, box.Min.Y+30), strings.ToUpper(st.File), gfx.Face(true, 13), lcdDim, gfx.Right)
	a.drawScrubber(c, box, st)

	segments(c, image.Rect(40, box.Max.Y+8, W-40, box.Max.Y+22), st.Completion/100)
	a.temps(c, box.Max.Y+30, 70, st.Tool, st.ToolTarget, st.Bed, st.BedTarget, st.Connected)

	label, fn := "Pause", pr.Pause
	if st.Paused {
		label, fn = "Resume", pr.Resume
	}
	third := (W - 80 - 16) / 3
	a.button(c, image.Rect(40, 626, 40+third, 626+80), label, orange, onAcc, fn)
	a.button(c, image.Rect(40+third+8, 626, 40+2*third+8, 626+80), "Tune", raised, text, func() { a.tune = true })
	a.button(c, image.Rect(W-40-third, 626, W-40, 626+80), "Cancel", raised, red, func() {
		a.confirm = &confirmBox{question: "Cancel this print?", yes: "Cancel print", danger: true, onYes: pr.Cancel}
	})
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
	now := "--"
	if st.HasNow {
		now = lr(strings.ToUpper(st.NowKind), fmt.Sprintf("%.0fMM/S", st.NowSpeed*float64(a.speedFactor(st))/100))
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

func (a *App) drawFiles(c *gfx.Canvas) {
	heading(c, "Choose a file")
	a.button(c, image.Rect(W-130, 70, W-24, 118), "Back", raised, text, func() { a.files = false })
	files := a.Printer.Files()
	if len(files) == 0 {
		c.Text(image.Rect(30, 300, W-30, 350), "No files", gfx.Face(false, 22), dim, gfx.Center)
	}
	for i, f := range files {
		if i >= 8 {
			break
		}
		f := f
		r := image.Rect(24, 136+i*72, W-24, 136+i*72+64)
		plate(c, r)
		if a.isPressed(r) {
			c.RoundRect(r.Inset(1), 4, raised)
		}
		c.Text(image.Rect(r.Min.X+16, r.Min.Y, r.Max.X-16, r.Max.Y), f.Name, gfx.Face(false, 22), text, gfx.Left)
		a.addHit(hit{r: r, tap: func() {
			a.confirm = &confirmBox{question: "Print " + gfx.Fit(gfx.Face(true, 28), f.Name, 300) + "?", yes: "Print", onYes: func() {
				a.Printer.PrintFile(f.Path)
				a.files = false
			}}
		}})
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
