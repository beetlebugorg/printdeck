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
	wide, textArea, infoArea := a.plain(c, "Printer setup")
	boards, probing := pr.Boards()
	if !a.setupProbed {
		a.setupProbed = true
		pr.ProbeBoards()
	}
	// The text and the keys go in the left column of a wide screen, the boards in the right one. A
	// tall screen has them all in one column, in this order.
	left := kit.NewStack(textArea, 0)
	right := left
	if wide {
		right = kit.NewStack(infoArea, 0)
	}
	para := func(s *kit.Stack, t string, px int, col color.RGBA) {
		face := gfx.Face(false, px)
		for _, l := range wrapText(face, t, s.Rest().Dx()) {
			c.Text(s.Next(px+10), l, face, col, gfx.Left)
		}
	}
	para(left, "Klipper has no printer yet. Connect the printer with USB, and switch it on.", 20, text)
	left.Skip(20)

	switch {
	case probing:
		c.Text(right.Next(40), "Looking for the printer board…", gfx.Face(true, 22), dim, gfx.Left)
		right.Skip(20)
	case len(boards) == 0:
		c.Text(right.Next(40), "No printer board found", gfx.Face(true, 22), orange, gfx.Left)
		right.Skip(20)
	}
	if !probing {
		for i, b := range boards {
			r := right.Next(132)
			a.drawBoard(c, r, b)
			if i == 0 {
				a.region("Board", r)
			}
			right.Skip(16)
		}
	}

	// The keys: under the boards, or at the bottom of the left column.
	var rows [][]func(r image.Rectangle)
	if !probing && len(boards) > 0 {
		b := boards[0]
		for _, x := range boards {
			if x.Klipper {
				b = x
				break
			}
		}
		if (!b.Klipper || !board.SameVersion(b.Version, printer.HostVersion)) && board.CanFlash(b.MCU) {
			rows = append(rows, []func(image.Rectangle){func(r image.Rectangle) {
				a.button(c, r, "Install Klipper firmware", orange, onAcc, func() { a.askFlash(b.Device) })
			}})
		}
		rows = append(rows, []func(image.Rectangle){func(r image.Rectangle) {
			a.button(c, r, "Choose printer", orange, onAcc, func() {
				a.configBoard, a.configScroll, a.configAll = b, 0, false
				a.open(pageConfigs)
			})
		}, func(r image.Rectangle) { a.button(c, r, "Search again", raised, text, pr.ProbeBoards) }})
	} else if !probing {
		rows = append(rows, []func(image.Rectangle){func(r image.Rectangle) { a.button(c, r, "Search again", raised, text, pr.ProbeBoards) }})
	}
	// Wi-Fi and help, and Software update: this screen has no pages.
	if a.Setup != nil || a.Updater != nil {
		rows = append(rows, []func(image.Rectangle){func(r image.Rectangle) { a.systemKeys(c, r) }})
	}
	keys := right
	if wide {
		h := len(rows)*92 - 20
		keys = kit.NewStack(kit.Rows(textArea, 0, kit.Fr(1), kit.Px(max(0, h)))[1], 20)
	}
	for _, row := range rows {
		sizes := make([]kit.Size, len(row))
		for i := range sizes {
			sizes[i] = kit.Fr(1)
		}
		for i, r := range kit.Cols(keys.Next(72), 12, sizes...) {
			row[i](r)
		}
		if !wide {
			keys.Skip(20)
		}
	}
	own, ownText := right.Y(), "Or copy your own printer.cfg to the PRINTDECK partition of the disk, and restart the Pi."
	ownW := right.Rest().Dx()
	para(right, ownText, 18, dim)
	a.textRegion("Your own config", image.Rect(right.Rest().Min.X, own, right.Rest().Max.X, right.Y()), gfx.Face(false, 18), wrapText(gfx.Face(false, 18), ownText, ownW)...)
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
		a.textRegion("Chip", what, face, wrapText(face, msg, what.Dx())...)
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
		if i == 0 {
			a.region("Printer", r)
		}
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

