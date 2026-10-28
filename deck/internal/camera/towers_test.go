package camera

import (
	"bytes"
	"encoding/json"
	"image"
	"math"
	"math/rand"
	"testing"
)

// slingerCamera is a camera on the frame of a printer with a bed that moves in Y, as a TAZ
// Workhorse: low and in front of the bed, looking at the line of the nozzle. It projects the
// places in the frame of the printer (Calibration.Frame): a place (x, y, z) of the toolhead
// coordinates, with the bed at the toolhead Y bedY, is at (x, y - bedY, z).
func slingerCamera() synthCamera {
	eye := [3]float64{146, -330, 70}
	target := [3]float64{146, 0, 15}
	fwd := unit(sub(target, eye))
	right := unit(cross(fwd, [3]float64{0, 0, 1}))
	down := cross(fwd, right)
	return synthCamera{f: 1000, w: 1280, h: 720, eye: eye, r: [3][3]float64{right, down, fwd}}
}

// projectBed is the pixel of the place p of the toolhead coordinates, with the bed at bedY.
func (c synthCamera) projectBed(p [3]float64, bedY float64) (float64, float64) {
	return c.project([3]float64{p[0], p[1] - bedY, p[2]})
}

// The axis limits of the TAZ Workhorse.
var tazLo, tazHi = [3]float64{-50, -17, -2}, [3]float64{293, 308, 297}

// fitTowers fits a calibration of the bed slinger cam from the marks of the towers, with taps of
// the noise in pixels, and with no marks at the indexes of skip.
func fitTowers(t *testing.T, cam synthCamera, noise float64, skip map[int]bool, seed int64) (*Calibration, error) {
	t.Helper()
	alo, ahi := PrintArea([2]float64{tazLo[0], tazLo[1]}, [2]float64{tazHi[0], tazHi[1]})
	places := TowerPlaces(alo, ahi)
	beds := TowerBedPlaces(tazLo[1], tazHi[1], alo[1], ahi[1])
	rng := rand.New(rand.NewSource(seed))
	var pts [][3]float64
	var pix [][2]float64
	cal := &Calibration{BedY: true, Kind: KindTowers}
	for i, m := range TowerMarks(true) {
		if skip[i] {
			continue
		}
		p, bedY := m.Place(places), beds[m.Bed]
		u, v := cam.projectBed(p, bedY)
		pts = append(pts, cal.Frame(p, bedY))
		pix = append(pix, [2]float64{u + rng.NormFloat64()*noise, v + rng.NormFloat64()*noise})
	}
	m, rms, err := Fit(pts, pix, int(cam.w), int(cam.h))
	if err != nil {
		return nil, err
	}
	cal.Device, cal.Width, cal.Height, cal.P, cal.Error, cal.Points = "test", int(cam.w), int(cam.h), m, rms, len(pts)
	return cal, nil
}

// The towers at the corners and in the middle of the print area, the marks in their order, and
// the two places of the bed.
func TestTowerPlaces(t *testing.T) {
	alo, ahi := PrintArea([2]float64{-7, -5}, [2]float64{168, 192}) // a LulzBot Mini 2
	if alo != [2]float64{0, 0} || ahi != [2]float64{168, 168} {
		t.Errorf("area %v %v", alo, ahi)
	}
	p := TowerPlaces(alo, ahi)
	if len(p) != TowerCount || p[0] != [2]float64{25, 25} || p[2] != [2]float64{143, 143} || p[4] != [2]float64{84, 84} {
		t.Errorf("places %v", p)
	}
	if n := len(TowerMarks(true)); n != 15 {
		t.Errorf("%d marks with a bed in Y", n)
	}
	if m := TowerMarks(false); len(m) != 10 || !m[1].Top || m[2].Tower != 1 {
		t.Errorf("marks %v", m)
	}
	if b := TowerBedPlaces(-17, 308, 0, 293); b[0] != 146.5 || math.Abs(b[1]-(146.5+325.0/3)) > 1e-9 {
		t.Errorf("bed places %v", b)
	}
	if b := TowerBedPlaces(0, 200, 0, 200); math.Abs(b[1]-(100+200.0/3)) > 1e-9 {
		t.Errorf("bed places %v", b)
	}
	if b := TowerBedPlaces(0, 120, 0, 200); b[1] >= b[0] {
		t.Errorf("a short travel: bed places %v", b)
	}
}

