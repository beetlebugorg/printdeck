package ui

import (
	"fmt"
	"image"
	"os"
	"strings"

	"printdeck/internal/chitu"
	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/kit"
)

// PrinterSetup is the part of the setup host that sets the printer of the deck: a printer of the
// Klipper host on USB, or a ChiTu resin printer on the network. A SetupHost with no PrinterSetup
// has the setup of Wi-Fi only.
type PrinterSetup interface {
	// FindChiTu asks the networks of the deck for ChiTu boards. It waits a few seconds.
	FindChiTu() ([]chitu.Info, error)
	// CheckChiTu asks the board at addr for its board and its state. It sends only queries that
	// read.
	CheckChiTu(addr string) (chitu.Info, chitu.Status, error)
	// Printer is the printer of the deck now (printer.json).
	Printer() printer.Setting
	// SetPrinter writes the printer of the deck. The deck starts again with it after Done.
	SetPrinter(s printer.Setting) error
}

// The steps of the printer, between the Wi-Fi and the end of the setup.
const (
	setupPrinter = setupDone + 1 + iota // the kind of the printer
	setupFind                           // the ChiTu boards on the network
	setupBoard                          // the board that the user picked, and the name of the printer
)

// setupPrinterState is the printer of the setup: what the user picked.
type setupPrinterState struct {
	kind    string // printer.KindKlipper or printer.KindChiTu, or "" before the choice
	boards  []chitu.Info
	board   chitu.Info
	status  chitu.Status
	name    string
	finding bool
}

// printerSetup returns the PrinterSetup of the setup host, when it has one.
func (a *App) printerSetup() (PrinterSetup, bool) {
	ps, ok := a.Setup.(PrinterSetup)
	return ps, ok
}

// afterWifi is the step after the Wi-Fi: the printer, or the end with a host that has no
// PrinterSetup.
func (a *App) afterWifi() int {
	if _, ok := a.printerSetup(); ok {
		return setupPrinter
	}
	return setupDone
}

// drawSetupPrinter asks for the kind of the printer.
func (a *App) drawSetupPrinter(c *gfx.Canvas, box image.Rectangle) {
	s := a.setup
	label(c, image.Rect(24, 74, W-24, 100), "Which printer does this deck drive?", gfx.Left)
	rows := kit.Rows(image.Rect(24, 112, W-24, box.Max.Y-140), 16, kit.Fr(1), kit.Fr(1))
	choice := func(r image.Rectangle, title, line, kind string) {
		a.namedKey(c, r, title, func() {
			s.printer.kind = kind
			if kind == printer.KindChiTu {
				s.step = setupFind
				a.findBoards()
				return
			}
			s.step = setupDone
		})
		in := kit.Inset(r, 24, 0, 24, 0)
		t := kit.Rows(in, 6, kit.Fr(1), kit.Px(32), kit.Px(26), kit.Fr(1))
		c.Text(t[1], title, gfx.Face(true, 24), text, gfx.Left)
		c.Text(t[2], gfx.Fit(gfx.Face(false, 17), line, in.Dx()), gfx.Face(false, 17), dim, gfx.Left)
	}
	choice(rows[0], "Printer on USB", "A FDM printer with Klipper: its board on a USB port of the deck", printer.KindKlipper)
	choice(rows[1], "Resin printer on the network", "A ChiTu board, for example a Peopoly Phenom", printer.KindChiTu)
	y := box.Max.Y - 92
	half := (W - 48 - 8) / 2
	a.button(c, image.Rect(24, y, 24+half, y+68), "Back", raised, text, func() {
		if s.printerOnly {
			a.setup = nil // opened from Settings
			return
		}
		s.step = setupWifi
		a.setupScan()
	})
}

// findBoards asks the host for the boards on the network.
func (a *App) findBoards() {
	s := a.setup
	ps, ok := a.printerSetup()
	if !ok || s.printer.finding {
		return
	}
	s.printer.finding, s.msg = true, "Searching the network…"
	go func() {
		boards, err := ps.FindChiTu()
		a.post(func() {
			s.printer.finding, s.printer.boards, s.msg = false, boards, ""
			switch {
			case err != nil:
				s.msg = "The search failed: " + err.Error()
			case len(boards) == 0:
				s.msg = "No printer answered. Check that it is on, or type its address."
			}
		})
	}()
}

