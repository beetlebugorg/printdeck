package printer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"printdeck/internal/klipper/batch"
)

// The TAZ: a 0.5 mm nozzle, 2.85 mm filament, the mesh area, and an [input_shaper] section.
func ringMachine() Machine {
	return Machine{NozzleDiameter: 0.5, FilamentDiameter: 2.85, MinX: -50, MaxX: 293, MinY: -17, MaxY: 308,
		HasMesh: true, MeshMin: [2]float64{20, 15}, MeshMax: [2]float64{265, 265},
		PressureAdvance: 0.115, MaxAccel: 3000, SquareCornerVelocity: 5, MinCruiseRatio: 0.5,
		HasShaper: true, ShaperFreqX: 44, ShaperFreqY: 31, ShaperType: "mzv",
		Macros: []string{"PRINT_START", "PRINT_END"}}
}

// The tower turns off the pressure advance and the shaper, raises the acceleration with the
// height, and sets the values of printer.cfg again at the end.
func TestRingTower(t *testing.T) {
	g := RingTower(ringMachine(), 210, 60, "PLA")
	for _, want := range []string{"PRINT_START BED=60 EXTRUDER=210", "SET_PRESSURE_ADVANCE ADVANCE=0\n",
		"SET_INPUT_SHAPER SHAPER_FREQ_X=0 SHAPER_FREQ_Y=0", "SET_VELOCITY_LIMIT MINIMUM_CRUISE_RATIO=0",
		"TUNING_TOWER COMMAND=SET_VELOCITY_LIMIT PARAMETER=ACCEL START=1000 STEP_DELTA=500 STEP_HEIGHT=5",
		"SET_PRESSURE_ADVANCE ADVANCE=0.115", "SET_INPUT_SHAPER SHAPER_FREQ_X=44 SHAPER_FREQ_Y=31",
		"SET_VELOCITY_LIMIT ACCEL=3000 MINIMUM_CRUISE_RATIO=0.5", "PRINT_END", ";LAYER_COUNT:200", "G1 Z40.000",
		"F4800"} {
		if !strings.Contains(g, want) {
			t.Errorf("no %q", want)
		}
	}
	// The tower is in the middle of the mesh area: 142.5 +- 30, one wall of 0.55 mm.
	if !strings.Contains(g, "G1 X172.225") {
		t.Errorf("not in the middle of the mesh area")
	}
	// The V notch of the right wall: 12 mm under the back corner, 3 mm in, 6 mm wide.
	for _, want := range []string{"G1 X172.225 Y157.725", "G1 X169.225 Y154.725", "G1 X172.225 Y151.725"} {
		if !strings.Contains(g, want) {
			t.Errorf("no notch point %q", want)
		}
	}
	m := ringMachine()
	m.HasShaper = false
	if g := RingTower(m, 210, 60, "PLA"); strings.Contains(g, "SET_INPUT_SHAPER") {
		t.Error("SET_INPUT_SHAPER with no [input_shaper] section")
	}
	if RingAccel(0) != 1000 || RingAccel(12.4) != 2000 || RingAccel(39.8) != 4500 {
		t.Errorf("accelerations %g %g %g", RingAccel(0), RingAccel(12.4), RingAccel(39.8))
	}
	// 6 ripples over 10.8 mm at 80 mm/s: 44.4 Hz.
	if f := RingFrequency(6, 10.8); f < 44.4 || f > 44.5 {
		t.Errorf("frequency %g", f)
	}
	if RingFrequency(0, 10) != 0 || RingFrequency(5, 0) != 0 {
		t.Error("frequency with no ripples or no distance")
	}
}

// The deck's Klipper runs the whole tower on the TAZ config with an input shaper: each command of
// the test is known, and each move is in the limits.
func TestRingTowerRuns(t *testing.T) {
	cfg, err := os.ReadFile("../../../printer/printer.cfg")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "printer.cfg")
	extra := "\n[input_shaper]\nshaper_freq_x: 44\nshaper_freq_y: 31\n"
	if err := os.WriteFile(path, append(cfg, extra...), 0o644); err != nil {
		t.Fatal(err)
	}
	// No PRINT_START: the batch run has no probe. The start heats and homes.
	m := ringMachine()
	m.Macros = nil
	gcode := filepath.Join(dir, "tower.gcode")
	if err := os.WriteFile(gcode, []byte(RingTower(m, 210, 60, "PLA")), 0o644); err != nil {
		t.Fatal(err)
	}
	result, msg := batch.Run(path, "../klipper/testdata/klipper.dict", gcode, filepath.Join(dir, "out.serial"))
	if result != "exit" {
		t.Fatalf("run result %q: %s", result, msg)
	}
}

// An option goes into its section: a new value for a line that is there, a new line at the end
// of the section, or a new section before the block of SAVE_CONFIG.
func TestSetConfigOption(t *testing.T) {
	cfg := "[input_shaper]\nshaper_freq_x: 40\n# a note\n\n[heater_bed]\n"
	got := setConfigOption(cfg, "input_shaper", "shaper_freq_x", "44.4")
	if !strings.Contains(got, "[input_shaper]\nshaper_freq_x: 44.4\n# a note\n") {
		t.Errorf("changed line:\n%s", got)
	}
	got = setConfigOption(cfg, "input_shaper", "shaper_freq_y", "31.2")
	if !strings.Contains(got, "[input_shaper]\nshaper_freq_x: 40\nshaper_freq_y: 31.2\n# a note\n\n[heater_bed]") {
		t.Errorf("added line:\n%s", got)
	}
	cfg = "[printer]\nkinematics: cartesian\n\n#*# <---------------------- SAVE_CONFIG ---------------------->\n#*# [bltouch]\n"
	got = setConfigOption(cfg, "input_shaper", "shaper_type", "mzv")
	if !strings.Contains(got, "\n[input_shaper]\nshaper_type: mzv\n\n#*# <---") || strings.Count(got, "#*# <") != 1 {
		t.Errorf("added section:\n%s", got)
	}
}
