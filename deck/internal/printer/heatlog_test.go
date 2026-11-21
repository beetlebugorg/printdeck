package printer

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"printdeck/internal/bambu"
)

var heatT0 = time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)

// feed sends one report each second from start: the temperature and the target of each second.
func feed(h *HeatLog, heater string, start time.Time, temps []float64, target func(i int) float64) time.Time {
	t := start
	for i, temp := range temps {
		t = start.Add(time.Duration(i) * time.Second)
		h.Sample(t, heater, temp, target(i))
	}
	return t
}

// ramp is the temperatures of a heater that rises by step each second from from to to, then
// holds at to for hold seconds.
func ramp(from, to, step float64, hold int) []float64 {
	var v []float64
	for x := from; x < to; x += step {
		v = append(v, x)
	}
	for range hold {
		v = append(v, to)
	}
	return v
}

// A normal heat-up: the target goes up, the bed rises, and stays in the band for 3 s. The sample
// has the start temperature, the target, the seconds to the band, and the room temperature.
func TestHeatLogHeatUp(t *testing.T) {
	h := &HeatLog{Path: filepath.Join(t.TempDir(), "heat.json")}
	h.SetRoom(24, true)
	h.Sample(heatT0, HeatBed, 22, 0)
	temps := ramp(22, 60, 0.5, 6) // 76 s to 60 °C
	feed(h, HeatBed, heatT0.Add(time.Second), temps, func(int) float64 { return 60 })
	got := h.Heats(HeatBed)
	if len(got) != 1 {
		t.Fatalf("%d samples, want 1", len(got))
	}
	s := got[0]
	// The first report in the band is 59.0 °C, 74 s after the start.
	if s.From != 22 || s.Target != 60 || s.Seconds != 74 {
		t.Errorf("sample %+v, want from 22 to 60 in 74 s", s)
	}
	if s.Room == nil || *s.Room != 24 {
		t.Errorf("room %v, want 24", s.Room)
	}
	// The log keeps the sample in its file.
	again := &HeatLog{Path: h.Path}
	if n := len(again.Heats(HeatBed)); n != 1 {
		t.Errorf("%d samples after a load, want 1", n)
	}
}

// A heat-up that leaves the band before 3 s ends later, at the first report of the band that
// holds.
func TestHeatLogHold(t *testing.T) {
	h := &HeatLog{}
	h.Sample(heatT0, HeatNozzle, 25, 0)
	temps := []float64{100, 199.5, 202, 197, 199.6, 200, 200, 200}
	feed(h, HeatNozzle, heatT0.Add(time.Second), temps, func(int) float64 { return 200 })
	got := h.Heats(HeatNozzle)
	if len(got) != 1 || got[0].Seconds != 4 || got[0].Room != nil {
		t.Fatalf("samples %+v, want one of 4 s with no room", got)
	}
}

// A change of the target in the middle drops the heat-up. A rise of the target starts a new one
// from the temperature then. A target of 0 drops the heat-up.
func TestHeatLogTargetChange(t *testing.T) {
	h := &HeatLog{}
	h.Sample(heatT0, HeatBed, 22, 0)
	temps := ramp(22, 70, 1, 5)
	// The target is 60 for 20 s, then 70.
	feed(h, HeatBed, heatT0.Add(time.Second), temps, func(i int) float64 {
		if i < 20 {
			return 60
		}
		return 70
	})
	got := h.Heats(HeatBed)
	if len(got) != 1 {
		t.Fatalf("%d samples, want 1: %+v", len(got), got)
	}
	if s := got[0]; s.Target != 70 || s.From != 42 || s.Seconds != 27 {
		t.Errorf("sample %+v, want from 42 to 70 in 27 s", s)
	}

	// The target goes off before the bed is at the target: no sample.
	h = &HeatLog{}
	h.Sample(heatT0, HeatBed, 22, 0)
	feed(h, HeatBed, heatT0.Add(time.Second), ramp(22, 60, 1, 5), func(i int) float64 {
		if i < 10 {
			return 60
		}
		return 0
	})
	if n := len(h.Heats(HeatBed)); n != 0 {
		t.Errorf("%d samples after the target went off, want 0", n)
	}

	// A target that goes down while the bed heats: no sample.
	h = &HeatLog{}
	h.Sample(heatT0, HeatBed, 22, 0)
	feed(h, HeatBed, heatT0.Add(time.Second), ramp(22, 50, 1, 5), func(i int) float64 {
		if i < 10 {
			return 60
		}
		return 50
	})
	if n := len(h.Heats(HeatBed)); n != 0 {
		t.Errorf("%d samples after the target went down, want 0", n)
	}
}

