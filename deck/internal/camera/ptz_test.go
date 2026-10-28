package camera

import (
	"encoding/json"
	"errors"
	"math"
	"sync"
	"testing"
	"time"
)

// fakeCtrls is a device with controls for the tests of ptzCtl. With noPanAtMin, it refuses a pan
// or a tilt away from 0 at the smallest zoom, as some cameras do.
type fakeCtrls struct {
	ranges     map[uint32]PTZRange
	v          map[uint32]int
	noPanAtMin bool
	sets       []uint32 // the controls of each set, in order
}

func newFakeCtrls(ids ...uint32) *fakeCtrls {
	d := &fakeCtrls{ranges: map[uint32]PTZRange{}, v: map[uint32]int{}}
	for _, id := range ids {
		r := map[uint32]PTZRange{cidZoomAbsolute: {Min: 100, Max: 500, Step: 1, Default: 100},
			cidPanAbsolute: {Min: -648000, Max: 648000, Step: 3600}, cidTiltAbsolute: {Min: -648000, Max: 648000, Step: 3600}}[id]
		d.ranges[id], d.v[id] = r, r.Default
	}
	return d
}

func (d *fakeCtrls) query(id uint32) (PTZRange, bool) { r, ok := d.ranges[id]; return r, ok }
func (d *fakeCtrls) get(id uint32) (int, error) {
	v, ok := d.v[id]
	if !ok {
		return 0, errors.New("no control")
	}
	return v, nil
}
func (d *fakeCtrls) set(id uint32, v int) error {
	d.sets = append(d.sets, id)
	if _, ok := d.ranges[id]; !ok {
		return errors.New("no control")
	}
	if d.noPanAtMin && id != cidZoomAbsolute && v != 0 && d.v[cidZoomAbsolute] == d.ranges[cidZoomAbsolute].Min {
		return errors.New("EINVAL")
	}
	d.v[id] = v
	return nil
}

// A camera with no zoom control has no PTZ control. Pan and tilt are optional.
func TestPTZCtlDetect(t *testing.T) {
	if newPTZCtl(newFakeCtrls(cidPanAbsolute, cidTiltAbsolute)) != nil {
		t.Error("PTZ control with no zoom")
	}
	c := newPTZCtl(newFakeCtrls(cidZoomAbsolute))
	if c == nil || c.p.Pan.Valid() || c.p.Tilt.Valid() || c.p.Zoom.Max != 500 {
		t.Fatalf("zoom only: %+v", c)
	}
	c = newPTZCtl(newFakeCtrls(cidZoomAbsolute, cidPanAbsolute, cidTiltAbsolute))
	if c == nil || !c.p.Pan.Valid() || !c.p.Tilt.Valid() || c.p.Pos != (PTZPos{Zoom: 100}) {
		t.Fatalf("PTZ: %+v", c)
	}
}

// apply sets the position of the settings on the steps of the ranges, applies a change once, and
// goes back to the wide position for no position.
func TestPTZCtlApply(t *testing.T) {
	d := newFakeCtrls(cidZoomAbsolute, cidPanAbsolute, cidTiltAbsolute)
	c := newPTZCtl(d)
	if !c.apply(CamSettings{PTZ: &PTZPos{Zoom: 250, Pan: 10000, Tilt: -9000}}) {
		t.Fatal("no change")
	}
	if want := (PTZPos{Zoom: 250, Pan: 10800, Tilt: -7200}); c.p.Pos != want || d.v[cidPanAbsolute] != 10800 {
		t.Fatalf("position %+v, want %+v", c.p.Pos, want)
	}
	// The zoom in comes before the pan.
	if d.sets[0] != cidZoomAbsolute {
		t.Errorf("sets %x", d.sets)
	}
	n := len(d.sets)
	if c.apply(CamSettings{PTZ: &PTZPos{Zoom: 250, Pan: 10000, Tilt: -9000}}) || len(d.sets) != n {
		t.Error("the same position again set the controls")
	}
	// Back to wide: the pan and the tilt first, then the zoom out.
	d.sets = nil
	if !c.apply(CamSettings{}) || c.p.Pos != (PTZPos{Zoom: 100}) {
		t.Fatalf("wide: %+v", c.p.Pos)
	}
	if d.sets[len(d.sets)-1] != cidZoomAbsolute {
		t.Errorf("sets %x", d.sets)
	}
}

