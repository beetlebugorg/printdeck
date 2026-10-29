package ui

import (
	"strings"
	"testing"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// The touch screen and a browser session both show the status panel.
func TestStatusPanelOnEachScreen(t *testing.T) {
	for _, wide := range []bool{false, true} {
		a, _, _ := newPrintApp(idle)
		a.Wide = wide
		w, h := 480, 800
		if wide {
			w, h = 800, 480
		}
		for _, s := range []*App{a, a.Session(w, h)} {
			cv, rec := gfx.NewRecording(s.Screen().W, s.Screen().H)
			s.Draw(cv)
			if f := string(rec.JSON()); !strings.Contains(f, `"NOZZLE"`) || !strings.Contains(f, `"Ready"`) {
				t.Errorf("wide %v: no status panel", wide)
			}
		}
	}
}

// The lines of the status panel during a print: the progress, the time left, the end, and the
// layer with the time to the next layer.
func TestPanelLinesPrint(t *testing.T) {
	a, pr, _ := newPrintApp(idle)
	a.Now = func() time.Time { return time.Date(2026, 10, 29, 14, 0, 0, 0, time.Local) }
	a.Printer = layerPrinter{recPrinter: pr, toLayer: 46, toEnd: 3600}
	st := printer.State{Reachable: true, Printing: true, Completion: 42, HasTimeLeft: true, TimeLeft: time.Hour,
		Elapsed: 30 * time.Minute, Layer: 12, Layers: 27, Z: 3.2}
	p := a.panelLines(st)
	if p.head != "Printing · 42%" || p.side != "1:00 left" || !p.bar || p.progress != 0.42 {
		t.Errorf("head %q, side %q, bar %v %v", p.head, p.side, p.bar, p.progress)
	}
	if p.msg != "Ends 3:00 PM · 0:30 so far" {
		t.Errorf("message %q", p.msg)
	}
	if p.detail != "Layer 12 of 27 · next in 0:46 · Z 3.20" {
		t.Errorf("detail %q", p.detail)
	}
}