// The fit of a bed slinger from the marks of the towers at two places of the bed: with exact taps
// it is the camera, and with noise and skipped marks the projection of the bed at other places is
// still within a few pixels. The marks at one place of the bed are not enough with a bed in Y.
func TestFitBedSlinger(t *testing.T) {
	cam := slingerCamera()
	// Places to check: a grid over the bed at 3 heights, at 3 places of the bed.
	var check [][4]float64
	for x := 20.0; x <= 270; x += 50 {
		for y := 20.0; y <= 270; y += 50 {
			for _, z := range []float64{0, 20, 60} {
				for _, bed := range []float64{60, 146, 220} {
					check = append(check, [4]float64{x, y, z, bed})
				}
			}
		}
	}
	for _, tc := range []struct {
		noise float64
		skip  map[int]bool
	}{
		{0, nil},
		{1, nil},
		{2, nil},
		{2, map[int]bool{0: true, 3: true, 12: true}}, // a hidden bottom, top, and a top at the second place
	} {
		c, err := fitTowers(t, cam, tc.noise, tc.skip, 3)
		if err != nil {
			t.Fatal(err)
		}
		worst, sum := 0.0, 0.0
		for _, q := range check {
			p := [3]float64{q[0], q[1], q[2]}
			u, v := cam.projectBed(p, q[3])
			pu, pv, ok := c.ProjectAt(p, q[3], 1280, 720, 0)
			if !ok {
				t.Fatalf("%v is behind the camera", q)
			}
			d := math.Hypot(pu-u, pv-v)
			worst, sum = max(worst, d), sum+d
		}
		t.Logf("noise %.0f px, %d marks: fit error %.2f px; bed: mean %.2f px, worst %.2f px", tc.noise, c.Points, c.Error, sum/float64(len(check)), worst)
		if tc.noise == 0 && (c.Error > 1e-6 || worst > 1e-3) {
			t.Errorf("exact taps: error %v px, worst %v px", c.Error, worst)
		}
		if tc.noise > 0 && (c.Error > 2*tc.noise || worst > 8*tc.noise) {
			t.Errorf("noise %v: error %.2f px, worst %.2f px", tc.noise, c.Error, worst)
		}
	}
	// All the marks of the second place skipped, and most of the first: too few points.
	skip := map[int]bool{}
	for i := range 15 {
		skip[i] = i >= 5
	}
	if _, err := fitTowers(t, cam, 0, skip, 3); err != ErrBadPoints {
		t.Errorf("5 marks: %v", err)
	}
}

