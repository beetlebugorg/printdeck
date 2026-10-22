package printer

import (
	"math"
	"testing"
	"time"
)

// steps makes the state of a print that prepares, with the step at index now running and the
// steps before it done.
func steps(st State, now int, kinds ...string) State {
	st.Printing, st.Preparing = true, now < len(kinds)
	st.PrepN = len(kinds)
	for i, k := range kinds {
		st.Prep[i] = PrepStep{Kind: k, Done: i < now, Now: i == now}
	}
	return st
}

// A print: the bed heats from 23° to 60° in 222 s, the nozzle from 24° to 210° in 130 s, and the
// probe takes 80 s. After the print, the bed takes 600 s to cool below ReleaseBed. The timer
// records the steps, the history keeps them, and the rates come from them.
func TestTimer(t *testing.T) {
	kinds := []string{"heat-bed", "heat-nozzle", "probe"}
	t0 := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	at := func(s float64) time.Time { return t0.Add(time.Duration(s * float64(time.Second))) }
	var tm timer
	tm.update(steps(State{Bed: 23, BedTarget: 60, Tool: 24}, 0, kinds...), at(0))
	tm.update(steps(State{Bed: 60, BedTarget: 60, Tool: 24, ToolTarget: 210}, 1, kinds...), at(222))
	tm.update(steps(State{Bed: 60, BedTarget: 60, Tool: 210, ToolTarget: 210}, 2, kinds...), at(352))
	tm.update(steps(State{Bed: 60, BedTarget: 60, Tool: 210, ToolTarget: 210}, 3, kinds...), at(432))
	want := []Phase{{"heat-bed", 222, 23, 60}, {"heat-nozzle", 130, 24, 210}, {"probe", 80, 0, 0}}
	if len(tm.phases) != len(want) {
		t.Fatalf("phases %+v", tm.phases)
	}
	for i, p := range want {
		if tm.phases[i] != p {
			t.Errorf("phase %d: %+v, want %+v", i, tm.phases[i], p)
		}
	}

	// The end of the print, then the cooling of the bed.
	h := &History{}
	done := State{Finished: "complete", FinishedFile: "x.gcode", FinishedPath: "x.gcode", FinishedTime: time.Hour,
		Bed: 58}
	tm.update(done, at(4000))
	key := h.add(done, at(4000), tm.phases, 3300, 0, nil)
	if key == "" {
		t.Fatal("no record")
	}
	tm.coolStart, tm.coolKey = at(4000), key
	if _, _, ok := tm.cooling(State{Bed: 40}, at(4300)); ok {
		t.Error("the bed is not cool yet")
	}
	s, k, ok := tm.cooling(State{Bed: 34}, at(4600))
	if !ok || s != 600 || k != key {
		t.Fatalf("cooling %v %q %v", s, k, ok)
	}
	h.setCool(k, s, 0, 0)

	r := h.Rates()
	if math.Abs(r.Heat["heat-bed"]-6.0) > 0.01 || math.Abs(r.Heat["heat-nozzle"]-130.0/186) > 0.01 ||
		r.Step["probe"] != 80 || r.CoolBed != 600 || r.Target["heat-bed"] != 60 {
		t.Errorf("rates %+v", r)
	}

	// The next print: the bed is at 30° of 60°, so the steps still need 30 × 6 s for the bed,
	// 186 × 0.70 s for the nozzle (its target is not set yet: the usual 210°), and 80 s.
	var tm2 timer
	st := steps(State{Bed: 30, BedTarget: 60, Tool: 24}, 0, kinds...)
	tm2.update(st, at(0))
	got := r.prepLeft(st, &tm2, at(0)).Seconds()
	if wantLeft := 30*6.0 + 130 + 80; math.Abs(got-wantLeft) > 0.5 {
		t.Errorf("prep left %.1f s, want %.1f s", got, wantLeft)
	}
}

func TestCoolModel(t *testing.T) {
	// A bed with tau 600 s in a room at 20°: from 60° to 35° in 600 * ln(40/15) s.
	want := 600 * math.Log(40.0/15.0)
	if tau := coolTau(want, 60, 35, 20); math.Abs(tau-600) > 1e-6 {
		t.Errorf("tau %v", tau)
	}
	if s, ok := coolTime(60, 35, 20, 600); !ok || math.Abs(s-want) > 1e-6 {
		t.Errorf("time %v %v", s, ok)
	}
	if _, ok := coolTime(60, 20.2, 20, 600); ok {
		t.Error("the bed does not reach the room temperature")
	}
	if s, ok := coolTime(30, 35, 20, 600); !ok || s != 0 {
		t.Error("a cool bed needs no time")
	}

	// The history gives tau: two prints that cooled from 60° and 100° in a room at 20°.
	recs := []Record{
		{CoolBed: want, CoolFrom: 60, CoolRoom: 20},
		{CoolBed: 600 * math.Log(80.0/15.0), CoolFrom: 100, CoolRoom: 20},
	}
	r := ComputeRates(recs)
	if math.Abs(r.CoolTau-600) > 1e-6 {
		t.Errorf("rates tau %v", r.CoolTau)
	}

	// A cooling that runs: the room from a cold bed, then a fall of the bed with its heater off.
	var tm timer
	t0 := time.Unix(1000, 0)
	tm.observe(State{Reachable: true, Bed: 20}, t0)
	tm.observe(State{Reachable: true, Bed: 60}, t0.Add(time.Hour))
	// After 300 s the bed is at 20 + 40 exp(-300/600).
	at := t0.Add(time.Hour + 300*time.Second)
	st := State{Reachable: true, Bed: 20 + 40*math.Exp(-0.5)}
	tm.observe(st, at)
	left, ok := tm.coolLeft(Rates{}, st, 35, at)
	if !ok || math.Abs(left.Seconds()-(want-300)) > 1 {
		t.Errorf("left %v, want %v s", left, want-300)
	}
	// The heater heats again: no more fall.
	tm.observe(State{Reachable: true, Bed: 50, BedTarget: 60, BedPower: 0.8}, at.Add(time.Second))
	if !tm.fallStart.IsZero() {
		t.Error("the fall goes on while the bed heats")
	}
}
