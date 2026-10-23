package printer

import (
	"strings"
	"testing"
)

func TestSetConfigPA(t *testing.T) {
	cfg := "[printer]\nkinematics: cartesian\n\n[extruder]\nnozzle_diameter: 0.5\npressure_advance: 0.1\n\n[heater_bed]\n"
	if got := setConfigPA(cfg, 0.045); !strings.Contains(got, "[extruder]\nnozzle_diameter: 0.5\npressure_advance: 0.045\n") {
		t.Errorf("changed line:\n%s", got)
	}
	// No line in the section: a new section before the block of SAVE_CONFIG.
	cfg = "[extruder]\nnozzle_diameter: 0.5\n\n#*# <---------------------- SAVE_CONFIG ---------------------->\n#*# [bed_mesh default]\n"
	got := setConfigPA(cfg, 0.05)
	if !strings.Contains(got, "\n[extruder]\npressure_advance: 0.05\n\n#*# <---") || strings.Count(got, "#*# <") != 1 {
		t.Errorf("added section:\n%s", got)
	}
}

func TestPATower(t *testing.T) {
	m := Machine{NozzleDiameter: 0.5, FilamentDiameter: 2.85, MinX: -20, MaxX: 300, MinY: -20, MaxY: 300,
		PressureAdvance: 0.02, MaxAccel: 3000, SquareCornerVelocity: 5, Macros: []string{"PRINT_START", "PRINT_END"}}
	g := PATower(m, 210, 60, "PLA")
	for _, want := range []string{"PRINT_START BED=60 EXTRUDER=210", "TUNING_TOWER COMMAND=SET_PRESSURE_ADVANCE PARAMETER=ADVANCE START=0 FACTOR=0.005",
		"SET_VELOCITY_LIMIT ACCEL=500 SQUARE_CORNER_VELOCITY=1", "SET_PRESSURE_ADVANCE ADVANCE=0.02",
		"SET_VELOCITY_LIMIT ACCEL=3000 SQUARE_CORNER_VELOCITY=5", "PRINT_END", ";LAYER_COUNT:100", "G1 Z30.000"} {
		if !strings.Contains(g, want) {
			t.Errorf("no %q", want)
		}
	}
	// The tower is in the middle of the bed: 140 +- 20.
	if !strings.Contains(g, "G1 X160.000") && !strings.Contains(g, "X159.") {
		t.Errorf("not in the middle")
	}
	if PATowerValue(9) != 0.045 {
		t.Errorf("value %g", PATowerValue(9))
	}
}
