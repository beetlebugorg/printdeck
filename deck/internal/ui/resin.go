package ui

import (
	"fmt"
	"image"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/kit"
)

// The screens of a resin printer (Machine.Process is "resin", for example the Phenom on its ChiTu
// board). A resin printer has no heaters, no filament and no toolhead: the status panel shows the
// height of the plate and the times, and the job screen shows the print with no model. The deck
// sends no command to a resin printer before the test of each control, so the job screen has no
// Pause and no Cancel: the screen of the printer has them.

// resin reports that the printer of the deck is a resin printer.
func (a *App) resin() bool {
	return a.Printer != nil && a.Printer.Machine().Process == printer.ProcessResin
}

// resinReadouts draws the readouts of the status panel of a resin printer in the three cells: the
// height of the plate, the time of the print, and the time left.
func resinReadouts(c *gfx.Canvas, cells []image.Rectangle, st printer.State) {
	plateZ, elapsed, left := "--", "--", "--"
	if st.Reachable && st.HasZ {
		plateZ = fmt.Sprintf("%.1f", st.Z)
	}
	if st.Reachable && st.Printing {
		elapsed = duration(st.Elapsed)
		if st.HasTimeLeft {
			left = duration(st.TimeLeft)
		}
	}
	readout(c, cells[0], "PLATE MM", plateZ, "", 0, false)
	readout(c, cells[1], "TIME", elapsed, "", 0, false)
	readout(c, cells[2], "LEFT", left, "", 0, false)
}

// drawResinJob is the screen of a running print on a resin printer: the status panel, the share
// key, and the window of the print.
func (a *App) drawResinJob(c *gfx.Canvas, st printer.State) {
	note := "Pause and stop on the screen of the printer."
	if W > H {
		a.jobPanel = true
		view, keys := a.statePage(c, st, gfx.Fit(gfx.Face(true, 24), st.Name(), a.navFrame().head.Dx()-20), actH)
		a.jobPanel = false
		a.resinKeys(c, keys, note)
		a.resinView(c, view, st)
		return
	}
	a.jobPanel = true
	a.statusPanel(c, st)
	a.jobPanel = false
	keys, box := a.jobAreas()
	a.resinKeys(c, keys, note)
	a.resinView(c, box, st)
}

// resinKeys draws the row of keys of a resin print: the note at the left, and Share at the right
// when the deck can share.
func (a *App) resinKeys(c *gfx.Canvas, keys image.Rectangle, note string) {
	text := keys
	if a.canShare() {
		k := kit.Cols(keys, 12, kit.Fr(2), kit.Fr(1))
		text = k[0]
		a.shareKey(c, k[1])
	}
	face := gfx.Face(false, 17)
	c.Text(kit.Inset(text, 8, 0, 8, 0), gfx.Fit(face, note, text.Dx()-16), face, dim, gfx.Left)
}

// resinView draws the window of a resin print: the height of the plate and the share of the file
// that the printer read. The deck has no copy of the file, so the window has no model.
func (a *App) resinView(c *gfx.Canvas, box image.Rectangle, st printer.State) {
	window(c, box)
	a.region("Print", box)
	in := kit.Inset(box, 24, 24, 24, 24)
	rows := kit.Rows(in, 10, kit.Fr(1), kit.Px(64), kit.Px(28), kit.Px(14), kit.Px(28), kit.Fr(1))
	big := gfx.Face(true, 56)
	c.Text(rows[1], fmt.Sprintf("Z %.2f mm", st.Z), big, lcdInk, gfx.Center)
	small := gfx.Face(false, 18)
	c.Text(rows[2], fmt.Sprintf("%.0f%% of the file read", st.Completion), small, lcdInk, gfx.Center)
	bar := kit.Inset(rows[3], 40, 2, 40, 2)
	c.RoundRect(bar, 3, lcdCell)
	if w := int(float64(bar.Dx()) * max(0, min(1, st.Completion/100))); w > 0 {
		c.RoundRect(image.Rect(bar.Min.X, bar.Min.Y, bar.Min.X+w, bar.Max.Y), 3, lcdInk)
	}
	c.Text(rows[4], "No model: the deck has no copy of the file.", gfx.Face(false, 15), lcdDim, gfx.Center)
}
