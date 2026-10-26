package printer

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

// The PRINT_START of the TAZ Workhorse (printer/printer.cfg), in short.
const workhorseStart = `
{% set bed = params.BED|default(80)|float %}
M104 S{wipe}
M140 S{bed}
G28
M117 Heating...
TEMPERATURE_WAIT SENSOR=extruder MINIMUM={wipe - 2}
G1 E-3 F500                 ; retract to reduce drool
NOZZLE_WIPE
M104 S0
TEMPERATURE_WAIT SENSOR=extruder MAXIMUM={probe}
BED_TILT_CALIBRATE
M104 S{extruder}
TEMPERATURE_WAIT SENSOR=heater_bed MINIMUM={bed - 1}
TEMPERATURE_WAIT SENSOR=extruder MINIMUM={extruder - 2}
G0 Z{layer} F1500
G1 E4 F300
G1 X{prime_x1} E{prime_e} F{speed}
`

func TestPrepPlan(t *testing.T) {
	got := strings.Join(prepPlan(workhorseStart), " ")
	want := "home heat-nozzle wipe cool probe heat-bed heat-nozzle prime"
	if got != want {
		t.Errorf("plan\n got %s\nwant %s", got, want)
	}
}

func TestPrepSteps(t *testing.T) {
	macros := map[string]string{"print_start": workhorseStart}
	var tr prepTracker
	now := func(stack ...string) string {
		var out []string
		for _, s := range tr.steps(stack, macros, 2) {
			mark := " "
			switch {
			case s.Now:
				mark = ">"
			case s.Done:
				mark = "x"
			}
			out = append(out, mark+s.Kind)
		}
		return strings.Join(out, " ")
	}
	// While the bed heats in the background, the printer wipes: the step is the wipe.
	now("PRINT_START BED=80", "G28")
	now("PRINT_START BED=80", "TEMPERATURE_WAIT SENSOR=extruder MINIMUM=168")
	got := now("PRINT_START BED=80", "NOZZLE_WIPE", "G1 X10 Y300 F3000")
	if !strings.HasPrefix(got, "xhome xheat-nozzle >wipe") {
		t.Errorf("wipe: %s", got)
	}
	// A move in the macro between two steps keeps the step.
	if got := now("PRINT_START BED=80", "G1 X20 Y2 Z10 F5000"); !strings.Contains(got, ">wipe") {
		t.Errorf("between steps: %s", got)
	}
	now("PRINT_START BED=80", "BED_TILT_CALIBRATE")
	got = now("PRINT_START BED=80", "TEMPERATURE_WAIT SENSOR=heater_bed MINIMUM=79")
	if !strings.Contains(got, ">heat-bed") || !strings.Contains(got, "xprobe") {
		t.Errorf("bed: %s", got)
	}
	got = now("PRINT_START BED=80", "G1 X260 E1.2 F900")
	if !strings.HasSuffix(got, ">prime") {
		t.Errorf("prime: %s", got)
	}
	// A G-code line of the file: no steps.
	if s := tr.steps([]string{"G1 X10 Y10 E0.5"}, macros, 2); s != nil {
		t.Errorf("steps for a line of the file: %v", fmt.Sprint(s))
	}
}

// An end macro that waits for the bed to cool has one step: Cool the bed.
func TestEndMacroCoolsBed(t *testing.T) {
	macros := map[string]string{"print_end": "M400\nM140 S45\nM104 S0\nG1 Z10\nM117 Cooling, please wait\nTEMPERATURE_WAIT SENSOR=heater_bed MAXIMUM=46\nG1 Y260 F1000\nM84"}
	var tr prepTracker
	if s := tr.steps([]string{"PRINT_END"}, macros, 2); s != nil {
		t.Errorf("a start needs two steps: %v", s)
	}
	s := tr.steps([]string{"PRINT_END", "TEMPERATURE_WAIT SENSOR=heater_bed MAXIMUM=46"}, macros, 1)
	if len(s) != 1 || s[0].Kind != "cool-bed" || s[0].Name != "Cool the bed" || !s[0].Now {
		t.Errorf("steps %+v", s)
	}
}

