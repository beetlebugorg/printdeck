package ui

import (
	"testing"

	"printdeck/internal/eq"
	"printdeck/internal/gfx"
	"printdeck/internal/spotify"
	"printdeck/internal/touch"
)

func TestEQSliders(t *testing.T) {
	e := &DemoEQ{}
	a := New()
	a.Music, a.Printer, a.Vol, a.Equalizer = &DemoMusic{State_: spotify.State{Authorized: true}}, &DemoPrinter{View: -1}, DemoVolume(10), e
	a.OpenEQ()
	cv := gfx.New(W, BaseH)
	col := (W - 48) / len(eq.Bands)
	tap := func(band, y int) {
		a.Draw(cv)
		x := 24 + band*col + col/2
		ev(a, touch.Down, x, y)
		ev(a, touch.Up, x, y)
	}
	tap(0, eqTop)    // top of the bass slider: +12
	tap(4, eqBottom) // bottom of the treble slider: -12
	tap(2, (eqTop+eqBottom)/2)
	if e.G[0] != eq.MaxGain || e.G[4] != -eq.MaxGain || e.G[2] < -0.5 || e.G[2] > 0.5 {
		t.Errorf("gains %v", e.G)
	}
}
