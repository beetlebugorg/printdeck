package octoapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"printdeck/internal/gcodestore"
	"printdeck/internal/printer"
)

const mainKey = "main-key"

// fakePrinter records what the API asks of the printer.
type fakePrinter struct {
	mu      sync.Mutex
	st      printer.State
	printed []string
	gcode   []string
	paused  bool
}

func (p *fakePrinter) State() printer.State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.st
}
func (p *fakePrinter) PrintFile(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.printed = append(p.printed, path)
}
func (p *fakePrinter) Pause()   { p.paused = true }
func (p *fakePrinter) Resume()  { p.paused = false }
func (p *fakePrinter) Cancel()  {}
func (p *fakePrinter) Connect() {}
func (p *fakePrinter) Gcode(lines ...string) error {
	if len(lines) > 0 && lines[0] == "G1 X999" {
		return errors.New("Move out of range")
	}
	p.gcode = append(p.gcode, lines...)
	return nil
}
func (p *fakePrinter) Home(axes ...string) error            { return nil }
func (p *fakePrinter) Jog(x, y, z float64, speed int) error { return nil }
func (p *fakePrinter) SetToolTemp(t int) error              { return nil }
func (p *fakePrinter) SetBedTemp(t int) error               { return nil }

func newTest(t *testing.T) (*Server, *httptest.Server, *fakePrinter) {
	t.Helper()
	p := &fakePrinter{st: printer.State{Reachable: true, Connected: true, Text: "Operational", Tool: 24}}
	dir := t.TempDir()
	s := New(p, &gcodestore.Store{Dir: dir}, mainKey, filepath.Join(dir, ".keys.json"))
	s.Version = "v2.2.0"
	mux := http.NewServeMux()
	s.Register(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return s, ts, p
}

func do(t *testing.T, method, url, key string, body io.Reader, contentType string) (*http.Response, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, url, body)
	if key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var m map[string]any
	json.NewDecoder(r.Body).Decode(&m)
	return r, m
}

func upload(t *testing.T, base, key, name, content string, fields map[string]string) (*http.Response, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields { // the fields first, as OrcaSlicer sends them
		mw.WriteField(k, v)
	}
	fw, _ := mw.CreateFormFile("file", name)
	fw.Write([]byte(content))
	mw.Close()
	return do(t, "POST", base+"/api/files/local", key, &buf, mw.FormDataContentType())
}

// OrcaSlicer: check the version, then upload a file and print it.
func TestOrcaSlicerUploadAndPrint(t *testing.T) {
	_, ts, p := newTest(t)
	r, v := do(t, "GET", ts.URL+"/api/version", "", nil, "")
	if r.StatusCode != 200 || !strings.HasPrefix(v["text"].(string), "OctoPrint") || !strings.Contains(v["text"].(string), "Printdeck v2.2.0") || v["api"] != "0.1" {
		t.Fatalf("version %d %v", r.StatusCode, v)
	}
	r, m := upload(t, ts.URL, mainKey, "benchy.gcode", "G28\n", map[string]string{"print": "true", "path": ""})
	if r.StatusCode != http.StatusCreated || m["effectivePrint"] != true {
		t.Fatalf("upload %d %v", r.StatusCode, m)
	}
	if len(p.printed) != 1 || p.printed[0] != "benchy.gcode" {
		t.Errorf("printed %v", p.printed)
	}
	_, list := do(t, "GET", ts.URL+"/api/files", mainKey, nil, "")
	files := list["files"].([]any)
	if len(files) != 1 || files[0].(map[string]any)["path"] != "benchy.gcode" || files[0].(map[string]any)["type"] != "machinecode" {
		t.Errorf("files %v", list)
	}
}

func TestUploadToAFolderWithoutPrint(t *testing.T) {
	_, ts, p := newTest(t)
	r, m := upload(t, ts.URL, mainKey, "cap.gcode", "G28\n", map[string]string{"path": "parts"})
	if r.StatusCode != http.StatusCreated || m["effectivePrint"] != false {
		t.Fatalf("upload %d %v", r.StatusCode, m)
	}
	if loc := r.Header.Get("Location"); !strings.HasSuffix(loc, "/api/files/local/parts/cap.gcode") {
		t.Errorf("location %q", loc)
	}
	if len(p.printed) != 0 {
		t.Errorf("printed %v with no print field", p.printed)
	}
	if r, _ := upload(t, ts.URL, mainKey, "notes.txt", "x", nil); r.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("a text file: %d", r.StatusCode)
	}
}

func TestKeysAreChecked(t *testing.T) {
	_, ts, _ := newTest(t)
	for _, key := range []string{"", "wrong"} {
		if r, _ := do(t, "GET", ts.URL+"/api/job", key, nil, ""); r.StatusCode != http.StatusForbidden {
			t.Errorf("key %q: %d, want 403", key, r.StatusCode)
		}
	}
	if r, _ := do(t, "GET", ts.URL+"/api/job?apikey="+mainKey, "", nil, ""); r.StatusCode != 200 {
		t.Errorf("apikey parameter: %d", r.StatusCode)
	}
}

