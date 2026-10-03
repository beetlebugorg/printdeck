package ui

import (
	"fmt"
	"image"
	"strings"
	"time"

	"speakerdeck/internal/gfx"
)

func duration(d time.Duration) string {
	m := int(d.Minutes())
	if m >= 60 {
		return fmt.Sprintf("%dh %02dm", m/60, m%60)
	}
	return fmt.Sprintf("%d min", max(m, 1))
}

func (a *App) drawPrint(c *gfx.Canvas) {
	if a.files {
		a.drawFiles(c)
		return
	}
	st := a.Printer.State()
	pr := a.Printer

	if !st.Reachable {
		c.Text(image.Rect(30, 240, W-30, 300), "No OctoPrint", gfx.Face(true, 32), text, gfx.Center)
		c.Text(image.Rect(30, 310, W-30, 350), st.Err, gfx.Face(false, 20), dim, gfx.Center)
		c.Text(image.Rect(30, 350, W-30, 390), "Check OCTOPRINT_API_KEY", gfx.Face(false, 20), dim, gfx.Center)
		return
	}

	// State pill
	col := dim
	switch {
	case st.Paused:
		col = orange
	case st.Printing:
		col = green
	case strings.Contains(strings.ToLower(st.Text), "error"):
		col = red
	}
	pill := image.Rect(W/2-100, 76, W/2+100, 120)
	c.RoundRect(pill, 22, surface)
	c.Text(pill, st.Text, gfx.Face(true, 22), col, gfx.Center)

	if st.Busy() {
		c.Text(image.Rect(30, 140, W-30, 180), st.File, gfx.Face(true, 24), text, gfx.Center)
		c.Text(image.Rect(30, 190, W-30, 290), pct(st.Completion), gfx.Face(true, 96), orange, gfx.Center)
		bar := image.Rect(40, 306, W-40, 324)
		c.RoundRect(bar, 9, raised)
		fill := bar
		fill.Max.X = bar.Min.X + int(st.Completion/100*float64(bar.Dx()))
		c.RoundRect(fill, 9, orange)
		left := "Time left unknown"
		if st.HasTimeLeft {
			left = duration(st.TimeLeft) + " left"
		}
		c.Text(image.Rect(30, 336, W-30, 372), left, gfx.Face(false, 22), dim, gfx.Center)
	}

	a.temps(c, 400, st.Tool, st.ToolTarget, st.Bed, st.BedTarget, st.Connected)

	y := 520
	switch {
	case st.Busy():
		label, fn := "Pause", pr.Pause
		if st.Paused {
			label, fn = "Resume", pr.Resume
		}
		a.button(c, image.Rect(40, y, W-40, y+84), label, orange, onAcc, fn)
		a.button(c, image.Rect(40, y+104, W-40, y+176), "Cancel print", raised, red, func() {
			a.confirm = &confirmBox{question: "Cancel this print?", yes: "Cancel print", danger: true, onYes: pr.Cancel}
		})
	case !st.Connected:
		a.button(c, image.Rect(40, y, W-40, y+84), "Connect printer", orange, onAcc, pr.Connect)
	default:
		a.button(c, image.Rect(40, y, W-40, y+84), "Print a file", orange, onAcc, func() {
			go func() { pr.LoadFiles(); a.Redraw() }()
			a.files = true
		})
		half := (W - 80 - 16) / 2
		a.button(c, image.Rect(40, y+104, 40+half, y+176), fmt.Sprintf("Preheat %d/%d", a.PreheatTool, a.PreheatBed), raised, text, func() { pr.Heat(a.PreheatTool, a.PreheatBed) })
		a.button(c, image.Rect(40+half+16, y+104, W-40, y+176), "Cool down", raised, text, func() { pr.Heat(0, 0) })
	}
}

func (a *App) temps(c *gfx.Canvas, y int, tool, toolT, bed, bedT float64, ok bool) {
	card := func(r image.Rectangle, name string, v, target float64) {
		c.RoundRect(r, 18, surface)
		c.Text(image.Rect(r.Min.X+18, r.Min.Y+10, r.Max.X-18, r.Min.Y+40), name, gfx.Face(false, 18), dim, gfx.Left)
		val := "—"
		if ok {
			val = fmt.Sprintf("%.0f°", v)
		}
		c.Text(image.Rect(r.Min.X+18, r.Min.Y+40, r.Max.X-18, r.Min.Y+96), val, gfx.Face(true, 40), text, gfx.Left)
		if ok && target > 0 {
			c.Text(image.Rect(r.Min.X+18, r.Min.Y+40, r.Max.X-18, r.Min.Y+96), fmt.Sprintf("→ %.0f°", target), gfx.Face(false, 20), orange, gfx.Right)
		}
	}
	half := (W - 80 - 16) / 2
	card(image.Rect(40, y, 40+half, y+100), "Nozzle", tool, toolT)
	card(image.Rect(40+half+16, y, W-40, y+100), "Bed", bed, bedT)
}

func (a *App) drawFiles(c *gfx.Canvas) {
	c.Text(image.Rect(24, 68, W-150, 120), "Choose a file", gfx.Face(true, 28), text, gfx.Left)
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
		fill := surface
		if a.isPressed(r) {
			fill = raised
		}
		c.RoundRect(r, 14, fill)
		c.Text(image.Rect(r.Min.X+16, r.Min.Y, r.Max.X-16, r.Max.Y), f.Name, gfx.Face(false, 22), text, gfx.Left)
		a.addHit(hit{r: r, tap: func() {
			a.confirm = &confirmBox{question: "Print " + gfx.Fit(gfx.Face(true, 28), f.Name, 300) + "?", yes: "Print", onYes: func() {
				a.Printer.PrintFile(f.Path)
				a.files = false
			}}
		}})
	}
}
