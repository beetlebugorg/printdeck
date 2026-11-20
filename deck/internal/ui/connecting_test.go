package ui

import (
	"strings"
	"testing"
	"time"

	"printdeck/internal/printer"
)

// A Bambu Lab printer that does not answer shows "Connecting" for connectGrace, then "Printer not
// answering". It never shows a screen of Klipper. A refused access code shows at once.
func TestConnectingScreen(t *testing.T) {
	now := time.Date(2026, 11, 19, 20, 0, 0, 0, time.UTC)
	p := newX2D()
	p.State_ = printer.State{Text: "Offline"}
	a := New()
	a.Printer = p
	a.Now = func() time.Time { return now }
	f := strings.ToUpper(frameText(a))
	if !strings.Contains(f, "CONNECTING") || strings.Contains(f, "KLIPPER") {
		t.Fatalf("at the start: %s", f)
	}
	p.State_.Err = "No answer from the printer at 192.168.77.171"
	now = now.Add(20 * time.Second)
	if f := strings.ToUpper(frameText(a)); !strings.Contains(f, "CONNECTING") {
		t.Error("no Connecting in the grace time")
	}
	now = now.Add(15 * time.Second)
	f = strings.ToUpper(frameText(a))
	if !strings.Contains(f, "PRINTER NOT ANSWERING") || strings.Contains(f, "KLIPPER") {
		t.Errorf("after the grace time: %s", f)
	}
	shot(t, a, "offline")
	// A link again, then a refused access code: the error at once.
	p.State_ = printer.State{Reachable: true, Connected: true, Text: "Ready"}
	frameText(a)
	p.State_ = printer.State{Text: "Offline", Err: "The printer refused the access code", ErrFinal: true}
	if f := strings.ToUpper(frameText(a)); !strings.Contains(f, "PRINTER ERROR") {
		t.Errorf("a refused code: %s", f)
	}
	if s := shortState(printer.State{}); s != "Connecting" {
		t.Errorf("status bar %q", s)
	}
}
