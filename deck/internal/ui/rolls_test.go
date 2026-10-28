package ui

import (
	"testing"
	"time"

	"printdeck/internal/printer"
)

// The page of a roll counts its prints, and Prints lists them. A tap opens the page of a print.
func TestRollPrints(t *testing.T) {
	a, pr, cv := newPrintApp(idle)
	pr.Spools_ = &printer.Spools{}
	black := pr.Spools_.Add("Black", "PLA", 1200, 200, 0, time.Unix(1, 0))
	white := pr.Spools_.Add("White", "PLA", 1200, 200, 0, time.Unix(2, 0))
	pr.History_ = []printer.Record{
		{File: "bracket.gcode", Result: "complete", Filament: 4000, End: time.Unix(30, 0), Spool: "Black", SpoolID: black.ID, Grams: 12},
		{File: "cube.gcode", Result: "complete", Filament: 2000, End: time.Unix(20, 0), Spool: "White", SpoolID: white.ID, Grams: 6},
		{File: "benchy.gcode", Result: "cancelled", Filament: 3000, End: time.Unix(10, 0), Spool: "Black"},
	}
	top := func() page { return a.pages[len(a.pages)-1] }
	a.pages = []page{pageFilament, pageSpools, pageSpool}
	a.spoolID = black.ID
	tapLabel(t, a, cv, "Prints")
	if top() != pageSpoolPrints {
		t.Fatalf("page %v, want the prints of the roll", top())
	}
	a.Draw(cv)
	seen := map[string]bool{}
	for _, f := range a.focus {
		seen[f.label] = true
	}
	if !seen[lcdText("Bracket")] || !seen[lcdText("Benchy")] || seen[lcdText("Cube")] {
		t.Fatalf("the list has %v", seen)
	}
	tapLabel(t, a, cv, "Benchy")
	if top() != pageRecord || a.record.File != "benchy.gcode" {
		t.Errorf("page %v, record %q", top(), a.record.File)
	}
}