func TestPrinterAndJob(t *testing.T) {
	_, ts, p := newTest(t)
	p.st = printer.State{Reachable: true, Connected: true, Text: "Printing", Printing: true, File: "cap.gcode",
		FilePath: "cap.gcode", FileSize: 1000, FilePos: 250, Completion: 25, TimeLeft: time.Hour, HasTimeLeft: true,
		Tool: 209.6, ToolTarget: 210}
	_, pr := do(t, "GET", ts.URL+"/api/printer", mainKey, nil, "")
	tool := pr["temperature"].(map[string]any)["tool0"].(map[string]any)
	flags := pr["state"].(map[string]any)["flags"].(map[string]any)
	if tool["actual"] != 209.6 || flags["printing"] != true || flags["ready"] != false {
		t.Errorf("printer %v", pr)
	}
	_, job := do(t, "GET", ts.URL+"/api/job", mainKey, nil, "")
	progress := job["progress"].(map[string]any)
	if job["state"] != "Printing" || progress["completion"] != 25.0 || progress["printTimeLeft"] != 3600.0 {
		t.Errorf("job %v", job)
	}
	r, _ := do(t, "POST", ts.URL+"/api/job", mainKey, strings.NewReader(`{"command":"pause","action":"pause"}`), "application/json")
	if r.StatusCode != 204 || !p.paused {
		t.Errorf("pause: %d, paused %v", r.StatusCode, p.paused)
	}
	if r, _ := do(t, "DELETE", ts.URL+"/api/files/local/cap.gcode", mainKey, nil, ""); r.StatusCode != http.StatusConflict {
		t.Errorf("delete the printing file: %d, want 409", r.StatusCode)
	}

	p.st = printer.State{Reachable: true, Err: "Klipper is not running", Text: "Offline"}
	if r, _ := do(t, "GET", ts.URL+"/api/printer", mainKey, nil, ""); r.StatusCode != http.StatusConflict {
		t.Errorf("printer while offline: %d, want 409", r.StatusCode)
	}
}

func TestCommandRefusal(t *testing.T) {
	_, ts, p := newTest(t)
	r, _ := do(t, "POST", ts.URL+"/api/printer/command", mainKey, strings.NewReader(`{"commands":["M105","G28"]}`), "application/json")
	if r.StatusCode != 204 || strings.Join(p.gcode, "|") != "M105|G28" {
		t.Errorf("command: %d %v", r.StatusCode, p.gcode)
	}
	r, m := do(t, "POST", ts.URL+"/api/printer/command", mainKey, strings.NewReader(`{"command":"G1 X999"}`), "application/json")
	if r.StatusCode != http.StatusConflict || m["error"] != "Move out of range" {
		t.Errorf("refused command: %d %v", r.StatusCode, m)
	}
}

// Cura: ask for a key, the user allows it on the touch screen, and the key works.
func TestAppKeysAllow(t *testing.T) {
	s, ts, _ := newTest(t)
	asked := make(chan func(bool), 1)
	s.Ask = func(q, yes, no string, answer func(bool)) {
		if q != "Allow Cura to use the Printdeck?" {
			t.Errorf("question %q", q)
		}
		asked <- answer
	}
	if r, _ := do(t, "GET", ts.URL+"/plugin/appkeys/probe", "", nil, ""); r.StatusCode != 204 {
		t.Fatalf("probe %d", r.StatusCode)
	}
	r, m := do(t, "POST", ts.URL+"/plugin/appkeys/request", "", strings.NewReader(`{"app":"Cura"}`), "application/json")
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("request %d", r.StatusCode)
	}
	poll := ts.URL + "/plugin/appkeys/request/" + m["app_token"].(string)
	if r, _ := do(t, "GET", poll, "", nil, ""); r.StatusCode != http.StatusAccepted {
		t.Errorf("poll before the answer: %d, want 202", r.StatusCode)
	}
	(<-asked)(true)
	r, m = do(t, "GET", poll, "", nil, "")
	key, _ := m["api_key"].(string)
	if r.StatusCode != 200 || key == "" {
		t.Fatalf("poll after allow: %d %v", r.StatusCode, m)
	}
	if r, _ := do(t, "GET", ts.URL+"/api/job", key, nil, ""); r.StatusCode != 200 {
		t.Errorf("the new key: %d", r.StatusCode)
	}
	if r, _ := do(t, "GET", poll, "", nil, ""); r.StatusCode != http.StatusNotFound {
		t.Errorf("the key was given twice: %d", r.StatusCode)
	}
	// The key is saved, so it works after a restart.
	if !newKeyStore("", s.keys.file).valid(key) {
		t.Error("the key is not saved")
	}
}

func TestAppKeysDeny(t *testing.T) {
	s, ts, _ := newTest(t)
	s.Ask = func(q, yes, no string, answer func(bool)) { answer(false) }
	_, m := do(t, "POST", ts.URL+"/plugin/appkeys/request", "", strings.NewReader(`{"app":"Cura"}`), "application/json")
	if r, _ := do(t, "GET", ts.URL+"/plugin/appkeys/request/"+m["app_token"].(string), "", nil, ""); r.StatusCode != http.StatusNotFound {
		t.Errorf("poll after deny: %d, want 404", r.StatusCode)
	}
}
