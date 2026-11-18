package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	registerSlicer(mux, rs, dir)
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
	registerSlicer(mux, remote.New(), filepath.Join(t.TempDir(), "none"))
	ts := httptest.NewServer(mux)
	defer ts.Close()
	r, _ := http.Get(ts.URL + "/slicer/")
	if r.StatusCode != http.StatusNotFound {
		t.Errorf("%d", r.StatusCode)
	}
}
