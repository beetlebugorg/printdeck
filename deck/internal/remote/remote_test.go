package remote

import (
	"bufio"
	"context"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/touch"
)

// fakeUI is a UI session that sends a frame at the start, and records the touches.
type fakeUI struct {
	touches chan touch.Event
	ended   chan struct{}
}

func newTest(t *testing.T) (*Server, *httptest.Server, *http.Client, *fakeUI) {
	t.Helper()
	wrongDelay = 0
	ui := &fakeUI{touches: make(chan touch.Event, 4), ended: make(chan struct{}, 4)}
	s := New()
	s.Password = func() string { return "stamp-ocean-cider-brick" }
	s.NewSession = func(ctx context.Context, size image.Point, deck bool, frame func([]byte)) (image.Point, Input) {
		frame([]byte(`[["f",0,0,480,800,1184273]]`))
		go func() { <-ctx.Done(); ui.ended <- struct{}{} }()
		return image.Pt(480, max(800, size.Y)), Input{Touch: func(e touch.Event) { ui.touches <- e },
			Key: func(text, name string) {}}
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	return s, ts, &http.Client{Jar: jar}, ui
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

func TestLoginProtectsTheUI(t *testing.T) {
	_, ts, c, _ := newTest(t)
	for _, path := range []string{"/ui/events", "/ui/img/1"} {
		r, _ := c.Get(ts.URL + path)
		r.Body.Close()
		if r.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s with no login: %d, want 401", path, r.StatusCode)
		}
	}
	r, _ := c.Post(ts.URL+"/ui/touch?s=x", "application/json", strings.NewReader(`{"kind":"down","x":0.5,"y":0.5}`))
	r.Body.Close()
	if r.StatusCode != http.StatusUnauthorized {
		t.Errorf("a touch with no login: %d, want 401", r.StatusCode)
	}
	r, _ = c.Get(ts.URL + "/")
	r.Body.Close()
	if r.Request.URL.Path != "/login" {
		t.Errorf("/ with no login went to %s, want /login", r.Request.URL.Path)
	}
	if r := login(t, c, ts.URL, "wrong"); r.Request.URL.Path != "/login" {
		t.Errorf("a wrong password went to %s, want /login", r.Request.URL.Path)
	}
	if r := login(t, c, ts.URL, "stamp-ocean-cider-brick"); r.Request.URL.Path != "/" || r.StatusCode != http.StatusOK {
		t.Errorf("the right password went to %s (%d), want / (200)", r.Request.URL.Path, r.StatusCode)
	}
}

func TestNoPasswordNoLogin(t *testing.T) {
	s, ts, c, _ := newTest(t)
	s.Password = func() string { return "" }
	login(t, c, ts.URL, "")
	r, _ := c.Get(ts.URL + "/ui/events")
	r.Body.Close()
	if r.StatusCode != http.StatusUnauthorized {
		t.Errorf("an empty password logged in: %d", r.StatusCode)
	}
}

// A browser gets its session number and the frames of its session, and its touches reach the
// session. The session ends with the stream.
func TestSession(t *testing.T) {
	_, ts, c, ui := newTest(t)
	login(t, c, ts.URL, "stamp-ocean-cider-brick")
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/ui/events?h=1040", nil)
	r, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	lines := bufio.NewScanner(r.Body)
	next := func() string {
		for lines.Scan() {
			if l := lines.Text(); l != "" {
				return l
			}
		}
		return ""
	}
	if l := next(); l != "event: session" {
		t.Fatalf("first line %q", l)
	}
	id, size, _ := strings.Cut(strings.TrimPrefix(next(), "data: "), " ")
	if size != "480 1040" {
		t.Errorf("size %q, want 480 1040", size)
	}
	if l := next(); l != `data: [["f",0,0,480,800,1184273]]` {
		t.Errorf("frame %q", l)
	}

	tr, _ := c.Post(ts.URL+"/ui/touch?s="+id, "application/json", strings.NewReader(`{"kind":"down","x":0.25,"y":0.5}`))
	tr.Body.Close()
	if tr.StatusCode != http.StatusNoContent {
		t.Fatalf("touch: %d", tr.StatusCode)
	}
	select {
	case e := <-ui.touches:
		if e.Kind != touch.Down || e.X != 0.25 || e.Y != 0.5 {
			t.Errorf("touch %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("the touch did not reach the session")
	}
	for _, bad := range []string{`{"kind":"down","x":2,"y":0.5}`, `{"kind":"tap","x":0,"y":0}`} {
		br, _ := c.Post(ts.URL+"/ui/touch?s="+id, "application/json", strings.NewReader(bad))
		br.Body.Close()
		if br.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", bad, br.StatusCode)
		}
	}
	cancel()
	r.Body.Close()
	select {
	case <-ui.ended:
	case <-time.After(2 * time.Second):
		t.Fatal("the session did not end with the stream")
	}
	time.Sleep(50 * time.Millisecond) // the server removes the session after the stream ends
	tr, _ = c.Post(ts.URL+"/ui/touch?s="+id, "application/json", strings.NewReader(`{"kind":"up","x":0,"y":0}`))
	tr.Body.Close()
	if tr.StatusCode != http.StatusGone {
		t.Errorf("a touch for an ended session: %d, want 410", tr.StatusCode)
	}
}

// The images of the frames load by their number.
func TestImages(t *testing.T) {
	_, ts, c, _ := newTest(t)
	login(t, c, ts.URL, "stamp-ocean-cider-brick")
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	cv, rec := gfx.NewRecording(480, 800)
	cv.Cover(image.Rect(0, 0, 30, 20), img)
	op := rec.Ops[0].([]any)
	r, err := c.Get(ts.URL + "/ui/img/" + strconv.Itoa(op[5].(int)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	got, err := png.Decode(r.Body)
	if err != nil || got.Bounds().Dx() != 3 || got.Bounds().Dy() != 2 {
		t.Errorf("image %v, %v", got, err)
	}
}

func TestFontsNeedNoLogin(t *testing.T) {
	_, ts, c, _ := newTest(t)
	r, err := c.Get(ts.URL + "/ui/font/bold.ttf")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != http.StatusOK || len(b) != len(gfx.FontBold()) {
		t.Errorf("font: %d, %d bytes", r.StatusCode, len(b))
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

func TestAPIKeyOpensOnlyTheScreenData(t *testing.T) {
	s, ts, c, _ := newTest(t)
	s.APIKey = func(r *http.Request) bool { return r.Header.Get("X-Api-Key") == "key1" }
	get := func(path, key string) int {
		req, _ := http.NewRequest("GET", ts.URL+path, nil)
		if key != "" {
			req.Header.Set("X-Api-Key", key)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		r, err := c.Do(req.WithContext(ctx))
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	if code := get("/ui/events?w=800&h=480", "key1"); code != http.StatusOK {
		t.Errorf("events with the key: %d", code)
	}
	if code := get("/ui/events", "wrong"); code != http.StatusUnauthorized {
		t.Errorf("events with a wrong key: %d", code)
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if code := get("/", "key1"); code != http.StatusSeeOther {
		t.Errorf("the page with the key: %d, want the login", code)
	}
}

func TestLocalPath(t *testing.T) {
	for in, want := range map[string]string{"/roll/3": "/roll/3", "": "/", "//evil.example": "/",
		"https://evil.example": "/", "/\\evil": "/", "/system": "/system",
		"/\t/evil.example": "/", "/\n/evil.example": "/", "/a\\b": "/", "/": "/", "/roll/3?x=1": "/roll/3?x=1"} {
		if got := localPath(in); got != want {
			t.Errorf("localPath(%q) = %q, want %q", in, got, want)
		}
	}
}
