package camera

import (
	"math"
	"sort"
)

// The digital pan, tilt and zoom (PTZ) of a camera. A UVC camera with digital PTZ crops its
// sensor before the compression: the zoom keeps the detail of the sensor, and the deck gets the
// frames with no change. The controls are the V4L2 controls zoom, pan and tilt (absolute). The
// range and the units of each are of the camera.
//
// The wide frame is the frame at the smallest zoom, with pan and tilt at their defaults. A frame at
// another position shows a rectangle of the wide frame with the shape of the frame: its size is
// the size of the wide frame divided by the zoom factor, and pan and tilt move its center. The
// center stays where the rectangle is inside the wide frame. PTZMap is this model for one camera,
// with the values that MeasurePTZ measures. The calibration is kept in the coordinates of the wide
// frame, and the map gives the projection at each position (Calibration.AtPTZ).

// PTZRange is the range of one control: from Min to Max in steps of Step. Default is the value of
// the camera at its start.
type PTZRange struct {
	Min, Max, Step, Default int
}

// Valid reports that the range has more than one value.
func (r PTZRange) Valid() bool { return r.Max > r.Min }

// Clamp returns the value nearest to v in the range, on a step.
func (r PTZRange) Clamp(v int) int {
	v = max(r.Min, min(r.Max, v))
	if r.Step > 1 {
		v = r.Min + (v-r.Min+r.Step/2)/r.Step*r.Step
		if v > r.Max {
			v -= r.Step
		}
	}
	return v
}

// PTZPos is a position of the zoom, the pan and the tilt, in the units of the camera.
type PTZPos struct {
	Zoom int `json:"zoom"`
	Pan  int `json:"pan,omitempty"`
	Tilt int `json:"tilt,omitempty"`
}

// PTZ is the PTZ control of a camera: the ranges, and the position now. A camera with PTZ has a
// zoom control. Pan and Tilt have no range (Valid is false) for a camera without them.
type PTZ struct {
	Zoom, Pan, Tilt PTZRange
	Pos             PTZPos
	// Mapped reports that the settings of the camera have a valid PTZMap. Cam.Status sets it.
	Mapped bool
}

// Wide returns the position of the wide frame: the smallest zoom, and pan and tilt at their
// defaults.
func (p PTZ) Wide() PTZPos {
	w := PTZPos{Zoom: p.Zoom.Min}
	if p.Pan.Valid() {
		w.Pan = p.Pan.Clamp(p.Pan.Default)
	}
	if p.Tilt.Valid() {
		w.Tilt = p.Tilt.Clamp(p.Tilt.Default)
	}
	return w
}

// Target returns the position of the setting s: the position in the ranges, or the wide position
// for nil.
func (p PTZ) Target(s *PTZPos) PTZPos {
	if s == nil {
		return p.Wide()
	}
	t := PTZPos{Zoom: p.Zoom.Clamp(s.Zoom)}
	if p.Pan.Valid() {
		t.Pan = p.Pan.Clamp(s.Pan)
	}
	if p.Tilt.Valid() {
		t.Tilt = p.Tilt.Clamp(s.Tilt)
	}
	return t
}

// IsWide reports that the position pos is the wide position. A nil position is the wide
// position.
func (p PTZ) IsWide(pos *PTZPos) bool { return pos == nil || *pos == p.Wide() }

// PTZMap is the model of the crop of a camera: the zoom factor of each zoom value, and the move
// of the center of the crop for each unit of pan and tilt. The places are in normalized
// coordinates of the wide frame (0 to 1 across the width and the height) after the turn Rotate.
type PTZMap struct {
	// Rotate is the turn of the camera at the measurement. The map is valid only at this turn.
	Rotate int `json:"rotate"`
	// Zoom are the measured points (zoom value, factor), in the order of the values. The first is
	// the smallest zoom, with the factor 1. The factor between two points is on the line between
	// them.
	Zoom [][2]float64 `json:"zoom"`
	// Pan and Tilt are the moves (u, v) of the center of the crop for one unit of pan and of tilt.
	Pan  [2]float64 `json:"pan"`
	Tilt [2]float64 `json:"tilt"`
	// PanZero and TiltZero are the pan and the tilt of a crop in the middle of the wide frame.
	PanZero  int `json:"panZero,omitempty"`
	TiltZero int `json:"tiltZero,omitempty"`
}

// ValidAt reports that the map is complete, and valid for a camera with the turn rotate.
func (m *PTZMap) ValidAt(rotate int) bool {
	return m != nil && len(m.Zoom) >= 2 && m.Rotate == ValidTurn(rotate)
}

