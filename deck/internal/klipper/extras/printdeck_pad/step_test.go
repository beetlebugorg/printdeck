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

func TestBisect(t *testing.T) {
	// A pad edge at 107.34: the probe is on the pad below it.
	n := 0
	got := Bisect(100, 120, 0.1, func(v float64) bool { n++; return v < 107.34 })
	if math.Abs(got-107.34) > 0.05 {
		t.Errorf("edge %g, want 107.34", got)
	}
	if n > 9 {
		t.Errorf("%d probes, want 9 or less", n)
	}
	// The other way: in is above the edge.
	if got := Bisect(120, 100, 0.1, func(v float64) bool { return v > 113.2 }); math.Abs(got-113.2) > 0.05 {
		t.Errorf("edge %g, want 113.2", got)
	}
}

func TestFindCenter(t *testing.T) {
	// The pad is at (150, 140), 20 mm. The real pin is 0.4 mm left and 5.3 mm to the front of the
	// place of the offsets, and its tip is 1 mm wide: it sits on the pad 0.5 mm past each edge.
	const px, py, side = 150.0, 140.0, 20.0
	dx, dy := -0.4, -5.3
	on := func(x, y float64) bool {
		rx, ry := x+dx, y+dy
		return math.Abs(rx-px) < side/2+0.5 && math.Abs(ry-py) < side/2+0.5
	}
	fx, fy, w, h, err := FindCenter(px, py, side, on)
	if err != nil {
		t.Fatal(err)
	}
	// The pin is on the middle when the place of the offsets is (px - dx, py - dy). The offsets
	// then change by (dx, dy): off - (f - p).
	if math.Abs(-(fx-px)-dx) > 0.1 || math.Abs(-(fy-py)-dy) > 0.1 {
		t.Errorf("middle (%.2f, %.2f), want (%.2f, %.2f)", fx, fy, px-dx, py-dy)
	}
	if math.Abs(w-21) > 0.2 || math.Abs(h-21) > 0.2 {
		t.Errorf("width %.2f by %.2f, want 21 by 21", w, h)
	}
	// 9 mm wrong: the middle misses, and a place around it finds the pad.
	dy = -12
	if fx, fy, _, _, err = FindCenter(px, py, side, on); err != nil || math.Abs(fy-(py+12)) > 0.1 || math.Abs(fx-(px+0.4)) > 0.1 {
		t.Errorf("12 mm: (%.2f, %.2f) %v", fx, fy, err)
	}
	// 30 mm wrong: no pad.
	dy = -30
	if _, _, _, _, err = FindCenter(px, py, side, on); err == nil {
		t.Error("30 mm: no error")
	}
}