// A gap in the reports drops the heat-up.
func TestHeatLogGap(t *testing.T) {
	h := &HeatLog{}
	h.Sample(heatT0, HeatBed, 22, 0)
	h.Sample(heatT0.Add(time.Second), HeatBed, 22, 60)
	h.Sample(heatT0.Add(5*time.Minute), HeatBed, 60, 60)
	for i := range 5 {
		h.Sample(heatT0.Add(5*time.Minute+time.Duration(i+1)*time.Second), HeatBed, 60, 60)
	}
	if n := len(h.Heats(HeatBed)); n != 0 {
		t.Errorf("%d samples across a gap, want 0", n)
	}
}

// Observe feeds each heater of the state: the bed, the nozzle, the aux nozzle and the chamber.
// The chamber temperature is the room of the other heaters.
func TestHeatLogObserve(t *testing.T) {
	h := &HeatLog{}
	st := State{Reachable: true, Bed: 25, Tool: 25, Tool2: 25, HasTool2: true, Chamber: 28, HasChamber: true}
	h.Observe(st, heatT0)
	st.BedTarget, st.ToolTarget, st.Tool2Target, st.ChamberTarget = 55, 220, 220, 35
	h.Observe(st, heatT0.Add(time.Second))
	st.Bed, st.Tool, st.Tool2, st.Chamber = 55, 220, 220, 35
	for i := range 4 {
		h.Observe(st, heatT0.Add(time.Duration(30+i)*time.Second))
	}
	for _, heater := range []string{HeatBed, HeatNozzle, HeatAux, HeatChamber} {
		got := h.Heats(heater)
		if len(got) != 1 || got[0].Seconds != 29 {
			t.Errorf("%s: %+v, want one sample of 29 s", heater, got)
			continue
		}
		if heater == HeatChamber && got[0].Room != nil {
			t.Errorf("chamber: room %v, want none", *got[0].Room)
		}
		if heater != HeatChamber && (got[0].Room == nil || *got[0].Room != 28) {
			t.Errorf("%s: room %v, want 28", heater, got[0].Room)
		}
	}
}

