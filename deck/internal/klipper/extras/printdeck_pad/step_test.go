package printdeck_pad

import (
	"math"
	"testing"
)

func TestStep(t *testing.T) {
	for _, c := range []struct{ err, adjust, want float64 }{
		{0.06, 0, -0.03},    // half of the way to -0.06
		{0.2, 0, -0.05},     // no more than max_step
		{-0.2, 0, 0.05},     // the other way
		{0.06, -0.06, 0},    // at the target
		{0.02, -0.06, 0.02}, // past the target: back by half of 0.04
		{1.0, -0.48, -0.02}, // no more than max_adjust
		{0, 0.5, -0.05},     // no error: the correction goes back
	} {
		if got := Step(c.err, c.adjust, 0.5, 0.05, 0.5); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("Step(%g, %g) %g, want %g", c.err, c.adjust, got, c.want)
		}
	}
	// A constant error: the correction gets to its negative, and then the print has no error.
	adj := 0.0
	for i := 0; i < 20; i++ {
		adj += Step(0.08, adj, 0.5, 0.05, 0.5)
	}
	if math.Abs(adj+0.08) > 0.001 {
		t.Errorf("correction %g after 20 checks, want -0.08", adj)
	}
}
