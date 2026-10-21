package ui

import (
	"image"
	"image/color"
	"printdeck/kit"
	"sort"
	"strings"

	"golang.org/x/image/font"

	"printdeck/internal/board"
	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// The screen while Klipper is not ready: the setup of a new printer, or the error of Klipper.

// drawPrinterSetup draws the screen when Klipper has no printer (no [mcu] in printer.cfg). It
// shows the boards that the probe found on the USB ports.
func (a *App) drawPrinterSetup(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	heading(c, "Printer setup")
	boards, probing := pr.Boards()
	if !a.setupProbed {
		a.setupProbed = true
		pr.ProbeBoards()
	}

	y := 74
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

	if !probing && len(boards) > 0 {
		b := boards[0]
		for _, x := range boards {
			if x.Klipper {
				b = x
				break
			}
		}
		if (!b.Klipper || !board.SameVersion(b.Version, printer.HostVersion)) && board.CanFlash(b.MCU) {
			a.button(c, image.Rect(24, y, W-24, y+72), "Install Klipper firmware", orange, onAcc, func() { a.askFlash(b.Device) })
			y += 92
		}
		half := (W - 48 - 12) / 2
		a.button(c, image.Rect(24, y, 24+half, y+72), "Choose printer", orange, onAcc, func() {
			a.configBoard, a.configScroll, a.configAll = b, 0, false
			a.open(pageConfigs)
		})
		a.button(c, image.Rect(W-24-half, y, W-24, y+72), "Search again", raised, text, pr.ProbeBoards)
		y += 92
	} else if !probing {
		a.button(c, image.Rect(24, y, W-24, y+72), "Search again", raised, text, pr.ProbeBoards)
		y += 92
	}
	para("Or copy your own printer.cfg to the PRINTDECK partition of the disk, and restart the Pi.", 18, dim)
}

// drawConfigs is the list of Klipper's example configs for complete printers. It shows the configs
// for the chip of the board first. A tap writes the config as printer.cfg, after a question.
func (a *App) drawConfigs(c *gfx.Canvas) {
	pr := a.Printer
	f := a.page(c, "Your printer")
	b := a.configBoard
	// The printers of the brand of the board come first.
	brand := printer.SuggestedBrand(b.Name)
	var list, rest []printer.Example
	for _, e := range printer.Examples() {
		switch {
		case !a.configAll && b.MCU != "" && !e.Fits(b.MCU):
		case brand != "" && e.Brand == brand && (b.MCU == "" || e.Fits(b.MCU)):
			list = append(list, e)
		default:
			rest = append(rest, e)
		}
	}
	// The newest printers of the brand first.
	sort.SliceStable(list, func(i, j int) bool { return list[i].Year > list[j].Year })
	suggested := len(list)
	list = append(list, rest...)
	// The board and the key for all printers: over and under the list on a tall screen, in the
	// left column of a wide screen.
	area := a.listArea(f)
	var what, all image.Rectangle
	if b.MCU != "" {
		if f.wide {
			r := kit.Rows(f.aside(), 12, kit.Px(56), kit.Px(64))
			what, all = r[0], r[1]
		} else {
			r := kit.Rows(kit.Inset(area, 0, -4, 0, 0), 2, kit.Px(28), kit.Fr(1), kit.Px(72))
			what, area, all = r[0], kit.Inset(r[1], 0, 0, 0, 8), kit.Inset(r[2], 0, 8, 0, 0)
		}
		msg := "Printers for your " + b.MCU + " board"
		label := "Show all printers"
		if a.configAll {
			msg, label = "All printers. Your board: "+b.MCU, "Only for this board"
		}
		face := gfx.Face(false, 16)
		for i, l := range wrapText(face, msg, what.Dx()) {
			c.Text(image.Rect(what.Min.X, what.Min.Y+i*24, what.Max.X, what.Min.Y+i*24+28), l, face, dim, gfx.Left)
		}
		a.button(c, all, label, raised, text, func() { a.configAll, a.configScroll = !a.configAll, 0 })
	}
	l := kit.List{Area: area, N: len(list), RowH: 76, Gap: 8, Scroll: &a.configScroll}
	draw := func(c *gfx.Canvas, i int, r image.Rectangle) {
		e := list[i]
		sub := e.Year
		if e.MCU != "" {
			sub += " · " + e.MCU
		}
		if i < suggested {
			sub = "Suggested · " + sub
		}
		col := dim
		if e.Fits(b.MCU) {
			col = orange
		}
		a.menuRow(c, r, e.Name(), sub, col, nil)
	}
	choose := func(i int) {
		e := list[i]
		a.confirm = &confirmBox{question: "Use the config of the " + gfx.Fit(gfx.Face(true, 28), e.Name(), 300) + "?", yes: "Use", onYes: func() {
			a.Toast("Writing printer.cfg…")
			a.do(func() error {
				if err := pr.UseConfig(e.File, b.Device); err != nil {
					return err
				}
				a.post(func() { a.pages = nil })
				return nil
			})
		}}
	}
	a.list(c, l, func(i int) string { return list[i].Name() }, draw, choose)
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

// drawKlipperError draws the screen when Klipper is not running, or stopped with an error: all
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
	y := 74
	for _, l := range wrapText(face, msg, W-48) {
		if y > 560 {
			break
		}
		c.Text(image.Rect(24, y, W-24, y+26), l, face, text, gfx.Left)
		y += 26
	}
	restart, cap, ink := image.Rect(24, 554, W-24, 634), orange, onAcc
	if strings.Contains(strings.ToLower(msg), "shutdown") {
		// The board stopped itself. Only a reset of the board clears this state.
		half := (W - 48 - 12) / 2
		restart.Max.X, cap, ink = 24+half, raised, text
		a.button(c, image.Rect(W-24-half, 554, W-24, 634), "Firmware restart", orange, onAcc, func() {
			a.Toast("Restarting the firmware…")
			a.do(pr.FirmwareRestart)
		})
	} else if strings.Contains(msg, "older version of the") && board.CanFlash("") {
		half := (W - 48 - 12) / 2
		restart.Max.X, cap, ink = 24+half, raised, text
		a.button(c, image.Rect(W-24-half, 554, W-24, 634), "Write firmware", orange, onAcc, func() { a.askFlash("") })
	}
	a.button(c, restart, "Restart Klipper", cap, ink, func() {
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
