package octoapi

import (
	"bytes"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"printdeck/internal/printer"
	"printdeck/internal/timelapse"
)

// fakeLapse is the time lapse of the deck, with videos in a folder.
type fakeLapse struct {
	dir    string
	videos []timelapse.Video
}

func (f *fakeLapse) Settings() timelapse.Settings { return timelapse.Settings{On: true} }
func (f *fakeLapse) Info() timelapse.Info         { return timelapse.Info{Videos: f.videos} }
func (f *fakeLapse) VideoPath(name string) (string, bool) {
	for _, v := range f.videos {
		if v.Name == name {
			return filepath.Join(f.dir, name), true
		}
	}
	return "", false
}

// lapseVideo writes a video of n gray frames to dir, and returns its bytes and its last frame.
func lapseVideo(t *testing.T, dir, name string, n int) (video, last []byte) {
	t.Helper()
	r := timelapse.NewRecorder()
	for i := range n {
		img := image.NewGray(image.Rect(0, 0, 64, 48))
		for k := range img.Pix {
			img.Pix[k] = uint8(20 * i)
		}
		var b bytes.Buffer
		jpeg.Encode(&b, img, nil)
		last = b.Bytes()
		r.Add(last, image.Pt(64, 48), i+1)
	}
	var b bytes.Buffer
	if err := r.WriteVideo(&b); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return b.Bytes(), last
}

// The timelapse API lists the videos with their prints, and sends a video in parts and its last
// frame.
func TestTimelapseAPI(t *testing.T) {
	s, ts, _ := newTest(t)
	if _, m := do(t, "GET", ts.URL+"/api/timelapse", mainKey, nil, ""); m["enabled"] != false {
		t.Errorf("no time lapse: %v", m)
	}
	dir := t.TempDir()
	video, last := lapseVideo(t, dir, "clip-20261027-1432.mov", 10)
	lapseVideo(t, dir, "old-20261026-0900.mov", 3)
	at := time.Date(2026, 10, 27, 14, 32, 0, 0, time.UTC)
	s.Timelapse = &fakeLapse{dir: dir, videos: []timelapse.Video{
		{Name: "clip-20261027-1432.mov", Size: int64(len(video)), Time: at},
		{Name: "old-20261026-0900.mov", Size: 900, Time: at.Add(-29 * time.Hour)}}}
	s.History = func() []printer.Record {
		return []printer.Record{{File: "clip.gcode", Path: "parts/clip.gcode", Result: "complete", End: at,
			Duration: time.Hour, Video: "clip-20261027-1432.mov"}}
	}

	if r, _ := do(t, "GET", ts.URL+"/api/timelapse", "", nil, ""); r.StatusCode != http.StatusForbidden {
		t.Errorf("no key: %d", r.StatusCode)
	}
	_, m := do(t, "GET", ts.URL+"/api/timelapse", mainKey, nil, "")
	files, _ := m["files"].([]any)
	if m["enabled"] != true || len(files) != 2 {
		t.Fatalf("list %v", m)
	}
	f := files[0].(map[string]any)
	pd, _ := f["printdeck"].(map[string]any)
	pr, _ := pd["print"].(map[string]any)
	if f["name"] != "clip-20261027-1432.mov" || f["bytes"] != float64(len(video)) || f["date"] != "2026-10-27 14:32" ||
		f["url"] != "/downloads/timelapse/clip-20261027-1432.mov" {
		t.Errorf("video %v", f)
	}
	fps := timelapse.Rate(10)
	if pd["frames"] != float64(10+fps) || pd["fps"] != float64(fps) || pd["width"] != 64.0 || pd["still"] != "/downloads/timelapse/clip-20261027-1432.mov/still" {
		t.Errorf("printdeck %v", pd)
	}
	if pr["file"] != "clip.gcode" || pr["path"] != "parts/clip.gcode" || pr["duration"] != 3600.0 {
		t.Errorf("print %v", pr)
	}
	if old := files[1].(map[string]any)["printdeck"].(map[string]any); old["print"] != nil {
		t.Errorf("a video with no record has a print: %v", old)
	}

	// The download: the whole file, and a part.
	get := func(path, rng string) (*http.Response, []byte) {
		req, _ := http.NewRequest("GET", ts.URL+path, nil)
		req.Header.Set("X-Api-Key", mainKey)
		if rng != "" {
			req.Header.Set("Range", rng)
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r, b
	}
	r, b := get("/downloads/timelapse/clip-20261027-1432.mov", "")
	if r.StatusCode != http.StatusOK || !bytes.Equal(b, video) || r.Header.Get("Content-Type") != "video/quicktime" {
		t.Errorf("download: %d, %d bytes, %s", r.StatusCode, len(b), r.Header.Get("Content-Type"))
	}
	r, b = get("/downloads/timelapse/clip-20261027-1432.mov", "bytes=100-199")
	if r.StatusCode != http.StatusPartialContent || !bytes.Equal(b, video[100:200]) {
		t.Errorf("range: %d, %d bytes", r.StatusCode, len(b))
	}
	r, b = get("/downloads/timelapse/clip-20261027-1432.mov/still", "")
	if r.StatusCode != http.StatusOK || !bytes.Equal(b, last) {
		t.Errorf("still: %d, %d bytes", r.StatusCode, len(b))
	}
	for _, bad := range []string{"/downloads/timelapse/gone.mov", "/downloads/timelapse/..%2Fclip-20261027-1432.mov",
		"/downloads/timelapse/gone.mov/still"} {
		if r, _ := get(bad, ""); r.StatusCode != http.StatusNotFound {
			t.Errorf("%s: %d", bad, r.StatusCode)
		}
	}
	if r, _ := do(t, "GET", ts.URL+"/downloads/timelapse/clip-20261027-1432.mov", "", nil, ""); r.StatusCode != http.StatusForbidden {
		t.Errorf("download with no key: %d", r.StatusCode)
	}
}

func TestHumanSize(t *testing.T) {
	for n, want := range map[int64]string{900: "900B", 2048: "2.0KB", 5 << 20: "5.0MB", 3 << 30: "3.0GB"} {
		if got := humanSize(n); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", n, got, want)
		}
	}
}
