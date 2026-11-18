package remote

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := CORS(inner, "https://zslice.beetlebug.org")
	do := func(method, path, origin string, hdr map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	// The listed origin on the API: it can read the answer, with no credentials.
	w := do("GET", "/api/version", "https://zslice.beetlebug.org", nil)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://zslice.beetlebug.org" {
		t.Fatalf("allow origin = %q", got)
	}
	if w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("credentials must not be allowed")
	}

	// A preflight with Private Network Access.
	w = do("OPTIONS", "/api/files/local", "https://zslice.beetlebug.org", map[string]string{
		"Access-Control-Request-Method":          "POST",
		"Access-Control-Request-Private-Network": "true",
	})
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Private-Network") != "true" ||
		w.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Fatalf("preflight: %d %v", w.Code, w.Header())
	}

	// Another origin, and a page of the remote screen: no CORS headers.
	for _, c := range []struct{ path, origin string }{
		{"/api/version", "https://evil.example"},
		{"/", "https://zslice.beetlebug.org"},
		{"/system", "https://zslice.beetlebug.org"},
	} {
		w = do("GET", c.path, c.origin, nil)
		if w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("%s from %s: CORS headers", c.path, c.origin)
		}
	}

	// A post from a page of another origin: 403, before the API. The listed origin, the deck's own
	// pages, and a slicer with no Origin header get through.
	for _, c := range []struct {
		method, path, origin string
		want                 int
	}{
		{"POST", "/plugin/appkeys/request", "https://evil.example", http.StatusForbidden},
		{"DELETE", "/api/files/local/a.gcode", "http://192.168.1.9", http.StatusForbidden},
		{"POST", "/plugin/appkeys/request", "https://zslice.beetlebug.org", http.StatusOK},
		{"POST", "/api/files/local", "http://example.com", http.StatusOK}, // the Host of httptest
		{"POST", "/plugin/appkeys/request", "", http.StatusOK},
		{"GET", "/api/version", "https://evil.example", http.StatusOK},
		{"POST", "/login", "https://evil.example", http.StatusOK}, // not the API: the login checks it
	} {
		if w := do(c.method, c.path, c.origin, nil); w.Code != c.want {
			t.Errorf("%s %s from %q: %d, want %d", c.method, c.path, c.origin, w.Code, c.want)
		}
	}

	// "none" turns it off.
	off := CORS(inner, "none")
	r := httptest.NewRequest("GET", "/api/version", nil)
	r.Header.Set("Origin", "https://zslice.beetlebug.org")
	w = httptest.NewRecorder()
	off.ServeHTTP(w, r)
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("none: CORS headers")
	}
	r = httptest.NewRequest("POST", "/plugin/appkeys/request", nil)
	r.Header.Set("Origin", "https://evil.example")
	w = httptest.NewRecorder()
	off.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("none: a post from another origin: %d", w.Code)
	}
}
