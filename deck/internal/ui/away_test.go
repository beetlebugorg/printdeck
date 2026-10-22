package ui

import (
	"fmt"
	"testing"
	"time"

	"printdeck/internal/printer"
)

func (r *recPrinter) Heat(tool, bed int) { r.rec(fmt.Sprint("heat ", tool, " ", bed)) }

var printing = printer.State{Reachable: true, Text: "Printing", Connected: true, Printing: true,
	File: "benchy.gcode", FilePath: "benchy.gcode", Layer: 12, Layers: 27, Tool: 210, ToolTarget: 210, Bed: 60, BedTarget: 60}

// hasKey draws a frame and reports that it has a key with the label.
func hasKey(a *App, label string) bool {
	for _, f := range a.focus {
		if f.label == lcdText(label) {
			return true
		}
	}
	return false
}

// Home on the screen of the print shows the home screen with the print bar. The bar goes back.
func TestAwayAndBack(t *testing.T) {
	a, _, cv := newPrintApp(printing)
	tapLabel(t, a, cv, "Home")
	a.Draw(cv)
	if !a.away || !hasKey(a, "Back to the print") || !hasKey(a, "Rolls") {
		t.Fatalf("away %v: no home screen with the print bar", a.away)
	}
	if contentY1 != H-barH {
		t.Errorf("content ends at %d, want %d", contentY1, H-barH)
	}
	tapLabel(t, a, cv, "Rolls")
	a.Draw(cv)
	if a.topPage() != pageSpools || !hasKey(a, "Back to the print") {
		t.Fatal("the Rolls page has no print bar")
	}
	tapLabel(t, a, cv, "Back to the print")
	a.Draw(cv)
	if a.away || len(a.pages) != 0 || !hasKey(a, "Pause") {
		t.Fatal("the bar did not go back to the print")
	}
}

// While away, the keys that change the printer wait: a tap gives a toast and no command.
func TestAwayLocksPrinterKeys(t *testing.T) {
	a, pr, cv := newPrintApp(printing)
	tapLabel(t, a, cv, "Home")
	a.Draw(cv)
	if hasKey(a, "Cool down") {
		t.Error("the display menu has a key that waits")
	}
	tapRegion(t, a, cv, "Cool down")
	time.Sleep(20 * time.Millisecond)
	if c := pr.Calls(); len(c) != 0 {
		t.Errorf("calls %v, want none", c)
	}
	if len(a.posted) != 1 {
		t.Errorf("%d toasts, want 1", len(a.posted))
	}
}

// A pause, and a long time with no touch, bring back the screen of the print.
func TestAwayComesBack(t *testing.T) {
	a, pr, cv := newPrintApp(printing)
	now := time.Unix(1000, 0)
	a.Now = func() time.Time { return now }
	tapLabel(t, a, cv, "Home")
	tapLabel(t, a, cv, "History")
	pr.State_.Paused = true
	a.Draw(cv)
	if a.away || len(a.pages) != 0 || !hasKey(a, "Resume") {
		t.Fatal("a pause did not show the print")
	}

	pr.State_.Paused = false
	a.Draw(cv)
	tapLabel(t, a, cv, "Home")
	now = now.Add(awayIdle - time.Second)
	a.Draw(cv)
	if !a.away {
		t.Fatal("back too soon")
	}
	now = now.Add(2 * time.Second)
	a.Draw(cv)
	if a.away || !hasKey(a, "Pause") {
		t.Fatal("no touch for a long time did not show the print")
	}
}

// A new print shows its screen, also over a page.
func TestNewPrintShowsIt(t *testing.T) {
	a, pr, cv := newPrintApp(printer.State{Reachable: true, Connected: true, Text: "Ready"})
	a.pages = []page{pageMachine}
	a.Draw(cv)
	pr.State_ = printing
	a.Draw(cv)
	if len(a.pages) != 0 || !hasKey(a, "Pause") {
		t.Fatal("the new print did not show its screen")
	}
}
