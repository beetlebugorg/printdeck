package camera

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"
)

// The calibration of the camera: a 3 x 4 projection matrix from the places of the printer (x, y
// and z in mm, the coordinates of the toolhead) to the pixels of the camera picture. The deck
// moves the nozzle to points over the bed, and the user taps the nozzle tip on the picture at
// each point. Fit computes the matrix from these pairs with the normalized DLT (a least squares
// fit), and then decreases the pixel error with a few Levenberg-Marquardt steps. A simpler pinhole
// camera replaces the matrix when it fits as well (pinhole.go).
//
// A calibration with BedY is for a printer with a bed that moves in Y (a cartesian printer, such as
// a TAZ or a Prusa i3), and a camera on the frame of the printer. Its matrix maps the places in the
// frame of the printer: a place (x, y, z) of the toolhead coordinates, with the bed at the toolhead
// Y bedY, is at (x, y - bedY, z). The nozzle is at y 0 for each Y. The deck fits it from the marks
// of printed towers at two places of the bed (KindTowers). A calibration of an older deck has no
// kind and no BedY: its matrix maps the toolhead coordinates, as a calibration of KindNozzle.
//
// The matrix maps to normalized coordinates of the frame: u and v go from 0 to 1 across the width
// and the height of the picture, after the turn of the settings. A frame of another size with the
// same shape (the camera gives smaller frames when the picture turns) uses the same matrix. A
// change of the turn is a change of u to 1-u and v to 1-v.

// MaxCalibrationError is the largest RMS pixel error of a calibration that the deck keeps.
const MaxCalibrationError = 15.0

// MinCalibrationPoints is the least number of points of a calibration.
const MinCalibrationPoints = 6

// The kinds of a calibration.
const (
	KindNozzle = "nozzle" // the user taps the nozzle tip at points over the bed
	KindTowers = "towers" // the user taps the bottoms and the tops of printed towers
)

// Calibration is a calibration of the camera.
type Calibration struct {
	// Device is the ID of the camera (Device.ID).
	Device string `json:"device"`
	// InW and InH are the size of the pictures of the camera (Format.InW and InH). The
	// calibration is valid only for the same device and this size.
	InW int `json:"inW"`
	InH int `json:"inH"`
	// Width and Height are the size of the frames at the calibration, in pixels.
	Width  int `json:"width"`
	Height int `json:"height"`
	// Rotate is the turn of the settings at the calibration: 0 or 180.
	Rotate int `json:"rotate"`
	// Kind is KindNozzle or KindTowers. A calibration of an older deck has no kind: KindNozzle.
	Kind string `json:"kind,omitempty"`
	// BedY is true for a matrix of the places in the frame of a printer with a bed that moves in
	// Y. See Frame.
	BedY bool `json:"bedY,omitempty"`
	// P is the matrix, row by row: (x, y, z, 1) goes to (a, b, c), and the point is at u = a/c
	// and v = b/c in normalized coordinates of the frame.
	P [12]float64 `json:"p"`
	// Error is the RMS distance in pixels of the frame from the taps to the projected points.
	Error  float64   `json:"error"`
	Points int       `json:"points"`
	Time   time.Time `json:"time"`
}

// ParseCalibration reads a calibration from its file.
func ParseCalibration(b []byte) (*Calibration, error) {
	var c Calibration
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	if c.Device == "" || c.Width <= 0 || c.Height <= 0 || c.P == [12]float64{} {
		return nil, errors.New("camera: the calibration file is not complete")
	}
	return &c, nil
}

