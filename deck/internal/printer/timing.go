package printer

import (
	"math"
	"sort"
	"time"
)

// The timings of a print: how long each step of the start macro takes on this printer, and how
// long the bed takes to cool after a print. The history keeps them with each print. While a print
// prepares, its time left uses them: the heating from the rate of this printer (seconds for each
// degree), and the other steps from their usual time.

// ReleaseBed is the bed temperature below which a part comes off a PEI sheet easily.
const ReleaseBed = 35.0

// The cooling of the bed. A bed with its heater off cools toward the room temperature by Newton's
// law: the difference to the room falls by the same fraction in each second,
//
//	T(t) = room + (T0 - room) * exp(-t / tau)
//
// tau, the time constant of the bed, comes from the cooling after each print in the history, and
// from the cooling that runs now. The room temperature is the lowest bed temperature with the
// heater off in the last day: a bed that is off for some hours has the temperature of the room.
const (
	defaultRoom = 22.0  // the room temperature before the deck saw a cold bed
	maxRoom     = 32.0  // a warmer bed is not at the room temperature
	defaultTau  = 900.0 // seconds; a guess for a large bed, before the history has a cooling
	// A cooling that runs gives its own tau after a fall of liveDrop degrees, and its tau has the
	// full weight after liveFull seconds.
	liveDrop = 3.0
	liveFull = 600.0
)

// coolTau is the time constant of a cooling from the temperature from to to in seconds, with the
// room temperature room; 0 when the temperatures give no time constant.
func coolTau(seconds, from, to, room float64) float64 {
	if seconds <= 0 || from-room <= 0 || to-room <= 0 || from <= to {
		return 0
	}
	return seconds / math.Log((from-room)/(to-room))
}

// coolTime is the time that the bed needs to cool from the temperature from to to, with the
// time constant tau. ok is false when the bed does not reach to: to is at the room temperature
// or under it.
func coolTime(from, to, room, tau float64) (seconds float64, ok bool) {
	if from <= to {
		return 0, true
	}
	if to-room < 0.5 || tau <= 0 {
		return 0, false
	}
	return tau * math.Log((from-room)/(to-room)), true
}

// Phase is the time of one step of the start macro of a print.
type Phase struct {
	Kind    string  `json:"kind"` // the kind of the step, as PrepStep.Kind
	Seconds float64 `json:"s"`
	From    float64 `json:"from,omitempty"` // a heating step: the temperature at its start
	To      float64 `json:"to,omitempty"`   // and its target
}

// The rates and times before this printer has a history: a guess for a printer like the TAZ
// Workhorse. The history replaces each one when it has a step of that kind.
var defaultRates = map[string]float64{"heat-bed": 6.0, "heat-nozzle": 0.7}                          // seconds for each degree
var defaultTimes = map[string]float64{"home": 20, "probe": 90, "wipe": 40, "prime": 20, "cool": 60} // seconds

// timer follows the steps of the start macro of the running print, and the cooling of the bed
// after the end of a print.
type timer struct {
	busy     bool   // a print runs
	kind     string // the kind of the step that runs now; "" is none
	start    time.Time
	from, to float64
	phases   []Phase
	planned  float64 // the seconds of the moves in the plan of the file

	coolStart time.Time // the end of the last print; zero when no cooling is followed
	coolKey   string    // the key of its record in the history
	coolFrom  float64   // the bed temperature at the end of the last print

	// The bed with its heater off: the start of the fall of its temperature, and the temperature
	// then. fallStart is zero while the heater is on.
	fallStart time.Time
	fallFrom  float64
	// The room temperature: the lowest bed temperature with the heater off, and its time.
	room   float64
	roomAt time.Time
}

// observe follows the bed with its heater off: the fall of its temperature, and the room
// temperature.
func (t *timer) observe(st State, now time.Time) {
	if !st.Reachable {
		return
	}
	off := st.BedTarget == 0 || st.Bed > st.BedTarget+1
	if off && st.BedPower == 0 {
		// The fall starts at the highest temperature: the bed can still rise a little after the
		// heater stops.
		if t.fallStart.IsZero() || st.Bed > t.fallFrom {
			t.fallStart, t.fallFrom = now, st.Bed
		}
	} else {
		t.fallStart = time.Time{}
	}
	if st.BedTarget == 0 && st.Bed > 5 && st.Bed < maxRoom &&
		(t.room == 0 || st.Bed < t.room || now.Sub(t.roomAt) > 24*time.Hour) {
		t.room, t.roomAt = st.Bed, now
	}
}

