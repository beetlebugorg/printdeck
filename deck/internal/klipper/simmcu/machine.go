package simmcu

import (
	"math"
	"strconv"
	"strings"
	"sync"
)

// The mechanics of the simulated printer, from the config of the host (Configure). With them the
// steps of the board are places in mm: an axis starts in the middle of its travel, an endstop
// triggers when its axis gets to position_endstop, and the probe triggers when the nozzle touches
// a washer. The place of an axis in mm is the place of the printer: after a homing, the host and
// the board agree on it.

// axis is a stepper of an axis of the printer.
type axis struct {
	index    int     // 0 X, 1 Y, 2 Z
	stepDist float64 // mm for each step of the board, less than 0 for an inverted dir_pin
	start    float64 // the place in mm at the step 0 of the World: the middle of the travel
}

// washer is a conductive washer of the probe: the nozzle triggers the probe on it.
type washer struct{ x, y, r float64 }

// machine is the mechanics. A nil machine has no mm: an endstop triggers in its move, as before
// Configure.
type machine struct {
	axes     map[string]axis    // by step pin
	switches map[string]float64 // the place in mm of each endstop on its axis, by pin
	probe    string             // the pin of the probe
	contact  float64            // the height of the washer tops over the bed, in mm
	washers  []washer
}

var (
	machineMu sync.Mutex
	mech      *machine
)

// The washers of the TAZ Workhorse: the centers in the CAD of LulzBot (desktop/cad/export.py) are
// 308 mm apart, around the middle of the bed (140, 140), so each is 4 mm out from its probe point
// (-10 and 290). They are 22 mm wide.
var workhorseWashers = []washer{{-14, -14, 11}, {294, -14, 11}, {294, 294, 11}, {-14, 294, 11}}

// Configure gives the board the mechanics of the printer: get reads an option of a section of the
// config. The washer tops are at the z_offset of the probe, so the probe puts Z 0 on the bed.
func Configure(get func(section, option string) (string, bool)) {
	m := &machine{axes: map[string]axis{}, switches: map[string]float64{}, washers: workhorseWashers}
	f := func(section, option string) (float64, bool) {
		v, ok := get(section, option)
		if !ok {
			return 0, false
		}
		x, err := strconv.ParseFloat(strings.TrimSpace(strings.SplitN(v, "#", 2)[0]), 64)
		return x, err == nil
	}
	pin := func(v string) (string, bool) {
		v = strings.TrimSpace(strings.SplitN(v, "#", 2)[0])
		return strings.TrimLeft(v, "^~!"), strings.Contains(v, "!")
	}
	for i, name := range []string{"stepper_x", "stepper_y", "stepper_z"} {
		step, ok1 := get(name, "step_pin")
		dir, ok2 := get(name, "dir_pin")
		rot, ok3 := f(name, "rotation_distance")
		micro, ok4 := f(name, "microsteps")
		if !ok1 || !ok2 || !ok3 || !ok4 {
			continue
		}
		full, ok := f(name, "full_steps_per_rotation")
		if !ok {
			full = 200
		}
		lo, _ := f(name, "position_min")
		hi, _ := f(name, "position_max")
		sp, _ := pin(step)
		_, inverted := pin(dir)
		a := axis{index: i, stepDist: rot / (full * micro), start: (lo + hi) / 2}
		if inverted {
			a.stepDist = -a.stepDist
		}
		m.axes[sp] = a
		if e, ok := get(name, "endstop_pin"); ok {
			if at, ok := f(name, "position_endstop"); ok {
				ep, _ := pin(e)
				m.switches[ep] = at
			}
		}
	}
	if p, ok := get("probe", "pin"); ok {
		m.probe, _ = pin(p)
		m.contact, _ = f("probe", "z_offset")
	}
	machineMu.Lock()
	mech = m
	machineMu.Unlock()
}

func theMachine() *machine {
	machineMu.Lock()
	defer machineMu.Unlock()
	return mech
}

// mm is the place in mm of the axis of the stepper at the clock.
func (m *machine) mm(s *stepper, clock uint64) (float64, int, bool) {
	a, ok := m.axes[s.pin]
	if !ok {
		return 0, 0, false
	}
	return a.start + a.stepDist*s.physical(clock), a.index, true
}

// place is the place of the nozzle at the clock: X, Y and Z in mm. b.mu is locked.
func (m *machine) place(b *Board, clock uint64) (p [3]float64, ok bool) {
	var have [3]bool
	for _, s := range b.steppers {
		if v, i, ok := m.mm(s, clock); ok && !have[i] {
			p[i], have[i] = v, true
		}
	}
	return p, have[0] && have[1] && have[2]
}

// touches is true when the nozzle at p is on a washer, at its top or under it.
func (m *machine) touches(p [3]float64) bool {
	for _, w := range m.washers {
		if math.Hypot(p[0]-w.x, p[1]-w.y) <= w.r {
			return p[2] <= m.contact
		}
	}
	return false
}

// Place is the place of the nozzle of the simulated printer now, in mm, for the 3D window.
func Place() ([3]float64, bool) {
	m := theMachine()
	boardMu.Lock()
	b := current
	boardMu.Unlock()
	if m == nil || b == nil {
		return [3]float64{}, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return m.place(b, b.now())
}
