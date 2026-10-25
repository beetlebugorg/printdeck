package ui

import (
	"image"
	"image/color"
	"strings"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// The simulated printer: Klipper runs the config of a real printer on the simulated board of the
// deck. The page lists the simulated printers and the real printer. While a simulated printer is
// in use, a band at the top of each screen shows it.

// simLine is the line of the Simulated printer row, and its color.
func simLine(st printer.State) (string, color.RGBA) {
	if st.Simulated != "" {
		return "On · " + st.Simulated + " · no real printer moves", orange
	}
	return "Off · try prints with no printer", dim
}

// drawSimulator is the page of the simulated printers. A tap switches Klipper to a simulated
// printer, or back to the real printer, after a question.
func (a *App) drawSimulator(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	f := a.page(c, "Simulated printer")
	use := func(id, question, yes, toast string) func() {
		return func() {
			a.confirm = &confirmBox{question: question, yes: yes, onYes: func() {
				a.Toast(toast)
				a.do(func() error { return pr.UseSim(id) })
			}}
		}
	}
	realSub, realCol := "In use", orange
	if st.Simulated != "" {
		realSub, realCol = "Your printer.cfg, kept as printer.cfg.real", dim
	}
	items := []menuItem{{"Real printer", realSub, realCol, nil}}
	if st.Simulated != "" {
		items[0].tap = use("", "Go back to the real printer?", "Go back", "Restoring printer.cfg…")
	}
	for _, m := range printer.SimModels() {
		sub, col, tap := m.Note, dim, use(m.ID, "Simulate the "+gfx.Fit(gfx.Face(true, 28), m.Name, 300)+"?", "Simulate", "Starting the simulated printer…")
		if m.Name == st.Simulated {
			sub, col, tap = "In use · "+m.Note, orange, nil
		}
		items = append(items, menuItem{"Simulated " + m.Name, sub, col, tap})
	}
	a.menuRowsWait(c, f.body(), f.wide, 76, 8, items, st.Busy())
}

// simBandH is the height of the band of a simulated printer: in the margin at the top of the
// screen, over the display and the titles.
func simBandH() int {
	if W > H {
		return gridM - 2
	}
	return lcdTop - 2
}

// drawSimBand draws the band at the top of the screen while a simulated printer is in use.
func (a *App) drawSimBand(c *gfx.Canvas, st printer.State) {
	if st.Simulated == "" {
		return
	}
	r := image.Rect(0, 0, W, simBandH())
	c.Fill(r, orange)
	face := gfx.Face(true, r.Dy()-3)
	label := "Simulated printer · " + st.Simulated
	if W > H {
		label += " · no real print"
	}
	label = gfx.Fit(face, strings.ToUpper(label), W-16)
	c.Text(r, label, face, onAcc, gfx.Center)
	a.region("Simulated", r)
}
