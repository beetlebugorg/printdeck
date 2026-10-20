package ui

import (
	"testing"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/spotify"
	"printdeck/internal/touch"
)

func TestNightHours(t *testing.T) {
	a := New()
	at := func(h int) bool {
		a.Now = func() time.Time { return time.Date(2026, 10, 9, h, 30, 0, 0, time.Local) }
		return a.isNight()
	}
	if at(19) || !at(20) || !at(23) || !at(3) || !at(6) || at(7) || at(12) {
		t.Error("auto: night should be 20:00 to 7:00")
	}
	a.Theme = "day"
	if at(23) {
		t.Error("day mode at night")
	}
	a.Theme = "night"
	if !at(12) {
		t.Error("night mode at noon")
	}
}

func TestClockCyclesTheme(t *testing.T) {
	a := New()
	a.Theme = "day"
	var saved []string
	a.OnTheme = func(s string) { saved = append(saved, s) }
	a.Music, a.Printer, a.Vol = &DemoMusic{State_: spotify.State{Authorized: true}}, &DemoPrinter{View: -1}, DemoVolume(10)
	cv := gfx.New(W, H)
	for i := 0; i < 3; i++ {
		a.Draw(cv)
		ev(a, touch.Down, 40, 28)
		ev(a, touch.Up, 40, 28)
	}
	if len(saved) != 3 || saved[0] != "night" || saved[1] != "auto" || saved[2] != "day" {
		t.Errorf("themes %v", saved)
	}
	a.Theme = "night"
	a.Draw(cv)
	if !night || cv.RGBAAt(W/2, H-4) != nightPalette.bg {
		t.Error("the night palette is not drawn")
	}
	a.Theme = "day"
	a.Draw(cv)
	if night {
		t.Error("still night")
	}
}
