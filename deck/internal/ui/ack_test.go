package ui

import (
	"testing"
	"time"

	"printdeck/internal/printer"
)

// A tapped key that stays in view flashes: an orange ring around it, which fades.
func TestTapFlash(t *testing.T) {
	a, _, cv := newPrintApp(printer.State{Reachable: true, Connected: true, Text: "Ready"})
	now := time.Unix(1000, 0)
	a.Now = func() time.Time { return now }
	tapLabel(t, a, cv, "Cool down")
	r, _ := a.KeyRect("Cool down")
	now = now.Add(100 * time.Millisecond)
	a.Draw(cv)
	ring := cv.RGBAAt(r.Min.X+r.Dx()/2, r.Min.Y-2)
	if ring.R < 200 || ring.G > 160 {
		t.Errorf("no orange ring over the key: %v", ring)
	}
	now = now.Add(ackTime)
	a.Draw(cv)
	if after := cv.RGBAAt(r.Min.X+r.Dx()/2, r.Min.Y-2); after != bg {
		t.Errorf("the ring stays: %v", after)
	}
}

// Preheat is a toggle: on while the heaters hold its temperatures, and a tap then turns them off.
func TestPreheatToggle(t *testing.T) {
	a, pr, cv := newPrintApp(printer.State{Reachable: true, Connected: true, Text: "Ready", ToolTarget: 200, BedTarget: 60})
	a.PreheatTool, a.PreheatBed = 200, 60
	tapLabel(t, a, cv, "Preheat 200/60")
	if c := pr.wait(1); len(c) != 1 || c[0] != "heat 0 0" {
		t.Fatalf("calls %v, want heat 0 0", c)
	}
}
