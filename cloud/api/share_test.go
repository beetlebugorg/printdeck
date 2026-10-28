package api

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"printdeck/internal/cloud"
)

// devService returns a local service with the pages, and its URL.
func devService(t *testing.T) (*DevServer, string) {
	st := NewMemStore()
	ctx := context.Background()
	st.Put(ctx, SharePage, []byte("<!doctype html><title>Share</title>"), PutOptions{ContentType: "text/html"})
	st.Put(ctx, "404.html", []byte("<!doctype html><title>Not found</title>"), PutOptions{ContentType: "text/html"})
	g := &LocalGranter{}
	d := &DevServer{API: &Server{Store: st, Grants: g}, Store: st, Grants: g}
	srv := httptest.NewServer(d)
	t.Cleanup(srv.Close)
	g.URL, d.API.Base = srv.URL+"/upload", srv.URL
	return d, srv.URL
}

// connectedLink enrolls a deck at the service and returns its link.
func connectedLink(t *testing.T, d *DevServer, url string) *cloud.Link {
	code, err := NewCode(context.Background(), d.Store, "deck", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	l := &cloud.Link{Path: filepath.Join(t.TempDir(), "cloud.json"), Name: "Workhorse"}
	l.Connect(url, code)
	waitFor(t, func() bool { return l.State().Connected })
	return l
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out")
}

func get(t *testing.T, url string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// A share goes up with its status, its camera frame and its time lapse frames. At the end of the
// print the service gets the end, and the sweep deletes the share 24 hours later.
func TestShareFlow(t *testing.T) {
	d, url := devService(t)
	l := connectedLink(t, d, url)
	var mu sync.Mutex
	state := "printing"
	seq := uint64(1)
	sh := &cloud.Sharer{
		Link:  l,
		Every: 20 * time.Millisecond,
		Views: []string{"main"},
		Status: func() cloud.ShareStatus {
			mu.Lock()
			defer mu.Unlock()
			return cloud.ShareStatus{State: state, Job: "Hex planter", Progress: 0.5}
		},
		Frame: func(string) ([]byte, uint64) {
			mu.Lock()
			defer mu.Unlock()
			return []byte("jpeg"), seq
		},
		LapseFPS: 12,
	}
	sh.Start("Hex planter", "Workhorse", [][]byte{[]byte("l0"), []byte("l1")})
	waitFor(t, func() bool { return sh.State().URL != "" })
	shareURL := sh.State().URL
	if code, _ := get(t, shareURL); code != 200 {
		t.Fatalf("page: %d", code)
	}
	sh.AddLapseFrame([]byte("l2"))
	var st cloud.ShareStatus
	waitFor(t, func() bool {
		code, b := get(t, shareURL+"/status.json")
		return code == 200 && json.Unmarshal(b, &st) == nil && st.Lapse == 3
	})
	if st.Job != "Hex planter" || st.Frame != 1 || st.LapseFPS != 12 || len(st.Views) != 1 {
		t.Fatalf("status %+v", st)
	}
	if code, b := get(t, shareURL+"/cam-main.jpg"); code != 200 || string(b) != "jpeg" {
		t.Fatalf("frame %d %q", code, b)
	}
	if code, b := get(t, shareURL+"/lapse/2.jpg"); code != 200 || string(b) != "l2" {
		t.Fatalf("lapse %d %q", code, b)
	}
	// The private record is not public.
	id := shareURL[len(url)+3:]
	if code, _ := get(t, url+"/shares/"+id+".json"); code != 404 {
		t.Fatalf("record: %d", code)
	}
	mu.Lock()
	state = "done"
	mu.Unlock()
	waitFor(t, func() bool { return sh.State().Ended })
	if exp := sh.State().Expires; time.Until(exp) < 23*time.Hour || time.Until(exp) > 25*time.Hour {
		t.Fatalf("expires %v", exp)
	}
	ctx := context.Background()
	d.API.Sweep(ctx)
	if code, _ := get(t, shareURL); code != 200 {
		t.Fatal("the sweep deleted a share before its time")
	}
	d.API.Now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	if err := d.API.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"", "/status.json", "/lapse/0.jpg"} {
		if code, _ := get(t, shareURL+p); code != 404 {
			t.Errorf("after the sweep %q: %d", p, code)
		}
	}
	if keys, _ := d.Store.List(ctx, "shares/"); len(keys) != 0 {
		t.Errorf("records left: %v", keys)
	}
}

// Stop sharing deletes the share at once. A grant does not allow keys of another share.
func TestShareStopAndGrant(t *testing.T) {
	d, url := devService(t)
	l := connectedLink(t, d, url)
	sh := &cloud.Sharer{Link: l, Every: 20 * time.Millisecond, Status: func() cloud.ShareStatus { return cloud.ShareStatus{State: "printing"} }}
	sh.Start("Tray", "Workhorse", nil)
	waitFor(t, func() bool { return sh.State().URL != "" })
	u := sh.State().URL
	waitFor(t, func() bool { code, _ := get(t, u+"/status.json"); return code == 200 })
	g, _ := d.Grants.Grant(context.Background(), "s/aaaaaaaaaaaaaaaaaaaaa/", 100, time.Hour)
	g.Prefix = "s/bbbbbbbbbbbbbbbbbbbbb/"
	err := cloud.PostFile(context.Background(), nil, g, "status.json", "application/json", "", []byte("{}"))
	if ue, ok := err.(*cloud.UploadError); !ok || ue.Status != 403 {
		t.Fatalf("other prefix: %v", err)
	}
	sh.Stop()
	waitFor(t, func() bool { code, _ := get(t, u); return code == 404 })
}

// exifJPEG is a JPEG of w x h pixels with an EXIF block: the orientation o and a GPS tag.
func exifJPEG(t *testing.T, w, h, o int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	// TIFF, little endian: IFD0 with Orientation (0x0112) and GPSInfo (0x8825).
	tiff := []byte{'I', 'I', 42, 0, 8, 0, 0, 0, 2, 0,
		0x12, 0x01, 3, 0, 1, 0, 0, 0, byte(o), 0, 0, 0,
		0x25, 0x88, 4, 0, 1, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0}
	app1 := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xff, 0xe1, byte((len(app1) + 2) >> 8), byte(len(app1) + 2)}
	b := buf.Bytes()
	out := append([]byte{0xff, 0xd8}, seg...)
	out = append(out, app1...)
	return append(out, b[2:]...)
}

