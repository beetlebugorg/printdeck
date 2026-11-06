package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"printdeck/internal/printer"
)

type cfgFake struct {
	st       printer.State
	restarts int
}

func (f *cfgFake) State() printer.State  { return f.st }
func (f *cfgFake) RestartKlipper() error { f.restarts++; return nil }

// The endpoint reads printer.cfg, and a PUT keeps the old file as the backup and restarts
// Klipper. It refuses a request with no access, a file with no [printer], and a PUT during a print.
func TestPrinterCfg(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "printer.cfg")
	old := "[printer]\nkinematics: cartesian\n"
	os.WriteFile(path, []byte(old), 0o664)
	pr := &cfgFake{}
	mux := http.NewServeMux()
	registerPrinterCfg(mux, path, filepath.Join(dir, "state.bak"), pr, func(r *http.Request) bool { return r.Header.Get("X-Api-Key") == "k" })
	do := func(method, url, body, key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, strings.NewReader(body))
		if key != "" {
			r.Header.Set("X-Api-Key", key)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if w := do("GET", "/system/printer.cfg", "", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("GET with no key: %d", w.Code)
	}
	if w := do("GET", "/system/printer.cfg", "", "k"); w.Code != 200 || w.Body.String() != old {
		t.Errorf("GET: %d %q", w.Code, w.Body.String())
	}
	if w := do("PUT", "/system/printer.cfg", "[fan]\npin: PB4\n", "k"); w.Code != http.StatusBadRequest {
		t.Errorf("PUT with no [printer]: %d", w.Code)
	}
	if w := do("PUT", "/system/printer.cfg", "[printer]\n  bad line at the start\n", "k"); w.Code != http.StatusBadRequest {
		t.Errorf("PUT with a parse error: %d %s", w.Code, w.Body.String())
	}
	pr.st = printer.State{Printing: true, Text: "Printing"}
	if w := do("PUT", "/system/printer.cfg", old, "k"); w.Code != http.StatusConflict {
		t.Errorf("PUT during a print: %d", w.Code)
	}
	pr.st = printer.State{}
	neu := "[printer]\r\nkinematics: cartesian\r\nmax_velocity: 300\r\n"
	if w := do("PUT", "/system/printer.cfg", neu, "k"); w.Code != 200 || !strings.Contains(w.Body.String(), `"restarted":true`) {
		t.Fatalf("PUT: %d %s", w.Code, w.Body.String())
	}
	if b, _ := os.ReadFile(path); string(b) != strings.ReplaceAll(neu, "\r", "") {
		t.Errorf("printer.cfg %q", b)
	}
	if w := do("GET", "/system/printer.cfg?backup=1", "", "k"); w.Body.String() != old {
		t.Errorf("backup %q", w.Body.String())
	}
	if pr.restarts != 1 {
		t.Errorf("%d restarts, want 1", pr.restarts)
	}
	if b, _ := os.ReadFile(path); string(b) == old {
		t.Error("no change")
	}
}
