package octoapi

import (
	"bufio"
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"sync"
	"testing"
	"time"

	"printdeck/internal/camera"
)

// fakeCamera makes a new frame each 10 ms while a viewer watches.
type fakeCamera struct {
	on      bool
	pic     []byte
	mu      sync.Mutex
	seq     uint64
	viewers int
}

func (c *fakeCamera) On() bool { return c.on }
func (c *fakeCamera) Status() camera.Status {
	return camera.Status{State: "on", Device: camera.Device{Name: "Test camera"}, Format: camera.Format{OutW: 640, OutH: 360, OutFPS: 15}}
}
func (c *fakeCamera) frame() *camera.Frame {
	f, err := camera.FrameOf(c.pic)
	if err != nil {
		panic(err)
	}
	c.mu.Lock()
	c.seq++
	f.Seq = c.seq
	c.mu.Unlock()
	return f
}
func (c *fakeCamera) Snapshot(ctx context.Context) (*camera.Frame, error) { return c.frame(), nil }
func (c *fakeCamera) Next(ctx context.Context, after uint64) (*camera.Frame, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(10 * time.Millisecond):
	}
	return c.frame(), nil
}
func (c *fakeCamera) Watch() func() {
	c.mu.Lock()
	c.viewers++
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		c.viewers--
		c.mu.Unlock()
	}
}

func cameraJPEG(t *testing.T) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 640, 360))
	for y := range 360 {
		for x := range 640 {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 90, 255})
		}
	}
	var b bytes.Buffer
	jpeg.Encode(&b, img, nil)
	return b.Bytes()
}

func get(t *testing.T, url, key string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	if key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Body.Close() })
	return r
}

// The webcam is the camera when it is on, at the size that the client asks for, and the screen
// when it is off.
func TestCameraWebcam(t *testing.T) {
	s, ts, _ := newTest(t)
	cam := &fakeCamera{on: true, pic: cameraJPEG(t)}
	s.Camera = cam
	s.Frame = func() image.Image { return image.NewRGBA(image.Rect(0, 0, 80, 48)) }

	size := func(r *http.Response) image.Point {
		t.Helper()
		img, err := jpeg.Decode(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		return img.Bounds().Size()
	}
	if got := size(get(t, ts.URL+"/webcam/snapshot", mainKey)); got != image.Pt(640, 360) {
		t.Errorf("snapshot %v", got)
	}
	if got := size(get(t, ts.URL+"/webcam/snapshot?w=320", mainKey)); got != image.Pt(320, 180) {
		t.Errorf("small snapshot %v", got)
	}
	if r := get(t, ts.URL+"/webcam/snapshot", ""); r.StatusCode != http.StatusForbidden {
		t.Errorf("no key: %d", r.StatusCode)
	}
	// Allow lets the login of the remote screen through.
	s.Allow = func(r *http.Request) bool { return r.Header.Get("Cookie") == "login=ok" }
	req, _ := http.NewRequest("GET", ts.URL+"/webcam/snapshot", nil)
	req.Header.Set("Cookie", "login=ok")
	if r, err := http.DefaultClient.Do(req); err != nil || r.StatusCode != 200 {
		t.Errorf("login: %v %v", err, r.StatusCode)
	}

	// The stream: frames of the size asked, at the rate asked.
	r := get(t, ts.URL+"/webcam/stream?w=160&fps=20", mainKey)
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	mr := multipart.NewReader(bufio.NewReader(r.Body), params["boundary"])
	start := time.Now()
	for i := range 5 {
		p, err := mr.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(p)
		img, err := jpeg.Decode(bytes.NewReader(b))
		if err != nil || img.Bounds().Dx() != 160 {
			t.Fatalf("frame %d: %v %v", i, err, img.Bounds())
		}
	}
	if d := time.Since(start); d < 4*50*time.Millisecond {
		t.Errorf("5 frames at 20 fps in %v", d)
	}
	cam.mu.Lock()
	if cam.viewers != 1 {
		t.Errorf("%d viewers while the stream runs", cam.viewers)
	}
	cam.mu.Unlock()
	r.Body.Close()

	// The Printdeck API tells the camera.
	_, m := do(t, "GET", ts.URL+"/api/plugin/printdeck", mainKey, nil, "")
	if c, _ := m["camera"].(map[string]any); c["name"] != "Test camera" || c["width"] != 640.0 {
		t.Errorf("camera %v", m["camera"])
	}

	// With the camera off, the webcam is the screen.
	cam.on = false
	if got := size(get(t, ts.URL+"/webcam/snapshot", mainKey)); got != image.Pt(80, 48) {
		t.Errorf("screen snapshot %v", got)
	}
	if got := size(get(t, ts.URL+"/webcam/snapshot?w=64", mainKey)); got.X != 64 {
		t.Errorf("small screen snapshot %v", got)
	}
	if _, m := do(t, "GET", ts.URL+"/api/plugin/printdeck", mainKey, nil, ""); m["camera"] != nil {
		t.Errorf("camera with the camera off: %v", m["camera"])
	}
}
