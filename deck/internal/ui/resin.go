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
// height of the plate and the times, and the job screen shows the print with no model.

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

// drawResinJob is the screen of a running print on a resin printer: the status panel, the keys,
// and the window of the print.
func (a *App) drawResinJob(c *gfx.Canvas, st printer.State) {
	if W > H {
		a.jobPanel = true
		view, keys := a.statePage(c, st, gfx.Fit(gfx.Face(true, 24), st.Name(), a.navFrame().head.Dx()-20), actH)
		a.jobPanel = false
		a.resinKeys(c, keys, st)
		a.resinView(c, view, st)
		return
	}
	a.jobPanel = true
	a.statusPanel(c, st)
	a.jobPanel = false
	keys, box := a.jobAreas()
	a.resinKeys(c, keys, st)
	a.resinView(c, box, st)
}

// resinKeys draws the row of keys of a resin print: Stop at the left, Share, and Pause or Resume
// at the right.
func (a *App) resinKeys(c *gfx.Canvas, keys image.Rectangle, st printer.State) {
	pr := a.Printer
	sizes := []kit.Size{kit.Fr(1), kit.Fr(1)}
	if a.canShare() {
		sizes = append(sizes, kit.Fr(1))
	}
	k := kit.Cols(keys, 12, sizes...)
	a.button(c, k[0], "Stop", raised, red, func() {
		a.confirm = &confirmBox{question: "Stop this print?", yes: "Stop print", danger: true, onYes: pr.Cancel}
	})
	if a.canShare() {
		a.shareKey(c, k[1])
	}
	last := k[len(k)-1]
	if st.Paused {
		a.button(c, last, "Resume", orange, onAcc, pr.Resume)
	} else {
		a.button(c, last, "Pause", orange, onAcc, pr.Pause)
	}
}

// resinView draws the window of a resin print: the preview of the file when the deck has a copy
// of it, the height of the plate, and the share of the file that the printer read.
func (a *App) resinView(c *gfx.Canvas, box image.Rectangle, st printer.State) {
	window(c, box)
	a.region("Print", box)
	if img := a.Printer.FilePicture(printer.File{Path: st.Title}, ""); img != nil && box.Dy() > 260 {
		pic, rest := kit.Rows(box, 0, kit.Fr(1), kit.Px(200))[0], kit.Rows(box, 0, kit.Fr(1), kit.Px(200))[1]
		c.Cover(kit.Inset(pic, 16, 16, 16, 0), img)
		box = rest
	}
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
	if a.Printer.FilePicture(printer.File{Path: st.Title}, "") == nil {
		c.Text(rows[4], "No preview: the deck has no copy of the file.", gfx.Face(false, 15), lcdDim, gfx.Center)
	}
}

// drawResinOffline is the screen of a resin printer that does not answer: "Connecting" at the start
// of the deck, then "Printer not answering" with the address, a key to change the printer, and
// Wi-Fi and help and Software update.
func (a *App) drawResinOffline(c *gfx.Canvas, st printer.State) {
	title, msg := "Connecting", "Connecting to the printer…"
	if st.Err != "" {
		title = "Printer not answering"
		msg = st.Err + ". Check that the printer is on and on the network. The deck tries again every 10 seconds."
	}
	wide, textArea, info := a.plain(c, title)
	n := len(a.systemKeyList(c))
	sysH := max(0, n*keyH+(n-1)*12)
	var keys, sys image.Rectangle
	if wide {
		r := kit.Rows(textArea, 12, kit.Fr(1), kit.Px(72), kit.Px(sysH))
		keys, sys = r[1], r[2]
	} else {
		r := kit.Rows(info, 12, kit.Fr(1), kit.Px(72), kit.Px(sysH))
		info, keys, sys = r[0], r[1], r[2]
	}
	a.systemKeys(c, sys)
	face := gfx.Face(false, 20)
	s := kit.NewStack(info, 0)
	for _, l := range wrapText(face, msg, info.Dx()) {
		c.Text(s.Next(30), l, face, text, gfx.Left)
	}
	if _, ok := a.printerSetup(); ok {
		a.button(c, keys, "Printer", raised, text, a.OpenPrinterSetup)
	}
}
