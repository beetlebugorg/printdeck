package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
