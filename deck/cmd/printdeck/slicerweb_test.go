package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"printdeck/internal/printer"
	"printdeck/internal/ui"
	"strings"
	"testing"
	"time"

	"printdeck/internal/remote"
)

func TestSlicerPage(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<p>zslice</p>"), 0o644)
	os.WriteFile(filepath.Join(dir, "zslice.wasm"), []byte("\x00asm"), 0o644)
	rs := remote.New()
	rs.Password = func() string { return "pw" }
	mux := http.NewServeMux()
	mux.Handle("/", rs.Handler())
	registerSlicer(mux, rs, dir, nil)
	ts := httptest.NewServer(remote.SecurityHeaders(mux))
	defer ts.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	get := func(path, cookie string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest("GET", ts.URL+path, nil)
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		r, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	// No login: no app.
	if r := get("/slicer/", ""); r.StatusCode != http.StatusUnauthorized {
		t.Errorf("/slicer/ with no login: %d", r.StatusCode)
	}
	r, err := client.PostForm(ts.URL+"/login", map[string][]string{"password": {"pw"}})
	if err != nil {
		t.Fatal(err)
	}
	cookie := strings.Split(r.Header.Get("Set-Cookie"), ";")[0]

	r = get("/slicer/", cookie)
	body, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 || !strings.Contains(string(body), "zslice") {
		t.Fatalf("/slicer/: %d %q", r.StatusCode, body)
	}
	for k, v := range map[string]string{"Cross-Origin-Opener-Policy": "same-origin", "Cross-Origin-Embedder-Policy": "require-corp",
		"Content-Security-Policy": slicerCSP, "X-Frame-Options": "DENY"} {
		if got := r.Header.Get(k); got != v {
			t.Errorf("%s: %q", k, got)
		}
	}
	if r := get("/slicer/zslice.wasm", cookie); r.StatusCode != 200 || r.Header.Get("Content-Type") != "application/wasm" {
		t.Errorf("zslice.wasm: %d %q", r.StatusCode, r.Header.Get("Content-Type"))
	}

	r = get("/slicer/printer.json", cookie)
	var p map[string]string
	json.NewDecoder(r.Body).Decode(&p)
	if p["kind"] != "printdeck" || p["url"] != "" || p["name"] == "" || p["https"] != "" || len(p) != 4 {
		t.Errorf("printer.json: %v", p)
	}
	if r := get("/slicer/printer.json", ""); r.StatusCode != http.StatusUnauthorized {
		t.Errorf("printer.json with no login: %d", r.StatusCode)
	}
	if r := get("/slicer/../../etc/passwd", cookie); r.StatusCode == 200 {
		t.Errorf("a path out of the folder: %d", r.StatusCode)
	}
}

// An image with no zslice app has no /slicer/ page.
func TestSlicerPageNotInImage(t *testing.T) {
	mux := http.NewServeMux()
	registerSlicer(mux, remote.New(), filepath.Join(t.TempDir(), "none"), nil)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	r, _ := http.Get(ts.URL + "/slicer/")
	if r.StatusCode != http.StatusNotFound {
		t.Errorf("%d", r.StatusCode)
	}
}

// /slicer/printer.json gives the file format and the model of each printer, so a slicer sends a
// sliced 3MF to a Bambu printer and G-code to a Klipper printer.
func TestPrinterFileFormat(t *testing.T) {
	defer func(l func() []ui.Identity, f func(string) (string, string)) { deckPrinterList, printerFormat = l, f }(deckPrinterList, printerFormat)
	deckPrinterList = func() []ui.Identity {
		return []ui.Identity{{ID: "first", Name: "TAZ Workhorse", Label: "TAZ"}, {ID: "x2d", Name: "X2D", Label: "X2D"}}
	}
	printerFormat = func(id string) (string, string) {
		if id == "x2d" {
			return "gcode.3mf", "Bambu Lab X2D"
		}
		return "gcode", ""
	}
	b, _ := json.Marshal(slicerPrinterFile())
	var got struct {
		Files    string `json:"files"`
		Printers []struct {
			ID, URL, Files, Model string
		} `json:"printers"`
	}
	json.Unmarshal(b, &got)
	if got.Files != "gcode" || len(got.Printers) != 2 {
		t.Fatalf("%s", b)
	}
	if p := got.Printers[1]; p.URL != "/printers/x2d" || p.Files != "gcode.3mf" || p.Model != "Bambu Lab X2D" {
		t.Errorf("%+v", p)
	}
	if p := got.Printers[0]; p.Files != "gcode" || p.Model != "" {
		t.Errorf("%+v", p)
	}
}

func TestBackendFormat(t *testing.T) {
	if f, _ := backendFormat(printer.NewBambu("127.0.0.1", "S", "C", "", "X2D", nil)); f != "gcode.3mf" {
		t.Errorf("bambu %q", f)
	}
	if f, _ := backendFormat(printer.NewChiTu("127.0.0.1", "Phenom", nil)); f != "ctb" {
		t.Errorf("chitu %q", f)
	}
}

// /slicer/printer.json gives the state of each printer and the address of its camera.
func TestPrinterFileLive(t *testing.T) {
	defer func(l func() []ui.Identity, f func(string, string) slicerLive) { deckPrinterList, printerLive = l, f }(deckPrinterList, printerLive)
	deckPrinterList = func() []ui.Identity {
		return []ui.Identity{{ID: "first", Name: "TAZ Workhorse", Label: "TAZ"}, {ID: "x2d", Name: "X2D", Label: "X2D"}}
	}
	printerLive = func(id, url string) slicerLive {
		if id == "x2d" {
			return liveOf(printer.State{Reachable: true, Connected: true, Printing: true, Completion: 81, File: "panda.gcode.3mf",
				HasTimeLeft: true, TimeLeft: 17 * time.Minute}, true, url)
		}
		return liveOf(printer.State{Reachable: true, Connected: true, Text: "Ready"}, false, url)
	}
	b, _ := json.Marshal(slicerPrinterFile())
	var got struct {
		State    string `json:"state"`
		Printers []struct {
			ID, State, Job, Camera string
			Progress               float64
			Left                   int
		} `json:"printers"`
	}
	json.Unmarshal(b, &got)
	if got.State != "ready" || len(got.Printers) != 2 {
		t.Fatalf("%s", b)
	}
	if p := got.Printers[1]; p.State != "printing" || p.Progress != 0.81 || p.Left != 1020 || p.Job != "Panda" ||
		p.Camera != "/printers/x2d/webcam/stream" {
		t.Errorf("%+v", p)
	}
	if p := got.Printers[0]; p.State != "ready" || p.Camera != "" {
		t.Errorf("%+v", p)
	}
	for st, want := range map[*printer.State]string{
		{}: "connecting", {Err: "No answer"}: "offline", {Err: "refused", ErrFinal: true}: "offline",
		{Reachable: true, Connected: true, Paused: true, Printing: true}: "paused",
	} {
		if got := liveOf(*st, false, "").State; got != want {
			t.Errorf("%+v: %q, want %q", *st, got, want)
		}
	}
}
