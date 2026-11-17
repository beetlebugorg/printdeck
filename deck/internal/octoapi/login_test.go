package octoapi

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"testing"
)

// An upload with the login of the deck goes through only from a page of the deck.
func TestUploadWithLogin(t *testing.T) {
	s, ts, _ := newTest(t)
	s.Login = func(r *http.Request) bool {
		c, err := r.Cookie("printdeck")
		return err == nil && c.Value == "good"
	}
	host := ts.URL[len("http://"):]
	send := func(cookie, origin, site string) int {
		t.Helper()
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("file", "part.gcode")
		fw.Write([]byte("G28\n"))
		mw.Close()
		req, _ := http.NewRequest("POST", ts.URL+"/api/files/local", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: "printdeck", Value: cookie})
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if site != "" {
			req.Header.Set("Sec-Fetch-Site", site)
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	for _, c := range []struct {
		name, cookie, origin, site string
		want                       int
	}{
		{"the page of the deck", "good", "http://" + host, "same-origin", http.StatusCreated},
		{"an old browser with no Sec-Fetch-Site", "good", "http://" + host, "", http.StatusCreated},
		{"no login", "", "http://" + host, "same-origin", http.StatusForbidden},
		{"a bad login", "bad", "http://" + host, "same-origin", http.StatusForbidden},
		{"no Origin", "good", "", "", http.StatusForbidden},
		{"another site", "good", "https://evil.example", "cross-site", http.StatusForbidden},
		{"another site with the Origin of the deck", "good", "http://" + host, "same-site", http.StatusForbidden},
	} {
		if got := send(c.cookie, c.origin, c.site); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}

	// The login opens the upload only: the other requests still need an API key.
	req, _ := http.NewRequest("GET", ts.URL+"/api/files", nil)
	req.AddCookie(&http.Cookie{Name: "printdeck", Value: "good"})
	req.Header.Set("Origin", "http://"+host)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Errorf("GET /api/files with the login: %d", r.StatusCode)
	}
}