// Factor returns the zoom factor of the zoom value z: 1 at the smallest zoom.
func (m *PTZMap) Factor(z int) float64 {
	pts := m.Zoom
	if len(pts) == 0 {
		return 1
	}
	x := float64(z)
	if x <= pts[0][0] || len(pts) == 1 {
		return pts[0][1]
	}
	for i := 1; i < len(pts); i++ {
		if x <= pts[i][0] || i == len(pts)-1 {
			a, b := pts[i-1], pts[i]
			if b[0] == a[0] {
				return b[1]
			}
			return a[1] + (b[1]-a[1])*(x-a[0])/(b[0]-a[0])
		}
	}
	return pts[len(pts)-1][1]
}

// ZoomFor returns the zoom value of the factor k, on the lines between the points: the largest
// value with a factor of k or less.
func (m *PTZMap) ZoomFor(k float64) float64 {
	pts := m.Zoom
	if len(pts) == 0 {
		return 0
	}
	if k <= pts[0][1] {
		return pts[0][0]
	}
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		if k <= b[1] {
			if b[1] == a[1] {
				return a[0]
			}
			return a[0] + (b[0]-a[0])*(k-a[1])/(b[1]-a[1])
		}
	}
	return pts[len(pts)-1][0]
}

// Window returns the crop of the wide frame at the position p: its center (cu, cv) and the zoom
// factor k. The crop is 1/k of the wide frame, inside it.
func (m *PTZMap) Window(p PTZPos) (cu, cv, k float64) {
	k = max(1, m.Factor(p.Zoom))
	dp, dt := float64(p.Pan-m.PanZero), float64(p.Tilt-m.TiltZero)
	cu = 0.5 + m.Pan[0]*dp + m.Tilt[0]*dt
	cv = 0.5 + m.Pan[1]*dp + m.Tilt[1]*dt
	half := 0.5 / k
	cu = max(half, min(1-half, cu))
	cv = max(half, min(1-half, cv))
	return cu, cv, k
}

// ToFrame returns the place (u, v) of the wide frame in normalized coordinates of the frame at the
// position p.
func (m *PTZMap) ToFrame(u, v float64, p PTZPos) (float64, float64) {
	cu, cv, k := m.Window(p)
	return (u-cu)*k + 0.5, (v-cv)*k + 0.5
}

// ToWide returns the place (u, v) of the frame at the position p in normalized coordinates of the
// wide frame.
func (m *PTZMap) ToWide(u, v float64, p PTZPos) (float64, float64) {
	cu, cv, k := m.Window(p)
	return cu + (u-0.5)/k, cv + (v-0.5)/k
}

// PosFor returns the position that shows the rectangle r (u0, v0, u1, v1 in normalized
// coordinates of the wide frame) as large as the frame allows, in the ranges of c. The rectangle
// is in the middle of the frame when the ranges allow it.
func (m *PTZMap) PosFor(r [4]float64, c PTZ) PTZPos {
	w, h := max(r[2]-r[0], 1e-6), max(r[3]-r[1], 1e-6)
	k := max(1, min(1/w, 1/h))
	z := c.Zoom.Min
	if zf := m.ZoomFor(k); zf > float64(c.Zoom.Min) {
		// The zoom value on a step, with a crop that still has the whole rectangle.
		step := max(1, c.Zoom.Step)
		z = c.Zoom.Min + int(math.Floor((zf-float64(c.Zoom.Min))/float64(step)+1e-9))*step
		z = min(z, c.Zoom.Max)
	}
	return m.Center(PTZPos{Zoom: z}, (r[0]+r[2])/2, (r[1]+r[3])/2, c)
}

// Center returns the position p with the pan and the tilt that put the center of the crop at
// (u, v) of the wide frame, or as near as the crop allows, in the ranges of c.
func (m *PTZMap) Center(p PTZPos, u, v float64, c PTZ) PTZPos {
	half := 0.5 / max(1, m.Factor(p.Zoom))
	du := max(half, min(1-half, u)) - 0.5
	dv := max(half, min(1-half, v)) - 0.5
	p.Pan, p.Tilt = 0, 0
	if c.Pan.Valid() {
		p.Pan = c.Pan.Clamp(m.PanZero)
	}
	if c.Tilt.Valid() {
		p.Tilt = c.Tilt.Clamp(m.TiltZero)
	}
	if dp, dt, ok := m.solve(du, dv, c.Pan.Valid(), c.Tilt.Valid()); ok {
		if c.Pan.Valid() {
			p.Pan = c.Pan.Clamp(m.PanZero + int(math.Round(dp)))
		}
		if c.Tilt.Valid() {
			p.Tilt = c.Tilt.Clamp(m.TiltZero + int(math.Round(dt)))
		}
	}
	return p
}

