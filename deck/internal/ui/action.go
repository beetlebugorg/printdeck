package ui

import (
	"fmt"
	"image/color"
	"printdeck/kit"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// A maintenance action (bed tilt, PID tune, a macro) runs in Klipper for seconds or minutes. While
// it runs, the deck shows it over everything, on every screen, and the other keys are off.
// The display of the printer shows it too.

// runAction runs a maintenance action.
func (a *App) runAction(name string, lines ...string) {
	go a.Printer.RunAction(name, lines...)
}

// drawAction is the screen of the action that runs now.
func (a *App) drawAction(c *gfx.Canvas, st printer.State) {
	area, stop := a.takeover(c, st.Action)
	s := kit.NewStack(area, 0)
	win := s.Next(150)
	s.Skip(30)
	window(c, win)
	el := max(0, a.Now().Sub(st.ActionStart))
	r := kit.Rows(kit.Inset(win, 16, 14, 16, 10), 0, kit.Px(36), kit.Px(20), kit.Px(30), kit.Px(10), kit.Px(30))
	c.Text(r[0], "RUNNING", gfx.Face(true, 22), lcdInk, gfx.Left)
	sec := int(el / time.Second)
	c.Text(r[0], fmt.Sprintf("%d:%02d", sec/60, sec%60), gfx.Face(true, 22), lcdInk, gfx.Right)
	// A block that moves to and fro, one step a second.
	const cells = 12
	p := sec % (2*cells - 2)
	if p >= cells {
		p = 2*cells - 2 - p
	}
	sizes := make([]kit.Size, cells)
	for i := range sizes {
		sizes[i] = kit.Fr(1)
	}
	for i, cell := range kit.Cols(r[2], 4, sizes...) {
		col := lcdCell
		if i == p {
			col = lcdInk
		}
		c.RoundRect(cell, 3, col)
	}
	c.Text(r[4], fmt.Sprintf("NOZZLE %.0f°  BED %.0f°", st.Tool, st.Bed), gfx.Face(true, 17), lcdDim, gfx.Left)
	paragraph(c, s, "The printer runs "+st.Action+". The other keys are off until it ends.", text)
	pr := a.Printer
	a.button(c, stop, "Stop the printer", redCap, onRed, func() {
		a.confirm = &confirmBox{question: "Stop the printer now?", yes: "Stop", danger: true, onYes: func() {
			a.do(pr.EmergencyStop)
		}}
	})
}

// paragraph draws a text in the next lines of a stack, broken to its width, and leaves 20 pixels
// under it.
func paragraph(c *gfx.Canvas, s *kit.Stack, t string, col color.RGBA) {
	face := gfx.Face(false, 20)
	width := s.Rest().Dx()
	for _, l := range wrapText(face, t, width) {
		c.Text(s.Next(30), l, face, col, gfx.Left)
	}
	s.Skip(20)
}

// drawActionError is the screen of an action that failed, until OK.
func (a *App) drawActionError(c *gfx.Canvas, r printer.ActionResult) {
	area, ok := a.takeover(c, r.Name)
	s := kit.NewStack(kit.Inset(area, 0, 6, 0, 0), 0)
	c.Text(s.Next(36), "It failed", gfx.Face(true, 24), red, gfx.Left)
	s.Skip(20)
	paragraph(c, s, r.Err, text)
	paragraph(c, s, "Check the printer, then try again.", dim)
	a.button(c, ok, "OK", orange, onAcc, func() { a.actionSeen = r.End })
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