// The owner key sets Ready and adds a photo. The photo is upright from its EXIF orientation and
// has no EXIF block. A wrong key gets 401. Ready moves the delete to 24 hours after Ready.
func TestShareReadyAndPhotos(t *testing.T) {
	d, url := devService(t)
	l := connectedLink(t, d, url)
	sh := &cloud.Sharer{Link: l, Every: 20 * time.Millisecond, Status: func() cloud.ShareStatus { return cloud.ShareStatus{State: "done"} }}
	sh.Start("Tray", "Workhorse", nil)
	waitFor(t, func() bool { return sh.State().Ended })
	st := sh.State()
	if !strings.Contains(st.OwnerURL, "#owner=") {
		t.Fatalf("owner URL %q", st.OwnerURL)
	}
	id := strings.TrimPrefix(st.URL, url+"/s/")
	key := st.OwnerURL[strings.Index(st.OwnerURL, "#owner=")+7:]
	post := func(path, k string, body any) (int, []byte) {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", url+"/api/v1/shares/"+id+path, bytes.NewReader(b))
		req.Header.Set(cloud.HeaderShareKey, k)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, out
	}
	if code, _ := post("/ready", "wrong", cloud.ReadyRequest{Ready: true}); code != 401 {
		t.Fatalf("wrong key: %d", code)
	}
	if code, _ := post("/end", key, nil); code == 200 {
		t.Fatal("the owner key ends a share")
	}
	code, b := post("/photo", key, nil)
	var pr cloud.PhotoReply
	if code != 200 || json.Unmarshal(b, &pr) != nil {
		t.Fatalf("photo: %d %s", code, b)
	}
	if err := cloud.PostFile(context.Background(), nil, pr.Grant, "photo", "image/jpeg", "", exifJPEG(t, 40, 20, 6)); err != nil {
		t.Fatal(err)
	}
	if code, b := post("/photo/"+pr.Photo+"/done", key, nil); code != 200 {
		t.Fatalf("done: %d %s", code, b)
	}
	code, b = post("/ready", key, cloud.ReadyRequest{Ready: true, Note: "On the porch"})
	var rd cloud.ShareReady
	if code != 200 || json.Unmarshal(b, &rd) != nil || !rd.Ready || rd.Note != "On the porch" || len(rd.Photos) != 1 {
		t.Fatalf("ready: %d %s", code, b)
	}
	if p := rd.Photos[0]; p.W != 20 || p.H != 40 {
		t.Errorf("the photo is %dx%d, want 20x40 (upright)", p.W, p.H)
	}
	code, photo := get(t, url+"/s/"+id+"/"+rd.Photos[0].File)
	if code != 200 || bytes.Contains(photo, []byte("Exif")) {
		t.Errorf("photo: %d, EXIF %v", code, bytes.Contains(photo, []byte("Exif")))
	}
	if code, b := get(t, url+"/s/"+id+"/ready.json"); code != 200 || !bytes.Contains(b, []byte(`"ready":true`)) {
		t.Errorf("ready.json: %d %s", code, b)
	}
	if time.Until(rd.Expires) < 23*time.Hour {
		t.Errorf("expires %v", rd.Expires)
	}
	// The deck sets Ready too, with its signature.
	sh.SetReady(false, "")
	waitFor(t, func() bool { return !sh.State().Ready })
}

// The page of a share has the name of the print and the preview tags, with the name escaped.
func TestSharePageFor(t *testing.T) {
	page := []byte("<head>\n<title>Print · Printdeck</title>\n<!-- preview -->\n</head>")
	got := string(SharePageFor(page, "https://example.org/s/abc", Share{ID: "abc", Job: `Hex "planter" <v2>`}))
	for _, want := range []string{
		`<title>Hex &#34;planter&#34; &lt;v2&gt; · Printdeck</title>`,
		`<meta property="og:title" content="Hex &#34;planter&#34; &lt;v2&gt;">`,
		`<meta property="og:image" content="https://example.org/s/abc/model.jpg">`,
		`<meta property="og:url" content="https://example.org/s/abc">`,
		`<meta name="twitter:card" content="summary_large_image">`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %s in\n%s", want, got)
		}
	}
	if strings.Contains(got, previewMark) {
		t.Error("the mark stays")
	}
}
