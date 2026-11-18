package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
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