// The heats of the PRINT_START of printer/printer.cfg: the nozzle heats to wipe, cools to probe,
// and heats to print.
const heatStart = `
{% set bed = params.BED|default(80)|float %}
{% set extruder = params.EXTRUDER|default(235)|float %}
{% set wipe = params.WIPE|default(170)|float %}
{% set probe = params.PROBE|default(120)|float %}
{% set prime_x0, prime_x1 = 20.0, 260.0 %}
{% set prime_speed = [15, 15.0 / 0.24]|min %}
M104 S{wipe}
M140 S{bed}
G28
TEMPERATURE_WAIT SENSOR=extruder MINIMUM={wipe - 2}
NOZZLE_WIPE
M104 S0
TEMPERATURE_WAIT SENSOR=extruder MAXIMUM={probe}
BED_TILT_CALIBRATE
M104 S{extruder}
TEMPERATURE_WAIT SENSOR=heater_bed MINIMUM={bed - 1}
TEMPERATURE_WAIT SENSOR=extruder MINIMUM={extruder - 2}
G1 X{prime_x1} E5 F{prime_speed}
`

// Each heating and cooling step has its target from the macro and the parameters of its call,
// or from the defaults of the macro.
func TestPrepTargets(t *testing.T) {
	for _, tc := range []struct {
		call string
		want []float64
	}{
		{"PRINT_START BED=60 EXTRUDER=210", []float64{0, 168, 0, 120, 0, 59, 208, 0}},
		{"PRINT_START", []float64{0, 168, 0, 120, 0, 79, 233, 0}},
		{"PRINT_START BED=100 EXTRUDER=250 WIPE=180 PROBE=150", []float64{0, 178, 0, 150, 0, 99, 248, 0}},
	} {
		kinds, targets := prepSteps(heatStart, prepParams(tc.call))
		if got := strings.Join(kinds, " "); got != "home heat-nozzle wipe cool probe heat-bed heat-nozzle prime" {
			t.Errorf("%s: steps %s", tc.call, got)
		}
		if fmt.Sprint(targets) != fmt.Sprint(tc.want) {
			t.Errorf("%s: targets %v, want %v", tc.call, targets, tc.want)
		}
	}
	// M109 and M190 give the target with S.
	if _, targets := prepSteps("M190 S{params.BED}\nM109 S215\n", prepParams("X BED=65")); fmt.Sprint(targets) != "[65 215]" {
		t.Errorf("M109 and M190: %v", targets)
	}
	// An expression that is not simple gives no target.
	if _, targets := prepSteps("TEMPERATURE_WAIT SENSOR=extruder MINIMUM={[a, b]|max}\n", nil); targets[0] != 0 {
		t.Errorf("an expression: %v", targets)
	}
}

// The nozzle heats twice: from the room to the wipe temperature, and from the probe temperature to
// the print temperature. The time is the sum of the two heats, at the rate of the nozzle.
func TestPrepLeftHeatsEachStepFromTheStepBefore(t *testing.T) {
	r := ComputeRates(nil)
	r.Heat["heat-nozzle"], r.Heat["heat-bed"] = 0.5, 4
	for k := range r.Step {
		r.Step[k] = 0
	}
	var tr prepTracker
	st := State{Bed: 25, Tool: 25, BedTarget: 60, ToolTarget: 170}
	st.PrepN = copy(st.Prep[:], tr.steps([]string{"PRINT_START BED=60 EXTRUDER=210"}, map[string]string{"print_start": heatStart}, 2))
	got := r.prepLeft(st, &timer{}, time.Time{}).Seconds()
	// The nozzle: 25 to 168, then 120 to 208. The bed: 25 to 59.
	if want := (168-25)*0.5 + (208-120)*0.5 + (59-25)*4.0; math.Abs(got-want) > 1e-9 {
		t.Errorf("prep left %.1f s, want %.1f s", got, want)
	}
	// After the wipe, the nozzle cools: the heat to print starts at the probe temperature.
	st.Tool = 165
	for i := range st.Prep[:st.PrepN] {
		st.Prep[i].Done, st.Prep[i].Now = i < 3, i == 3
	}
	got = r.prepLeft(st, &timer{}, time.Time{}).Seconds()
	if want := (208-120)*0.5 + (59-25)*4.0; math.Abs(got-want) > 1e-9 {
		t.Errorf("after the wipe: prep left %.1f s, want %.1f s", got, want)
	}
}