// A camera that refuses a pan at the smallest zoom reports the position that it has, and the next
// zoom sets the pan.
func TestPTZCtlRefusedPan(t *testing.T) {
	d := newFakeCtrls(cidZoomAbsolute, cidPanAbsolute)
	d.noPanAtMin = true
	c := newPTZCtl(d)
	c.apply(CamSettings{PTZ: &PTZPos{Zoom: 100, Pan: 36000}})
	if c.p.Pos != (PTZPos{Zoom: 100}) {
		t.Fatalf("refused pan: %+v", c.p.Pos)
	}
	if !c.apply(CamSettings{PTZ: &PTZPos{Zoom: 200, Pan: 36000}}) || c.p.Pos != (PTZPos{Zoom: 200, Pan: 36000}) {
		t.Fatalf("pan after the zoom: %+v", c.p.Pos)
	}
}

// testMap is the map of the test camera at the turn 0.
func testMap() *PTZMap {
	return &PTZMap{Zoom: [][2]float64{{100, 1}, {400, 4}}, Pan: [2]float64{testPanShift, 0}, Tilt: [2]float64{0, testTiltShift}}
}

var testCtl = PTZ{Zoom: testZoom, Pan: testPan, Tilt: testTilt}

// The map agrees with the crop of the test camera, and ToFrame and ToWide are inverse.
func TestPTZMapRoundTrip(t *testing.T) {
	m := testMap()
	for _, p := range []PTZPos{{Zoom: 100}, {Zoom: 200, Pan: 12000, Tilt: -6000}, {Zoom: 400, Pan: 36000, Tilt: 36000}, {Zoom: 150, Pan: -36000}} {
		cu, cv, k := m.Window(p)
		tu, tv, tk := testWindow(p)
		if math.Abs(cu-tu) > 1e-9 || math.Abs(cv-tv) > 1e-9 || math.Abs(k-tk) > 1e-9 {
			t.Errorf("%+v: window %.4f %.4f %.3f, want %.4f %.4f %.3f", p, cu, cv, k, tu, tv, tk)
		}
		for _, q := range [][2]float64{{0.5, 0.5}, {0.1, 0.9}, {0.73, 0.2}} {
			u, v := m.ToWide(q[0], q[1], p)
			u2, v2 := m.ToFrame(u, v, p)
			if math.Abs(u2-q[0]) > 1e-12 || math.Abs(v2-q[1]) > 1e-12 {
				t.Errorf("%+v: %v goes back to %.6f %.6f", p, q, u2, v2)
			}
		}
		// The corners of the frame are the corners of the crop.
		if u, v := m.ToWide(0, 0, p); math.Abs(u-(cu-0.5/k)) > 1e-12 || math.Abs(v-(cv-0.5/k)) > 1e-12 {
			t.Errorf("%+v: corner %.4f %.4f", p, u, v)
		}
	}
	if f := m.Factor(250); math.Abs(f-2.5) > 1e-12 {
		t.Errorf("factor %v", f)
	}
	if z := m.ZoomFor(2.5); math.Abs(z-250) > 1e-9 {
		t.Errorf("zoom for 2.5: %v", z)
	}
}

// A calibration of the wide frame projects the places of the printer at each PTZ position as the
// map of the crop does, at each turn. A calibration of a zoomed frame goes back to the wide frame.
func TestCalibrationAtPTZ(t *testing.T) {
	wide := synthCalibration(t, newSynthCamera())
	pts := [][3]float64{{140, 140, 0}, {60, 200, 30}, {220, 80, 100}}
	for _, mrot := range []int{0, 180, 90} {
		m := testMap()
		m.Rotate = mrot
		for _, p := range []PTZPos{{Zoom: 100}, {Zoom: 230, Pan: 9000, Tilt: -4000}, {Zoom: 400, Pan: -36000, Tilt: 36000}} {
			at := wide.ToWide(m).AtPTZ(m, p)
			if at.PTZ == nil || *at.PTZ != p {
				t.Fatalf("position %+v", at.PTZ)
			}
			for _, x := range pts {
				// The truth: the place in the wide frame at the turn of the map, and then the
				// crop.
				u, v, _ := wide.Project(x, 1, 1, mrot)
				wu, wv := m.ToFrame(u, v, p)
				gu, gv, ok := at.Project(x, 960, 540, mrot)
				if !ok || math.Abs(gu-wu*960) > 1e-6 || math.Abs(gv-wv*540) > 1e-6 {
					t.Errorf("turn %d %+v %v: %.3f %.3f, want %.3f %.3f", mrot, p, x, gu, gv, wu*960, wv*540)
				}
			}
			back := at.ToWide(m)
			if back.PTZ != nil {
				t.Error("ToWide kept a position")
			}
			for _, x := range pts {
				u, v, _ := wide.Project(x, 960, 540, 0)
				gu, gv, _ := back.Project(x, 960, 540, 0)
				if math.Abs(gu-u) > 1e-6 || math.Abs(gv-v) > 1e-6 {
					t.Errorf("turn %d %+v %v: back at %.3f %.3f, want %.3f %.3f", mrot, p, x, gu, gv, u, v)
				}
			}
		}
	}
}

