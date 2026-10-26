package printer

import (
	"math"
	"time"
)

// The time left of a print. The position in the file is a poor measure of time: the first layers
// and the walls take long for few bytes, and the heating at the start takes time for no bytes. So
// the estimate follows the timeline of the slicer (gcode.Model.TimeAt), corrected by the speed of
// this printer, and it moves smoothly.

const (
	trustAfter = 10 * 60.0 // seconds of the slicer after which the speed of the printer counts fully
	smoothing  = 120.0     // seconds: the time constant of the end time
)

// estimate is the state of the estimate of one print.
type estimate struct {
	file    string    // fileKey of the print
	started bool      // the moves after the start macro started
	clock0  float64   // the print duration of Klipper when the clock of the slicer was 0, in seconds
	end     time.Time // the end of the print, smoothed
	last    time.Time // the time of the last update
}

// TimeLeft sets the time left of a running print from the timeline of its G-code. It keeps the
// estimate of the backend when the G-code is not loaded or has no timeline. prep is the time
// that the steps of the start macro still need (Rates.prepLeft). planFactor is the real time of
// the moves for each second of the plan (Rates.PlanFactor).
func (v *View) TimeLeft(st *State, now time.Time, prep time.Duration, planFactor float64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	key := fileKey(*st)
	if !st.Busy() || v.gc == nil || v.file != key {
		if !st.Busy() {
			v.est = estimate{}
		}
		return
	}
	gc := v.gc
	// The plan of the motion planner is the time of the moves on this printer, corrected by the
	// plan factor of the past prints. Without it, the timeline of the slicer: the plan factor is
	// from the plan, so it does not apply to the slicer. The speed of the printer below (k) is
	// against the corrected plan: against the raw plan, k also contains the plan factor, and the
	// correction counts twice.
	var el, total float64
	if v.plan != nil && v.plan.total > 0 {
		el, total = v.plan.at(st.FilePos)
		el, total = el*planFactor, total*planFactor
	} else if e, t, ok := gc.TimeAt(st.FilePos); ok {
		el, total = e, t
	} else {
		return
	}
	e := &v.est
	if e.file != key {
		*e = estimate{file: key}
	}
	d := st.PrintDuration.Seconds()
	// The clock of the moves starts after the start macro and the heating. Until then the time
	// left is the time of the start macro, from the timings of this printer, and the time of the
	// moves.
	if !e.started {
		if st.Preparing || st.FilePos <= gc.StartEnd {
			st.TimeLeft, st.HasTimeLeft = time.Duration(total*float64(time.Second))+prep, true
			return
		}
		// The printer is assumed to have run at the speed of the slicer until now: this also
		// covers a deck that starts during a print.
		e.started, e.clock0 = true, d-el
	}
	// The speed of this printer against the corrected plan or the slicer: it counts more as the
	// print goes on.
	k := 1.0
	if actual := d - e.clock0; el > 60 && actual > 0 {
		w := min(1, el/trustAfter)
		k = max(0.5, min(3, 1+w*(actual/el-1)))
	}
	left := time.Duration((total - el) * k * float64(time.Second))
	target := now.Add(left)
	if e.end.IsZero() || st.Paused || now.Sub(e.last) > 10*time.Minute {
		e.end = target // a pause moves the end with it
	} else {
		a := 1 - math.Exp(-now.Sub(e.last).Seconds()/smoothing)
		e.end = e.end.Add(time.Duration(float64(target.Sub(e.end)) * a))
	}
	e.last = now
	st.TimeLeft, st.HasTimeLeft = max(0, e.end.Sub(now)), true
}

// PlanTotal returns the seconds of the moves in the plan of the file with the key.
func (v *View) PlanTotal(key string) (float64, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.file != key || v.plan == nil || v.plan.total <= 0 {
		return 0, false
	}
	return v.plan.total, true
}
