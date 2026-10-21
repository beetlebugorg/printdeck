package ui

import (
	"strings"
	"testing"
	"time"

	"printdeck/internal/printer"
)

func TestMirrorLines(t *testing.T) {
	now := time.Date(2026, 10, 20, 14, 24, 0, 0, time.Local)
	for _, c := range []struct {
		st     printer.State
		mat    string
		l1, l2 string
	}{
		{printer.State{Reachable: true, Homed: "xyz"}, "PLA", "READY        PLA", "XYZ HOMED       "},
		{printer.State{Reachable: true, Printing: true, Completion: 42.4, Layer: 12, Layers: 27, HasTimeLeft: true,
			TimeLeft: 83 * time.Minute}, "", "PRINTING     42%", "END 15:47   1:23"},
		{printer.State{Reachable: true, Printing: true, Paused: true, Completion: 42}, "", "PAUSED       42%", "RESUME ON DECK  "},
		{printer.State{Reachable: true, Printing: true, Preparing: true, Activity: "Heating bed  52/60°"}, "", "PREPARING       ", "HEAT BED 52/60  "},
		{printer.State{Reachable: true, Finished: "complete", FinishedFile: "benchy.gcode"}, "", "PRINT DONE      ", "BENCHY.GCODE    "},
	} {
		l1, l2 := MirrorLines(c.st, c.mat, now)
		if l1 != c.l1 || l2 != c.l2 {
			t.Errorf("%+v:\n got %q %q\nwant %q %q", c.st, l1, l2, c.l1, c.l2)
		}
		if len(l1) != mirrorCols || len(l2) != mirrorCols {
			t.Errorf("lengths %d %d", len(l1), len(l2))
		}
	}
	// No quote or comment character reaches the G-code.
	cmd := Mirror{Line1: mirrorText(`a"b;c#d*e'f`)}.Command()
	if strings.Count(cmd[0], `"`) != 2 || strings.ContainsAny(cmd[0][strings.Index(cmd[0], "VALUE="):], ";#*") {
		t.Errorf("command %q", cmd[0])
	}
}
