package printer

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"printdeck/internal/gcode"
)

// curaFile is a print of 60 minutes in the slicer: a start macro, then 60 layers of one minute.
func curaFile() (string, []int64) {
	var b strings.Builder
	b.WriteString(";TIME:3600\nPRINT_START BED=60 EXTRUDER=210\n")
	var ends []int64
	for i := range 60 {
		fmt.Fprintf(&b, ";LAYER:%d\nG1 X10 Y%d E%d F600\n;TIME_ELAPSED:%d\n", i, i, i+1, (i+1)*60)
		ends = append(ends, int64(b.Len()))
	}
	return b.String(), ends
}

func TestTimeLeftFollowsTheSlicerAndThePrinter(t *testing.T) {
	src, ends := curaFile()
	gc, _ := gcode.Parse(strings.NewReader(src))
	st := State{Reachable: true, Printing: true, Text: "Printing", FilePath: "a.gcode", FileSize: int64(len(src))}
	v := &View{gc: gc, file: fileKey(st), view: -1}
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

	// The start macro heats for 10 minutes: the time left is the time of the slicer.
	st.Preparing, st.FilePos, st.PrintDuration = true, gc.StartEnd, 10*time.Minute
	v.TimeLeft(&st, now, 0, 1)
	if !st.HasTimeLeft || st.TimeLeft != time.Hour {
		t.Fatalf("while heating: %v", st.TimeLeft)
	}

	// This printer is 1.5 times slower than the slicer. The heating does not count.
	st.Preparing = false
	var left []time.Duration
	for i, end := range ends {
		slicer := float64(i+1) * 60
		real := 1.5 * slicer
		st.FilePos, st.PrintDuration = end, 10*time.Minute+time.Duration(real*float64(time.Second))
		v.TimeLeft(&st, now.Add(time.Duration(real*float64(time.Second))), 0, 1)
		left = append(left, st.TimeLeft)
	}
	// From a quarter of the print, the estimate is within 10% of the true time left: 1.5 times the
	// time of the slicer. It never jumps: from one layer to the next, it changes by at most 5% of
	// the print time, besides the 1.5 minutes of the layer.
	for i, l := range left {
		truth := time.Duration(float64(59-i) * 1.5 * float64(time.Minute))
		if i >= 15 && (l < truth*9/10-time.Minute || l > truth*11/10+time.Minute) {
			t.Errorf("layer %d: %v, want about %v", i, l, truth)
		}
		if step := 90 * time.Minute / 20; i > 0 && (l-left[i-1] > step || left[i-1]-l > step+90*time.Second) {
			t.Errorf("layer %d: a jump from %v to %v", i, left[i-1], l)
		}
	}
	if left[59] > time.Minute {
		t.Errorf("at the end: %v", left[59])
	}
}

// planView is a view of curaFile with a plan of the motion planner: one minute for each layer.
func planView() (*View, State, []int64) {
	src, ends := curaFile()
	gc, _ := gcode.Parse(strings.NewReader(src))
	st := State{Reachable: true, Printing: true, Text: "Printing", FilePath: "a.gcode", FileSize: int64(len(src))}
	pl := &plan{total: 3600}
	for i, end := range ends {
		pl.pos, pl.sec = append(pl.pos, end), append(pl.sec, float64(i+1)*60)
	}
	return &View{gc: gc, file: fileKey(st), view: -1, plan: pl}, st, ends
}

// runPlan runs the print of planView at speed times the plan, after a start macro of 10 minutes,
// and returns the time left at the start and at the end of each layer.
func runPlan(r Rates, speed float64) (start time.Duration, left []time.Duration) {
	v, st, ends := planView()
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	st.Preparing, st.FilePos, st.PrintDuration = true, v.gc.StartEnd, 10*time.Minute
	v.TimeLeft(&st, now, 0, r.PlanFactor())
	start = st.TimeLeft
	st.Preparing = false
	for i, end := range ends {
		real := time.Duration(speed * float64(i+1) * float64(time.Minute))
		st.FilePos, st.PrintDuration = end, 10*time.Minute+real
		v.TimeLeft(&st, now.Add(real), 0, r.PlanFactor())
		left = append(left, st.TimeLeft)
	}
	return start, left
}

// The plan factor of the past prints corrects the plan from the start of the print. The printer
// runs at the corrected plan, so the speed of the printer does not change the estimate: the
// correction does not count twice when the speed counts fully.
func TestTimeLeftUsesThePlanFactor(t *testing.T) {
	start, left := runPlan(Rates{PlanError: 0.2, PlanSamples: 5}, 1.2)
	if want := 72 * time.Minute; start != want {
		t.Errorf("at the start: %v, want %v", start, want)
	}
	for i, l := range left {
		truth := time.Duration(float64(59-i) * 1.2 * float64(time.Minute))
		if d := l - truth; d < -truth/50-time.Second || d > truth/50+time.Second {
			t.Errorf("layer %d: %v, want %v", i, l, truth)
		}
	}
}

// With fewer than planMinSamples prints, the plan factor is 1: the estimate starts at the plan,
// and only the speed of the printer corrects it.
func TestTimeLeftPlanFactorNeedsSamples(t *testing.T) {
	if f := (Rates{PlanError: 0.2, PlanSamples: planMinSamples - 1}).PlanFactor(); f != 1 {
		t.Errorf("factor %v, want 1", f)
	}
	start, left := runPlan(Rates{PlanError: 0.2, PlanSamples: planMinSamples - 1}, 1.2)
	if start != time.Hour {
		t.Errorf("at the start: %v, want the plan", start)
	}
	if truth := 59 * 72 * time.Second; left[0] > truth*95/100 {
		t.Errorf("layer 0: %v, want less than %v", left[0], truth)
	}
}

// A strange record does not throw the plan factor far: it stays between planMinFactor and
// planMaxFactor.
func TestTimeLeftPlanFactorClamps(t *testing.T) {
	for _, tc := range []struct {
		err  float64
		want float64
	}{{2.0, planMaxFactor}, {-0.9, planMinFactor}, {0.2, 1.2}, {0, 1}} {
		r := Rates{PlanError: tc.err, PlanSamples: 10}
		if f := r.PlanFactor(); math.Abs(f-tc.want) > 1e-9 {
			t.Errorf("error %v: factor %v, want %v", tc.err, f, tc.want)
		}
		start, _ := runPlan(r, tc.want)
		if want := time.Duration(tc.want * float64(time.Hour)); (start - want).Abs() > time.Second {
			t.Errorf("error %v: at the start %v, want %v", tc.err, start, want)
		}
	}
}
