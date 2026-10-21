package ui

import (
	"testing"
	"time"

	"printdeck/internal/gfx"
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

// The Theme row of the Machine page goes day, night, auto.
func TestThemeRowCyclesTheme(t *testing.T) {
	a := New()
	a.Theme = "day"
	var saved []string
	a.OnTheme = func(s string) { saved = append(saved, s) }
	a.Printer = &DemoPrinter{View: -1}
	cv := gfx.New(W, BaseH)
	a.pages = []page{pageMachine}
	for i := 0; i < 3; i++ {
		a.Draw(cv)
		ev(a, touch.Down, W/2, 210+4*65+30) // the fifth row
		ev(a, touch.Up, W/2, 210+4*65+30)
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

// The Clock row of the Machine page changes between the 12-hour and the 24-hour clock.
func TestClockRowTogglesClock(t *testing.T) {
	a := New()
	var saved []bool
	a.OnClock = func(h24 bool) { saved = append(saved, h24) }
	a.Printer = &DemoPrinter{View: -1}
	cv := gfx.New(W, BaseH)
	a.pages = []page{pageMachine}
	for i := 0; i < 2; i++ {
		a.Draw(cv)
		ev(a, touch.Down, W/2, 210+5*65+30) // the sixth row
		ev(a, touch.Up, W/2, 210+5*65+30)
	}
	if len(saved) != 2 || !saved[0] || saved[1] {
		t.Errorf("clocks %v", saved)
	}
}
