package camera

import (
	"math"
	"math/rand"
	"testing"
)

// synthCamera is a pinhole camera in front of the bed of a printer of 280 x 280 x 285 mm, up and
// looking down at the bed: f is the focal length in pixels of a frame of w x h.
type synthCamera struct {
	f, w, h float64
	eye     [3]float64
	r       [3][3]float64 // the rows: right, down and forward of the camera, in printer axes
}

func newSynthCamera() synthCamera {
	eye := [3]float64{140, -260, 260}
	target := [3]float64{140, 140, 0}
	fwd := unit(sub(target, eye))
	right := unit(cross(fwd, [3]float64{0, 0, 1}))
	down := cross(fwd, right)
	return synthCamera{f: 900, w: 960, h: 540, eye: eye, r: [3][3]float64{right, down, fwd}}
}

func (c synthCamera) project(p [3]float64) (float64, float64) {
	d := sub(p, c.eye)
	x, y, z := dot(c.r[0], d), dot(c.r[1], d), dot(c.r[2], d)
	return c.w/2 + c.f*x/z, c.h/2 + c.f*y/z
}

func sub(a, b [3]float64) [3]float64 { return [3]float64{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
func dot(a, b [3]float64) float64    { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
func cross(a, b [3]float64) [3]float64 {
	return [3]float64{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}
func unit(a [3]float64) [3]float64 {
	l := math.Sqrt(dot(a, a))
	return [3]float64{a[0] / l, a[1] / l, a[2] / l}
}

// synthCalibration is the calibration of the synthetic camera, from exact points.
func synthCalibration(t *testing.T, cam synthCamera) *Calibration {
	t.Helper()
	pts := CalibrationPoints([3]float64{0, 0, 0}, [3]float64{280, 280, 285})
	pix := make([][2]float64, len(pts))
	for i, p := range pts {
		pix[i][0], pix[i][1] = cam.project(p)
	}
	m, rms, err := Fit(pts, pix, int(cam.w), int(cam.h))
	if err != nil {
		t.Fatal(err)
	}
	return &Calibration{Device: "test", InW: 960, InH: 540, Width: 960, Height: 540, P: m, Error: rms, Points: len(pts)}
}

// The fit recovers the projection of a known camera: with exact taps the error is near 0, and
// with taps of 2 px of noise the projection of other places is still within a few pixels.
func TestFitSynthetic(t *testing.T) {
	cam := newSynthCamera()
	pts := CalibrationPoints([3]float64{0, 0, 0}, [3]float64{280, 280, 285})
	if len(pts) < MinCalibrationPoints {
		t.Fatalf("%d points", len(pts))
	}
	for _, p := range pts {
		if p[2] < 5 {
			t.Errorf("point %v is lower than 5 mm", p)
		}
	}
	// Places that are not calibration points: a grid over the bed, at 3 heights.
	var check [][3]float64
	for x := 20.0; x <= 260; x += 40 {
		for y := 20.0; y <= 260; y += 40 {
			for _, z := range []float64{0, 40, 120} {
				check = append(check, [3]float64{x, y, z})
			}
		}
	}
	rng := rand.New(rand.NewSource(1))
	for _, noise := range []float64{0, 1, 2, 4} {
		pix := make([][2]float64, len(pts))
		for i, p := range pts {
			u, v := cam.project(p)
			pix[i] = [2]float64{u + rng.NormFloat64()*noise, v + rng.NormFloat64()*noise}
		}
		m, rms, err := Fit(pts, pix, 960, 540)
		if err != nil {
			t.Fatal(err)
		}
		c := &Calibration{P: m}
		worst, sum := 0.0, 0.0
		for _, p := range check {
			u, v := cam.project(p)
			pu, pv, ok := c.Project(p, 960, 540, 0)
			if !ok {
				t.Fatalf("noise %v: %v is behind the camera", noise, p)
			}
			d := math.Hypot(pu-u, pv-v)
			worst, sum = max(worst, d), sum+d
		}
		t.Logf("noise %.0f px: fit error %.2f px, over the bed: mean %.2f px, worst %.2f px", noise, rms, sum/float64(len(check)), worst)
		if noise == 0 && (rms > 1e-6 || worst > 1e-4) {
			t.Errorf("exact taps: error %v px, worst %v px", rms, worst)
		}
		if noise > 0 && (rms > 2*noise || worst > 6*noise) {
			t.Errorf("noise %v: error %.2f px, worst %.2f px", noise, rms, worst)
		}
	}
}

// Fit returns ErrBadPoints for points in one plane, and for too few points.
func TestFitBadPoints(t *testing.T) {
	cam := newSynthCamera()
	var flat [][3]float64
	for _, p := range [][2]float64{{50, 50}, {230, 50}, {230, 230}, {50, 230}, {140, 140}, {100, 180}, {180, 100}} {
		flat = append(flat, [3]float64{p[0], p[1], 10})
	}
	pix := make([][2]float64, len(flat))
	for i, p := range flat {
		pix[i][0], pix[i][1] = cam.project(p)
	}
	if _, _, err := Fit(flat, pix, 960, 540); err != ErrBadPoints {
		t.Errorf("points in one plane: %v", err)
	}
	pts := CalibrationPoints([3]float64{0, 0, 0}, [3]float64{280, 280, 285})[:5]
	if _, _, err := Fit(pts, pix[:5], 960, 540); err != ErrBadPoints {
		t.Errorf("5 points: %v", err)
	}
}

// Taps at random places give an error larger than MaxCalibrationError.
func TestFitWrongTaps(t *testing.T) {
	pts := CalibrationPoints([3]float64{0, 0, 0}, [3]float64{280, 280, 285})
	rng := rand.New(rand.NewSource(2))
	pix := make([][2]float64, len(pts))
	for i := range pix {
		pix[i] = [2]float64{rng.Float64() * 960, rng.Float64() * 540}
	}
	_, rms, err := Fit(pts, pix, 960, 540)
	if err == nil && rms <= MaxCalibrationError {
		t.Errorf("random taps: error %.1f px", rms)
	}
}

// The projection follows the turn and the size of the frame at each turn of 90°, and the calibration is valid only
// for its camera.
func TestCalibrationTurnAndSize(t *testing.T) {
	cam := newSynthCamera()
	c := synthCalibration(t, cam)
	p := [3]float64{100, 120, 30}
	u, v := cam.project(p)
	pu, pv, _ := c.Project(p, 480, 270, 0)
	if math.Abs(pu-u/2) > 0.01 || math.Abs(pv-v/2) > 0.01 {
		t.Errorf("half size: %.2f,%.2f, want %.2f,%.2f", pu, pv, u/2, v/2)
	}
	pu, pv, _ = c.Project(p, 960, 540, 180)
	if math.Abs(pu-(960-u)) > 0.01 || math.Abs(pv-(540-v)) > 0.01 {
		t.Errorf("turned: %.2f,%.2f, want %.2f,%.2f", pu, pv, 960-u, 540-v)
	}
	// A turn of 90° or 270° makes the frame tall: 540 x 960.
	for _, tc := range []struct {
		rot    int
		wu, wv float64
	}{{90, 540 - v, u}, {270, v, 960 - u}} {
		pu, pv, _ = c.Project(p, 540, 960, tc.rot)
		if math.Abs(pu-tc.wu) > 0.01 || math.Abs(pv-tc.wv) > 0.01 {
			t.Errorf("turn %d: %.2f,%.2f, want %.2f,%.2f", tc.rot, pu, pv, tc.wu, tc.wv)
		}
	}
	// A calibration made at a turn of 90° has the matrix of the turned frame: u' = 1 - v and
	// v' = u. It gives the same places as c at each turn.
	at90 := *c
	at90.Rotate = 90
	for i := range 4 {
		at90.P[i], at90.P[4+i] = c.P[8+i]-c.P[4+i], c.P[i]
	}
	for _, rot := range []int{0, 90, 180, 270} {
		w, h := TurnedSize(960, 540, rot)
		wu, wv, _ := c.Project(p, w, h, rot)
		gu, gv, _ := at90.Project(p, w, h, rot)
		if math.Abs(gu-wu) > 0.01 || math.Abs(gv-wv) > 0.01 {
			t.Errorf("calibration at 90, turn %d: %.2f,%.2f, want %.2f,%.2f", rot, gu, gv, wu, wv)
		}
	}
	if !c.ValidFor(Device{ID: "test"}, Format{InW: 960, InH: 540}) || !c.ValidFor(Device{ID: "test"}, Format{}) {
		t.Error("not valid for its camera")
	}
	if c.ValidFor(Device{ID: "other"}, Format{InW: 960, InH: 540}) || c.ValidFor(Device{ID: "test"}, Format{InW: 1280, InH: 720}) {
		t.Error("valid for another camera or size")
	}
	back, err := ParseCalibration(c.Bytes())
	if err != nil || back.P != c.P || back.Device != c.Device {
		t.Errorf("file: %v %+v", err, back)
	}
}
