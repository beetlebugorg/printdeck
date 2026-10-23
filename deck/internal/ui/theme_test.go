package ui

import (
	"testing"
	"time"

	"printdeck/internal/gfx"
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

// The Theme row of the Settings page goes day, night, auto.
func TestThemeRowCyclesTheme(t *testing.T) {
	a := New()
	a.Theme = "day"
	var saved []string
	a.OnTheme = func(s string) { saved = append(saved, s) }
	a.Printer = &DemoPrinter{View: -1}
	cv := gfx.New(W, BaseH)
	a.pages = []page{pageMachine, pageSettings}
	for i := 0; i < 3; i++ {
		a.Draw(cv)
		tapLabel(t, a, cv, "Theme")
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

// The Clock row of the Settings page changes between the 12-hour and the 24-hour clock.
func TestClockRowTogglesClock(t *testing.T) {
	a := New()
	var saved []bool
	a.OnClock = func(h24 bool) { saved = append(saved, h24) }
	a.Printer = &DemoPrinter{View: -1}
	cv := gfx.New(W, BaseH)
	a.pages = []page{pageMachine, pageSettings}
	for i := 0; i < 2; i++ {
		a.Draw(cv)
		tapLabel(t, a, cv, "Clock")
	}
	if len(saved) != 2 || !saved[0] || saved[1] {
		t.Errorf("clocks %v", saved)
	}
}

// The Screen row asks for the next quarter turn, and gives it to OnRotate.
func TestScreenRowTurns(t *testing.T) {
	a := New()
	a.Printer = &DemoPrinter{View: -1}
	got := -1
	a.Rotate, a.OnRotate = 270, func(r int) { got = r }
	cv := gfx.New(W, BaseH)
	a.pages = []page{pageMachine, pageSettings}
	tapLabel(t, a, cv, "Screen")
	if a.confirm == nil {
		t.Fatal("no question")
	}
	tapLabel(t, a, cv, "Turn")
	if got != 0 {
		t.Errorf("turn %d, want 0", got)
	}
}
