package camera

import (
	"encoding/json"
	"errors"
	"image"
	"math"
	"sync"
	"testing"
	"time"
)

// fakeCtrls is a device with PTZ controls for the test of resetPTZ.
type fakeCtrls struct {
	ranges map[uint32]PTZRange
	v      map[uint32]int
}

func (d *fakeCtrls) query(id uint32) (PTZRange, bool) { r, ok := d.ranges[id]; return r, ok }
func (d *fakeCtrls) set(id uint32, v int) error {
	if _, ok := d.ranges[id]; !ok {
		return errors.New("no control")
	}
	d.v[id] = v
	return nil
}

// resetPTZ sets a zoomed camera to its whole picture.
func TestResetPTZ(t *testing.T) {
	d := &fakeCtrls{ranges: map[uint32]PTZRange{cidZoomAbsolute: {Min: 16, Max: 116, Step: 1, Default: 16},
		cidPanAbsolute: {Min: -36000, Max: 36000, Step: 3600}, cidTiltAbsolute: {Min: -36000, Max: 36000, Step: 3600}},
		v: map[uint32]int{cidZoomAbsolute: 116, cidPanAbsolute: 18000, cidTiltAbsolute: 21600}}
	resetPTZ(d)
	if d.v[cidZoomAbsolute] != 16 || d.v[cidPanAbsolute] != 0 || d.v[cidTiltAbsolute] != 0 {
		t.Errorf("after the reset: %v", d.v)
	}
	// A camera with no PTZ controls.
	resetPTZ(&fakeCtrls{})
}

// testMap is the map of the deck PTZ at the turn 0.
func testMap() *PTZMap { return DeckMap(0) }

var testCtl = DeckPTZ()

// deckWindow is the crop of the deck PTZ at the position p: its center (cu, cv) and its zoom
// factor k, in normalized coordinates of the wide frame.
func deckWindow(p PTZPos) (cu, cv, k float64) {
	k = float64(p.Zoom) / 100
	half := 0.5 / k
	cu = max(half, min(1-half, 0.5+float64(p.Pan)/1000))
	cv = max(half, min(1-half, 0.5-float64(p.Tilt)/1000))
	return cu, cv, k
}

