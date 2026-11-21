package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"printdeck/internal/gcodestore"
	"printdeck/internal/octoapi"
	"printdeck/internal/printer"
	"printdeck/internal/remote"
)

// A request of the API with the key of another printer goes to that printer. Each other request,
// and each page, goes to the first printer.
func TestKeyRouter(t *testing.T) {
	answer := func(s string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, s) })
	}
	mini := keyRoute{valid: func(r *http.Request) bool { return r.Header.Get("X-Api-Key") == "MINI" }, h: answer("mini")}
	h := keyRouter(answer("first"), []keyRoute{mini})
	for _, c := range []struct{ path, key, want string }{
		{"/api/job", "MINI", "mini"},
		{"/api/job", "FIRST", "first"},
		{"/api/job", "", "first"},
		{"/plugin/printdeck/x", "MINI", "mini"},
		{"/downloads/files/local/a.gcode", "MINI", "mini"},
		{"/system", "MINI", "first"},
		{"/", "MINI", "first"},
	} {
		r := httptest.NewRequest("GET", c.path, nil)
		if c.key != "" {
			r.Header.Set("X-Api-Key", c.key)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Body.String() != c.want {
			t.Errorf("%s with %q: %q, want %q", c.path, c.key, w.Body.String(), c.want)
		}
	}
}

// A deck key works on each printer, but the router routes only by the own keys of a printer
// (octoapi.Server.OwnKey), so a request at the root with a deck key stays with the first printer.
// The list of the printers takes a deck key, and refuses a request with no login and no key.
func TestDeckKeyRoutes(t *testing.T) {
	dir := t.TempDir()
	dk := octoapi.NewDeckKeys(filepath.Join(dir, "deck-apikeys.json"))
	p := printer.NewBambu("127.0.0.1", "S", "C", "", "X2D", nil) // an octoapi.Printer with no link
	first := octoapi.New(p, &gcodestore.Store{Dir: dir}, "", filepath.Join(dir, "a.json"))
	other := octoapi.New(p, &gcodestore.Store{Dir: dir}, "", filepath.Join(dir, "b.json"))
	first.DeckKeys, first.DeckWide, other.DeckKeys = dk, true, dk
	first.Ask = func(q, yes, no string, answer func(bool)) { answer(true) }
	fm, om := http.NewServeMux(), http.NewServeMux()
	first.Register(fm)
	other.Register(om)
	h := keyRouter(fm, []keyRoute{{valid: other.OwnKey, h: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "other")
	})}})
	// Get a deck key with the App Keys flow at the root.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/plugin/appkeys/request", strings.NewReader(`{"app":"zslice"}`)))
	var req struct {
		Token string `json:"app_token"`
	}
	json.Unmarshal(w.Body.Bytes(), &req)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/plugin/appkeys/request/"+req.Token, nil))
	var ans struct {
		Key   string `json:"api_key"`
		Scope string `json:"scope"`
	}
	json.Unmarshal(w.Body.Bytes(), &ans)
	if ans.Key == "" || ans.Scope != "deck" {
		t.Fatalf("answer %s", w.Body.String())
	}
	r := httptest.NewRequest("GET", "/api/version", nil)
	r.Header.Set("X-Api-Key", ans.Key)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Body.String() == "other" || w.Code != 200 {
		t.Errorf("a deck key at the root: %d %q", w.Code, w.Body.String())
	}

	mux := http.NewServeMux()
	registerSlicer(mux, remote.New(), filepath.Join(dir, "none"), listKey(dk, []*octoapi.Server{first, nil, other}))
	// A key of the second printer only, as a slicer that paired under /printers/<id>/.
	other.Ask = first.Ask
	w = httptest.NewRecorder()
	om.ServeHTTP(w, httptest.NewRequest("POST", "/plugin/appkeys/request", strings.NewReader(`{"app":"old"}`)))
	json.Unmarshal(w.Body.Bytes(), &req)
	w = httptest.NewRecorder()
	om.ServeHTTP(w, httptest.NewRequest("GET", "/plugin/appkeys/request/"+req.Token, nil))
	var own struct {
		Key   string `json:"api_key"`
		Scope string `json:"scope"`
	}
	json.Unmarshal(w.Body.Bytes(), &own)
	if own.Key == "" || own.Scope != "printer" {
		t.Fatalf("printer key %s", w.Body.String())
	}
	for _, c := range []struct {
		key  string
		want int
	}{{ans.Key, 200}, {own.Key, 200}, {"", http.StatusUnauthorized}, {"wrong", http.StatusUnauthorized}} {
		r := httptest.NewRequest("GET", "/slicer/printer.json", nil)
		if c.key != "" {
			r.Header.Set("X-Api-Key", c.key)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("printer.json with %q: %d, want %d", c.key, w.Code, c.want)
		}
	}
}
