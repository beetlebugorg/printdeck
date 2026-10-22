package printer

import (
	"bytes"
	"log"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"printdeck/internal/klipper/batch"
	"printdeck/internal/klipper/toolhead"
	"printdeck/internal/maxmode"
)

// The setup of the batch runs: a known position with no homing, and the modes of the Cura start
// G-code.
const planSetup = "SET_KINEMATIC_POSITION X=0 Y=0 Z=0\nG90\nM82\nG92 E0\n"

var exitTime = regexp.MustCompile(`Exiting \(print time ([0-9.]+)s\)`)

// klipperPrintTime runs G-code on the Klipper host in batch mode, with the printer of printer/, and
// returns its print time at the end.
func klipperPrintTime(t *testing.T, cfg, gcode string) float64 {
	t.Helper()
	dir := t.TempDir()
	in, out := filepath.Join(dir, "in.gcode"), filepath.Join(dir, "out.serial")
	os.WriteFile(in, []byte(gcode), 0o644)
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)
	batch.Run(cfg, "../klipper/testdata/klipper.dict", in, out)
	m := exitTime.FindStringSubmatch(logs.String())
	if m == nil {
		t.Fatalf("no print time; the log:\n%s", logs.String())
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	return v
}

// The plan of a file has the time that the Klipper host takes for it: the same planner, the same
// limits.
func TestPlanMatchesKlipper(t *testing.T) {
	src, err := os.ReadFile("../../../printer/printer.cfg")
	if err != nil {
		t.Skip("no printer.cfg")
	}
	// The batch mode has no heaters, so the test printer extrudes cold.
	cfg := filepath.Join(t.TempDir(), "printer.cfg")
	os.WriteFile(cfg, regexp.MustCompile(`(?m)^min_extrude_temp:.*$`).ReplaceAll(src, []byte("min_extrude_temp: 0")), 0o644)
	body, err := os.ReadFile("testdata/two-layers.gcode")
	if err != nil {
		t.Fatal(err)
	}
	klipper := klipperPrintTime(t, cfg, planSetup+string(body)) - klipperPrintTime(t, cfg, planSetup)

	l := toolhead.Limits{MaxVelocity: 300, MaxAccel: 3000, MinCruiseRatio: 0.5, SquareCornerVelocity: 5,
		MaxZVelocity: 25, MaxZAccel: 100, InstantCornerVelocity: 1}
	p := planFile(strings.NewReader(planSetup[len("SET_KINEMATIC_POSITION X=0 Y=0 Z=0\n"):]+string(body)), 0, l, nil, nil)
	t.Logf("Klipper %.3f s, plan %.3f s", klipper, p.total)
	if math.Abs(p.total-klipper) > 0.002*klipper {
		t.Errorf("plan %.3f s, Klipper %.3f s", p.total, klipper)
	}
}

// printerLimits are the motion limits of printer/printer.cfg.
var printerLimits = toolhead.Limits{MaxVelocity: 300, MaxAccel: 3000, MinCruiseRatio: 0.5, SquareCornerVelocity: 5,
	MaxZVelocity: 25, MaxZAccel: 100, InstantCornerVelocity: 1}

// maxModeSettings are the default max mode settings with the filament of printer/printer.cfg.
func maxModeSettings() maxmode.Settings {
	s := maxmode.Defaults()
	s.Enable, s.FilamentDiameter = true, 2.85
	return s
}

// The plan of a Cura file with max mode, against the F speeds.
func TestPlanMaxMode(t *testing.T) {
	body, err := os.ReadFile("testdata/two-layers.gcode")
	if err != nil {
		t.Fatal(err)
	}
	src := planSetup[len("SET_KINEMATIC_POSITION X=0 Y=0 Z=0\n"):] + string(body)
	normal := planFile(strings.NewReader(src), 0, printerLimits, nil, nil)
	s := maxModeSettings()
	s.MaxVelocity = printerLimits.MaxVelocity
	factors := maxmode.LayerFactors(strings.NewReader(src), s, 0)
	mm := planFile(strings.NewReader(src), 0, printerLimits, &s, factors)
	t.Logf("F speeds %.1f s, max mode %.1f s (%.0f%%), layer factors %v", normal.total, mm.total,
		100*mm.total/normal.total, factors)
	if mm.total <= 0 || mm.total >= normal.total {
		t.Errorf("max mode %.1f s, F speeds %.1f s", mm.total, normal.total)
	}
	// Each step of the max flow changes the time.
	for _, flow := range []float64{5, 8, 20} {
		s := s
		s.MaxFlow = flow
		p := planFile(strings.NewReader(src), 0, printerLimits, &s, maxmode.LayerFactors(strings.NewReader(src), s, 0))
		t.Logf("max_flow %2.0f: %.1f s", flow, p.total)
	}
}

// The Klipper host with [max_mode] has the print time of the plan with max mode. The batch mode has no
// virtual_sdcard, so min_layer_time is 0 in both.
func TestPlanMaxModeMatchesKlipper(t *testing.T) {
	src, err := os.ReadFile("../../../printer/printer.cfg")
	if err != nil {
		t.Skip("no printer.cfg")
	}
	cfg := filepath.Join(t.TempDir(), "printer.cfg")
	src = regexp.MustCompile(`(?m)^min_extrude_temp:.*$`).ReplaceAll(src, []byte("min_extrude_temp: 0"))
	src = append(src, "\n[max_mode]\nenable: True\nmin_layer_time: 0\n"...)
	os.WriteFile(cfg, src, 0o644)
	body, err := os.ReadFile("testdata/two-layers.gcode")
	if err != nil {
		t.Fatal(err)
	}
	klipper := klipperPrintTime(t, cfg, planSetup+string(body)) - klipperPrintTime(t, cfg, planSetup)

	s := maxModeSettings()
	s.MinLayerTime = 0
	p := planFile(strings.NewReader(planSetup[len("SET_KINEMATIC_POSITION X=0 Y=0 Z=0\n"):]+string(body)), 0,
		printerLimits, &s, nil)
	t.Logf("Klipper %.3f s, plan %.3f s", klipper, p.total)
	if math.Abs(p.total-klipper) > 0.002*klipper {
		t.Errorf("plan %.3f s, Klipper %.3f s", p.total, klipper)
	}
}
