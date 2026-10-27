package octoapi

import (
	"bufio"
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"printdeck/internal/camera"
)

// A deck with two cameras: the parameter camera chooses the camera of the webcam, the settings
// list the cameras as OctoPrint 1.9 does, and the Printdeck API lists them with their roles.
func TestTwoCamerasAPI(t *testing.T) {
	t.Setenv(camera.TestCameraEnv, "2")
	s, ts, _ := newTest(t)
	hub := camera.NewHub(camera.Settings{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go hub.Run(ctx)
	s.Camera = hub

	snap := func(q string) (int, []byte) {
		t.Helper()
		r := get(t, ts.URL+"/webcam/snapshot"+q, mainKey)
		b, _ := io.ReadAll(r.Body)
		return r.StatusCode, b
	}
	code, main := snap("")
	if code != 200 {
		t.Fatalf("main camera: %d", code)
	}
	code, one := snap("?camera=1")
	if code != 200 {
		t.Fatalf("camera 1: %d", code)
	}
	code, two := snap("?camera=2")
	if code != 200 {
		t.Fatalf("camera 2: %d", code)
	}
	if bytes.Equal(one, two) || len(main) == 0 {
		t.Error("cameras 1 and 2 send the same picture")
	}
	for q, want := range map[string]int{"?camera=3": 404, "?camera=nope": 404, "?camera=test2&w=320": 200,
		"?camera=picture": 200, "?camera=lapse": 200} {
		if code, _ := snap(q); code != want {
			t.Errorf("%s: %d, want %d", q, code, want)
		}
	}
	if r := get(t, ts.URL+"/webcam/snapshot?camera=2", ""); r.StatusCode != http.StatusForbidden {
		t.Errorf("no key: %d", r.StatusCode)
	}

	// The stream of camera 2.
	r := get(t, ts.URL+"/webcam/stream?camera=2&w=160&fps=20", mainKey)
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	mr := multipart.NewReader(bufio.NewReader(r.Body), params["boundary"])
	for i := range 2 {
		p, err := mr.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		img, err := jpeg.Decode(p)
		if err != nil || img.Bounds().Size() != image.Pt(160, 90) {
			t.Fatalf("frame %d: %v", i, err)
		}
	}
	r.Body.Close()

	// The settings of OctoPrint 1.9: a webcam for each camera, with the key in the addresses.
	_, m := do(t, "GET", ts.URL+"/api/settings", mainKey, nil, "")
	wc, _ := m["webcam"].(map[string]any)
	list, _ := wc["webcams"].([]any)
	if len(list) != 2 || wc["defaultWebcam"] != "camera1" || wc["snapshotWebcam"] != "camera1" {
		t.Fatalf("webcam %v", wc)
	}
	w2, _ := list[1].(map[string]any)
	compat, _ := w2["compat"].(map[string]any)
	if w2["name"] != "camera2" || w2["displayName"] != "Test camera 2" ||
		compat["stream"] != "/webcam/stream?camera=2&apikey="+mainKey || compat["snapshot"] != "/webcam/snapshot?camera=2&apikey="+mainKey {
		t.Errorf("webcam 2 %v", w2)
	}
	if wc["streamUrl"] != "/webcam/stream?apikey="+mainKey {
		t.Errorf("stream %v", wc["streamUrl"])
	}

	// The Printdeck API: the main camera, and the list with the roles.
	_, m = do(t, "GET", ts.URL+"/api/plugin/printdeck", mainKey, nil, "")
	cams, _ := m["cameras"].([]any)
	if len(cams) != 2 {
		t.Fatalf("cameras %v", m["cameras"])
	}
	c1, _ := cams[0].(map[string]any)
	c2, _ := cams[1].(map[string]any)
	if c1["number"] != 1.0 || c1["id"] != "test" || len(c1["roles"].([]any)) != 3 || c2["number"] != 2.0 || c2["id"] != "test2" ||
		c2["name"] != "Test camera 2" || c2["roles"] != nil {
		t.Errorf("cameras %v", cams)
	}
	if c, _ := m["camera"].(map[string]any); c["id"] != "test" || c["name"] != "Test camera 1" {
		t.Errorf("main camera %v", c)
	}

	// The picture role on camera 2, and camera 2 off.
	hub.SetSettings(camera.Settings{Picture: "test2"})
	_, m = do(t, "GET", ts.URL+"/api/settings", mainKey, nil, "")
	if wc, _ := m["webcam"].(map[string]any); wc["snapshotWebcam"] != "camera2" {
		t.Errorf("snapshot webcam %v", wc["snapshotWebcam"])
	}
	off := camera.Settings{}
	off.SetCamera(camera.CamSettings{ID: "test2", Off: true})
	hub.SetSettings(off)
	if code, b := snap("?camera=2"); code != http.StatusServiceUnavailable || !strings.Contains(string(b), "off") {
		t.Errorf("camera that is off: %d %s", code, b)
	}
	_, m = do(t, "GET", ts.URL+"/api/settings", mainKey, nil, "")
	if wc, _ := m["webcam"].(map[string]any); len(wc["webcams"].([]any)) != 1 {
		t.Errorf("webcams with camera 2 off: %v", wc["webcams"])
	}
	_, m = do(t, "GET", ts.URL+"/api/plugin/printdeck", mainKey, nil, "")
	if cams, _ := m["cameras"].([]any); len(cams) != 2 || cams[1].(map[string]any)["state"] != "off" {
		t.Errorf("cameras with camera 2 off: %v", m["cameras"])
	}
}

// A deck with one camera that has no list: camera 1 is the main camera.
func TestOneCameraParameter(t *testing.T) {
	s, ts, _ := newTest(t)
	s.Camera = &fakeCamera{on: true, pic: cameraJPEG(t)}
	if r := get(t, ts.URL+"/webcam/snapshot?camera=1", mainKey); r.StatusCode != 200 {
		t.Errorf("camera 1: %d", r.StatusCode)
	}
	if r := get(t, ts.URL+"/webcam/snapshot?camera=2", mainKey); r.StatusCode != 404 {
		t.Errorf("camera 2: %d", r.StatusCode)
	}
	_, m := do(t, "GET", ts.URL+"/api/settings", mainKey, nil, "")
	wc, _ := m["webcam"].(map[string]any)
	if list, _ := wc["webcams"].([]any); len(list) != 1 {
		t.Errorf("webcams %v", wc["webcams"])
	}
}
