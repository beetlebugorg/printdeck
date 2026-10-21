package ui

import (
	"testing"
	"time"

	"printdeck/internal/printer"
)

func TestStatusLines(t *testing.T) {
	now := time.Date(2026, 10, 20, 14, 24, 0, 0, time.Local)
	for _, c := range []struct {
		st     printer.State
		mat    string
		l1, l2 string
	}{
		{printer.State{Reachable: true, Homed: "xyz"}, "PLA", "READY        PLA", "XYZ HOMED       "},
		{printer.State{Reachable: true, Printing: true, Completion: 42.4, Layer: 12, Layers: 27, HasTimeLeft: true,
			TimeLeft: 83 * time.Minute}, "", "PRINTING     42%", "END 3:47PM  1:23"},
		{printer.State{Reachable: true, Printing: true, Paused: true, Completion: 42}, "", "PAUSED       42%", "RESUME ON DECK  "},
		{printer.State{Reachable: true, Printing: true, Preparing: true, Activity: "Heating bed  52/60°"}, "", "PREPARING       ", "HEAT BED 52/60  "},
		{printer.State{Reachable: true, Finished: "complete", FinishedFile: "benchy.gcode"}, "", "PRINT DONE      ", "BENCHY.GCODE    "},
	} {
		l1, l2 := StatusLines(c.st, c.mat, now, false)
		if l1 != c.l1 || l2 != c.l2 {
			t.Errorf("%+v:\n got %q %q\nwant %q %q", c.st, l1, l2, c.l1, c.l2)
		}
		if len(l1) != lcdCols || len(l2) != lcdCols {
			t.Errorf("lengths %d %d", len(l1), len(l2))
		}
	}
}

// The 24-hour clock shows the end of a print as 15:47.
func TestStatusLines24h(t *testing.T) {
	now := time.Date(2026, 10, 20, 14, 24, 0, 0, time.Local)
	st := printer.State{Reachable: true, Printing: true, Completion: 42, HasTimeLeft: true, TimeLeft: 83 * time.Minute}
	if _, l2 := StatusLines(st, "", now, true); l2 != "END 15:47   1:23" {
		t.Errorf("got %q", l2)
	}
}