// The map agrees with the units of the deck PTZ, and ToFrame and ToWide are inverse.
func TestPTZMapRoundTrip(t *testing.T) {
	m := testMap()
	for _, p := range []PTZPos{{Zoom: 100}, {Zoom: 200, Pan: 120, Tilt: -60}, {Zoom: 400, Pan: 500, Tilt: 500}, {Zoom: 150, Pan: -500}, {Zoom: 800, Pan: 30}} {
		cu, cv, k := m.Window(p)
		tu, tv, tk := deckWindow(p)
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
		for _, p := range []PTZPos{{Zoom: 100}, {Zoom: 230, Pan: 90, Tilt: -40}, {Zoom: 400, Pan: -500, Tilt: 500}} {
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
		// A box in the middle, a quarter of the width: the zoom is 4.
		{[4]float64{0.375, 0.4, 0.625, 0.6}, func(p PTZPos) bool { return p.Zoom == 400 && iabs(p.Pan) <= 1 && iabs(p.Tilt) <= 1 }},
		// A box at the right: the crop has the box at its center.
		{[4]float64{0.8, 0.4, 0.99, 0.6}, func(p PTZPos) bool { return p.Zoom == 500 && p.Pan == 395 }},
		// A wide box: almost no zoom.
		{[4]float64{0.05, 0.1, 0.95, 0.9}, func(p PTZPos) bool { return p.Zoom == 110 }},
		// A tiny box: the largest zoom.
		{[4]float64{0.5, 0.5, 0.51, 0.51}, func(p PTZPos) bool { return p.Zoom == 800 }},
	} {
		p := m.PosFor(c.r, testCtl)
		if !c.want(p) {
			t.Errorf("%v: %+v", c.r, p)
		}
		// The crop has the whole box, except where the box is larger than the largest zoom
		// allows.
		cu, cv, k := m.Window(p)
		half := 0.5 / k
		if p.Zoom < 800 && (c.r[0] < cu-half-1e-3 || c.r[2] > cu+half+1e-3 || c.r[1] < cv-half-1e-3 || c.r[3] > cv+half+1e-3) {
			t.Errorf("%v: the crop %.3f %.3f %.3f misses the box", c.r, cu, cv, k)
		}
	}
	// A camera with zoom only: pan and tilt stay 0.
	if p := m.PosFor([4]float64{0.8, 0.4, 0.99, 0.6}, PTZ{Zoom: testCtl.Zoom}); p.Pan != 0 || p.Tilt != 0 {
		t.Errorf("zoom only: %+v", p)
	}
	// Limit stops a pan where the crop meets the side.
	if p := m.Limit(PTZPos{Zoom: 200, Pan: 500}, testCtl); p.Pan != 250 {
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

// A calibration is valid at each PTZ position: it projects to the crop of the position now.
func TestCalibrationAcrossPTZ(t *testing.T) {
	h := ptzHub(t)
	cam := h.Cam("test")
	if _, ok := cam.Calibration(); !ok {
		t.Fatal("the wide calibration is not valid at the wide position")
	}
	pos := PTZPos{Zoom: 200, Pan: 60}
	h.SetPTZ("test", &pos)
	cal, ok := cam.Calibration()
	if !ok || cal.PTZ == nil || *cal.PTZ != pos {
		t.Fatalf("at a zoom: %v %+v", ok, cal)
	}
	wide := synthCalibration(t, newSynthCamera())
	x := [3]float64{150, 120, 10}
	u, v, _ := wide.Project(x, 1, 1, 0)
	wu, wv := testMap().ToFrame(u, v, pos)
	if gu, gv, _ := cal.Project(x, 960, 540, 0); math.Abs(gu-wu*960) > 1e-6 || math.Abs(gv-wv*540) > 1e-6 {
		t.Errorf("projection %.2f %.2f, want %.2f %.2f", gu, gv, wu*960, wv*540)
	}
}

// A calibration made at a zoomed position is kept in the coordinates of the wide frame.
func TestSaveZoomedCalibration(t *testing.T) {
	h := ptzHub(t)
	cam := h.Cam("test")
	pos := PTZPos{Zoom: 300, Pan: -90, Tilt: 30}
	wide := synthCalibration(t, newSynthCamera())
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
	if cs := s.Camera("a"); cs.PTZ != nil || cs.FollowPart || cs.Focus != 300 {
		t.Errorf("old file: %+v", cs)
	}
	b := Settings{Cameras: []CamSettings{{ID: "a"}}}.Bytes()
	var raw map[string][]map[string]any
	json.Unmarshal(b, &raw)
	if len(raw["cameras"][0]) != 1 {
		t.Errorf("fields with no PTZ: %s", b)
	}
	s.SetCamera(CamSettings{ID: "a", PTZ: &PTZPos{Zoom: 200, Pan: 36}, FollowPart: true})
	back := ParseSettings(s.Bytes()).Camera("a")
	if back.PTZ == nil || *back.PTZ != (PTZPos{Zoom: 200, Pan: 36}) || !back.FollowPart {
		t.Errorf("round trip: %+v", back)
	}
	// A position in the units of a camera, from an older deck, is the wide position.
	old := ParseSettings([]byte(`{"cameras":[{"id":"a","ptz":{"zoom":116,"pan":18000,"tilt":21600},"ptzMap":{"rotate":0}}]}`))
	if cs := old.Camera("a"); cs.PTZ != nil {
		t.Errorf("old position: %+v", cs.PTZ)
	}
}

// The hub crops each frame of a camera at a PTZ position: a crop frame of the size of the pixels
// of the crop, at least ptzMinW wide, with the picture of the crop. A frame at the wide position
// is the frame of the camera.
func TestPTZCropFrame(t *testing.T) {
	h := viewHub(nil, nil)
	c := h.Cam("test")
	pic := texturePicture(1920, 1080)
	b, err := encodeJPEG(pic)
	if err != nil {
		t.Fatal(err)
	}
	latest := func() *Frame {
		h.mu.Lock()
		defer h.mu.Unlock()
		return c.latest
	}
	src, err := FrameOf(b)
	if err != nil {
		t.Fatal(err)
	}
	c.publish(src)
	if latest() != src {
		t.Fatal("a frame at the wide position is not the frame of the camera")
	}
	for _, tc := range []struct {
		pos  PTZPos
		w, h int
	}{
		{PTZPos{Zoom: 125, Pan: 50}, 1536, 864},
		{PTZPos{Zoom: 300, Pan: -100, Tilt: 100}, 1280, 720},
	} {
		h.SetPTZ("test", &tc.pos)
		src, _ := FrameOf(b)
		c.publish(src)
		f := latest()
		if f == src || f.Size() != image.Pt(tc.w, tc.h) {
			t.Fatalf("%+v: frame %v, want %dx%d", tc.pos, f.Size(), tc.w, tc.h)
		}
		cs := h.Settings().Camera("test")
		cr, _, _ := ptzRect(src.Size(), cs)
		want := cropScale(pic, cr, tc.w, tc.h)
		got, err := FrameOf(f.JPEG())
		if err != nil {
			t.Fatal(err)
		}
		if d := meanDiff(got.picture(), want); d > 4 {
			t.Errorf("%+v: the crop differs by %.1f", tc.pos, d)
		}
		// A small frame of the crop comes from the frame of the camera.
		if s := f.Small(320); s.Size().X != 320 || meanDiff(s.picture(), scaleYCbCr(want, 320, s.Size().Y, 0)) > 6 {
			t.Errorf("%+v: the small frame %v", tc.pos, s.Size())
		}
	}
}

// meanDiff is the mean difference of the luma of two pictures of one size.
func meanDiff(a, b *image.YCbCr) float64 {
	sum := 0
	w, h := a.Rect.Dx(), a.Rect.Dy()
	for y := range h {
		for x := range w {
			d := int(a.Y[a.YOffset(a.Rect.Min.X+x, a.Rect.Min.Y+y)]) - int(b.Y[b.YOffset(b.Rect.Min.X+x, b.Rect.Min.Y+y)])
			sum += max(d, -d)
		}
	}
	return float64(sum) / float64(w*h)
}