// A cool-down after a print: the bed to 40 °C and the nozzle to 50 °C. A target that goes up
// drops the cool-down.
func TestHeatLogCoolDown(t *testing.T) {
	h := &HeatLog{}
	st := State{Reachable: true, Bed: 60, BedTarget: 60, Tool: 220, ToolTarget: 220}
	h.Observe(st, heatT0)
	st.BedTarget, st.ToolTarget = 0, 0
	h.Observe(st, heatT0.Add(time.Second))
	h.PrintEnded(heatT0.Add(time.Second))
	for i := 1; i <= 1200; i++ {
		st.Bed = max(30, 60-float64(i)/30)  // 40 °C after 600 s
		st.Tool = max(30, 220-float64(i)/2) // 50 °C after 340 s
		h.Observe(st, heatT0.Add(time.Duration(1+i)*time.Second))
	}
	bed, tool := h.Cools(HeatBed), h.Cools(HeatNozzle)
	if len(bed) != 1 || bed[0].Seconds != 600 || bed[0].From != 60 || bed[0].To != 40 {
		t.Errorf("bed %+v, want 600 s from 60 to 40", bed)
	}
	if len(tool) != 1 || tool[0].Seconds != 340 || tool[0].To != 50 {
		t.Errorf("nozzle %+v, want 340 s to 50", tool)
	}

	// A release temperature of the printer, and a new heat-up before the end of the cool-down.
	h = &HeatLog{Release: 35}
	st = State{Reachable: true, Bed: 60, Tool: 220}
	h.Observe(st, heatT0)
	h.PrintEnded(heatT0)
	st.Bed = 50
	h.Observe(st, heatT0.Add(time.Minute))
	st.BedTarget, st.Bed = 60, 34
	h.Observe(st, heatT0.Add(2*time.Minute))
	if n := len(h.Cools(HeatBed)); n != 0 {
		t.Errorf("%d bed samples after a new heat-up, want 0", n)
	}
	h.PrintEnded(heatT0.Add(3 * time.Minute))
	st.BedTarget, st.Bed = 0, 60
	h.Observe(st, heatT0.Add(4*time.Minute))
	h.PrintEnded(heatT0.Add(4 * time.Minute))
	st.Bed = 34.5
	h.Observe(st, heatT0.Add(14*time.Minute))
	if c := h.Cools(HeatBed); len(c) != 1 || c[0].To != 35 || c[0].Seconds != 600 {
		t.Errorf("bed %+v, want 600 s to 35", c)
	}
}

// The fit of the heat-ups: seconds = intercept + per degree × rise, and the median and the spread
// of each target.
func TestHeatReportFit(t *testing.T) {
	h := &HeatLog{}
	h.load()
	// The bed: 30 s to settle, then 2 s for each degree.
	for _, c := range []struct{ from, target float64 }{{22, 60}, {25, 60}, {20, 100}, {40, 100}, {30, 80}, {22, 60}} {
		h.data.Heat[HeatBed] = append(h.data.Heat[HeatBed], HeatSample{From: c.from, Target: c.target, Seconds: 30 + 2*(c.target-c.from)})
	}
	// The nozzle: only targets of 200 from about 25 °C, so too little spread for the full fit.
	for _, from := range []float64{24, 25, 26} {
		h.data.Heat[HeatNozzle] = append(h.data.Heat[HeatNozzle], HeatSample{From: from, Target: 200, Seconds: 0.5 * (200 - from)})
	}
	h.data.Cool[HeatBed] = []CoolSample{{From: 60, To: 40, Seconds: 500}, {From: 60, To: 40, Seconds: 700}, {From: 60, To: 40, Seconds: 600}}
	r := h.Report()
	if len(r.Heaters) != 2 || r.Heaters[0].Heater != HeatBed || r.Heaters[1].Heater != HeatNozzle {
		t.Fatalf("heaters %+v", r.Heaters)
	}
	bed := r.Heaters[0]
	if f := bed.Fit; f == nil || f.Method != "theil-sen" || math.Abs(f.Intercept-30) > 1e-9 || math.Abs(f.PerDegree-2) > 1e-9 || f.Samples != 6 {
		t.Errorf("bed fit %+v, want 30 s + 2 s/°C from 6 samples", bed.Fit)
	}
	if got := bed.Fit.Seconds(25, 65); math.Abs(got-110) > 1e-9 {
		t.Errorf("fit 25 to 65: %v s, want 110", got)
	}
	if len(bed.Targets) != 3 || bed.Targets[0].Target != 60 || bed.Targets[0].Samples != 3 {
		t.Fatalf("bed targets %+v", bed.Targets)
	}
	// To 60 °C: 106, 100 and 106 s.
	if tt := bed.Targets[0]; tt.Median != 106 || tt.Low != 103 || tt.High != 106 || tt.From != 22 {
		t.Errorf("bed to 60: %+v", tt)
	}
	noz := r.Heaters[1]
	if f := noz.Fit; f == nil || f.Method != "rate" || f.Intercept != 0 || f.PerDegree != 0.5 {
		t.Errorf("nozzle fit %+v, want a rate of 0.5 s/°C", noz.Fit)
	}
	c, ok := r.CoolOf(HeatBed)
	if !ok || c.Median != 600 || c.Low != 550 || c.High != 650 || c.To != 40 || c.Samples != 3 {
		t.Errorf("bed cool %+v", c)
	}

	// The fit of the bed replaces the rate of the history; the nozzle fit has 3 samples and
	// replaces it too.
	rates := r.Apply(ComputeRates(nil))
	if rates.Heat["heat-bed"] != 2 || rates.Settle["heat-bed"] != 30 || rates.Heat["heat-nozzle"] != 0.5 {
		t.Errorf("rates %v, settle %v", rates.Heat, rates.Settle)
	}
	// A rate fit leaves a history with a settle time as it is.
	hist := ComputeRates(nil)
	hist.Heat["heat-nozzle"], hist.Settle["heat-nozzle"] = 0.7, 20
	if got := r.Apply(hist); got.Heat["heat-nozzle"] != 0.7 || got.Settle["heat-nozzle"] != 20 || got.Heat["heat-bed"] != 2 {
		t.Errorf("rates %v, settle %v", got.Heat, got.Settle)
	}
}

