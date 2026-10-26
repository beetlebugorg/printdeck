package octoapi

import (
	"bufio"
	"context"
	"image"
	"image/jpeg"
	"mime"
	"mime/multipart"
	"testing"
	"time"

	"printdeck/internal/camera"
)

// viewScene is a nozzle and the boxes of a print, for the views.
type viewScene struct{}

func (viewScene) Nozzle() ([3]float64, bool) { return [3]float64{150, 150, 10}, true }
func (viewScene) Part() (camera.Box, bool) {
	return camera.Box{Min: [3]float64{100, 100, 0}, Max: [3]float64{200, 200, 40}}, true
}
func (viewScene) FirstLayer() (camera.Box, bool) {
	return camera.Box{Min: [3]float64{100, 100, 0}, Max: [3]float64{200, 200, 0.3}}, true
}

// The views of the webcam: the parameter view with w and fps, the view of each picture in a
// header, and the calibration and the views in the Printdeck API. With no calibration, each
// view is the whole picture.
func TestCameraViewsAPI(t *testing.T) {
	t.Setenv(camera.TestCameraEnv, "1")
	s, ts, _ := newTest(t)
	hub := camera.NewHub(camera.Settings{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go hub.Run(ctx)
	hub.SetScene(viewScene{})
	// A camera over a bed of 300 x 300 mm: x goes right, y goes up the picture, z up.
	cal := &camera.Calibration{Device: camera.TestCamera.ID, InW: 960, InH: 540, Width: 960, Height: 540,
		P: [12]float64{1.0 / 300, 0, 0, 0, 0, -1.0 / 300, -1.0 / 600, 1, 0, 0, 0, 1}, Error: 1.23,
		Points: 9, Time: time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)}
	hub.LoadCalibration(cal)
	s.Camera = hub

	snap := func(q string) (image.Point, string) {
		t.Helper()
		r := get(t, ts.URL+"/webcam/snapshot"+q, mainKey)
		img, err := jpeg.Decode(r.Body)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return img.Bounds().Size(), r.Header.Get("X-Printdeck-View")
	}
	for _, tc := range []struct {
		q, view string
		size    image.Point
	}{
		{"", "full", image.Pt(960, 540)},
		{"?view=toolhead", "toolhead", image.Pt(960, 540)},
		{"?view=toolhead&w=320", "toolhead", image.Pt(320, 180)},
		{"?view=firstlayer&w=480", "firstlayer", image.Pt(480, 270)},
		{"?view=part", "part", image.Pt(960, 540)},
		{"?view=fisheye&w=320", "full", image.Pt(320, 180)},
	} {
		if size, view := snap(tc.q); size != tc.size || view != tc.view {
			t.Errorf("%q: %v %q, want %v %q", tc.q, size, view, tc.size, tc.view)
		}
	}

	// The stream of a view.
	r := get(t, ts.URL+"/webcam/stream?view=part&w=160&fps=20", mainKey)
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	mr := multipart.NewReader(bufio.NewReader(r.Body), params["boundary"])
	for i := range 3 {
		p, err := mr.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		img, err := jpeg.Decode(p)
		if err != nil || img.Bounds().Dx() != 160 || p.Header.Get("X-Printdeck-View") != "part" {
			t.Fatalf("frame %d: %v %v %q", i, err, img.Bounds(), p.Header.Get("X-Printdeck-View"))
		}
	}
	r.Body.Close()

	_, m := do(t, "GET", ts.URL+"/api/plugin/printdeck", mainKey, nil, "")
	c, _ := m["camera"].(map[string]any)
	views, _ := c["views"].([]any)
	if c["calibrated"] != true || c["calibratedAt"] != "2026-10-25T12:00:00Z" || c["calibrationError"] != 1.2 ||
		len(views) != 4 || views[0] != "full" || views[3] != "part" {
		t.Errorf("camera %v", c)
	}

	// No calibration: each view is the whole picture.
	hub.LoadCalibration(nil)
	if size, view := snap("?view=part&w=320"); size != image.Pt(320, 180) || view != "full" {
		t.Errorf("no calibration: %v %q", size, view)
	}
	_, m = do(t, "GET", ts.URL+"/api/plugin/printdeck", mainKey, nil, "")
	c, _ = m["camera"].(map[string]any)
	if views, _ := c["views"].([]any); c["calibrated"] != false || len(views) != 1 || views[0] != "full" {
		t.Errorf("no calibration: camera %v", c)
	}
	if _, ok := c["calibratedAt"]; ok {
		t.Errorf("no calibration: calibratedAt %v", c["calibratedAt"])
	}
}
