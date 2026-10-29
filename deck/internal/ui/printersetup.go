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
			h := 132
			if gfx.FinePrint() {
				h = boardH(b, right.Rest().Dx()-32)
			}
			r := right.Next(h)
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
	// A simulated printer, to try the deck with no printer: in the rows of keys on a tall screen,
	// under the text of the right column on a wide screen.
	simKey := func(r image.Rectangle) {
		a.button(c, r, "Simulated printer", raised, text, func() { a.open(pageSimulator) })
	}
	if !wide {
		rows = append(rows, []func(image.Rectangle){simKey})
	}
	// Wi-Fi and help, and Software update: this screen has no pages.
	if sk := a.systemKeyList(c); len(sk) > 0 {
		rows = append(rows, sk)
	}
	keys, rowH := right, 72
	if wide {
		// The narrow column: each key in its own row, at the bottom of the column.
		var flat [][]func(image.Rectangle)
		for _, row := range rows {
			for _, k := range row {
				flat = append(flat, []func(image.Rectangle){k})
			}
		}
		rows, rowH = flat, 56
		h := len(rows)*(rowH+12) - 12
		keys = kit.NewStack(kit.Rows(textArea, 0, kit.Fr(1), kit.Px(max(0, h)))[1], 12)
	}
	for _, row := range rows {
		sizes := make([]kit.Size, len(row))
		for i := range sizes {
			sizes[i] = kit.Fr(1)
		}
		for i, r := range kit.Cols(keys.Next(rowH), 12, sizes...) {
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
	if wide {
		right.Skip(12)
		para(right, "Or try the deck with a simulated printer.", 18, dim)
		right.Skip(8)
		simKey(kit.Cols(right.Next(56), 12, kit.Fr(1), kit.Fr(1))[0])
	}
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
	if gfx.FinePrint() {
		y += 8
		for _, l := range boardLines(b, r.Dx()-32) {
			c.Text(image.Rect(r.Min.X+16, y, r.Max.X-16, y+l.h), l.s, l.face, l.col, gfx.Left)
			y += l.h
		}
		return
	}
	c.Text(image.Rect(r.Min.X+16, y+10, r.Max.X-16, y+44), b.Name, gfx.Face(true, 22), lcdInk, gfx.Left)
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
	c.Text(image.Rect(r.Min.X+16, y+104, r.Max.X-16, y+128), b.Version, gfx.Face(false, 14), lcdDim, gfx.Left)
}

// boardLine is a line of the window of a board with the large text.
type boardLine struct {
	s    string
	face font.Face
	col  color.RGBA
	h    int
}

// boardLines are the lines of the window of a board with the large text, for the width w: the
// name and the version can have two lines.
func boardLines(b board.Info, w int) []boardLine {
	var out []boardLine
	name, small := gfx.Face(true, 22), gfx.Face(false, 18)
	lines := wrapText(name, b.Name, w)
	for _, l := range lines[:min(2, len(lines))] {
		out = append(out, boardLine{l, name, lcdInk, 32})
	}
	if !b.Klipper {
		return append(out, boardLine{"No Klipper firmware on this board", small, lcdInk, 28},
			boardLine{"It needs the Klipper firmware first.", small, lcdDim, 28})
	}
	v, col := "Klipper firmware is current", lcdDim
	if !board.SameVersion(b.Version, printer.HostVersion) {
		v, col = "Klipper firmware is old", lcdInk
	}
	out = append(out, boardLine{"MCU  " + b.MCU, small, lcdDim, 28}, boardLine{v, small, col, 28})
	ver := gfx.Face(false, 14)
	for _, l := range dashWrap(ver, b.Version, w) {
		out = append(out, boardLine{l, ver, lcdDim, 26})
	}
	return out
}

// boardH is the height of the window of a board with the large text, for text of the width w.
func boardH(b board.Info, w int) int {
	h := 8 + 8
	for _, l := range boardLines(b, w) {
		h += l.h
	}
	return h
}

// dashWrap breaks a text with no spaces, such as a version, into at most two lines that fit in
// width. The first line ends after a dash.
func dashWrap(face font.Face, s string, width int) []string {
	if gfx.Width(face, s) <= width {
		return []string{s}
	}
	cut := 0
	for i, r := range s {
		if r == '-' && gfx.Width(face, s[:i+1]) <= width {
			cut = i + 1
		}
	}
	if cut == 0 {
		return []string{s}
	}
	return []string{s[:cut], s[cut:]}
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
	var keys, simKey, sys image.Rectangle
	if wide {
		// The left column is narrow: the key of the simulated printers has its own row, and the
		// system keys go under each other (systemKeys).
		n := len(a.systemKeyList(c))
		r := kit.Rows(textArea, 12, kit.Fr(1), kit.Px(72), kit.Px(max(0, n*keyH+(n-1)*12)), kit.Px(80))
		simKey, sys, keys = r[1], r[2], r[3]
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
	// The keys: a restart of Klipper, a key for the cause of the error, and a simulated printer
	// to try the deck while the real printer does not work.
	type key struct {
		label    string
		cap, ink color.RGBA
		tap      func()
	}
	restart := key{"Restart Klipper", orange, onAcc, func() {
		a.Toast("Restarting Klipper…")
		a.do(pr.RestartKlipper)
	}}
	var cause *key
	shutdown := strings.Contains(strings.ToLower(msg), "shutdown")
	switch {
	case shutdown:
		// The board stopped itself. Only a reset of the board clears this state.
		cause = &key{"Firmware restart", orange, onAcc, func() {
			a.Toast("Restarting the firmware…")
			a.do(pr.FirmwareRestart)
		}}
	case st.Simulated != "":
		// The page of the simulated printers puts the real printer.cfg back.
		cause = &key{"Real printer", orange, onAcc, func() { a.open(pageSimulator) }}
	case strings.Contains(msg, "older version of the") && board.CanFlash(""):
		cause = &key{"Write firmware", orange, onAcc, func() { a.askFlash("") }}
	}
	row := []key{restart}
	if cause != nil {
		row[0].cap, row[0].ink = raised, text
		row = append(row, *cause)
	}
	if st.Simulated == "" {
		sim := key{"Simulated printer", raised, text, func() { a.open(pageSimulator) }}
		if wide {
			a.button(c, simKey, sim.label, sim.cap, sim.ink, sim.tap)
		} else {
			row = append(row, sim)
		}
	}
	var tracks []kit.Size
	for range row {
		tracks = append(tracks, kit.Fr(1))
	}
	for i, r := range kit.Cols(keys, 12, tracks...) {
		a.button(c, r, row[i].label, row[i].cap, row[i].ink, row[i].tap)
	}
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
