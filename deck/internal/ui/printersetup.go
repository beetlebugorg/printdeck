package ui

import (
	"image"
	"image/color"
	"strings"

	"golang.org/x/image/font"

	"printdeck/internal/board"
	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// The Print tab while Klipper is not ready: the setup of a new printer, or the error of Klipper.

// drawPrinterSetup draws the Print tab when Klipper has no printer (no [mcu] in printer.cfg). It
// shows the boards that the probe found on the USB ports.
func (a *App) drawPrinterSetup(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	heading(c, "Printer setup")
	boards, probing := pr.Boards()
	if !a.setupProbed {
		a.setupProbed = true
		pr.ProbeBoards()
	}

	y := 120
	para := func(s string, px int, col color.RGBA) {
		face := gfx.Face(false, px)
		for _, l := range wrapText(face, s, W-48) {
			c.Text(image.Rect(24, y, W-24, y+px+10), l, face, col, gfx.Left)
			y += px + 10
		}
	}
	para("Klipper has no printer yet. Connect the printer with USB, and switch it on.", 20, text)
	y += 20

	switch {
	case probing:
		c.Text(image.Rect(24, y, W-24, y+40), "Looking for the printer board…", gfx.Face(true, 22), dim, gfx.Left)
		y += 60
	case len(boards) == 0:
		c.Text(image.Rect(24, y, W-24, y+40), "No printer board found", gfx.Face(true, 22), orange, gfx.Left)
		y += 60
	}
	if !probing {
		for _, b := range boards {
			y = a.drawBoard(c, y, b) + 16
		}
	}

	if !probing {
		a.button(c, image.Rect(24, y, W-24, y+72), "Search again", raised, text, pr.ProbeBoards)
		y += 92
	}
	para("Then copy printer.cfg to the PRINTDECK partition of the disk, and restart the Pi.", 18, dim)
}

// drawBoard draws one probe result in a window, and returns the bottom of the window.
func (a *App) drawBoard(c *gfx.Canvas, y int, b board.Info) int {
	r := image.Rect(24, y, W-24, y+132)
	window(c, r)
	c.Text(image.Rect(r.Min.X+16, y+10, r.Max.X-16, y+44), gfx.Fit(gfx.Face(true, 22), b.Name, r.Dx()-32), gfx.Face(true, 22), lcdInk, gfx.Left)
	small := gfx.Face(false, 18)
	// The window is a dark display: its text is amber (lcdInk), and dimmer for the details.
	if !b.Klipper {
		c.Text(image.Rect(r.Min.X+16, y+50, r.Max.X-16, y+78), "No Klipper firmware on this board", small, lcdInk, gfx.Left)
		c.Text(image.Rect(r.Min.X+16, y+80, r.Max.X-16, y+108), "It needs the Klipper firmware first.", small, lcdDim, gfx.Left)
		return r.Max.Y
	}
	c.Text(image.Rect(r.Min.X+16, y+50, r.Max.X-16, y+78), "MCU  "+b.MCU, small, lcdDim, gfx.Left)
	v, col := "Klipper firmware is current", lcdDim
	if !board.SameVersion(b.Version, printer.HostVersion) {
		v, col = "Klipper firmware is old", lcdInk
	}
	c.Text(image.Rect(r.Min.X+16, y+78, r.Max.X-16, y+106), v, small, col, gfx.Left)
	c.Text(image.Rect(r.Min.X+16, y+104, r.Max.X-16, y+128), gfx.Fit(gfx.Face(false, 14), b.Version, r.Dx()-32), gfx.Face(false, 14), lcdDim, gfx.Left)
	return r.Max.Y
}

// drawKlipperError draws the Print tab when Klipper is not running, or stopped with an error: all
// the lines of the message, and a restart of Klipper, which reads printer.cfg again.
func (a *App) drawKlipperError(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	if st.ErrDetail == "" {
		heading(c, "No Klipper")
	} else {
		heading(c, "Klipper error")
	}
	msg := st.ErrDetail
	if msg == "" {
		msg = st.Err
	}
	face := gfx.Face(false, 18)
	y := 120
	for _, l := range wrapText(face, msg, W-48) {
		if y > 560 {
			break
		}
		c.Text(image.Rect(24, y, W-24, y+26), l, face, text, gfx.Left)
		y += 26
	}
	a.button(c, image.Rect(24, 600, W-24, 680), "Restart Klipper", orange, onAcc, func() {
		a.Toast("Restarting Klipper…")
		a.do(pr.RestartKlipper)
	})
}

// wrapText breaks a text into lines that fit a width. An empty line starts a new paragraph, with
// an empty line before it. The other line breaks of the text become spaces.
func wrapText(face font.Face, s string, width int) []string {
	var out []string
	for i, para := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n\n") {
		if i > 0 {
			out = append(out, "")
		}
		line := ""
		for _, w := range strings.Fields(para) {
			if line != "" && gfx.Width(face, line+" "+w) > width {
				out = append(out, line)
				line = ""
			}
			if line == "" {
				line = w
			} else {
				line += " " + w
			}
		}
		out = append(out, line)
	}
	return out
}
