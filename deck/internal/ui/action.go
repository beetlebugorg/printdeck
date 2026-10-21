package ui

import (
	"fmt"
	"image"
	"image/color"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// A maintenance action (bed tilt, PID tune, a macro) runs in Klipper for seconds or minutes. While
// it runs, the Print tab shows it over everything, on every screen, and the other keys are off.
// The display of the printer shows it too.

// runAction runs a maintenance action. The display of the printer shows the action before it
// starts: Klipper runs one command at a time, so the deck cannot change the display while the
// action runs.
func (a *App) runAction(name string, lines ...string) {
	pr := a.Printer
	var script []string
	if pr.Machine().Mirror {
		script = Mirror{pad(name, ""), pad("Please wait", ""), "work"}.Command()
	}
	script = append(script, lines...)
	go pr.RunAction(name, script...)
}

// drawAction is the screen of the action that runs now.
func (a *App) drawAction(c *gfx.Canvas, st printer.State) {
	heading(c, st.Action)
	y := 124
	win := image.Rect(24, y, W-24, y+150)
	window(c, win)
	el := a.Now().Sub(st.ActionStart)
	if el < 0 {
		el = 0
	}
	c.Text(image.Rect(win.Min.X+16, y+14, win.Max.X-16, y+50), "RUNNING", gfx.Face(true, 22), lcdInk, gfx.Left)
	s := int(el / time.Second)
	c.Text(image.Rect(win.Min.X+16, y+14, win.Max.X-16, y+50), fmt.Sprintf("%d:%02d", s/60, s%60), gfx.Face(true, 22), lcdInk, gfx.Right)
	// A block that moves to and fro, one step a second.
	const cells = 12
	cw := (win.Dx() - 32) / cells
	p := s % (2*cells - 2)
	if p >= cells {
		p = 2*cells - 2 - p
	}
	for i := range cells {
		col := lcdCell
		if i == p {
			col = lcdInk
		}
		x := win.Min.X + 16 + i*cw
		c.RoundRect(image.Rect(x+2, y+70, x+cw-2, y+100), 3, col)
	}
	c.Text(image.Rect(win.Min.X+16, y+110, win.Max.X-16, y+140),
		fmt.Sprintf("NOZZLE %.0f°  BED %.0f°", st.Tool, st.Bed), gfx.Face(true, 17), lcdDim, gfx.Left)

	y = win.Max.Y + 30
	face := gfx.Face(false, 20)
	for _, l := range wrapText(face, "The printer runs "+st.Action+". The other keys are off until it ends.", W-48) {
		c.Text(image.Rect(24, y, W-24, y+30), l, face, text, gfx.Left)
		y += 30
	}
	pr := a.Printer
	a.button(c, image.Rect(24, contentY1-96, W-24, contentY1-16), "Stop the printer", redCap, onRed, func() {
		a.confirm = &confirmBox{question: "Stop the printer now?", yes: "Stop", danger: true, onYes: func() {
			a.do(pr.EmergencyStop)
		}}
	})
}

// drawActionError is the screen of an action that failed, until OK.
func (a *App) drawActionError(c *gfx.Canvas, r printer.ActionResult) {
	heading(c, r.Name)
	y := 130
	c.Text(image.Rect(24, y, W-24, y+36), "It failed", gfx.Face(true, 24), red, gfx.Left)
	y += 56
	face := gfx.Face(false, 20)
	para := func(t string, col color.RGBA) {
		for _, l := range wrapText(face, t, W-48) {
			c.Text(image.Rect(24, y, W-24, y+30), l, face, col, gfx.Left)
			y += 30
		}
		y += 20
	}
	para(r.Err, text)
	para("Check the printer, then try again.", dim)
	a.button(c, image.Rect(24, contentY1-96, W-24, contentY1-16), "OK", orange, onAcc, func() { a.actionSeen = r.End })
}

// actionScreen draws the screen of a running or failed action, and reports that it drew one. The
// end of an action that worked is a short message.
func (a *App) actionScreen(c *gfx.Canvas, st printer.State) bool {
	if st.Action != "" && !st.ManualProbe {
		a.drawAction(c, st)
		return true
	}
	r := st.LastAction
	if r.End.IsZero() || !r.End.After(a.actionSeen) || r.End.Before(a.created) {
		return false
	}
	if r.Err == "" {
		a.actionSeen = r.End
		a.Toast(r.Name + " is done")
		return false
	}
	a.drawActionError(c, r)
	return true
}
