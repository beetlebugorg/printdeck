package main

import (
	"image"
	"math"
	"testing"

	"printdeck/internal/camera"
)

// The view of the time lapse maps a G-code place to the same pixel of the cropped, turned frame as
// the calibration of the camera.
func TestComposeView(t *testing.T) {
	cal := &camera.Calibration{BedY: true, Rotate: 0,
		P: [12]float64{0.004, 0.0005, -0.0008, 0.1, 0.0002, -0.0012, -0.004, 0.9, 0.00001, 0.002, 0.00003, 1}}
	// The G-code coordinates are the toolhead coordinates moved by (5, -3, 0.2).
	l := [4][4]float64{{1, 0, 0, 5}, {0, 1, 0, -3}, {0, 0, 1, 0.2}, {0, 0, 0, 1}}
	src, crop := image.Pt(1280, 720), image.Rect(300, 100, 900, 520)
	const bedY = 140.0
	for _, rot := range []int{0, 90, 180, 270} {
		q, aspect := composeView(cal, bedY, l, rot, src, crop)
		if math.Abs(aspect-600.0/420) > 1e-9 {
			t.Fatalf("aspect %v", aspect)
		}
		for _, g := range [][3]float64{{100, 80, 0}, {150, 120, 20}, {60, 200, 5}} {
			p := [3]float64{g[0] + 5, g[1] - 3, g[2] + 0.2}
			px, py, ok := cal.ProjectAt(p, bedY, src.X, src.Y, rot)
			if !ok {
				t.Fatal("behind the camera")
			}
			a := q[0]*g[0] + q[1]*g[1] + q[2]*g[2] + q[3]
			b := q[4]*g[0] + q[5]*g[1] + q[6]*g[2] + q[7]
			c := q[8]*g[0] + q[9]*g[1] + q[10]*g[2] + q[11]
			u, v := a/c, b/c
			wu, wv := (px-300)/600, (py-100)/420
			if math.Abs(u-wu) > 1e-9 || math.Abs(v-wv) > 1e-9 {
				t.Errorf("turn %d, %v: got (%.6f, %.6f), want (%.6f, %.6f)", rot, g, u, v, wu, wv)
			}
		}
	}
}