// The time lapse crop of a calibration with towers: the crop of a part at a park Y that is not a
// place of the bed of the calibration is the crop of the true camera.
func TestPartCropBedSlinger(t *testing.T) {
	cam := slingerCamera()
	c, err := fitTowers(t, cam, 1.5, map[int]bool{4: true}, 5)
	if err != nil {
		t.Fatal(err)
	}
	sz := image.Pt(1280, 720)
	for _, tc := range []struct {
		box   Box
		parkY float64
	}{
		{Box{Min: [3]float64{110, 60, 0}, Max: [3]float64{180, 110, 40}}, 85},
		{Box{Min: [3]float64{40, 180, 0}, Max: [3]float64{120, 250, 25}}, 215},
		{Box{Min: [3]float64{130, 130, 0}, Max: [3]float64{160, 160, 80}}, 145},
	} {
		got, ok := c.PartCrop(tc.box, tc.parkY, sz, 0)
		var pts [][2]float64
		for _, p := range boxCorners(tc.box) {
			u, v := cam.projectBed(p, tc.parkY)
			pts = append(pts, [2]float64{u, v})
		}
		want, wok := gridCrop(bounds(pts), sz)
		if !ok || !wok {
			t.Fatalf("no crop: %v %v", ok, wok)
		}
		// The projected bounds, before the grid.
		var fit [][2]float64
		for _, p := range boxCorners(tc.box) {
			u, v, _ := c.ProjectAt(p, tc.parkY, sz.X, sz.Y, 0)
			fit = append(fit, [2]float64{u, v})
		}
		fb, tb := bounds(fit), bounds(pts)
		edge := 0.0
		for i := range 4 {
			edge = max(edge, math.Abs(fb[i]-tb[i]))
		}
		d := max(abs(got.Min.X-want.Min.X), abs(got.Min.Y-want.Min.Y), abs(got.Max.X-want.Max.X), abs(got.Max.Y-want.Max.Y))
		t.Logf("park Y %.0f: crop %v, true %v; edges of the box: %.2f px; crop: %d px", tc.parkY, got, want, edge, d)
		if edge > 4 || d > cropGrid {
			t.Errorf("park Y %.0f: crop %v, want %v (edge %.2f px)", tc.parkY, got, want, edge)
		}
	}
	// A calibration of the nozzle (no BedY) ignores the Y of the bed.
	n := *c
	n.BedY = false
	a, _ := n.PartCrop(Box{Max: [3]float64{10, 10, 10}}, 0, sz, 0)
	b, _ := n.PartCrop(Box{Max: [3]float64{10, 10, 10}}, 100, sz, 0)
	if a != b {
		t.Errorf("crop %v with the bed at 0, %v at 100", a, b)
	}
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

// A file of an older deck has no kind and no bed model: it loads as a calibration of the nozzle
// in the toolhead coordinates. A calibration with towers keeps its kind and model.
func TestOldCalibrationLoads(t *testing.T) {
	old := `{"device": "usb-cam", "inW": 1280, "inH": 720, "width": 1280, "height": 720, "rotate": 0,
		"p": [0.005, 0, 0, 0, 0, 0.01, 0, 0, 0, 0, 0, 1], "error": 2.5, "points": 9, "time": "2026-10-25T12:00:00Z"}`
	cs, err := ParseCalibrations([]byte(old))
	if err != nil || len(cs) != 1 {
		t.Fatal(err)
	}
	c := cs[0]
	if c.Kind != "" || c.KindName() != KindNozzle || c.BedY {
		t.Errorf("kind %q, bed %v", c.Kind, c.BedY)
	}
	u, v, _ := c.ProjectAt([3]float64{100, 50, 0}, 200, 1280, 720, 0)
	if math.Abs(u-640) > 1e-9 || math.Abs(v-360) > 1e-9 {
		t.Errorf("projection %.1f,%.1f, want 640,360", u, v)
	}
	if b := CalibrationsBytes(cs); !json.Valid(b) || bytes.Contains(b, []byte("kind")) || bytes.Contains(b, []byte("bedY")) {
		t.Errorf("an old calibration gets new fields: %s", b)
	}
	c.Kind, c.BedY = KindTowers, true
	back, err := ParseCalibrations(CalibrationsBytes([]*Calibration{c}))
	if err != nil || back[0].Kind != KindTowers || !back[0].BedY {
		t.Errorf("towers: %v %+v", err, back)
	}
	u, _, _ = back[0].ProjectAt([3]float64{100, 250, 0}, 200, 1280, 720, 0)
	if math.Abs(u-640) > 1e-9 {
		t.Errorf("projection with the bed %.1f", u)
	}
}

// The views of a calibration of a bed in Y: the part view projects the box of the part with the
// bed at the live Y of the toolhead, as the true camera shows it. With no place of the nozzle,
// the bed has no place, and the part views are the whole picture.
func TestViewsBedSlinger(t *testing.T) {
	cam := slingerCamera()
	c, err := fitTowers(t, cam, 0, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	part := Box{Min: [3]float64{100, 60, 0}, Max: [3]float64{180, 120, 30}}
	scene := &fakeScene{nozzle: [3]float64{140, 200, 30}, part: part, first: part}
	h := NewHub(Settings{})
	h.scan = func() []Device { return []Device{{Path: "test", ID: "test", Name: "Test"}} }
	h.SetScene(scene)
	c.InW, c.InH = 1280, 720
	h.LoadCalibration(c)
	f := picFrame(texturePicture(1280, 720))
	f.Seq = 1
	cm := h.roleCam(RoleMain)
	p := cm.makePlan(f, ViewPart)
	if p == nil {
		t.Fatal("no part view")
	}
	sz := image.Pt(1280, 720)
	truth := func(q [3]float64) (float64, float64, bool) {
		u, v := cam.projectBed(q, scene.nozzle[1])
		return u, v, true
	}
	want, _, _ := boxTarget(truth, part, sz)
	for i := range 4 {
		if math.Abs(p.crop[i]-want[i]) > 1 {
			t.Errorf("part crop %v, want %v", p.crop, want)
			break
		}
	}
	// The toolhead view: the nozzle is at y 0 in the frame for each Y.
	th, ok := toolheadTarget(projectAt(c, scene.nozzle[1], sz, 0), scene.nozzle, sz)
	nx, ny := cam.project([3]float64{140, 0, 30})
	if !ok || math.Abs((th[0]+th[2])/2-nx) > 1 || math.Abs((th[1]+th[3])/2-ny) > 1 {
		t.Errorf("toolhead crop %v, nozzle at %.0f,%.0f", th, nx, ny)
	}
	scene.noNozzle = true
	if v := cm.Views(); len(v) != 1 {
		t.Errorf("no nozzle: views %v", v)
	}
	if p := cm.makePlan(f, ViewPart); p != nil {
		t.Error("a part view with no place of the bed")
	}
}

// A camera with pixels that are not square: the pinhole does not fit, and the DLT stays.
func TestFitNotSquare(t *testing.T) {
	cam := slingerCamera()
	stretched := func(p [3]float64) (float64, float64) {
		u, v := cam.project(p)
		return 640 + (u-640)*1.25, v
	}
	pts := CalibrationPoints(tazLo, tazHi)
	pix := make([][2]float64, len(pts))
	for i, p := range pts {
		p[1] -= 146
		pts[i] = p
		pix[i][0], pix[i][1] = stretched(p)
	}
	_, rms, err := Fit(pts, pix, 1280, 720)
	if err != nil || rms > 1e-3 {
		t.Errorf("error %v px: %v", rms, err)
	}
}
