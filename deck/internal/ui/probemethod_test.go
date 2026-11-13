package ui

import (
	"strings"
	"testing"
	"time"

	"printdeck/internal/printer"
)

// Settings has the Probe method row when Klipper has [printdeck_mesh]. It shows the last result,
// and a tap sets the next method.
func TestSettingsProbeMethod(t *testing.T) {
	st := idle
	st.ProbeMethod = "map"
	st.ProbeMapPoints = 49
	st.ProbeMapTemp = 60
	st.ProbeLast = printer.ProbeResult{Method: "map", Used: "map", Points: 5, Error: 0.012, Seconds: 6.4}
	a, pr, cv := newPrintApp(st)
	a.pages = []page{pageMachine, pageSettings}
	f := simFrame(t, a, "settings-probe-method")
	for _, s := range []string{"PROBE METHOD", "Saved map + check", "5 points"} {
		if !strings.Contains(f, s) {
			t.Errorf("no %q in Settings", s)
		}
	}
	tapLabel(t, a, cv, "Probe method")
	want := "gcode PRINTDECK_MESH_METHOD METHOD=adaptive"
	for i := 0; i < 100; i++ {
		pr.mu.Lock()
		calls := strings.Join(pr.calls, "\n")
		pr.mu.Unlock()
		if strings.Contains(calls, want) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("no %q", want)
}

// With no saved map, the line asks for a full probe.
func TestProbeMethodLine(t *testing.T) {
	st := printer.State{ProbeMethod: "map"}
	if l := probeMethodLine(st); !strings.Contains(l, "probe the full bed") {
		t.Errorf("line %q", l)
	}
	st.ProbeMethod = "grid"
	if l := probeMethodLine(st); !strings.Contains(l, "Full grid") {
		t.Errorf("line %q", l)
	}
}
