package remote

import (
	"image/png"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/touch"
)

func newTest(t *testing.T) (*Server, *httptest.Server, *http.Client, chan touch.Event) {
	t.Helper()
	wrongDelay = 0
	touches := make(chan touch.Event, 4)
	s := New()
	s.Password = func() string { return "stamp-ocean-cider-brick" }
	s.Touch = func(e touch.Event) { touches <- e }
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	return s, ts, &http.Client{Jar: jar}, touches
}

func login(t *testing.T, c *http.Client, base, password string) *http.Response {
	t.Helper()
	r, err := c.PostForm(base+"/login", url.Values{"password": {password}})
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	return r
}

func TestLoginProtectsTheScreen(t *testing.T) {
	_, ts, c, _ := newTest(t)
	for _, path := range []string{"/frame.png", "/events"} {
		r, _ := c.Get(ts.URL + path)
		r.Body.Close()
		if r.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s with no login: %d, want 401", path, r.StatusCode)
		}
	}
	r, _ := c.Get(ts.URL + "/screen")
	r.Body.Close()
	if r.Request.URL.Path != "/login" {
		t.Errorf("/screen with no login went to %s, want /login", r.Request.URL.Path)
	}
	if r := login(t, c, ts.URL, "wrong"); r.Request.URL.Path != "/login" {
		t.Errorf("a wrong password went to %s, want /login", r.Request.URL.Path)
	}
	// The login goes to the page of the deck (/), which the web server of the deck serves.
	if r := login(t, c, ts.URL, "stamp-ocean-cider-brick"); r.Request.URL.Path != "/" {
		t.Errorf("the right password went to %s, want /", r.Request.URL.Path)
	}
	r, _ = c.Get(ts.URL + "/screen")
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Errorf("/screen after the login: %d, want 200", r.StatusCode)
	}
}

func TestNoPasswordNoLogin(t *testing.T) {
	s, ts, c, _ := newTest(t)
	s.Password = func() string { return "" }
	login(t, c, ts.URL, "")
	r, _ := c.Get(ts.URL + "/frame.png")
	r.Body.Close()
	if r.StatusCode != http.StatusUnauthorized {
		t.Errorf("an empty password logged in: %d", r.StatusCode)
	}
}

func TestFramesChangeOnlyWithThePicture(t *testing.T) {
	s, ts, c, _ := newTest(t)
	login(t, c, ts.URL, "stamp-ocean-cider-brick")
	cv := gfx.New(4, 8)
	s.Publish(cv)
	s.Publish(cv) // the same picture
	if s.seq != 1 {
		t.Fatalf("seq is %d after the same picture twice, want 1", s.seq)
	}
	cv.Pix[0], cv.Pix[3] = 255, 255 // an opaque red pixel, like the pixels of the deck
	s.Publish(cv)
	if s.seq != 2 {
		t.Fatalf("seq is %d after a change, want 2", s.seq)
	}
	r, err := c.Get(ts.URL + "/frame.png")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	img, err := png.Decode(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	if red, _, _, _ := img.At(0, 0).RGBA(); red>>8 != 255 {
		t.Errorf("the frame is not the last picture")
	}
}

func TestEventsTellNewFrames(t *testing.T) {
	s, ts, c, _ := newTest(t)
	login(t, c, ts.URL, "stamp-ocean-cider-brick")
	s.Publish(gfx.New(4, 8))
	r, err := c.Get(ts.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	buf := make([]byte, 64)
	n, _ := r.Body.Read(buf)
	if got := string(buf[:n]); got != "data: 1\n\n" {
		t.Errorf("first event %q, want frame 1", got)
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		cv := gfx.New(4, 8)
		cv.Pix[0] = 1
		s.Publish(cv)
	}()
	n, _ = r.Body.Read(buf)
	if got := string(buf[:n]); got != "data: 2\n\n" {
		t.Errorf("second event %q, want frame 2", got)
	}
}

func TestTouchesReachTheDeck(t *testing.T) {
	_, ts, c, touches := newTest(t)
	login(t, c, ts.URL, "stamp-ocean-cider-brick")
	post := func(body string) int {
		r, err := c.Post(ts.URL+"/touch", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	if code := post(`{"kind":"down","x":0.25,"y":0.75}`); code != http.StatusNoContent {
		t.Fatalf("touch: %d", code)
	}
	if e := <-touches; e.Kind != touch.Down || e.X != 0.25 || e.Y != 0.75 {
		t.Errorf("got %+v", e)
	}
	for _, bad := range []string{`{"kind":"tap","x":0,"y":0}`, `{"kind":"down","x":2,"y":0}`, `nonsense`} {
		if code := post(bad); code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", bad, code)
		}
	}
}

func TestPagesLinkTheSource(t *testing.T) {
	s, ts, c, _ := newTest(t)
	s.Source = "https://github.com/beetlebugorg/printdeck/tree/v2.2.0"
	ts.Config.Handler = s.Handler() // the pages are made with the source link
	r, err := c.Get(ts.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b := new(strings.Builder)
	io.Copy(b, r.Body)
	if !strings.Contains(b.String(), `href="https://github.com/beetlebugorg/printdeck/tree/v2.2.0"`) {
		t.Error("the login page has no link to the source of this version")
	}
}