// ParseCalibrations reads the calibrations of the cameras from their file: a JSON list, or the
// one calibration of an older deck. It returns the valid calibrations, and an error for the first
// calibration that is not valid.
func ParseCalibrations(b []byte) ([]*Calibration, error) {
	if t := bytes.TrimSpace(b); len(t) > 0 && t[0] == '{' {
		c, err := ParseCalibration(b)
		if err != nil {
			return nil, err
		}
		return []*Calibration{c}, nil
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	var out []*Calibration
	var first error
	for _, r := range raw {
		c, err := ParseCalibration(r)
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		out = append(out, c)
	}
	return out, first
}

// CalibrationsBytes is the text of the file of the calibrations: a JSON list.
func CalibrationsBytes(cs []*Calibration) []byte {
	if cs == nil {
		cs = []*Calibration{}
	}
	b, _ := json.MarshalIndent(cs, "", "  ")
	return append(b, '\n')
}

// Bytes is the text of the calibration for its file.
func (c *Calibration) Bytes() []byte {
	b, _ := json.MarshalIndent(c, "", "  ")
	return append(b, '\n')
}

// ValidFor reports that the calibration is for the camera d with the format f. A format with no
// size (the camera does not run now) matches any size.
func (c *Calibration) ValidFor(d Device, f Format) bool {
	if c == nil || c.Device != d.ID {
		return false
	}
	return f.InW == 0 || (f.InW == c.InW && f.InH == c.InH)
}

// KindName is the kind of the calibration: KindNozzle for a calibration of an older deck.
func (c *Calibration) KindName() string {
	if c.Kind == "" {
		return KindNozzle
	}
	return c.Kind
}

// Frame returns the place p of the toolhead coordinates, with the bed at the toolhead Y bedY, in
// the coordinates of the matrix. With BedY, it is (x, y - bedY, z). Else it is p.
func (c *Calibration) Frame(p [3]float64, bedY float64) [3]float64 {
	if c.BedY {
		p[1] -= bedY
	}
	return p
}

// ProjectAt returns the place p of the toolhead coordinates, with the bed at the toolhead Y bedY,
// in pixels of a frame of w x h with the turn rotate. For the nozzle, bedY is the Y of the nozzle.
// A calibration with no BedY ignores bedY.
func (c *Calibration) ProjectAt(p [3]float64, bedY float64, w, h, rotate int) (px, py float64, ok bool) {
	return c.Project(c.Frame(p, bedY), w, h, rotate)
}

// Project returns the place p in the coordinates of the matrix (see Frame) in pixels of a frame of
// w x h with the turn rotate. ok is false for a place behind the camera.
func (c *Calibration) Project(p [3]float64, w, h, rotate int) (px, py float64, ok bool) {
	u, v, ok := projectNorm(&c.P, p)
	if !ok {
		return 0, 0, false
	}
	if rotate != c.Rotate {
		u, v = 1-u, 1-v
	}
	return u * float64(w), v * float64(h), true
}

// projectNorm returns p in normalized coordinates of the frame.
func projectNorm(m *[12]float64, p [3]float64) (u, v float64, ok bool) {
	a := m[0]*p[0] + m[1]*p[1] + m[2]*p[2] + m[3]
	b := m[4]*p[0] + m[5]*p[1] + m[6]*p[2] + m[7]
	c := m[8]*p[0] + m[9]*p[1] + m[10]*p[2] + m[11]
	if c <= 1e-12 {
		return 0, 0, false
	}
	return a / c, b / c, true
}

// ErrBadPoints is the error of points that do not fix a projection: too few points, or points in
// one plane.
var ErrBadPoints = errors.New("camera: the points do not fix the projection")

// Fit computes the projection from the places pts (mm) to the pixels pix of a frame of w x h. It
// returns the matrix for normalized coordinates of the frame, and the RMS pixel error.
func Fit(pts [][3]float64, pix [][2]float64, w, h int) (m [12]float64, rms float64, err error) {
	n := len(pts)
	if n != len(pix) || n < MinCalibrationPoints || w <= 0 || h <= 0 {
		return m, 0, ErrBadPoints
	}
	if !spread3(pts) {
		return m, 0, ErrBadPoints
	}
	// The normalization of Hartley: the points go to the origin, at a mean distance of sqrt(3)
	// (places) and sqrt(2) (pixels).
	c3, s3 := normalization3(pts)
	var c2 [2]float64
	for _, q := range pix {
		c2[0] += q[0] / float64(n)
		c2[1] += q[1] / float64(n)
	}
	d2 := 0.0
	for _, q := range pix {
		d2 += math.Hypot(q[0]-c2[0], q[1]-c2[1]) / float64(n)
	}
	if d2 < 1e-9 {
		return m, 0, ErrBadPoints
	}
	s2 := math.Sqrt2 / d2

	// The normal equations of the DLT: A^T A, with two rows of A for each point.
	var ata [12][12]float64
	for i := range n {
		X := [4]float64{(pts[i][0] - c3[0]) * s3, (pts[i][1] - c3[1]) * s3, (pts[i][2] - c3[2]) * s3, 1}
		u, v := (pix[i][0]-c2[0])*s2, (pix[i][1]-c2[1])*s2
		var r1, r2 [12]float64
		for j := range 4 {
			r1[j], r1[8+j] = X[j], -u*X[j]
			r2[4+j], r2[8+j] = X[j], -v*X[j]
		}
		for a := range 12 {
			for b := range 12 {
				ata[a][b] += r1[a]*r1[b] + r2[a]*r2[b]
			}
		}
	}
	vals, vecs := jacobiEigen(ata)
	lo := 0
	for i := range vals {
		if vals[i] < vals[lo] {
			lo = i
		}
	}
	var pn [12]float64
	for i := range 12 {
		pn[i] = vecs[i][lo]
	}

	// P = T2^-1 Pn T3, and then the pixels to normalized coordinates of the frame.
	t3 := [4][4]float64{{s3, 0, 0, -s3 * c3[0]}, {0, s3, 0, -s3 * c3[1]}, {0, 0, s3, -s3 * c3[2]}, {0, 0, 0, 1}}
	t2inv := [3][3]float64{{1 / s2, 0, c2[0]}, {0, 1 / s2, c2[1]}, {0, 0, 1}}
	var tmp [3][4]float64
	for r := range 3 {
		for c := range 4 {
			for k := range 4 {
				tmp[r][c] += pn[r*4+k] * t3[k][c]
			}
		}
	}
	for r := range 3 {
		for c := range 4 {
			v := 0.0
			for k := range 3 {
				v += t2inv[r][k] * tmp[k][c]
			}
			m[r*4+c] = v
		}
	}
	for c := range 4 {
		m[c] /= float64(w)
		m[4+c] /= float64(h)
	}
	// The sign: the points are in front of the camera (c > 0).
	front := 0
	for _, p := range pts {
		if m[8]*p[0]+m[9]*p[1]+m[10]*p[2]+m[11] > 0 {
			front++
		} else {
			front--
		}
	}
	if front < 0 {
		for i := range m {
			m[i] = -m[i]
		}
	}
	m = normalize12(m)
	m = refine(m, pts, pix, w, h)
	rms, ok := reprojection(&m, pts, pix, w, h)
	if !ok {
		return m, 0, ErrBadPoints
	}
	if pm, prms, ok := fitPinhole(m, pts, pix, w, h, rms); ok {
		m, rms = pm, prms
	}
	return m, rms, nil
}

// spread3 reports that the places are not all on one line or in one plane: the smallest spread
// of the places is at least 1 % of the largest.
func spread3(pts [][3]float64) bool {
	c, _ := normalization3(pts)
	var cov [12][12]float64 // the 3 x 3 covariance, in the 12 x 12 matrix of jacobiEigen
	for i := 3; i < 12; i++ {
		cov[i][i] = 1 // the rest has eigenvalue 1, with no effect on the 3 x 3 part
	}
	for _, p := range pts {
		d := [3]float64{p[0] - c[0], p[1] - c[1], p[2] - c[2]}
		for a := range 3 {
			for b := range 3 {
				cov[a][b] += d[a] * d[b]
			}
		}
	}
	vals, _ := jacobiEigen(cov)
	lo, hi := math.Inf(1), 0.0
	for _, v := range vals[:3] {
		lo, hi = min(lo, v), max(hi, v)
	}
	return hi > 0 && math.Sqrt(lo/hi) >= 0.01
}

// normalization3 returns the centroid of the places, and the scale that puts them at a mean
// distance of sqrt(3) from it.
func normalization3(pts [][3]float64) (c [3]float64, s float64) {
	n := float64(len(pts))
	for _, p := range pts {
		for i := range 3 {
			c[i] += p[i] / n
		}
	}
	d := 0.0
	for _, p := range pts {
		d += math.Sqrt((p[0]-c[0])*(p[0]-c[0])+(p[1]-c[1])*(p[1]-c[1])+(p[2]-c[2])*(p[2]-c[2])) / n
	}
	if d < 1e-9 {
		return c, 1
	}
	return c, math.Sqrt(3) / d
}

// normalize12 scales the matrix so that its third row (without the last value) has a length of 1.
func normalize12(m [12]float64) [12]float64 {
	l := math.Sqrt(m[8]*m[8] + m[9]*m[9] + m[10]*m[10])
	if l < 1e-15 {
		return m
	}
	for i := range m {
		m[i] /= l
	}
	return m
}

// reprojection returns the RMS distance in pixels from pix to the projected places.
func reprojection(m *[12]float64, pts [][3]float64, pix [][2]float64, w, h int) (float64, bool) {
	s := 0.0
	for i, p := range pts {
		u, v, ok := projectNorm(m, p)
		if !ok {
			return 0, false
		}
		du, dv := u*float64(w)-pix[i][0], v*float64(h)-pix[i][1]
		s += du*du + dv*dv
	}
	return math.Sqrt(s / float64(len(pts))), true
}

// refine decreases the pixel error of the matrix with Levenberg-Marquardt steps. The DLT
// decreases an algebraic error. These steps decrease the distances on the picture.
func refine(m [12]float64, pts [][3]float64, pix [][2]float64, w, h int) [12]float64 {
	cost := func(m *[12]float64) float64 {
		r, ok := reprojection(m, pts, pix, w, h)
		if !ok {
			return math.Inf(1)
		}
		return r
	}
	best := cost(&m)
	lambda := 1e-3
	for iter := 0; iter < 30; iter++ {
		// The Jacobian of the residuals (2 for each point), by the derivative of each value.
		n := len(pts)
		res := make([]float64, 2*n)
		jac := make([][12]float64, 2*n)
		for i, p := range pts {
			a := m[0]*p[0] + m[1]*p[1] + m[2]*p[2] + m[3]
			b := m[4]*p[0] + m[5]*p[1] + m[6]*p[2] + m[7]
			c := m[8]*p[0] + m[9]*p[1] + m[10]*p[2] + m[11]
			if c <= 1e-12 {
				return m
			}
			X := [4]float64{p[0], p[1], p[2], 1}
			W, H := float64(w), float64(h)
			res[2*i] = a/c*W - pix[i][0]
			res[2*i+1] = b/c*H - pix[i][1]
			for k := range 4 {
				jac[2*i][k] = X[k] / c * W
				jac[2*i][8+k] = -a * X[k] / (c * c) * W
				jac[2*i+1][4+k] = X[k] / c * H
				jac[2*i+1][8+k] = -b * X[k] / (c * c) * H
			}
		}
		var jtj [12][12]float64
		var jtr [12]float64
		for r := range res {
			for a := range 12 {
				jtr[a] += jac[r][a] * res[r]
				for b := range 12 {
					jtj[a][b] += jac[r][a] * jac[r][b]
				}
			}
		}
		improved := false
		for try := 0; try < 8; try++ {
			A := jtj
			for i := range 12 {
				A[i][i] += lambda * (jtj[i][i] + 1e-12)
			}
			var rhs [12]float64
			for i := range rhs {
				rhs[i] = -jtr[i]
			}
			d, ok := solve12(A, rhs)
			if !ok {
				lambda *= 10
				continue
			}
			next := m
			for i := range next {
				next[i] += d[i]
			}
			next = normalize12(next)
			if c := cost(&next); c < best {
				m, best, improved = next, c, true
				lambda = max(1e-9, lambda/10)
				break
			}
			lambda *= 10
		}
		if !improved {
			break
		}
	}
	return m
}

// solve12 solves A x = b by Gaussian elimination with partial pivoting.
func solve12(A [12][12]float64, b [12]float64) ([12]float64, bool) {
	var x [12]float64
	for col := range 12 {
		p := col
		for r := col + 1; r < 12; r++ {
			if math.Abs(A[r][col]) > math.Abs(A[p][col]) {
				p = r
			}
		}
		if math.Abs(A[p][col]) < 1e-300 {
			return x, false
		}
		A[col], A[p] = A[p], A[col]
		b[col], b[p] = b[p], b[col]
		for r := col + 1; r < 12; r++ {
			f := A[r][col] / A[col][col]
			for c := col; c < 12; c++ {
				A[r][c] -= f * A[col][c]
			}
			b[r] -= f * b[col]
		}
	}
	for r := 11; r >= 0; r-- {
		s := b[r]
		for c := r + 1; c < 12; c++ {
			s -= A[r][c] * x[c]
		}
		x[r] = s / A[r][r]
	}
	return x, true
}

// jacobiEigen returns the eigenvalues and the eigenvectors (the columns of vecs) of a symmetric
// matrix, with the cyclic Jacobi method.
func jacobiEigen(a [12][12]float64) (vals [12]float64, vecs [12][12]float64) {
	const n = 12
	for i := range n {
		vecs[i][i] = 1
	}
	for sweep := 0; sweep < 100; sweep++ {
		off := 0.0
		for p := range n {
			for q := p + 1; q < n; q++ {
				off += a[p][q] * a[p][q]
			}
		}
		if off < 1e-30 {
			break
		}
		for p := range n {
			for q := p + 1; q < n; q++ {
				if math.Abs(a[p][q]) < 1e-300 {
					continue
				}
				theta := (a[q][q] - a[p][p]) / (2 * a[p][q])
				t := 1 / (math.Abs(theta) + math.Sqrt(theta*theta+1))
				if theta < 0 {
					t = -t
				}
				c := 1 / math.Sqrt(t*t+1)
				s := t * c
				for k := range n {
					akp, akq := a[k][p], a[k][q]
					a[k][p], a[k][q] = c*akp-s*akq, s*akp+c*akq
				}
				for k := range n {
					apk, aqk := a[p][k], a[q][k]
					a[p][k], a[q][k] = c*apk-s*aqk, s*apk+c*aqk
				}
				for k := range n {
					vkp, vkq := vecs[k][p], vecs[k][q]
					vecs[k][p], vecs[k][q] = c*vkp-s*vkq, s*vkp+c*vkq
				}
			}
		}
	}
	for i := range n {
		vals[i] = a[i][i]
	}
	return vals, vecs
}

// CalibrationPoints are the places of the nozzle for a calibration, for a printer with the axis
// limits lo and hi (mm): the 4 corners of the print area at a low height, 4 corners nearer the
// middle at a larger height, and the middle at a height between them. The low height is 10 mm,
// and no point is lower than 5 mm.
func CalibrationPoints(lo, hi [3]float64) [][3]float64 {
	at := func(axis int, f float64) float64 { return lo[axis] + (hi[axis]-lo[axis])*f }
	zLow := max(10, lo[2]+10)
	zHigh := min(hi[2]-10, zLow+70)
	if zHigh < zLow+10 {
		zHigh = zLow + 10
	}
	var pts [][3]float64
	for _, c := range [][2]float64{{0.2, 0.2}, {0.8, 0.2}, {0.8, 0.8}, {0.2, 0.8}} {
		pts = append(pts, [3]float64{at(0, c[0]), at(1, c[1]), zLow})
	}
	for _, c := range [][2]float64{{0.3, 0.3}, {0.7, 0.3}, {0.7, 0.7}, {0.3, 0.7}} {
		pts = append(pts, [3]float64{at(0, c[0]), at(1, c[1]), zHigh})
	}
	pts = append(pts, [3]float64{at(0, 0.5), at(1, 0.5), (zLow + zHigh) / 2})
	for i := range pts {
		pts[i][2] = max(5, min(hi[2], pts[i][2]))
	}
	return pts
}

// String is the calibration for the log.
func (c *Calibration) String() string {
	bed := ""
	if c.BedY {
		bed = ", bed in Y"
	}
	return fmt.Sprintf("%s, %d points, %.1f px%s", c.KindName(), c.Points, c.Error, bed)
}
