package octoapi

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The API of another printer of a deck is under /printers/<id>/ (http.StripPrefix). Its links keep
// that prefix, so a client that starts a print through the link of the upload starts it on that
// printer, and the webcam of its settings is the camera of that printer.
func TestPrinterPrefixLinks(t *testing.T) {
	s, _, _ := newTest(t)
	mux := http.NewServeMux()
	s.Register(mux)
	top := http.NewServeMux()
	top.Handle("/printers/x2d/", http.StripPrefix("/printers/x2d", mux))
	top.Handle("/", mux)
	ts := httptest.NewServer(top)
	defer ts.Close()
	for _, c := range []struct{ base, want string }{{"/printers/x2d", "/printers/x2d"}, {"", ""}} {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		fw, _ := mw.CreateFormFile("file", "cube.gcode")
		fw.Write([]byte("G28\n"))
		mw.Close()
		req, _ := http.NewRequest("POST", ts.URL+c.base+"/api/files/local", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("X-Api-Key", mainKey)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if loc := r.Header.Get("Location"); loc != ts.URL+c.want+"/api/files/local/cube.gcode" {
			t.Errorf("%q: Location %q", c.base, loc)
		}
		_, m := do(t, "GET", ts.URL+c.base+"/api/files", mainKey, nil, "")
		files, _ := m["files"].([]any)
		if len(files) == 0 {
			t.Fatalf("%q: no files %v", c.base, m)
		}
		refs := files[0].(map[string]any)["refs"].(map[string]any)
		if !strings.HasPrefix(refs["resource"].(string), ts.URL+c.want+"/api/") {
			t.Errorf("%q: resource %v", c.base, refs["resource"])
		}
	}
}
