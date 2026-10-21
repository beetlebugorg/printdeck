package webui

import (
	"bytes"
	"encoding/json"
	"image"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"printdeck/internal/gcodestore"
	"printdeck/internal/printer"
)

// fakePrinter records the actions of the page.
type fakePrinter struct {
	st    printer.State
	calls []string
}

func (f *fakePrinter) State() printer.State  { return f.st }
func (f *fakePrinter) Files() []printer.File { return []printer.File{{Path: "a.gcode", Name: "a.gcode", Date: time.Unix(100, 0)}} }
func (f *fakePrinter) LoadFiles()            {}
func (f *fakePrinter) Preview() image.Image  { return nil }
func (f *fakePrinter) Pause()                { f.calls = append(f.calls, "pause") }
func (f *fakePrinter) Resume()               { f.calls = append(f.calls, "resume") }
func (f *fakePrinter) Cancel()               { f.calls = append(f.calls, "cancel") }
func (f *fakePrinter) Heat(t, b int)         { f.calls = append(f.calls, "heat "+itoa(t)+" "+itoa(b)) }
func (f *fakePrinter) PrintFile(p string)    { f.calls = append(f.calls, "print "+p) }

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func newTest(t *testing.T) (*Server, *fakePrinter, *httptest.Server) {
	t.Helper()
	p := &fakePrinter{st: printer.State{Reachable: true, Connected: true, Text: "Operational", Tool: 24.04, Bed: 23}}
	s := &Server{Printer: p, PreheatTool: 200, PreheatBed: 60, Store: &gcodestore.Store{Dir: t.TempDir()},
		Auth: func(h http.HandlerFunc) http.HandlerFunc { return h }}
	mux := http.NewServeMux()
	s.Register(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return s, p, ts
}

func post(t *testing.T, ts *httptest.Server, body string) int {
	t.Helper()
	r, err := http.Post(ts.URL+"/api/deck/action", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	return r.StatusCode
}

func TestState(t *testing.T) {
	_, _, ts := newTest(t)
	r, err := http.Get(ts.URL + "/api/deck/state")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var st state
	json.NewDecoder(r.Body).Decode(&st)
	if !st.Printer.Reachable || st.Printer.Tool != 24 || st.Preheat != [2]int{200, 60} || st.Music != nil || st.Printer.TimeLeft != -1 {
		t.Errorf("state %+v", st)
	}
}

func TestActions(t *testing.T) {
	_, p, ts := newTest(t)
	for _, a := range []string{`{"action":"preheat"}`, `{"action":"cooldown"}`, `{"action":"print","path":"a.gcode"}`} {
		if code := post(t, ts, a); code != http.StatusNoContent {
			t.Errorf("%s: %d", a, code)
		}
	}
	want := []string{"heat 200 60", "heat 0 0", "print a.gcode"}
	if strings.Join(p.calls, ",") != strings.Join(want, ",") {
		t.Errorf("calls %v, want %v", p.calls, want)
	}
	// A path out of the folder, an unknown action, and the music with no speaker are refused.
	for _, a := range []string{`{"action":"print","path":"../etc/passwd"}`, `{"action":"format"}`, `{"action":"music-toggle"}`} {
		if code := post(t, ts, a); code < 400 {
			t.Errorf("%s: %d", a, code)
		}
	}
	// No second print while one runs.
	p.st.Printing = true
	if code := post(t, ts, `{"action":"print","path":"a.gcode"}`); code != http.StatusConflict {
		t.Errorf("print during a print: %d", code)
	}
	if code := post(t, ts, `{"action":"pause"}`); code != http.StatusNoContent || p.calls[len(p.calls)-1] != "pause" {
		t.Errorf("pause: %d %v", code, p.calls)
	}
}

func TestUpload(t *testing.T) {
	s, p, ts := newTest(t)
	upload := func(name, print string) *http.Response {
		var b bytes.Buffer
		w := multipart.NewWriter(&b)
		fw, _ := w.CreateFormFile("file", name)
		fw.Write([]byte("G28\n"))
		if print != "" {
			w.WriteField("print", print)
		}
		w.Close()
		r, err := http.Post(ts.URL+"/api/deck/upload", w.FormDataContentType(), &b)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r
	}
	if r := upload("benchy.gcode", "1"); r.StatusCode != http.StatusOK {
		t.Fatalf("upload: %d", r.StatusCode)
	}
	if b, err := os.ReadFile(filepath.Join(s.Store.Dir, "benchy.gcode")); err != nil || string(b) != "G28\n" {
		t.Errorf("saved file %q, %v", b, err)
	}
	if len(p.calls) != 1 || p.calls[0] != "print benchy.gcode" {
		t.Errorf("calls %v", p.calls)
	}
	if r := upload("notes.txt", ""); r.StatusCode != http.StatusBadRequest {
		t.Errorf("a .txt upload: %d", r.StatusCode)
	}
}
