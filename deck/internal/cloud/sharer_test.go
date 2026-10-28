package cloud

import (
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeShares is the share API of the service and the storage of the uploads.
type fakeShares struct {
	mu      sync.Mutex
	srv     *httptest.Server
	created int
	ended   int
	files   map[string]int // the uploads of each file name
}

func newFakeShares(t *testing.T) *fakeShares {
	f := &fakeShares{files: map[string]int{}}
	grant := func() Grant {
		return Grant{URL: f.srv.URL + "/upload", Prefix: "s/s1/", Expires: time.Now().Add(time.Hour)}
	}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/shares":
			f.created++
			json.NewEncoder(w).Encode(ShareReply{ID: "s1", URL: "https://example.org/s/s1", OwnerKey: "owner1", Grant: grant()})
		case "/api/v1/shares/s1/grant":
			json.NewEncoder(w).Encode(grant())
		case "/api/v1/shares/s1/end":
			f.ended++
			json.NewEncoder(w).Encode(EndReply{Expires: time.Now().Add(24 * time.Hour)})
		case "/upload":
			r.ParseMultipartForm(1 << 20)
			f.files[r.FormValue("key")]++
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeShares) count(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.files["s/s1/"+name]
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for range 300 {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no %s", what)
}

// A share continues in the next deck program: the same share, with the count of its time lapse
// frames.
func TestShareHandoff(t *testing.T) {
	svc := newFakeShares(t)
	_, key, _ := ed25519.GenerateKey(nil)
	path := filepath.Join(t.TempDir(), "cloud.json")
	if err := (&Identity{Domain: svc.srv.URL, Deck: "d-1", Key: key.Seed()}).Save(path); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	state := "printing"
	status := func() ShareStatus {
		mu.Lock()
		defer mu.Unlock()
		return ShareStatus{State: state}
	}
	old := &Sharer{Link: &Link{Path: path}, Status: status, Every: 20 * time.Millisecond}
	old.Start("clip", "a Printdeck", [][]byte{[]byte("frame0")})
	waitFor(t, "first status", func() bool { return svc.count("status.json") > 0 && svc.count("lapse/0.jpg") == 1 })
	old.AddLapseFrame([]byte("frame1"))
	waitFor(t, "frame 1", func() bool { return svc.count("lapse/1.jpg") == 1 })

	h, ok := old.Handoff()
	if !ok || h.ID != "s1" || h.Owner != "owner1" || h.LapseN != 2 || h.Ended {
		t.Fatalf("Handoff = %+v, %v", h, ok)
	}
	// The old sharer uploads no more. A request that it sent before the handoff can still be in
	// the fake service, so the count starts after a moment.
	time.Sleep(40 * time.Millisecond)
	n := svc.count("status.json")
	time.Sleep(80 * time.Millisecond)
	if svc.count("status.json") != n {
		t.Error("the old sharer uploads after the handoff")
	}

	b, _ := json.Marshal(h)
	var back ShareHandoff
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	next := &Sharer{Link: &Link{Path: path}, Status: status, Every: 20 * time.Millisecond}
	if !next.Resume(back) {
		t.Fatal("Resume failed")
	}
	if st := next.State(); !st.Active || st.URL != "https://example.org/s/s1" || st.OwnerURL == "" {
		t.Errorf("state after Resume: %+v", st)
	}
	waitFor(t, "status of the next sharer", func() bool { return svc.count("status.json") > n })
	next.AddLapseFrame([]byte("frame2"))
	waitFor(t, "frame 2", func() bool { return svc.count("lapse/2.jpg") == 1 })
	if svc.count("lapse/0.jpg") != 1 || svc.count("lapse/1.jpg") != 1 {
		t.Error("a frame went up two times")
	}
	mu.Lock()
	state = "done"
	mu.Unlock()
	waitFor(t, "end", func() bool { return next.State().Ended })
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if svc.created != 1 || svc.ended != 1 {
		t.Errorf("%d shares made, %d ended; want 1 and 1", svc.created, svc.ended)
	}
}

// With no share, or before the service made the link, there is no handoff.
func TestShareHandoffNone(t *testing.T) {
	s := &Sharer{Link: &Link{Path: filepath.Join(t.TempDir(), "cloud.json")}}
	if _, ok := s.Handoff(); ok {
		t.Error("a handoff with no share")
	}
	if s.Resume(ShareHandoff{ID: "s1"}) {
		t.Error("Resume with no identity")
	}
}
