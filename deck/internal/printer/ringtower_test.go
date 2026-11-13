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
	g := RingTower(ringMachine(), 210, 60, "PLA", false)
	for _, want := range []string{"PRINT_START BED=60 EXTRUDER=210", "SET_PRESSURE_ADVANCE ADVANCE=0\n",
		"SET_INPUT_SHAPER SHAPER_FREQ_X=0 SHAPER_FREQ_Y=0", "SET_VELOCITY_LIMIT MINIMUM_CRUISE_RATIO=0",
		"TUNING_TOWER COMMAND=SET_VELOCITY_LIMIT PARAMETER=ACCEL START=1000 STEP_DELTA=500 STEP_HEIGHT=5",
		"SET_PRESSURE_ADVANCE ADVANCE=0.115", "SET_INPUT_SHAPER SHAPER_TYPE=mzv SHAPER_FREQ_X=44 SHAPER_FREQ_Y=31",
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
	if g := RingTower(m, 210, 60, "PLA", false); strings.Contains(g, "SET_INPUT_SHAPER") {
		t.Error("SET_INPUT_SHAPER with no [input_shaper] section")
	}
	// The check tower keeps the shaper on, and turns off the pressure advance.
	c := RingTower(ringMachine(), 210, 60, "PLA", true)
	if strings.Contains(c, "SET_INPUT_SHAPER") || !strings.Contains(c, "SET_PRESSURE_ADVANCE ADVANCE=0\n") ||
		!strings.Contains(c, "with the input shaper on") || !strings.Contains(c, "TUNING_TOWER COMMAND=SET_VELOCITY_LIMIT") {
		t.Error("the check tower changes the shaper, or does not test")
	}
	// Ringing from 22 mm (the band of 5000): max_accel is the band under it.
	if RingMaxAccel(22) != 2500 || RingMaxAccel(3) != 0 {
		t.Errorf("max accel %g %g", RingMaxAccel(22), RingMaxAccel(3))
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
	if err := os.WriteFile(gcode, []byte(RingTower(m, 210, 60, "PLA", false)), 0o644); err != nil {
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

// The sweep sets the frequency of each band when the band starts, from 20 Hz at the bottom to 47
// Hz at the top, and sets the shaper of printer.cfg again at the end.
func TestSweepTower(t *testing.T) {
	g := SweepTower(ringMachine(), 210, 60, "PLA", false)
	for _, want := range []string{"SET_INPUT_SHAPER SHAPER_TYPE=mzv\n", "SET_VELOCITY_LIMIT ACCEL=3000 MINIMUM_CRUISE_RATIO=0",
		";0.0-2.5 mm: 20 Hz", ";22.5-25.0 mm: 47 Hz", "SET_INPUT_SHAPER SHAPER_FREQ_X=20 SHAPER_FREQ_Y=20",
		"SET_INPUT_SHAPER SHAPER_FREQ_X=47 SHAPER_FREQ_Y=47", "SET_INPUT_SHAPER SHAPER_TYPE=mzv SHAPER_FREQ_X=44 SHAPER_FREQ_Y=31",
		";LAYER_COUNT:125", "G1 Z25.000"} {
		if !strings.Contains(g, want) {
			t.Errorf("no %q", want)
		}
	}
	// One change for each band: the frequency of a band holds over its layers.
	if n := strings.Count(g, "SET_INPUT_SHAPER SHAPER_FREQ_X="); n != SweepBands {
		t.Errorf("%d frequency changes, want %d", n, SweepBands)
	}
	// The layer at 2.6 mm is in the second band.
	i := strings.Index(g, "G1 Z2.600 F600")
	j := strings.Index(g, "SET_INPUT_SHAPER SHAPER_FREQ_X=23 ")
	if i < 0 || j < i {
		t.Error("the second band does not start at 2.6 mm")
	}
	if SweepFreqAt(0) != 20 || SweepFreqAt(6.2) != 26 || SweepFreqAt(24.9) != 47 || SweepFreqAt(80) != 47 {
		t.Errorf("frequencies %g %g %g", SweepFreqAt(0), SweepFreqAt(6.2), SweepFreqAt(24.9))
	}
}

// The pair sets the low frequencies before the left tower and the high ones before the right
// tower, in each layer, and lifts the nozzle on the way to the right tower.
func TestPairTowers(t *testing.T) {
	g := PairTowers(ringMachine(), 210, 60, "PLA", [2]float64{26.5, 30.5}, [2]float64{29.5, 33.5})
	layer := g[strings.Index(g, ";LAYER:5\n"):strings.Index(g, ";LAYER:6\n")]
	lo := strings.Index(layer, "SET_INPUT_SHAPER SHAPER_FREQ_X=26.50 SHAPER_FREQ_Y=30.50")
	hi := strings.Index(layer, "SET_INPUT_SHAPER SHAPER_FREQ_X=29.50 SHAPER_FREQ_Y=33.50")
	if lo < 0 || hi < lo {
		t.Fatalf("layer 5 has no low then high frequency:\n%s", layer)
	}
	// The left tower is 35 mm left of the middle (142.5), the right one 35 mm right.
	if !strings.Contains(layer[lo:hi], "X137.225") || !strings.Contains(layer[hi:], "X207.225") {
		t.Error("the towers are not left and right of the middle")
	}
	if !strings.Contains(layer[lo:hi+200], "G1 Z2.200 F600") {
		t.Error("no lift to the right tower")
	}
	if !strings.Contains(g, ";LAYER_COUNT:40") {
		t.Error("not 8 mm high")
	}
}

// The deck's Klipper runs the sweep and the pair on the TAZ config with an input shaper.
func TestShaperTestsRun(t *testing.T) {
	cfg, err := os.ReadFile("../../../printer/printer.cfg")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "printer.cfg")
	if err := os.WriteFile(path, append(cfg, "\n[input_shaper]\nshaper_freq_x: 28\nshaper_freq_y: 32\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	m := ringMachine()
	m.Macros = nil
	for name, g := range map[string]string{"sweep": SweepTower(m, 210, 60, "PLA", false), "sweep5": SweepTower(m, 210, 60, "PLA", true),
		"pair": PairTowers(m, 210, 60, "PLA", [2]float64{26.5, 30.5}, [2]float64{29.5, 33.5})} {
		gcode := filepath.Join(dir, name+".gcode")
		os.WriteFile(gcode, []byte(g), 0o644)
		if result, msg := batch.Run(path, "../klipper/testdata/klipper.dict", gcode, filepath.Join(dir, name+".serial")); result != "exit" {
			t.Errorf("%s: run result %q: %s", name, result, msg)
		}
	}
}

// Five towers: the middle and the corners of the mesh area, inside the bed, with one frequency
// change for each band (before the first tower of the layer).
func TestSweepFive(t *testing.T) {
	m := ringMachine()
	c := SweepCenters(m, true)
	if len(c) != 5 || c[0] != [2]float64{142.5, 140} {
		t.Fatalf("centers %v", c)
	}
	for _, p := range c[1:] {
		if p[0]-ringSide/2 < m.MeshMin[0]-5 || p[0]+ringSide/2 > m.MeshMax[0]+5 ||
			p[1]-ringSide/2 < m.MeshMin[1]-5 || p[1]+ringSide/2 > m.MeshMax[1]+5 {
			t.Errorf("tower at %v is out of the mesh area", p)
		}
	}
	g := SweepTower(m, 210, 60, "PLA", true)
	if n := strings.Count(g, "SET_INPUT_SHAPER SHAPER_FREQ_X="); n != SweepBands {
		t.Errorf("%d frequency changes, want %d", n, SweepBands)
	}
}
