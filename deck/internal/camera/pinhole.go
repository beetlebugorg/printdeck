package camera

import "math"

// The pinhole model of a calibration. The marks of a calibration span a small height (the towers
// are 21 mm high), and the matrix of the DLT has 11 free values, so it extrapolates badly: a part
// of 100 mm can be tens of pixels off. A webcam has square pixels and its middle near the middle
// of the picture. A camera with these 7 free values (the focal length and the pose) extrapolates
// much better. Fit keeps the model with the fewest values whose error is near the error of the
// DLT: a centered pinhole with square pixels, then a pinhole with square pixels, then the DLT.

// pinholeFactor and pinholeSlack are how much larger the error of a pinhole model can be than the error of the DLT,
// as a factor and in pixels: fewer free values give a slightly larger error with the same taps.
const (
	pinholeFactor = 1.3
	pinholeSlack  = 0.5
)

// fitPinhole returns the matrix of the simplest pinhole of the matrix m of the DLT that fits the
// points with an error near rms, the error of m, and its error. It is false when no pinhole fits.
func fitPinhole(m [12]float64, pts [][3]float64, pix [][2]float64, w, h int, rms float64) ([12]float64, float64, bool) {
	c, ok := decompose(m, w, h)
	if !ok {
		return m, rms, false
	}
	for _, centered := range []bool{true, false} {
		pm := normalize12(refinePinhole(c, pts, pix, w, h, true, centered).matrix(w, h))
		if prms, ok := reprojection(&pm, pts, pix, w, h); ok && prms <= rms*pinholeFactor+pinholeSlack {
			return pm, prms, true
		}
	}
	return m, rms, false
}

// pinhole is a camera with no skew: the focal lengths fx and fy and the middle cx, cy in pixels,
// and the pose: the rotation vector r (axis times angle) and the translation t.
type pinhole struct {
	fx, fy, cx, cy float64
	r, t           [3]float64
}

// decompose returns the pinhole of the matrix m (normalized coordinates of a frame of w x h), or
// false when m is not a camera.
func decompose(m [12]float64, w, h int) (pinhole, bool) {
	var P [3][4]float64
	for c := range 4 {
		P[0][c], P[1][c], P[2][c] = m[c]*float64(w), m[4+c]*float64(h), m[8+c]
	}
	row := func(i int) [3]float64 { return [3]float64{P[i][0], P[i][1], P[i][2]} }
	m1, m2, m3 := row(0), row(1), row(2)
	l := math.Sqrt(dot3(m3, m3))
	if l < 1e-12 {
		return pinhole{}, false
	}
	for i := range 3 {
		for c := range 4 {
			P[i][c] /= l
		}
	}
	m1, m2, m3 = row(0), row(1), row(2)
	r3 := m3
	cy := dot3(m2, r3)
	q2 := sub3(m2, scale3(r3, cy))
	fy := math.Sqrt(dot3(q2, q2))
	if fy < 1e-9 {
		return pinhole{}, false
	}
	r2 := scale3(q2, 1/fy)
	cx := dot3(m1, r3)
	s := dot3(m1, r2)
	q1 := sub3(sub3(m1, scale3(r3, cx)), scale3(r2, s))
	fx := math.Sqrt(dot3(q1, q1))
	if fx < 1e-9 {
		return pinhole{}, false
	}
	r1 := scale3(q1, 1/fx)
	if dot3(cross3(r1, r2), r3) < 0 {
		return pinhole{}, false
	}
	// t = K^-1 p4, with no skew.
	ty := P[1][3] - cy*P[2][3]
	tx := P[0][3] - cx*P[2][3] - s*ty/fy
	t := [3]float64{tx / fx, ty / fy, P[2][3]}
	return pinhole{fx: fx, fy: fy, cx: cx, cy: cy, r: rotVector([3][3]float64{r1, r2, r3}), t: t}, true
}

// matrix returns the matrix of the camera for normalized coordinates of a frame of w x h.
func (c pinhole) matrix(w, h int) [12]float64 {
	R := rotMatrix(c.r)
	var m [12]float64
	for j := range 3 {
		m[j] = (c.fx*R[0][j] + c.cx*R[2][j]) / float64(w)
		m[4+j] = (c.fy*R[1][j] + c.cy*R[2][j]) / float64(h)
		m[8+j] = R[2][j]
	}
	m[3] = (c.fx*c.t[0] + c.cx*c.t[2]) / float64(w)
	m[7] = (c.fy*c.t[1] + c.cy*c.t[2]) / float64(h)
	m[11] = c.t[2]
	return m
}

// rotMatrix is the rotation of the rotation vector r (Rodrigues).
func rotMatrix(r [3]float64) [3][3]float64 {
	a := math.Sqrt(dot3(r, r))
	if a < 1e-15 {
		return [3][3]float64{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}
	}
	k := scale3(r, 1/a)
	c, s := math.Cos(a), math.Sin(a)
	v := 1 - c
	return [3][3]float64{
		{c + k[0]*k[0]*v, k[0]*k[1]*v - k[2]*s, k[0]*k[2]*v + k[1]*s},
		{k[1]*k[0]*v + k[2]*s, c + k[1]*k[1]*v, k[1]*k[2]*v - k[0]*s},
		{k[2]*k[0]*v - k[1]*s, k[2]*k[1]*v + k[0]*s, c + k[2]*k[2]*v},
	}
}

