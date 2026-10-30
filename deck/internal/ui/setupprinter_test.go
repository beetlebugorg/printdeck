package ui

import (
	"errors"
	"testing"
	"time"

	"printdeck/internal/chitu"
	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// printerHost is a setup host with the printer steps, for the tests.
type printerHost struct {
	*DemoSetup
	boards []chitu.Info
	saved  *printer.Setting
	done   bool
}

func (h *printerHost) FindChiTu() ([]chitu.Info, error) { return h.boards, nil }
func (h *printerHost) CheckChiTu(addr string) (chitu.Info, chitu.Status, error) {
	for _, b := range h.boards {
		if b.IP == addr {
			return b, chitu.Status{Z: 12.5, Read: 40, Size: 100}, nil
		}
	}
	return chitu.Info{}, chitu.Status{}, errors.New("no answer")
}
func (h *printerHost) Printer() printer.Setting {
	if h.saved != nil {
		return *h.saved
	}
	return printer.Setting{Kind: printer.KindKlipper}
}
func (h *printerHost) SetPrinter(s printer.Setting) error { h.saved = &s; return nil }
func (h *printerHost) Done()                              { h.done = true }

// tapPart taps the middle of the named part of the last frame: the setup screen names its keys
// as parts.
func tapPart(t *testing.T, a *App, cv *gfx.Canvas, name string) {
	t.Helper()
	a.Draw(cv)
	r, ok := a.KeyRect(name)
	if !ok {
		t.Fatalf("no part %q", name)
	}
	p := r.Min.Add(r.Size().Div(2))
	tapAt(a, cv, p.X, p.Y)
}

// runPost runs the next work that a goroutine of the screen posts.
func runPost(t *testing.T, a *App) {
	t.Helper()
	select {
	case f := <-a.posted:
		f()
	case <-time.After(2 * time.Second):
		t.Fatal("no post")
	}
}

// The printer steps from Settings: a resin printer on the network, the board that the search
// found, its check, and Finish writes the printer.
func TestSetupResinPrinter(t *testing.T) {
	a, _, cv := newPrintApp(idle)
	cv = gfx.New(a.Screen().W, a.Screen().H)
	h := &printerHost{DemoSetup: &DemoSetup{}, boards: []chitu.Info{{MAC: "00:e0:4c:18:00:29", IP: "192.168.76.58", Version: "V1.4.1", Name: "CBD"}}}
	a.Setup = h
	a.OpenPrinterSetup()
	runPost(t, a) // the Wi-Fi status
	a.Draw(cv)
	if a.setup == nil || a.setup.step != setupPrinter {
		t.Fatal("no printer step")
	}
	tapPart(t, a, cv, "Resin printer on the network")
	runPost(t, a) // the search
	if a.setup.step != setupFind || len(a.setup.printer.boards) != 1 {
		t.Fatalf("step %d, boards %+v", a.setup.step, a.setup.printer.boards)
	}
	a.Draw(cv)
	list, ok := a.KeyRect("Printers")
	if !ok {
		t.Fatal("no list of the printers")
	}
	tapAt(a, cv, list.Min.X+40, list.Min.Y+setupRowH/2)
	runPost(t, a) // the check
	if a.setup.step != setupBoard || a.setup.printer.name != "Phenom" {
		t.Fatalf("step %d, name %q, msg %q", a.setup.step, a.setup.printer.name, a.setup.msg)
	}
	tapPart(t, a, cv, "Use this printer")
	a.Draw(cv)
	tapPart(t, a, cv, "Finish")
	want := printer.Setting{Kind: printer.KindChiTu, Addr: "192.168.76.58", MAC: "00:e0:4c:18:00:29", Name: "Phenom"}
	if h.saved == nil || *h.saved != want || !h.done || a.setup != nil {
		t.Fatalf("saved %+v, done %v, setup %v", h.saved, h.done, a.setup != nil)
	}

	// Back on the first step closes the steps that Settings opened.
	a.OpenPrinterSetup()
	runPost(t, a)
	tapPart(t, a, cv, "Back")
	if a.setup != nil {
		t.Error("Back did not close the printer steps")
	}
}
