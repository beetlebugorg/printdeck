package ui

import (
	"testing"
	"time"

	"printdeck/internal/printer"
	"printdeck/internal/touch"
)

// The mouse wheel of a remote screen scrolls the list of the rolls.
func TestWheelScrollsRolls(t *testing.T) {
	a := New()
	s := &printer.Spools{}
	for i := 0; i < 12; i++ {
		s.Add("Roll", "PLA", 1000, 200, 20, time.Unix(int64(i+1), 0))
	}
	a.Printer = &DemoPrinter{View: -1, Spools_: s}
	a.pages = []page{pageSpools}
	frameText(a)
	a.handle(touch.Event{Kind: touch.Wheel, X: 0.5, Y: 0.5, DY: 0.2})
	if a.spoolScroll <= 0 {
		t.Fatalf("scroll %d after the wheel", a.spoolScroll)
	}
}
