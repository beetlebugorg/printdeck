package camera

import (
	"math"
	"testing"

	"printdeck/internal/gcode"
)

// The directions of the axes in the frames of a calibration give the view of the camera: the
// turn and the elevation of the camera, seen from the point that it looks at.
func TestViewOfCalibration(t *testing.T) {
	target := [3]float64{140, 140, 0}
	for _, eye := range [][3]float64{{140, -260, 260}, {400, -200, 250}, {-150, 0, 300}} {
		fwd := unit(sub(target, eye))
		right := unit(cross(fwd, [3]float64{0, 0, 1}))
		cam := synthCamera{f: 900, w: 960, h: 540, eye: eye, r: [3][3]float64{right, cross(fwd, right), fwd}}
		c := synthCalibration(t, cam)
		pt := func(dx, dy, dz float64) (float64, float64) {
			u, v, ok := c.ProjectAt([3]float64{target[0] + dx, target[1] + dy, target[2] + dz}, 0, 960, 540, 0)
			if !ok {
				t.Fatal("a point behind the camera")
			}
			return u, v
		}
		// Central steps: the perspective of the two sides cancels.
		xu0, xv0 := pt(-10, 0, 0)
		xu1, xv1 := pt(10, 0, 0)
		yu0, yv0 := pt(0, -10, 0)
		yu1, yv1 := pt(0, 10, 0)
		_, zv0 := pt(0, 0, -10)
		_, zv1 := pt(0, 0, 10)
		got, ok := gcode.ViewOf(xu1-xu0, xv1-xv0, yu1-yu0, yv1-yv0, zv1-zv0)
		// The viewer of a view with the turn A is at (-sin A, -cos A) from the target.
		d := sub(eye, target)
		turn := math.Atan2(-d[0], -d[1]) * 180 / math.Pi
		elev := math.Atan2(d[2], math.Hypot(d[0], d[1])) * 180 / math.Pi
		if !ok || math.Abs(got.Turn-turn) > 0.5 || math.Abs(got.Elevation-elev) > 0.5 {
			t.Errorf("eye %v: view %.1f %.1f (%v), want %.1f %.1f", eye, got.Turn, got.Elevation, ok, turn, elev)
		}
	}
}