// checkBoard asks the board at addr for its state, and shows it.
func (a *App) checkBoard(addr string) {
	s := a.setup
	ps, ok := a.printerSetup()
	if !ok {
		return
	}
	s.busy, s.msg = true, "Asking the printer at "+addr+"…"
	go func() {
		in, st, err := ps.CheckChiTu(addr)
		a.post(func() {
			s.busy = false
			if err != nil {
				s.msg = "No answer from " + addr + ". Check the address."
				return
			}
			if in.IP == "" {
				in.IP = addr
			}
			s.msg, s.step = "", setupBoard
			s.printer.board, s.printer.status = in, st
			if s.printer.name == "" {
				s.printer.name = "Phenom"
			}
		})
	}()
}

// drawSetupFind lists the boards on the network.
func (a *App) drawSetupFind(c *gfx.Canvas, box image.Rectangle) {
	s := a.setup
	head := "Pick the printer."
	if s.printer.finding {
		head = "Searching the network for resin printers…"
	}
	label(c, image.Rect(24, 74, W-24, 100), head, gfx.Left)
	list := image.Rect(24, 112, W-24, box.Max.Y-140)
	window(c, list)
	a.region("Printers", list)
	for i, b := range s.printer.boards {
		y := list.Min.Y + i*setupRowH
		if y+setupRowH > list.Max.Y {
			break
		}
		row := image.Rect(list.Min.X, y, list.Max.X, y+setupRowH)
		if i > 0 {
			c.Fill(image.Rect(row.Min.X+12, y, row.Max.X-12, y+1), lcdCell)
		}
		in := kit.Inset(row, 20, 6, 20, 6)
		t := kit.Rows(in, 0, kit.Fr(1), kit.Fr(1))
		c.Text(t[0], strings.TrimSpace(b.Name+" "+b.Version)+"  ·  "+b.IP, gfx.Face(true, 20), lcdInk, gfx.Left)
		c.Text(t[1], "MAC "+b.MAC, gfx.Face(false, 15), lcdDim, gfx.Left)
		addr := b.IP
		a.addHit(kit.Hit{R: row, Tap: func() {
			if !s.busy {
				a.checkBoard(addr)
			}
		}})
	}
	y := box.Max.Y - 92
	bw := (W - 48 - 16) / 3
	a.button(c, image.Rect(24, y, 24+bw, y+68), "Back", raised, text, func() { s.step, s.msg = setupPrinter, "" })
	a.button(c, image.Rect(24+bw+8, y, 24+2*bw+8, y+68), "Search", raised, text, a.findBoards)
	a.button(c, image.Rect(W-24-bw, y, W-24, y+68), "Address", raised, text, func() {
		s.kb = &keyboard{title: "Address of the printer", max: 64, ok: "Next", cancel: "Back",
			value: s.printer.board.IP,
			back:  func() { s.kb = nil },
			done: func(addr string) {
				s.kb = nil
				if addr = strings.TrimSpace(addr); addr != "" {
					a.checkBoard(addr)
				}
			}}
	})
}