// roomTemp is the room temperature.
func (t *timer) roomTemp() float64 {
	if t.room == 0 {
		return defaultRoom
	}
	return t.room
}

// coolLeft is the time that the bed needs to cool to the temperature to: with the time constant
// of the history, and the time constant of the cooling that runs now when it fell enough.
func (t *timer) coolLeft(r Rates, st State, to float64, now time.Time) (time.Duration, bool) {
	room := t.roomTemp()
	tau := r.CoolTau
	if tau == 0 {
		tau = defaultTau
	}
	if !t.fallStart.IsZero() && t.fallFrom-st.Bed >= liveDrop {
		elapsed := now.Sub(t.fallStart).Seconds()
		if live := coolTau(elapsed, t.fallFrom, st.Bed, room); live > 0 {
			w := min(1, elapsed/liveFull)
			if r.CoolTau == 0 {
				w = 1 // no history: the cooling that runs knows more than the guess
			}
			tau = w*live + (1-w)*tau
		}
	}
	s, ok := coolTime(st.Bed, to, room, tau)
	return time.Duration(s * float64(time.Second)), ok
}

// heatTemps returns the temperature and the target of the heater of a heating step.
func heatTemps(kind string, st State) (temp, target float64, ok bool) {
	switch kind {
	case "heat-bed":
		return st.Bed, st.BedTarget, true
	case "heat-nozzle":
		return st.Tool, st.ToolTarget, true
	}
	return 0, 0, false
}

// update follows the steps of the start macro in the state st at the time now.
func (t *timer) update(st State, now time.Time) {
	if !st.Busy() {
		t.close(now)
		t.busy = false
		return
	}
	if !t.busy { // a new print: new timings, and no more cooling to follow
		*t = timer{busy: true}
	}
	current := ""
	for _, s := range st.Prep[:st.PrepN] {
		if s.Now {
			current = s.Kind
		}
	}
	if current != t.kind {
		t.close(now)
		if current != "" {
			t.kind, t.start = current, now
			t.from, t.to, _ = heatTemps(current, st)
		}
	}
	// The target of a heating step can come after the step starts.
	if _, target, ok := heatTemps(t.kind, st); ok && target > 0 {
		t.to = target
	}
}

// close ends the step that runs now.
func (t *timer) close(now time.Time) {
	if t.kind == "" {
		return
	}
	if s := now.Sub(t.start).Seconds(); s > 0 {
		p := Phase{Kind: t.kind, Seconds: s}
		if _, _, heat := heatTemps(t.kind, State{}); heat {
			p.From, p.To = t.from, t.to
		}
		t.phases = append(t.phases, p)
	}
	t.kind = ""
}

// cooling follows the bed after the end of a print, until it is below ReleaseBed with its heater
// off. It returns the seconds of the cooling and the key of the record, once.
func (t *timer) cooling(st State, now time.Time) (seconds float64, key string, done bool) {
	if t.coolStart.IsZero() || st.Busy() {
		return 0, "", false
	}
	if st.BedTarget > 0 {
		t.coolStart = time.Time{} // the bed heats again: this is no cooling
		return 0, "", false
	}
	if st.Bed > ReleaseBed {
		return 0, "", false
	}
	seconds, key = now.Sub(t.coolStart).Seconds(), t.coolKey
	t.coolStart = time.Time{}
	return seconds, key, true
}

// Rates are the timings of this printer, from its history: the seconds for each degree of each
// heater, the seconds of each other kind of step, and the minutes that the bed takes to cool.
type Rates struct {
	Heat    map[string]float64 // heat-bed, heat-nozzle: seconds for each degree
	Step    map[string]float64 // the other kinds: seconds
	Target  map[string]float64 // heat-bed, heat-nozzle: the usual target, for a step whose target is not set yet
	CoolBed float64            // seconds from the end of a print to ReleaseBed; 0 is not known
	CoolTau float64            // the time constant of the cooling of the bed in seconds; 0 is not known
	Samples map[string]int     // the number of steps of each kind in the history
	// PlanError is the median of (real - planned) / planned for the moves of the completed
	// prints: the real time of the moves is the print time less the steps of the start macro.
	// PlanSamples is the number of prints in it.
	PlanError   float64
	PlanSamples int
}

// The limits of the plan factor: the prints that it needs, and its range, so that one strange
// record does not change the estimates much.
const (
	planMinSamples = 3
	planMinFactor  = 0.8
	planMaxFactor  = 1.5
)

