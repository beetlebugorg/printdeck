package printer

import (
	"fmt"
	"strings"
	"testing"
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
