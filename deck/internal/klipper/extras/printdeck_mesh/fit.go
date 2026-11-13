package printdeck_mesh

import "math"

// Area is a rectangle of the bed, in probe positions.
type Area struct{ MinX, MinY, MaxX, MaxY float64 }

// clip returns the part of a that is in b.
func (a Area) clip(b Area) Area {
	return Area{math.Max(a.MinX, b.MinX), math.Max(a.MinY, b.MinY), math.Min(a.MaxX, b.MaxX), math.Min(a.MaxY, b.MaxY)}
}

// widen makes each side of a at least w long, around its center, and keeps it in b.
func (a Area) widen(w float64, b Area) Area {
	grow := func(lo, hi, blo, bhi float64) (float64, float64) {
		if hi-lo >= w {
			return lo, hi
		}
		c := (lo + hi) / 2
		lo, hi = c-w/2, c+w/2
		if lo < blo {
			lo, hi = blo, math.Min(bhi, blo+w)
		}
		if hi > bhi {
			lo, hi = math.Max(blo, bhi-w), bhi
		}
		return lo, hi
	}
	a.MinX, a.MaxX = grow(a.MinX, a.MaxX, b.MinX, b.MaxX)
	a.MinY, a.MaxY = grow(a.MinY, a.MaxY, b.MinY, b.MaxY)
	return a
}

// Plane is z = A + B x + C y.
type Plane struct{ A, B, C float64 }

// At returns the z of the plane at x, y.
func (p Plane) At(x, y float64) float64 { return p.A + p.B*x + p.C*y }

// Point is a probed point: the position and the difference between the probed z and the z of the
// saved map at that position.
type Point struct{ X, Y, D float64 }

// fitPlane fits a plane to the differences of the points (least squares). With 1 point it is a
// constant, with 2 points a slope along the line between them. ok is false with no points.
func fitPlane(pts []Point) (Plane, bool) {
	n := float64(len(pts))
	switch len(pts) {
	case 0:
		return Plane{}, false
	case 1:
		return Plane{A: pts[0].D}, true
	case 2:
		dx, dy := pts[1].X-pts[0].X, pts[1].Y-pts[0].Y
		l2 := dx*dx + dy*dy
		if l2 == 0 {
			return Plane{A: (pts[0].D + pts[1].D) / 2}, true
		}
		k := (pts[1].D - pts[0].D) / l2
		p := Plane{B: k * dx, C: k * dy}
		p.A = pts[0].D - p.B*pts[0].X - p.C*pts[0].Y
		return p, true
	}
	// Center the coordinates, so that the equations stay well scaled.
	var mx, my, md float64
	for _, p := range pts {
		mx, my, md = mx+p.X, my+p.Y, md+p.D
	}
	mx, my, md = mx/n, my/n, md/n
	var sxx, sxy, syy, sxd, syd float64
	for _, p := range pts {
		x, y, d := p.X-mx, p.Y-my, p.D-md
		sxx, sxy, syy, sxd, syd = sxx+x*x, sxy+x*y, syy+y*y, sxd+x*d, syd+y*d
	}
	det := sxx*syy - sxy*sxy
	if math.Abs(det) < 1e-9*(sxx*syy+1) {
		// The points are on a line: fit the slope along the line only.
		return fitPlane([]Point{pts[0], pts[len(pts)-1]})
	}
	b := (sxd*syy - syd*sxy) / det
	c := (syd*sxx - sxd*sxy) / det
	return Plane{A: md - b*mx - c*my, B: b, C: c}, true
}

// maxResidual returns the largest difference between the points and the plane.
func maxResidual(p Plane, pts []Point) float64 {
	r := 0.
	for _, q := range pts {
		r = math.Max(r, math.Abs(q.D-p.At(q.X, q.Y)))
	}
	return r
}

// firstPoints returns the 3 first points of an area: the two front corners and the back middle.
func firstPoints(a Area) [][2]float64 {
	return [][2]float64{{a.MinX, a.MinY}, {a.MaxX, a.MinY}, {(a.MinX + a.MaxX) / 2, a.MaxY}}
}

// nextPoint returns the point of a grid of n x n in the area that is the farthest from all the
// probed points: the place where the fit knows the least.
func nextPoint(a Area, pts []Point, n int) [2]float64 {
	best, bestD := [2]float64{(a.MinX + a.MaxX) / 2, (a.MinY + a.MaxY) / 2}, -1.
	for j := range n {
		for i := range n {
			x := a.MinX + (a.MaxX-a.MinX)*float64(i)/float64(n-1)
			y := a.MinY + (a.MaxY-a.MinY)*float64(j)/float64(n-1)
			d := math.Inf(1)
			for _, p := range pts {
				d = math.Min(d, math.Hypot(p.X-x, p.Y-y))
			}
			if d > bestD {
				best, bestD = [2]float64{x, y}, d
			}
		}
	}
	return best
}

// refine probes the 3 first points of the area, then a check point at a time. It stops when the
// last checks check points were each within tol of the plane of the points before them, and all
// the points are within tol of the plane of all the points. ok is false when it stops at
// maxPoints.
func refine(a Area, probe func(x, y float64) Point, tol float64, checks, maxPoints int) (fit Plane, pts []Point, worst float64, ok bool) {
	for _, q := range firstPoints(a) {
		pts = append(pts, probe(q[0], q[1]))
	}
	fit, _ = fitPlane(pts)
	passed := 0
	for len(pts) < maxPoints {
		q := nextPoint(a, pts, 7)
		pt := probe(q[0], q[1])
		if math.Abs(pt.D-fit.At(pt.X, pt.Y)) <= tol {
			passed++
		} else {
			passed = 0
		}
		pts = append(pts, pt)
		fit, _ = fitPlane(pts)
		worst = maxResidual(fit, pts)
		if passed >= checks && worst <= tol {
			return fit, pts, worst, true
		}
	}
	return fit, pts, worst, false
}