// PosFor zooms to the rectangle with the rectangle in the frame, and clamps to the ranges.
func TestPTZPosFor(t *testing.T) {
	m := testMap()
	for _, c := range []struct {
		r    [4]float64
		want func(p PTZPos) bool
	}{
		// A box in the middle, a quarter of the width: the zoom is near 4.
		{[4]float64{0.375, 0.4, 0.625, 0.6}, func(p PTZPos) bool { return p.Zoom == 400 && iabs(p.Pan) <= 100 && iabs(p.Tilt) <= 100 }},
		// A box at the right: the crop stops at the side.
		{[4]float64{0.8, 0.4, 0.99, 0.6}, func(p PTZPos) bool { return p.Zoom == 400 && p.Pan == 36000 }},
		// A wide box: no zoom.
		{[4]float64{0.05, 0.1, 0.95, 0.9}, func(p PTZPos) bool { return p.Zoom == 111 }},
		// A tiny box: the largest zoom.
		{[4]float64{0.5, 0.5, 0.51, 0.51}, func(p PTZPos) bool { return p.Zoom == 400 }},
	} {
		p := m.PosFor(c.r, testCtl)
		if !c.want(p) {
			t.Errorf("%v: %+v", c.r, p)
		}
		// The crop has the whole box, except where the box is larger than the largest zoom
		// allows.
		cu, cv, k := m.Window(p)
		half := 0.5 / k
		if p.Zoom < 400 && (c.r[0] < cu-half-1e-3 || c.r[2] > cu+half+1e-3 || c.r[1] < cv-half-1e-3 || c.r[3] > cv+half+1e-3) {
			t.Errorf("%v: the crop %.3f %.3f %.3f misses the box", c.r, cu, cv, k)
		}
	}
	// A camera with zoom only: pan and tilt stay 0.
	if p := m.PosFor([4]float64{0.8, 0.4, 0.99, 0.6}, PTZ{Zoom: testZoom}); p.Pan != 0 || p.Tilt != 0 {
		t.Errorf("zoom only: %+v", p)
	}
	// Limit stops a pan where the crop meets the side.
	if p := m.Limit(PTZPos{Zoom: 200, Pan: 36000}, testCtl); p.Pan != 24000 {
		t.Errorf("limit: %+v", p)
	}
}

func iabs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// ptzHub returns a hub with the test camera "test", its PTZ control, and a calibration of the wide
// frame. The capture does not run.
func ptzHub(t *testing.T) *Hub {
	h := viewHub(t, nil)
	h.ptzs = map[string]PTZ{"test": testCtl}
	h.formats = map[string]Format{"test": {InW: 960, InH: 540}}
	return h
}

// A calibration is valid at its PTZ position. At another position it needs the map, and with the
// map it projects to the crop of the position now.
func TestCalibrationAcrossPTZ(t *testing.T) {
	h := ptzHub(t)
	cam := h.Cam("test")
	if _, ok := cam.Calibration(); !ok {
		t.Fatal("the wide calibration is not valid at the wide position")
	}
	pos := PTZPos{Zoom: 200, Pan: 6000}
	h.SetPTZ("test", &pos)
	if _, ok := cam.Calibration(); ok {
		t.Error("valid at another position with no map")
	}
	if v := cam.Views(); len(v) != 1 {
		t.Errorf("views with no map: %v", v)
	}
	h.SetPTZMap("test", testMap())
	cal, ok := cam.Calibration()
	if !ok || cal.PTZ == nil || *cal.PTZ != pos {
		t.Fatalf("with the map: %v %+v", ok, cal)
	}
	wide := synthCalibration(t, newSynthCamera())
	x := [3]float64{150, 120, 10}
	u, v, _ := wide.Project(x, 1, 1, 0)
	wu, wv := testMap().ToFrame(u, v, pos)
	if gu, gv, _ := cal.Project(x, 960, 540, 0); math.Abs(gu-wu*960) > 1e-6 || math.Abs(gv-wv*540) > 1e-6 {
		t.Errorf("projection %.2f %.2f, want %.2f %.2f", gu, gv, wu*960, wv*540)
	}
	// A turn of the camera after the measurement: the map is not valid.
	s := h.Settings()
	cs := s.Camera("test")
	cs.Rotate = 90
	s.SetCamera(cs)
	h.SetSettings(s)
	if _, ok := cam.Calibration(); ok {
		t.Error("valid with a map of another turn")
	}
}