// solve returns the pan and the tilt (from PanZero and TiltZero) that move the center of the crop
// by (du, dv). With one control, it is the least squares solution of that control.
func (m *PTZMap) solve(du, dv float64, pan, tilt bool) (dp, dt float64, ok bool) {
	a, b := m.Pan, m.Tilt
	if !pan {
		a = [2]float64{}
	}
	if !tilt {
		b = [2]float64{}
	}
	det := a[0]*b[1] - a[1]*b[0]
	na, nb := a[0]*a[0]+a[1]*a[1], b[0]*b[0]+b[1]*b[1]
	switch {
	case math.Abs(det) > 1e-6*math.Sqrt(na*nb) && na > 0 && nb > 0:
		return (du*b[1] - dv*b[0]) / det, (a[0]*dv - a[1]*du) / det, true
	case na > 0:
		return (du*a[0] + dv*a[1]) / na, 0, true
	case nb > 0:
		return 0, (du*b[0] + dv*b[1]) / nb, true
	}
	return 0, 0, false
}

// Limit returns the position p with the pan and the tilt at the place where the center of the
// crop reaches the side of the wide frame, when p is past it: a pan past that place does not move
// the crop.
func (m *PTZMap) Limit(p PTZPos, c PTZ) PTZPos {
	half := 0.5 / max(1, m.Factor(p.Zoom))
	dp, dt := float64(p.Pan-m.PanZero), float64(p.Tilt-m.TiltZero)
	cu := 0.5 + m.Pan[0]*dp + m.Tilt[0]*dt
	cv := 0.5 + m.Pan[1]*dp + m.Tilt[1]*dt
	lu, lv := max(half, min(1-half, cu)), max(half, min(1-half, cv))
	if lu == cu && lv == cv {
		return p
	}
	sp, st, ok := m.solve(lu-0.5, lv-0.5, c.Pan.Valid(), c.Tilt.Valid())
	if !ok {
		return p
	}
	if c.Pan.Valid() {
		p.Pan = c.Pan.Clamp(m.PanZero + int(math.Round(sp)))
	}
	if c.Tilt.Valid() {
		p.Tilt = c.Tilt.Clamp(m.TiltZero + int(math.Round(st)))
	}
	return p
}

// sortZoom puts the points of the zoom in the order of their values, and keeps one point for each
// value.
func (m *PTZMap) sortZoom() {
	sort.Slice(m.Zoom, func(i, j int) bool { return m.Zoom[i][0] < m.Zoom[j][0] })
	out := m.Zoom[:0]
	for _, p := range m.Zoom {
		if len(out) > 0 && out[len(out)-1][0] == p[0] {
			continue
		}
		out = append(out, p)
	}
	m.Zoom = out
}

// turnRows changes the rows of the matrix P from the normalized coordinates of a frame to the
// coordinates after a clockwise turn of the frame by rotate degrees (see turnNorm).
func turnRows(p [12]float64, rotate int) [12]float64 {
	var a, b, c [4]float64
	copy(a[:], p[0:4])
	copy(b[:], p[4:8])
	copy(c[:], p[8:12])
	var na, nb [4]float64
	switch (rotate%360 + 360) % 360 {
	case 90: // (1-v, u)
		for i := range 4 {
			na[i], nb[i] = c[i]-b[i], a[i]
		}
	case 180: // (1-u, 1-v)
		for i := range 4 {
			na[i], nb[i] = c[i]-a[i], c[i]-b[i]
		}
	case 270: // (v, 1-u)
		for i := range 4 {
			na[i], nb[i] = b[i], c[i]-a[i]
		}
	default:
		return p
	}
	copy(p[0:4], na[:])
	copy(p[4:8], nb[:])
	return p
}

// affineRows changes the rows of the matrix P for u' = k u + du and v' = k v + dv.
func affineRows(p [12]float64, k, du, dv float64) [12]float64 {
	for i := range 4 {
		p[i] = k*p[i] + du*p[8+i]
		p[4+i] = k*p[4+i] + dv*p[8+i]
	}
	return p
}

// ToWide returns the calibration in the coordinates of the wide frame of the map m, at the turn of
// the map. A calibration with no position (PTZ nil) is a calibration of the wide frame.
func (c *Calibration) ToWide(m *PTZMap) *Calibration {
	out := *c
	out.P = turnRows(c.P, m.Rotate-c.Rotate)
	out.Rotate = m.Rotate
	out.PTZ = nil
	if c.PTZ != nil {
		cu, cv, k := m.Window(*c.PTZ)
		// u = cu + (u' - 0.5) / k
		out.P = affineRows(out.P, 1/k, cu-0.5/k, cv-0.5/k)
	}
	out.P = normalize12(out.P)
	return &out
}

// AtPTZ returns the calibration for the frames at the position p, from a calibration of the wide
// frame of the map m (ToWide).
func (c *Calibration) AtPTZ(m *PTZMap, p PTZPos) *Calibration {
	out := *c
	cu, cv, k := m.Window(p)
	// u' = (u - cu) k + 0.5
	out.P = normalize12(affineRows(c.P, k, 0.5-k*cu, 0.5-k*cv))
	pos := p
	out.PTZ = &pos
	return &out
}
