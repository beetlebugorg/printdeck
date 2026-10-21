package ui

import (
	"strings"
	"testing"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/internal/spotify"
	"printdeck/internal/touch"
)

func TestLayerScrubber(t *testing.T) {
	pr := &DemoPrinter{View: -1, State_: printer.State{Reachable: true, Text: "Printing", Connected: true, Printing: true,
		Activity: "Printing", Layer: 12, Layers: 27, Completion: 40, Tool: 210, ToolTarget: 210, HasTimeLeft: true, TimeLeft: time.Hour}}
	a := New()
	a.Music = &DemoMusic{State_: spotify.State{Authorized: true}}
	a.Printer, a.Vol = pr, DemoVolume(10)
	a.SetTab(TabPrint)
	cv := gfx.New(W, BaseH)
	a.Draw(cv)

	// The middle of the slider is the middle layer. 27 layers: 0.5 * 27 = 13.5, which rounds to 14.
	y := jobBox.Max.Y - 20
	ev(a, touch.Down, 240, y)
	ev(a, touch.Up, 240, y)
	if pr.View != 14 {
		t.Fatalf("slider in the middle gave layer %d, want 14", pr.View)
	}
	// The left end is layer 0 (nothing printed), the right end is the last layer.
	a.Draw(cv)
	ev(a, touch.Down, jobBox.Min.X+19, y)
	ev(a, touch.Up, jobBox.Min.X+19, y)
	if pr.View != 0 {
		t.Errorf("left end gave layer %d, want 0", pr.View)
	}
	a.Draw(cv)
	ev(a, touch.Down, jobBox.Max.X-19, y)
	ev(a, touch.Up, jobBox.Max.X-19, y)
	if pr.View != 27 {
		t.Errorf("right end gave layer %d, want 27", pr.View)
	}
	// While the scrubber shows a layer, the Live button goes back to the print.
	a.Draw(cv)
	ev(a, touch.Down, jobBox.Max.X-50, jobBox.Min.Y+20)
	ev(a, touch.Up, jobBox.Max.X-50, jobBox.Min.Y+20)
	time.Sleep(10 * time.Millisecond)
	if pr.View != -1 {
		t.Errorf("Live gave layer %d, want -1", pr.View)
	}
}

func TestLCDLines(t *testing.T) {
	a := New()
	st := printer.State{Activity: "Printing", Completion: 41.4, HasTimeLeft: true, TimeLeft: 30 * time.Minute,
		HasNow: true, NowKind: "Outer wall", NowSpeed: 60}
	lines := a.lcdLines(st)
	want := []string{"PRINTING     41% 0:30", "OUTER WALL    60MM/S"}
	if len(lines) != 2 || lines[1] != want[1] || !strings.HasPrefix(lines[0], "PRINTING") || !strings.HasSuffix(lines[0], "41% 0:30") {
		t.Errorf("lines %q, want like %q", lines, want)
	}
	for _, l := range lines {
		if len([]rune(l)) != 20 {
			t.Errorf("line %q is %d characters, want 20", l, len([]rune(l)))
		}
	}
}