// A calibration made at a zoomed position is kept in the coordinates of the wide frame when the
// map exists, and with no map it keeps its position.
func TestSaveZoomedCalibration(t *testing.T) {
	h := ptzHub(t)
	cam := h.Cam("test")
	pos := PTZPos{Zoom: 300, Pan: -9000, Tilt: 3000}
	wide := synthCalibration(t, newSynthCamera())
	zoomed := wide.AtPTZ(testMap(), pos)

	h.SetPTZ("test", &pos)
	cam.SetCalibration(zoomed)
	if c := h.cals["test"]; c.PTZ == nil || *c.PTZ != pos {
		t.Fatalf("no map: saved %+v", c.PTZ)
	}
	if _, ok := cam.Calibration(); !ok {
		t.Error("a zoomed calibration is not valid at its position")
	}
	h.SetPTZ("test", nil)
	if _, ok := cam.Calibration(); ok {
		t.Error("a zoomed calibration with no map is valid at the wide position")
	}

	h.SetPTZMap("test", testMap())
	h.SetPTZ("test", &pos)
	cam.SetCalibration(wide.AtPTZ(testMap(), pos))
	c := h.cals["test"]
	if c.PTZ != nil {
		t.Fatalf("with the map: saved at %+v", c.PTZ)
	}
	for i := range c.P {
		if math.Abs(c.P[i]-wide.P[i]) > 1e-9 {
			t.Fatalf("saved matrix %v, want %v", c.P, wide.P)
		}
	}
}

// A series of PTZ steps changes the settings at once, and saves them once after the last step.
func TestSetPTZSavesOnce(t *testing.T) {
	focusSaveAfter = 50 * time.Millisecond
	t.Cleanup(func() { focusSaveAfter = 3 * time.Second })
	h := NewHub(Settings{})
	var mu sync.Mutex
	var saves []Settings
	h.OnSettings = func(s Settings) {
		mu.Lock()
		saves = append(saves, s)
		mu.Unlock()
	}
	for z := 100; z <= 140; z += 20 {
		h.SetPTZ(camA.ID, &PTZPos{Zoom: z})
	}
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(saves) != 1 || saves[0].Camera(camA.ID).PTZ.Zoom != 140 {
		t.Fatalf("saves: %+v", saves)
	}
}

// The settings of an older deck have no PTZ fields, and a camera with no PTZ writes none.
func TestPTZSettingsJSON(t *testing.T) {
	s := ParseSettings([]byte(`{"cameras":[{"id":"a","focus":300,"fixedFocus":true}]}`))
	if cs := s.Camera("a"); cs.PTZ != nil || cs.PTZMap != nil || cs.FollowPart || cs.Focus != 300 {
		t.Errorf("old file: %+v", cs)
	}
	b := Settings{Cameras: []CamSettings{{ID: "a"}}}.Bytes()
	var raw map[string][]map[string]any
	json.Unmarshal(b, &raw)
	if len(raw["cameras"][0]) != 1 {
		t.Errorf("fields with no PTZ: %s", b)
	}
	s.SetCamera(CamSettings{ID: "a", PTZ: &PTZPos{Zoom: 200, Pan: 3600}, PTZMap: testMap(), FollowPart: true})
	back := ParseSettings(s.Bytes()).Camera("a")
	if back.PTZ == nil || *back.PTZ != (PTZPos{Zoom: 200, Pan: 3600}) || !back.PTZMap.ValidAt(0) || !back.FollowPart {
		t.Errorf("round trip: %+v", back)
	}
}
