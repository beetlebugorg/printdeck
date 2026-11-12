package printdeck_mesh

import (
	"math"
	"math/rand"
	"testing"
)

// The fit finds the plane of 3 or more points, and the slope of 2 points.
func TestFitPlane(t *testing.T) {
	want := Plane{0.12, 0.002, -0.004}
	var pts []Point
	for _, xy := range [][2]float64{{15, 15}, {265, 15}, {140, 230}, {60, 120}} {
		pts = append(pts, Point{xy[0], xy[1], want.At(xy[0], xy[1])})
	}
	for n := 3; n <= 4; n++ {
		got, ok := fitPlane(pts[:n])
		if !ok || math.Abs(got.A-want.A) > 1e-9 || math.Abs(got.B-want.B) > 1e-12 || math.Abs(got.C-want.C) > 1e-12 {
			t.Errorf("%d points: %+v, want %+v", n, got, want)
		}
	}
	if r := maxResidual(want, pts); r > 1e-12 {
		t.Errorf("residual %g", r)
	}
	two, _ := fitPlane(pts[:2])
	if math.Abs(two.At(140, 15)-want.At(140, 15)) > 1e-9 {
		t.Errorf("2 points: %+v", two)
	}
	// Points on a line do not give a plane: the fit keeps the slope along the line.
	line := []Point{{0, 0, 0}, {50, 50, 0.1}, {100, 100, 0.2}}
	if p, ok := fitPlane(line); !ok || math.Abs(p.At(100, 100)-0.2) > 1e-9 {
		t.Errorf("line: %+v", p)
	}
}

// The next point is the point of the area that is the farthest from the probed points.
func TestNextPoint(t *testing.T) {
	a := Area{15, 15, 265, 230}
	var pts []Point
	for _, q := range firstPoints(a) {
		pts = append(pts, Point{q[0], q[1], 0})
	}
	q := nextPoint(a, pts, 7)
	dist := func(x, y float64) float64 {
		d := math.Inf(1)
		for _, p := range pts {
			d = math.Min(d, math.Hypot(p.X-x, p.Y-y))
		}
		return d
	}
	for j := range 7 {
		for i := range 7 {
			x, y := 15+250*float64(i)/6, 15+215*float64(j)/6
			if dist(x, y) > dist(q[0], q[1])+1e-9 {
				t.Errorf("next %v, but %v is farther from the points", q, [2]float64{x, y})
			}
		}
	}
}

// A small area grows to the smallest side, and stays on the bed.
func TestWiden(t *testing.T) {
	full := Area{15, 15, 265, 230}
	got := Area{20, 100, 30, 110}.widen(60, full)
	if got != (Area{15, 75, 75, 135}) {
		t.Errorf("widen %+v", got)
	}
}

// A bed with the shape of the saved map, a new height and a new tilt, and probe noise: 5 points
// are enough. A bed that bows when it is hot does not fit a plane, and the module falls back to a
// grid.
func TestRefine(t *testing.T) {
	area := Area{15, 15, 265, 230}
	rng := rand.New(rand.NewSource(1))
	change := Plane{-0.31, 0.0012, -0.0008}
	tilted := func(x, y float64) Point {
		return Point{x, y, change.At(x, y) + rng.NormFloat64()*0.005}
	}
	fit, pts, worst, ok := refine(area, tilted, 0.04, 2, 8)
	if !ok || len(pts) != 5 || worst > 0.04 {
		t.Fatalf("tilted bed: ok %v, %d points, worst %.3f", ok, len(pts), worst)
	}
	for _, xy := range [][2]float64{{15, 230}, {265, 230}, {140, 120}} {
		if d := math.Abs(fit.At(xy[0], xy[1]) - change.At(xy[0], xy[1])); d > 0.02 {
			t.Errorf("fit at %v is %.3f mm off", xy, d)
		}
	}
	bow := func(x, y float64) Point {
		u := (x - 140) / 125
		return Point{x, y, change.At(x, y) + 0.15*u*u}
	}
	if _, pts, worst, ok := refine(area, bow, 0.04, 2, 8); ok {
		t.Errorf("bowed bed fits: %d points, worst %.3f", len(pts), worst)
	} else if len(pts) != 8 {
		t.Errorf("bowed bed: %d points, want 8", len(pts))
	}
}
