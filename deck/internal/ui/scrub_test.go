package ui

import (
	"testing"
	"time"

	"speakerdeck/internal/gfx"
	"speakerdeck/internal/octoprint"
	"speakerdeck/internal/spotify"
	"speakerdeck/internal/touch"
)

func TestLayerScrubber(t *testing.T) {
	pr := &DemoPrinter{View: -1, State_: octoprint.State{Reachable: true, Text: "Printing", Connected: true, Printing: true,
		Activity: "Printing", Layer: 12, Layers: 27, Completion: 40, Tool: 210, ToolTarget: 210, HasTimeLeft: true, TimeLeft: time.Hour}}
	a := New()
	a.Music = &DemoMusic{State_: spotify.State{Authorized: true}}
	a.Printer, a.Vol = pr, DemoVolume(10)
	a.SetTab(TabPrint)
	cv := gfx.New(W, H)
	a.Draw(cv)

	// The middle of the slider is the middle layer. 27 layers: 0.5 * 27 = 13.5, which rounds to 14.
	y := 104 + 260 - 20
	ev(a, touch.Down, 240, y)
	ev(a, touch.Up, 240, y)
	if pr.View != 14 {
		t.Fatalf("slider in the middle gave layer %d, want 14", pr.View)
	}
	// The left end is layer 0 (nothing printed), the right end is the last layer.
	a.Draw(cv)
	ev(a, touch.Down, 59, y)
	ev(a, touch.Up, 59, y)
	if pr.View != 0 {
		t.Errorf("left end gave layer %d, want 0", pr.View)
	}
	a.Draw(cv)
	ev(a, touch.Down, 421, y)
	ev(a, touch.Up, 421, y)
	if pr.View != 27 {
		t.Errorf("right end gave layer %d, want 27", pr.View)
	}
	// While the scrubber shows a layer, the Live button goes back to the print.
	a.Draw(cv)
	ev(a, touch.Down, 400, 104+20)
	ev(a, touch.Up, 400, 104+20)
	time.Sleep(10 * time.Millisecond)
	if pr.View != -1 {
		t.Errorf("Live gave layer %d, want -1", pr.View)
	}
}
