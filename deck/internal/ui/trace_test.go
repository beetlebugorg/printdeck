package ui

import (
	"testing"
	"time"
)

// The bars of the trace do not change between two frames in the same slot: only a new slot moves
// them, by one whole bar.
func TestTracePeaksSteady(t *testing.T) {
	const n = 100
	slot := traceSpan / n
	t0 := time.Unix(1000, 0).Truncate(slot)
	var trace []traceSample
	for i := 0; i < 300; i++ { // 10 samples a second, of a speed that changes each sample
		trace = append(trace, traceSample{at: t0.Add(time.Duration(i) * 100 * time.Millisecond), speed: float64(i%7) * 20})
	}
	base := trace[len(trace)-1].at.Truncate(slot) // the start of the slot of the last sample
	a, _ := tracePeaks(trace, base.Add(slot*9/10), n)
	b, _ := tracePeaks(trace, base.Add(slot*99/100), n)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("bar %d changed in the same slot: %v then %v", i, a[i], b[i])
		}
	}
	c, _ := tracePeaks(trace, base.Add(slot+slot/10), n)
	for i := 0; i < n-1; i++ {
		if c[i] != a[i+1] {
			t.Fatalf("bar %d after one slot: %v, want %v", i, c[i], a[i+1])
		}
	}
}
