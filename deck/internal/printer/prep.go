package printer

import (
	"regexp"
	"strconv"
	"strings"
)

// The steps of a start macro (PRINT_START) or an end macro (PRINT_END), from its text and from the
// commands that run now: the command stack of the Klipper host (printdeck_display). The steps
// follow the order of the macro, so they match the printer, also for a macro that wipes the nozzle
// while the bed heats.

// PrepStep is a step of the start of a print.
type PrepStep struct {
	Kind string // "home", "heat-nozzle", "heat-bed", "cool", "cool-bed", "wipe", "probe", "prime"
	Name string // for example "Wipe the nozzle"
	// Target is the temperature of a heating or a cooling step in the macro, in degrees; 0 is
	// not known. A macro can heat the nozzle twice: to wipe it, then to print.
	Target float64
	Done   bool
	Now    bool
}

var prepNames = map[string]string{
	"home": "Home", "heat-nozzle": "Heat the nozzle", "heat-bed": "Heat the bed", "cool": "Cool the nozzle", "cool-bed": "Cool the bed",
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
			if heater == "heat-bed" {
				return "cool-bed"
			}
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
	kinds, _ := prepSteps(text, nil)
	return kinds
}

// prepSteps is the steps of a macro text (prepPlan), and the target of each heating and cooling
// step, with the parameters of the call of the macro (prepParams). A target is 0 when the macro
// does not give it as a number or a simple expression.
func prepSteps(text string, params map[string]string) (kinds []string, targets []float64) {
	vars := map[string]float64{}
	extrudes := false
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if m := setLine.FindStringSubmatch(t); m != nil {
			if v, ok := macroValue(m[2], vars, params); ok {
				vars[m[1]] = v
			}
			continue
		}
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "{%") {
			continue
		}
		k := prepKind(t)
		if k == "" {
			u := strings.ToUpper(t)
			if (strings.HasPrefix(u, "G1 ") || strings.HasPrefix(u, "G0 ")) && strings.Contains(u, " E") && len(kinds) > 0 {
				extrudes = true
			}
			continue
		}
		extrudes = false
		if len(kinds) == 0 || kinds[len(kinds)-1] != k {
			kinds, targets = append(kinds, k), append(targets, 0)
		}
		if v := prepTarget(t, k, vars, params); v > 0 {
			targets[len(targets)-1] = v
		}
	}
	if extrudes && kinds[len(kinds)-1] != "prime" {
		kinds, targets = append(kinds, "prime"), append(targets, 0)
	}
	return kinds, targets
}

var (
	// setLine is a {% set name = value %} line of a macro.
	setLine = regexp.MustCompile(`^\{%-?\s*set\s+(\w+)\s*=\s*(.*?)\s*-?%\}$`)
	// The value of the target in a heating or a cooling command: a number or a {expression}.
	sValue   = regexp.MustCompile(`(?i)^M1(?:09|90)\b.*?\s[SR](\{[^}]*\}|[-\d.]+)`)
	minValue = regexp.MustCompile(`(?i)\bMINIMUM=(\{[^}]*\}|[-\d.]+)`)
	maxValue = regexp.MustCompile(`(?i)\bMAXIMUM=(\{[^}]*\}|[-\d.]+)`)
	// paramDefault is params.NAME|default(value) in a macro.
	paramDefault = regexp.MustCompile(`params\.(\w+)\s*\|\s*default\(\s*([^)]*?)\s*\)`)
	exprToken    = regexp.MustCompile(`\d+\.?\d*|\.\d+|[A-Za-z_][\w.]*|[-+*/()]`)
)

// prepParams is the parameters of the call of a macro: "PRINT_START BED=60 EXTRUDER=210" gives
// BED and EXTRUDER.
func prepParams(call string) map[string]string {
	params := map[string]string{}
	for _, f := range strings.Fields(call) {
		if k, v, ok := strings.Cut(f, "="); ok {
			params[strings.ToUpper(k)] = v
		}
	}
	return params
}