// PlanFactor is the real time of the moves on this printer for each second of the plan:
// 1 + PlanError, between planMinFactor and planMaxFactor. With fewer than planMinSamples prints
// in the history, it is 1.
func (r Rates) PlanFactor() float64 {
	if r.PlanSamples < planMinSamples {
		return 1
	}
	return max(planMinFactor, min(planMaxFactor, 1+r.PlanError))
}

// median of a list of numbers; 0 for none.
func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	v = append([]float64(nil), v...)
	sort.Float64s(v)
	if n := len(v); n%2 == 1 {
		return v[n/2]
	} else {
		return (v[n/2-1] + v[n/2]) / 2
	}
}

// ComputeRates makes the rates from the history. A kind with no step in the history keeps its
// default.
func ComputeRates(recs []Record) Rates {
	r := Rates{Heat: map[string]float64{}, Step: map[string]float64{}, Target: map[string]float64{}, Samples: map[string]int{}}
	heat, step, target := map[string][]float64{}, map[string][]float64{}, map[string][]float64{}
	var cool, taus, plan []float64
	for _, rec := range recs {
		if rec.Result == "complete" && rec.Planned > 0 {
			moves := rec.Duration.Seconds()
			for _, p := range rec.Phases {
				moves -= p.Seconds
			}
			plan = append(plan, (moves-rec.Planned)/rec.Planned)
		}
		for _, p := range rec.Phases {
			r.Samples[p.Kind]++
			if _, ok := defaultRates[p.Kind]; ok {
				// A short heating says little about the rate.
				if p.To-p.From >= 10 {
					heat[p.Kind] = append(heat[p.Kind], p.Seconds/(p.To-p.From))
				}
				if p.To > 0 {
					target[p.Kind] = append(target[p.Kind], p.To)
				}
			} else {
				step[p.Kind] = append(step[p.Kind], p.Seconds)
			}
		}
		if rec.CoolBed > 0 {
			cool = append(cool, rec.CoolBed)
			// An older record has no start temperature: the print ended at the usual bed
			// target, in a room at the default temperature.
			from, room := rec.CoolFrom, rec.CoolRoom
			if room == 0 {
				room = defaultRoom
			}
			if from == 0 {
				for _, p := range rec.Phases {
					if p.Kind == "heat-bed" && p.To > 0 {
						from = p.To
					}
				}
			}
			if tau := coolTau(rec.CoolBed, from, ReleaseBed, room); from >= ReleaseBed+5 && tau > 0 {
				taus = append(taus, tau)
			}
		}
	}
	for k, d := range defaultRates {
		r.Heat[k] = d
		if m := median(heat[k]); m > 0 {
			r.Heat[k] = m
		}
		r.Target[k] = median(target[k])
	}
	for k, d := range defaultTimes {
		r.Step[k] = d
	}
	for k, v := range step {
		r.Step[k] = median(v)
	}
	r.CoolBed = median(cool)
	r.CoolTau = median(taus)
	r.PlanError, r.PlanSamples = median(plan), len(plan)
	return r
}

// prepLeft is the time that the steps of the start macro still need: the step that runs now,
// and the steps after it. A heating step needs the degrees to its target at the rate of its
// heater; another step its usual time, less the time that it ran.
func (r Rates) prepLeft(st State, t *timer, now time.Time) time.Duration {
	left := 0.0
	for _, s := range st.Prep[:st.PrepN] {
		if s.Done {
			continue
		}
		if temp, target, ok := heatTemps(s.Kind, st); ok {
			if target == 0 {
				target = r.Target[s.Kind]
			}
			if target > temp {
				left += (target - temp) * r.Heat[s.Kind]
			}
			continue
		}
		d := r.Step[s.Kind]
		if s.Now && t.kind == s.Kind {
			d = max(0, d-now.Sub(t.start).Seconds())
		}
		left += d
	}
	return time.Duration(left * float64(time.Second))
}

// coolTarget is the temperature that the bed cools to: the target of the end of a print while the
// bed is above it, or ReleaseBed after a print. 0 is no cooling.
func coolTarget(st State) float64 {
	switch {
	case st.Finishing && st.BedTarget > 0 && st.Bed > st.BedTarget+1:
		return st.BedTarget
	case (st.Finishing || !st.Busy()) && st.BedTarget == 0 && st.Bed > ReleaseBed:
		return ReleaseBed
	}
	return 0
}
