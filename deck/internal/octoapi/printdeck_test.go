package octoapi

import (
	"image"
	"image/jpeg"
	"net/http"
	"strings"
	"testing"
	"time"

	"printdeck/internal/printer"
)

// The Printdeck API tells the roll in the printer and the print that runs.
func TestPrintdeckAPI(t *testing.T) {
	s, ts, p := newTest(t)
	s.Spools = &printer.Spools{}
	s.Spools.Add("Galaxy Black", "PLA", 1180, 180, 22, time.Unix(1, 0))
	s.Diameter = func() float64 { return 2.85 }
	p.st = printer.State{Reachable: true, Connected: true, Printing: true, FilePath: "benchy.gcode",
		Layer: 12, Layers: 27, Completion: 42, HasTimeLeft: true, TimeLeft: 83 * time.Minute}

	if r, _ := do(t, "GET", ts.URL+"/api/plugin/printdeck", "", nil, ""); r.StatusCode != http.StatusForbidden {
		t.Errorf("no key: %d", r.StatusCode)
	}
	_, m := do(t, "GET", ts.URL+"/api/plugin/printdeck", mainKey, nil, "")
	sp, _ := m["spool"].(map[string]any)
	job, _ := m["job"].(map[string]any)
	if m["material"] != "PLA" || m["diameter"] != 2.85 || sp["name"] != "Galaxy Black" || sp["left"] != 1000.0 {
		t.Errorf("roll: %v", m)
	}
	if job["file"] != "benchy.gcode" || job["layer"] != 12.0 || job["progress"] != 0.42 || job["left"] != 4980.0 {
		t.Errorf("job: %v", job)
	}
}

// With a frame, the settings have a webcam with the key in its address, and the snapshot is a JPEG.
func TestScreenWebcam(t *testing.T) {
	s, ts, _ := newTest(t)
	_, m := do(t, "GET", ts.URL+"/api/settings", mainKey, nil, "")
	if cam := m["webcam"].(map[string]any); cam["webcamEnabled"] != false {
		t.Errorf("webcam with no frame: %v", cam)
	}
	s.Frame = func() image.Image { return image.NewRGBA(image.Rect(0, 0, 80, 48)) }
	_, m = do(t, "GET", ts.URL+"/api/settings", mainKey, nil, "")
	cam := m["webcam"].(map[string]any)
	stream, _ := cam["streamUrl"].(string)
	if cam["webcamEnabled"] != true || stream != "/webcam/stream?apikey="+mainKey {
		t.Fatalf("webcam: %v", cam)
	}
	r, err := http.Get(ts.URL + strings.Replace(stream, "stream", "snapshot", 1))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	img, err := jpeg.Decode(r.Body)
	if err != nil || img.Bounds().Dx() != 80 {
		t.Errorf("snapshot: %v %v", err, img)
	}
	// The stream starts with a frame.
	r2, err := http.Get(ts.URL + stream)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Body.Close()
	buf := make([]byte, 64)
	n, _ := r2.Body.Read(buf)
	if !strings.HasPrefix(r2.Header.Get("Content-Type"), "multipart/x-mixed-replace") || !strings.Contains(string(buf[:n]), "--printdeckframe") {
		t.Errorf("stream: %q %q", r2.Header.Get("Content-Type"), buf[:n])
	}
}