// prepTarget is the target of a step of the kind k on the command line t, or 0.
func prepTarget(t, k string, vars map[string]float64, params map[string]string) float64 {
	var m []string
	switch k {
	case "heat-nozzle", "heat-bed":
		if m = sValue.FindStringSubmatch(t); m == nil {
			m = minValue.FindStringSubmatch(t)
		}
	case "cool", "cool-bed":
		m = maxValue.FindStringSubmatch(t)
	}
	if m == nil {
		return 0
	}
	v, _ := macroValue(strings.Trim(m[1], "{}"), vars, params)
	return v
}

// macroValue is the value of a simple expression of a macro: numbers, the variables of the set
// lines before it, params.NAME|default(value), the float and int filters, and + - * / with
// brackets. ok is false for another expression.
func macroValue(expr string, vars map[string]float64, params map[string]string) (v float64, ok bool) {
	expr = paramDefault.ReplaceAllStringFunc(expr, func(s string) string {
		m := paramDefault.FindStringSubmatch(s)
		if p, ok := params[strings.ToUpper(m[1])]; ok {
			return p
		}
		return m[2]
	})
	expr = strings.NewReplacer("|float", "", "|int", "", "| float", "", "| int", "").Replace(expr)
	toks := exprToken.FindAllString(expr, -1)
	if strings.Join(toks, "") != strings.Join(strings.Fields(expr), "") {
		return 0, false // a part that is no token: a filter, a list, a comparison
	}
	p := exprParser{toks: toks, vars: vars, params: params, ok: true}
	v = p.sum()
	return v, p.ok && p.i == len(toks)
}

// exprParser reads the tokens of macroValue.
type exprParser struct {
	toks   []string
	i      int
	vars   map[string]float64
	params map[string]string
	ok     bool
}

func (p *exprParser) next() string {
	if p.i < len(p.toks) {
		return p.toks[p.i]
	}
	return ""
}

func (p *exprParser) sum() float64 {
	v := p.product()
	for op := p.next(); op == "+" || op == "-"; op = p.next() {
		p.i++
		if r := p.product(); op == "+" {
			v += r
		} else {
			v -= r
		}
	}
	return v
}

func (p *exprParser) product() float64 {
	v := p.unit()
	for op := p.next(); op == "*" || op == "/"; op = p.next() {
		p.i++
		r := p.unit()
		if op == "*" {
			v *= r
		} else if r != 0 {
			v /= r
		} else {
			p.ok = false
		}
	}
	return v
}

func (p *exprParser) unit() float64 {
	t := p.next()
	p.i++
	switch {
	case t == "-":
		return -p.unit()
	case t == "(":
		v := p.sum()
		if p.next() != ")" {
			p.ok = false
		}
		p.i++
		return v
	case t != "" && (t[0] >= '0' && t[0] <= '9' || t[0] == '.'):
		v, err := strconv.ParseFloat(t, 64)
		p.ok = p.ok && err == nil
		return v
	}
	if v, ok := p.vars[t]; ok {
		return v
	}
	if name, ok := strings.CutPrefix(t, "params."); ok {
		if s, ok := p.params[strings.ToUpper(name)]; ok {
			if v, err := strconv.ParseFloat(s, 64); err == nil {
				return v
			}
		}
	}
	p.ok = false
	return 0
}

// prepTracker follows a start macro: the index of the step that runs now.
type prepTracker struct {
	macro   string
	plan    []string
	targets []float64
	at      int
}

// steps returns the steps of the macro of the command stack, or nil when the stack has no macro
// with at least min steps. macros has the text of each macro, by its name in lower case.
func (t *prepTracker) steps(stack []string, macros map[string]string, min int) []PrepStep {
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
		t.plan, t.targets = prepSteps(text, prepParams(stack[0]))
		t.macro, t.at = name, 0
	}
	if len(t.plan) < min {
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
		out[i] = PrepStep{Kind: k, Name: prepNames[k], Target: t.targets[i], Done: i < t.at, Now: i == t.at}
	}
	return out
}