// rotVector is the rotation vector of a rotation matrix. The rows of R need not be exactly
// orthogonal: the result is near.
func rotVector(R [3][3]float64) [3]float64 {
	c := max(-1, min(1, (R[0][0]+R[1][1]+R[2][2]-1)/2))
	a := math.Acos(c)
	if a < 1e-12 {
		return [3]float64{}
	}
	if math.Pi-a < 1e-6 {
		// Near 180°: the axis from the diagonal.
		k := [3]float64{math.Sqrt(max(0, (R[0][0]+1)/2)), math.Sqrt(max(0, (R[1][1]+1)/2)), math.Sqrt(max(0, (R[2][2]+1)/2))}
		if R[0][1] < 0 {
			k[1] = -k[1]
		}
		if R[0][2] < 0 {
			k[2] = -k[2]
		}
		return scale3(k, a)
	}
	k := [3]float64{R[2][1] - R[1][2], R[0][2] - R[2][0], R[1][0] - R[0][1]}
	return scale3(k, a/(2*math.Sin(a)))
}

func dot3(a, b [3]float64) float64              { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
func sub3(a, b [3]float64) [3]float64           { return [3]float64{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
func scale3(a [3]float64, k float64) [3]float64 { return [3]float64{a[0] * k, a[1] * k, a[2] * k} }
func cross3(a, b [3]float64) [3]float64 {
	return [3]float64{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}

// pinholeParams are the values of a pinhole for refinePinhole, in this order: fx, fy, cx, cy, the
// rotation vector and the translation.
func (c pinhole) params() []float64 {
	return []float64{c.fx, c.fy, c.cx, c.cy, c.r[0], c.r[1], c.r[2], c.t[0], c.t[1], c.t[2]}
}

func pinholeOf(p []float64) pinhole {
	return pinhole{fx: p[0], fy: p[1], cx: p[2], cy: p[3], r: [3]float64{p[4], p[5], p[6]}, t: [3]float64{p[7], p[8], p[9]}}
}

// refinePinhole decreases the pixel error of the pinhole c with Levenberg-Marquardt steps. With
// square, fx and fy are one value. With centered, the middle is the middle of the frame.
func refinePinhole(c pinhole, pts [][3]float64, pix [][2]float64, w, h int, square, centered bool) pinhole {
	if square {
		f := (c.fx + c.fy) / 2
		c.fx, c.fy = f, f
	}
	if centered {
		c.cx, c.cy = float64(w)/2, float64(h)/2
	}
	free := []int{4, 5, 6, 7, 8, 9}
	if !centered {
		free = append(free, 2, 3)
	}
	free = append(free, 0)
	if !square {
		free = append(free, 1)
	}
	set := func(p []float64, i int, v float64) {
		p[i] = v
		if square && i == 0 {
			p[1] = v
		}
	}
	res := func(p []float64) []float64 {
		m := pinholeOf(p).matrix(w, h)
		out := make([]float64, 0, 2*len(pts))
		for i, q := range pts {
			u, v, ok := projectNorm(&m, q)
			if !ok {
				return nil
			}
			out = append(out, u*float64(w)-pix[i][0], v*float64(h)-pix[i][1])
		}
		return out
	}
	cost := func(r []float64) float64 {
		if r == nil {
			return math.Inf(1)
		}
		s := 0.0
		for _, x := range r {
			s += x * x
		}
		return s
	}
	p := c.params()
	r := res(p)
	best := cost(r)
	lambda := 1e-3
	n := len(free)
	for iter := 0; iter < 50 && r != nil; iter++ {
		jac := make([][]float64, n)
		for k, i := range free {
			d := 1e-6 * max(1, math.Abs(p[i]))
			q := append([]float64(nil), p...)
			set(q, i, p[i]+d)
			rp := res(q)
			set(q, i, p[i]-d)
			rm := res(q)
			if rp == nil || rm == nil {
				return pinholeOf(p)
			}
			jac[k] = make([]float64, len(r))
			for j := range r {
				jac[k][j] = (rp[j] - rm[j]) / (2 * d)
			}
		}
		var jtj [12][12]float64
		var jtr [12]float64
		for a := range n {
			for j := range r {
				jtr[a] += jac[a][j] * r[j]
			}
			for b := range n {
				for j := range r {
					jtj[a][b] += jac[a][j] * jac[b][j]
				}
			}
		}
		for a := n; a < 12; a++ {
			jtj[a][a] = 1
		}
		improved := false
		for try := 0; try < 10; try++ {
			A := jtj
			for a := range n {
				A[a][a] += lambda * (jtj[a][a] + 1e-12)
			}
			var rhs [12]float64
			for a := range n {
				rhs[a] = -jtr[a]
			}
			d, ok := solve12(A, rhs)
			if !ok {
				lambda *= 10
				continue
			}
			q := append([]float64(nil), p...)
			for k, i := range free {
				set(q, i, q[i]+d[k])
			}
			if rq := res(q); cost(rq) < best {
				p, r, best, improved = q, rq, cost(rq), true
				lambda = max(1e-12, lambda/10)
				break
			}
			lambda *= 10
		}
		if !improved {
			break
		}
	}
	return pinholeOf(p)
}
