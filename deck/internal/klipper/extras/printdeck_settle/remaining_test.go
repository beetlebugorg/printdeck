package printdeck_settle

import (
	"math"
	"testing"
)

// A bed that comes to its shape as an exponential: the rate times tau is the movement that is
// still to come.
func TestRemaining(t *testing.T) {
	const tau, total = 600.0, 0.1
	var rs [][2]float64
	for s := 0.0; s <= 1200; s += 30 {
		rs = append(rs, [2]float64{s, total * (1 - math.Exp(-s/tau))})
		_, rem, ok := Remaining(rs, 180, tau)
		if s < 162 {
			if ok {
				t.Fatalf("at %v s: an estimate before the window is full", s)
			}
			continue
		}
		// The slope over the window is the rate in the middle of the window, so the estimate is a
		// little high.
		want := total * math.Exp(-s/tau)
		if !ok || rem < want || rem > want*1.25 {
			t.Errorf("at %v s: %.4f mm to come, want about %.4f", s, rem, want)
		}
	}
	// A bed that does not move, with noise of the probe of ±0.003 mm.
	rs = nil
	for i := 0; i < 7; i++ {
		rs = append(rs, [2]float64{float64(i) * 30, 0.003 * float64(1-2*(i%2))})
	}
	if _, rem, ok := Remaining(rs, 180, 900); !ok || math.Abs(rem) > 0.02 {
		t.Errorf("still bed: %.4f mm to come, ok %v", rem, ok)
	}
}