// The log keeps the last 200 samples of each heater.
func TestHeatLogKeep(t *testing.T) {
	h := &HeatLog{}
	h.load()
	for i := range heatKeep + 20 {
		h.addHeat(HeatBed, HeatSample{Target: 60, Seconds: float64(i)})
	}
	got := h.Heats(HeatBed)
	if len(got) != heatKeep || got[0].Seconds != 20 {
		t.Errorf("%d samples from %v, want %d from 20", len(got), got[0].Seconds, heatKeep)
	}
}

// The Bambu backend feeds the reports to the log: a heat-up of the bed in the start of a print,
// and its cool-down after the end. The next print has a time to release the part.
func TestBambuHeatLog(t *testing.T) {
	b := NewBambu("192.168.1.50", "00TEST000000000", "12345678", "", "X2D", nil)
	b.HeatLog = &HeatLog{}
	report := func(r bambu.Report, at time.Duration) { b.update(r, heatT0.Add(at)) }
	report(bambu.Report{State: "IDLE", Nozzle: 25, Bed: 24}, 0)
	run := bambu.Report{State: "PREPARE", Subtask: "Benchy", TaskID: "7", Stage: 2, Nozzle: 25, Bed: 24, BedTarget: 60}
	at := time.Second
	for bed := 24; bed <= 60; bed += 2 {
		run.Bed = bambu.Num(bed)
		report(run, at)
		at += 2 * time.Second
	}
	for range 3 {
		report(run, at)
		at += 2 * time.Second
	}
	if got := b.HeatLog.Heats(HeatBed); len(got) != 1 || got[0].From != 24 || got[0].Target != 60 || got[0].Seconds != 36 {
		t.Fatalf("bed heat-ups %+v, want one from 24 to 60 in 36 s", got)
	}
	run.State, run.Stage, run.BedTarget = "FINISH", 0, 0
	report(run, time.Hour)
	idle := bambu.Report{State: "FINISH", Subtask: "Benchy", TaskID: "7", Nozzle: 25, Bed: 60}
	for i := 1; i <= 20; i++ {
		idle.Bed = bambu.Num(60 - i)
		report(idle, time.Hour+time.Duration(i)*time.Minute)
	}
	cool := b.HeatLog.Cools(HeatBed)
	if len(cool) != 1 || cool[0].Seconds != 20*60 || cool[0].To != CoolBedTo {
		t.Fatalf("bed cool-downs %+v, want one of 20 min", cool)
	}
	next := bambu.Report{State: "RUNNING", Subtask: "Cube", TaskID: "8", Remaining: 30, Nozzle: 220, Bed: 60, BedTarget: 60}
	report(next, 2*time.Hour)
	if st := b.State(); !st.HasReleaseLeft || st.ReleaseLeft != 50*time.Minute {
		t.Errorf("release in %v (%v), want 50 min", st.ReleaseLeft, st.HasReleaseLeft)
	}
}
