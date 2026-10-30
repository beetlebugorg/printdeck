package ui

import (
	"testing"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// A resin printer has no heaters, no filament and no toolhead: its screens have no keys for them,
// and its job screen has no Pause and no Cancel. A FDM printer keeps them.
func TestResinScreens(t *testing.T) {
	for _, wide := range []bool{false, true} {
		a, pr, cv := newPrintApp(idle)
		a.Wide = wide
		cv = gfx.New(a.Screen().W, a.Screen().H)
		has := func(label string) bool {
			_, ok := a.KeyRect(label)
			return ok
		}
		a.Draw(cv)
		fdm := "Cool down"
		if wide {
			fdm = "Toolhead"
		}
		if !has(fdm) || !has("Nozzle") {
			t.Fatalf("wide %v: a FDM printer with no %q or no Nozzle", wide, fdm)
		}
		pr.Machine_ = printer.Machine{Process: printer.ProcessResin, Board: "CBD V1.4.1"}
		a.Draw(cv)
		for _, gone := range []string{"Cool down", "Nozzle", "Bed"} {
			if has(gone) {
				t.Errorf("wide %v: a resin printer has %q", wide, gone)
			}
		}
		if !has("Plate") {
			t.Errorf("wide %v: no height of the plate", wide)
		}
		if wide {
			for _, gone := range []string{"Filament", "Toolhead"} {
				if has(gone) {
					t.Errorf("a resin printer has the page key %q", gone)
				}
			}
			if !has("Files") || !has("Machine") {
				t.Error("no Files or Machine key")
			}
		}

		// A print: the window of the print, with no Pause, no Tune and no Cancel.
		pr.State_ = printer.State{Reachable: true, Connected: true, Printing: true, Text: "Printing", FilePath: "resin-1",
			Completion: 40, Z: 12.5, HasZ: true}
		a.Draw(cv)
		for _, gone := range []string{"Pause", "Tune", "Cancel"} {
			if has(gone) {
				t.Errorf("wide %v: a resin print has %q", wide, gone)
			}
		}
		if !has("Print") {
			t.Errorf("wide %v: no window of the print", wide)
		}

		// The Machine page: no Macros, no Console, no Maintenance.
		pr.State_ = idle
		a.open(pageMachine)
		a.Draw(cv)
		for _, gone := range []string{"Macros", "Console", "Maintenance", "Restart…"} {
			if has(gone) {
				t.Errorf("wide %v: the Machine page of a resin printer has %q", wide, gone)
			}
		}
	}
}
