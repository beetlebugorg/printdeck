package ui

import (
	"testing"
	"time"

	"printdeck/internal/printer"
)

// The layer clock measures a layer from its start to the start of the next. The layer that printed
// when the clock started, and a layer with a pause, have no time.
func TestLayerClock(t *testing.T) {
	var l layerClock
	t0 := time.Date(2026, 10, 23, 12, 0, 0, 0, time.Local)
	st := printer.State{Printing: true, File: "a.gcode", Layer: 3, Layers: 10}
	l.sample(st, t0)
	st.Layer = 4
	l.sample(st, t0.Add(50*time.Second))
	st.Layer = 5
	l.sample(st, t0.Add(140*time.Second))
	st.Paused = true
	l.sample(st, t0.Add(150*time.Second))
	st.Paused, st.Layer = false, 6
	l.sample(st, t0.Add(400*time.Second))
	if len(l.measure) != 1 || l.measure[4] != 90 {
		t.Errorf("measured %v, want only layer 4 at 90 s", l.measure)
	}
	// A new file starts again.
	st.File = "b.gcode"
	l.sample(st, t0.Add(500*time.Second))
	if len(l.measure) != 0 || l.known {
		t.Errorf("after a new file: %+v", l)
	}
}

func TestMMSS(t *testing.T) {
	if mmss(65) != "1:05" || mmss(3725) != "1:02:05" || mmss(9.6) != "0:10" {
		t.Errorf("%s %s %s", mmss(65), mmss(3725), mmss(9.6))
	}
}
