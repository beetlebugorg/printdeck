package printer

import (
	"strings"
	"testing"
)

func TestRetractTower(t *testing.T) {
	m := Machine{NozzleDiameter: 0.5, FilamentDiameter: 2.85, MinX: -20, MaxX: 300, MinY: -20, MaxY: 300,
		RetractLength: 1.5, Macros: []string{"PRINT_START", "PRINT_END"}}
	g := RetractTower(m, 210, 60, "PLA")
	for _, want := range []string{"PRINT_START BED=60 EXTRUDER=210", "SET_RETRACTION RETRACT_LENGTH=0.25",
		"SET_RETRACTION RETRACT_LENGTH=2.50", "SET_RETRACTION RETRACT_LENGTH=1.5\nPRINT_END", ";LAYER_COUNT:120", "G1 Z30.000"} {
		if !strings.Contains(g, want) {
			t.Errorf("no %q", want)
		}
	}
	if n := strings.Count(g, "SET_RETRACTION RETRACT_LENGTH="); n != RetractBands+1 {
		t.Errorf("%d SET_RETRACTION, want %d", n, RetractBands+1)
	}
	// Each G10 has its G11, and the length changes only when the filament is not retracted.
	retracted := false
	for i, l := range strings.Split(g, "\n") {
		switch {
		case l == "G10":
			if retracted {
				t.Fatalf("line %d: G10 twice", i)
			}
			retracted = true
		case l == "G11":
			if !retracted {
				t.Fatalf("line %d: G11 with no G10", i)
			}
			retracted = false
		case strings.HasPrefix(l, "SET_RETRACTION") || strings.HasPrefix(l, "G1 Z"):
			if retracted {
				t.Fatalf("line %d: %q while retracted", i, l)
			}
		case strings.HasPrefix(l, "G1 ") && strings.Contains(l, " E") && retracted:
			t.Fatalf("line %d: extrusion while retracted", i)
		}
	}
	for h, want := range map[float64]float64{0: 0.25, 2.9: 0.25, 3: 0.5, 15: 1.5, 29: 2.5, 40: 2.5} {
		if got := RetractAt(h); got != want {
			t.Errorf("RetractAt(%g) %g, want %g", h, got, want)
		}
	}
}

func TestSetRetractionConfig(t *testing.T) {
	cfg := "[extruder]\nnozzle_diameter: 0.5\n\n#*# <---------------------- SAVE_CONFIG ---------------------->\n"
	got := setConfigOption(cfg, "firmware_retraction", "retract_length", "1.25")
	if !strings.Contains(got, "[firmware_retraction]\nretract_length: 1.25\n\n#*# <") {
		t.Errorf("added section:\n%s", got)
	}
}