// drawSetupBoard shows the board that the user picked, and its name.
func (a *App) drawSetupBoard(c *gfx.Canvas, box image.Rectangle) {
	s := a.setup
	p := &s.printer
	label(c, image.Rect(24, 74, W-24, 100), "The printer answered.", gfx.Left)
	state := "No print"
	if p.status.Printing() {
		state = fmt.Sprintf("Printing · %.0f%% of the file · Z %.2f mm", p.status.Progress()*100, p.status.Z)
	}
	lines := []string{"Board", strings.TrimSpace(p.board.Name + " " + p.board.Version), "Address", p.board.IP,
		"MAC", p.board.MAC, "Now", state}
	win := image.Rect(24, 112, W-24, 112+2*(2*30+10)+14)
	window(c, win)
	cols := 1
	if W > H {
		cols = 2
	}
	n := len(lines) / 2
	rows := (n + cols - 1) / cols
	win.Max.Y = 112 + rows*(2*30+10) + 14
	for i, cell := range kit.Grid(kit.Inset(win, 16, 12, 16, 12), cols, rows, 10)[:n] {
		r := kit.Rows(cell, 0, kit.Px(30), kit.Px(30))
		c.Text(r[0], strings.ToUpper(lines[2*i]), gfx.Face(true, 13), lcdDim, gfx.Left)
		c.Text(r[1], gfx.Fit(gfx.Face(true, 20), lines[2*i+1], r[1].Dx()), gfx.Face(true, 20), lcdInk, gfx.Left)
	}
	// The name of the printer: a tap changes it.
	nameRow := image.Rect(24, win.Max.Y+16, W-24, win.Max.Y+16+setupRowH)
	a.namedKey(c, nameRow, "Name", func() {
		s.kb = &keyboard{title: "Name of the printer", max: 40, ok: "Done", cancel: "Back", value: p.name,
			back: func() { s.kb = nil },
			done: func(v string) {
				s.kb = nil
				if v = strings.TrimSpace(v); v != "" {
					p.name = v
				}
			}}
	})
	in := kit.Inset(nameRow, 24, 0, 24, 0)
	c.Text(in, "Name", gfx.Face(false, 17), dim, gfx.Left)
	c.Text(in, p.name, gfx.Face(true, 22), text, gfx.Right)

	y := box.Max.Y - 92
	half := (W - 48 - 8) / 2
	a.button(c, image.Rect(24, y, 24+half, y+68), "Back", raised, text, func() { s.step, s.msg = setupFind, "" })
	a.button(c, image.Rect(W-24-half, y, W-24, y+68), "Use this printer", green, onAcc, func() { s.step = setupDone })
}

// printerLine is the printer of the end of the setup, or "" when the setup has no printer step.
func (a *App) printerLine() string {
	p := a.setup.printer
	switch p.kind {
	case printer.KindChiTu:
		return p.name + " · resin · " + p.board.IP
	case printer.KindKlipper:
		return "On USB (Klipper)"
	}
	return ""
}

// savePrinter writes the printer of the setup, when the user picked one.
func (a *App) savePrinter() error {
	ps, ok := a.printerSetup()
	p := a.setup.printer
	if !ok || p.kind == "" {
		return nil
	}
	set := printer.Setting{Kind: p.kind}
	if p.kind == printer.KindChiTu {
		set.Addr, set.MAC, set.Name = p.board.IP, p.board.MAC, p.name
	}
	if set == ps.Printer() {
		return nil
	}
	return ps.SetPrinter(set)
}

// OpenPrinterSetup opens the printer steps of the setup, from the Settings page.
func (a *App) OpenPrinterSetup() {
	if _, ok := a.printerSetup(); !ok {
		return
	}
	host, _ := os.Hostname()
	a.setup = &setupFlow{host: host, step: setupPrinter, printerOnly: true}
	h := a.Setup
	go func() {
		st, _ := h.Status()
		a.post(func() { a.setup.status = st })
	}()
	a.Redraw()
}

// settingLine is the line of the printer of the deck on the Settings page.
func settingLine(s printer.Setting) string {
	if s.Kind == printer.KindChiTu {
		return s.Name + " · resin · " + s.Addr
	}
	return "On USB (Klipper)"
}

// namedKey is a key with its own text on it, which the caller draws: a key with no label, and the
// name for the focus and the parts of the screen.
func (a *App) namedKey(c *gfx.Canvas, r image.Rectangle, name string, tap func()) {
	a.drawAck(c, r)
	key(c, r, "", raised, text, a.isPressed(r))
	acked := func() {
		a.ackR, a.ackAt = r, a.Now()
		tap()
	}
	a.addHit(kit.Hit{R: r, Tap: acked})
	a.addFocus(name, r, acked)
	a.region(name, r)
}
