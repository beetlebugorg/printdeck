package amp

import "testing"

func TestWindow(t *testing.T) {
	a := New("x", 100, -40, -10)
	for _, c := range []struct{ p, step int }{{100, 187}, {50, 157}, {1, 128}, {0, 0}} {
		if got := a.stepFor(c.p); got != c.step {
			t.Errorf("stepFor(%d) = %d, want %d", c.p, got, c.step)
		}
	}
	// A step turns back into a percent that gives the same step.
	for p := 1; p <= 100; p++ {
		if got := a.stepFor(a.percentFor(a.stepFor(p))); got != a.stepFor(p) {
			t.Errorf("percent %d: step %d came back as step %d", p, a.stepFor(p), got)
		}
	}
	// Louder than the ceiling shows as 100%, quieter than the floor as 0%.
	if a.percentFor(207) != 100 || a.percentFor(100) != 0 {
		t.Errorf("out of window: %d %d", a.percentFor(207), a.percentFor(100))
	}
}