// drawBoard draws one probe result in a window r.
func (a *App) drawBoard(c *gfx.Canvas, r image.Rectangle, b board.Info) {
	y := r.Min.Y
	window(c, r)
	c.Text(image.Rect(r.Min.X+16, y+10, r.Max.X-16, y+44), gfx.Fit(gfx.Face(true, 22), b.Name, r.Dx()-32), gfx.Face(true, 22), lcdInk, gfx.Left)
	small := gfx.Face(false, 18)
	// The window is a dark display: its text is amber (lcdInk), and dimmer for the details.
	if !b.Klipper {
		c.Text(image.Rect(r.Min.X+16, y+50, r.Max.X-16, y+78), "No Klipper firmware on this board", small, lcdInk, gfx.Left)
		c.Text(image.Rect(r.Min.X+16, y+80, r.Max.X-16, y+108), "It needs the Klipper firmware first.", small, lcdDim, gfx.Left)
		return
	}
	c.Text(image.Rect(r.Min.X+16, y+50, r.Max.X-16, y+78), "MCU  "+b.MCU, small, lcdDim, gfx.Left)
	v, col := "Klipper firmware is current", lcdDim
	if !board.SameVersion(b.Version, printer.HostVersion) {
		v, col = "Klipper firmware is old", lcdInk
	}
	c.Text(image.Rect(r.Min.X+16, y+78, r.Max.X-16, y+106), v, small, col, gfx.Left)
	c.Text(image.Rect(r.Min.X+16, y+104, r.Max.X-16, y+128), gfx.Fit(gfx.Face(false, 14), b.Version, r.Dx()-32), gfx.Face(false, 14), lcdDim, gfx.Left)
}

// drawStarting draws the start screen while Klipper starts and connects to the printer. It is the
// screen of the boot, so the deck goes from the boot to its first screen with no error between.
func (a *App) drawStarting(c *gfx.Canvas, st printer.State) {
	version := ""
	if a.Updater != nil {
		version = a.Updater.Status().Current
	}
	drawStart(c, version)
}

// drawKlipperError draws the screen when Klipper is not running, or stopped with an error: all
// the lines of the message, and a restart of Klipper, which reads printer.cfg again.
func (a *App) drawKlipperError(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	title := "Klipper error"
	if st.ErrDetail == "" {
		title = "No Klipper"
	}
	msg := st.ErrDetail
	if msg == "" {
		msg = st.Err
	}
	// The message, the keys under it, and Wi-Fi and help and Software update; at the bottom of the
	// left column of a wide screen.
	wide, textArea, info := a.plain(c, title)
	var keys, sys image.Rectangle
	if wide {
		r := kit.Rows(textArea, 12, kit.Fr(1), kit.Px(72), kit.Px(80))
		sys, keys = r[1], r[2]
	} else {
		r := kit.Rows(info, 0, kit.Px(440), kit.Fr(1), kit.Px(80), kit.Px(12), kit.Px(72), kit.Px(76))
		info, keys, sys = r[0], r[2], r[4]
	}
	a.systemKeys(c, sys)
	face := gfx.Face(false, 18)
	s := kit.NewStack(info, 0)
	for _, l := range wrapText(face, msg, info.Dx()) {
		if s.Y()+26 > info.Max.Y {
			break
		}
		c.Text(s.Next(26), l, face, text, gfx.Left)
	}
	a.textRegion("Message", image.Rect(info.Min.X, info.Min.Y, info.Max.X, s.Y()), face, wrapText(face, msg, info.Dx())...)
	restart, cap, ink := keys, orange, onAcc
	if strings.Contains(strings.ToLower(msg), "shutdown") || strings.Contains(msg, "older version of the") && board.CanFlash("") {
		k := kit.Cols(keys, 12, kit.Fr(1), kit.Fr(1))
		restart, cap, ink = k[0], raised, text
		if strings.Contains(strings.ToLower(msg), "shutdown") {
			// The board stopped itself. Only a reset of the board clears this state.
			a.button(c, k[1], "Firmware restart", orange, onAcc, func() {
				a.Toast("Restarting the firmware…")
				a.do(pr.FirmwareRestart)
			})
		} else {
			a.button(c, k[1], "Write firmware", orange, onAcc, func() { a.askFlash("") })
		}
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
