package mathutil

import "testing"

func TestSum(t *testing.T) {
	// Values from Python 3.13: sum([0.1] * 10) == 1.0, and a plain loop gives 0.9999999999999999.
	ten := []float64{.1, .1, .1, .1, .1, .1, .1, .1, .1, .1}
	if got := Sum(ten...); got != 1.0 {
		t.Errorf("Sum(0.1 * 10) = %v, want 1.0", got)
	}
	if got := Sum(1e100, 1., -1e100); got != 1. {
		t.Errorf("Sum(1e100, 1, -1e100) = %v, want 1", got)
	}
	if got := Sum(); got != 0 {
		t.Errorf("Sum(nil) = %v, want 0", got)
	}
}
