package printer

import (
	"strings"
)

// The steps of a start macro (PRINT_START), from its text and from the commands that run now: the
// command stack of the Klipper host (printdeck_display). The steps follow the order of the macro,
// so they match the printer, also for a macro that wipes the nozzle while the bed heats.

// PrepStep is a step of the start of a print.
type PrepStep struct {
	Kind string // "home", "heat-nozzle", "heat-bed", "cool", "wipe", "probe", "prime"
	Name string // for example "Wipe the nozzle"
	Done bool
	Now  bool
}

var prepNames = map[string]string{
	"home": "Home", "heat-nozzle": "Heat the nozzle", "heat-bed": "Heat the bed", "cool": "Cool the nozzle",
	"wipe": "Wipe the nozzle", "probe": "Probe the bed", "prime": "Prime the nozzle",
}

// prepKind is the step of a command line, or "".
func prepKind(line string) string {
	l := strings.ToUpper(strings.TrimSpace(line))
	if i := strings.IndexByte(l, ';'); i >= 0 {
		l = strings.TrimSpace(l[:i])
	}
	f := strings.Fields(l)
	if len(f) == 0 {
		return ""
	}
	cmd := f[0]
	switch {
	case cmd == "G28":
		return "home"
	case cmd == "M109":
		return "heat-nozzle"
	case cmd == "M190":
		return "heat-bed"
	case cmd == "TEMPERATURE_WAIT":
		heater := "heat-nozzle"
		if strings.Contains(l, "SENSOR=HEATER_BED") {
			heater = "heat-bed"
		}
		if strings.Contains(l, "MAXIMUM") && !strings.Contains(l, "MINIMUM") {
			return "cool"
		}
		return heater
	case strings.Contains(cmd, "WIPE") || strings.Contains(cmd, "CLEAN_NOZZLE"):
		return "wipe"
	case cmd == "BED_TILT_CALIBRATE" || cmd == "BED_MESH_CALIBRATE" || cmd == "G29" || cmd == "Z_TILT_ADJUST" ||
		cmd == "QUAD_GANTRY_LEVEL" || cmd == "PROBE" || cmd == "SCREWS_TILT_CALCULATE":
		return "probe"
	case strings.Contains(cmd, "PRIME") || strings.Contains(cmd, "PURGE"):
		return "prime"
	}
	return ""
}

// prepPlan is the steps of a macro text, in their order. Two steps of the same kind next to each
// other are one step. Extrusion moves after the last step are the prime.
func prepPlan(text string) []string {
	var plan []string
	extrudes := false
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "{%") {
			continue
		}
		k := prepKind(t)
		if k == "" {
			u := strings.ToUpper(t)
			if (strings.HasPrefix(u, "G1 ") || strings.HasPrefix(u, "G0 ")) && strings.Contains(u, " E") && len(plan) > 0 {
				extrudes = true
			}
			continue
		}
		extrudes = false
		if len(plan) == 0 || plan[len(plan)-1] != k {
			plan = append(plan, k)
		}
	}
	if extrudes && plan[len(plan)-1] != "prime" {
		plan = append(plan, "prime")
	}
	return plan
}

// prepTracker follows a start macro: the index of the step that runs now.
type prepTracker struct {
	macro string
	plan  []string
	at    int
}

// steps returns the steps of the start macro of the command stack, or nil when the stack has no
// macro with steps. macros has the text of each macro, by its name in lower case.
func (t *prepTracker) steps(stack []string, macros map[string]string) []PrepStep {
	if len(stack) == 0 {
		t.macro = ""
		return nil
	}
	f := strings.Fields(stack[0])
	if len(f) == 0 {
		return nil
	}
	name := strings.ToLower(f[0])
	if name != t.macro {
		text, ok := macros[name]
		if !ok {
			t.macro = ""
			return nil
		}
		t.macro, t.plan, t.at = name, prepPlan(text), 0
	}
	if len(t.plan) < 2 {
		return nil
	}
	// The deepest command with a step is the step now. A step goes forward only.
	for i := len(stack) - 1; i > 0; i-- {
		k := prepKind(stack[i])
		if k == "" {
			u := strings.ToUpper(stack[i])
			if t.at > 0 && (strings.HasPrefix(u, "G1 ") || strings.HasPrefix(u, "G0 ")) && strings.Contains(u, " E") &&
				t.plan[len(t.plan)-1] == "prime" {
				k = "prime"
			} else {
				continue
			}
		}
		for j := t.at; j < len(t.plan); j++ {
			if t.plan[j] == k {
				t.at = j
				break
			}
		}
		break
	}
	out := make([]PrepStep, len(t.plan))
	for i, k := range t.plan {
		out[i] = PrepStep{Kind: k, Name: prepNames[k], Done: i < t.at, Now: i == t.at}
	}
	return out
}
