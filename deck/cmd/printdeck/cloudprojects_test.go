package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"printdeck/internal/cloud"
	"printdeck/internal/gcodestore"
	"printdeck/internal/printer"
)

// The deck sends each print of a project to the project at its end: after its video, or after
// autoSendAfter with no video. It sends a print once, with the ID of its share link.
func TestSendNewPrints(t *testing.T) {
	var mu sync.Mutex
	var prints []cloud.PrintRequest
	projects := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v1/projects":
			projects++
			json.NewEncoder(w).Encode(cloud.ProjectSummary{ID: "proj1"})
		case r.Method == "POST" && r.URL.Path == "/api/v1/projects/proj1/prints":
			var in cloud.PrintRequest
			json.Unmarshal(body, &in)
			prints = append(prints, in)
			json.NewEncoder(w).Encode(cloud.PrintReply{Print: "pr" + string(rune('0'+len(prints))),
				Grant: cloud.Grant{URL: srv.URL + "/upload", Fields: map[string]string{}, Expires: time.Now().Add(time.Hour)}})
		case r.URL.Path == "/upload":
			w.WriteHeader(204)
		case strings.HasSuffix(r.URL.Path, "/done"):
			w.Write([]byte("{}"))
		default:
			http.Error(w, "no such request", 404)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	id := cloud.Identity{Domain: srv.URL, Deck: "d1", Key: make([]byte, 32), Enrolled: time.Now()}
	b, _ := json.Marshal(id)
	os.WriteFile(filepath.Join(dir, "cloud.json"), b, 0o600)
	now := time.Now()
	recs := []printer.Record{
		{File: "c.gcode", Path: "c.gcode", Result: "complete", End: now.Add(-time.Minute), Project: "Garden", Video: "c.mp4"},
		{File: "b.gcode", Path: "b.gcode", Result: "complete", End: now.Add(-2 * time.Minute), Project: "Garden"},
		{File: "d.gcode", Path: "d.gcode", Result: "complete", End: now.Add(-3 * time.Minute)},
		{File: "a.gcode", Path: "a.gcode", Result: "complete", End: now.Add(-20 * time.Minute), Project: "Garden"},
		{File: "e.gcode", Path: "e.gcode", Result: "complete", End: now.Add(-7 * time.Hour), Project: "Garden"},
	}
	b, _ = json.Marshal(recs)
	os.WriteFile(filepath.Join(dir, "history.json"), b, 0o600)
	k := printer.NewKlippy(filepath.Join(dir, "none.sock"), &gcodestore.Store{Dir: dir})
	k.Hist = &printer.History{Path: filepath.Join(dir, "history.json")}
	p := &projectLink{Link: &cloud.Link{Path: filepath.Join(dir, "cloud.json")}, k: k, stateDir: dir,
		shareOf: func(r printer.Record) string {
			if r.Path == "c.gcode" {
				return "sh1"
			}
			return ""
		}}
	names := func() []string {
		mu.Lock()
		defer mu.Unlock()
		var out []string
		for _, pr := range prints {
			out = append(out, pr.File+"/"+pr.Share)
		}
		return out
	}
	ctx := context.Background()
	p.sendNew(ctx, now)
	if got := strings.Join(names(), " "); got != "c.gcode/sh1 a.gcode/" || projects != 1 {
		t.Fatalf("the first sends: %q, %d projects", got, projects)
	}
	p.sendNew(ctx, now.Add(time.Minute))
	if got := len(names()); got != 2 {
		t.Fatalf("a print went twice: %v", names())
	}
	// b has no video: it goes after autoSendAfter.
	p.sendNew(ctx, now.Add(autoSendAfter))
	if got := strings.Join(names(), " "); got != "c.gcode/sh1 a.gcode/ b.gcode/" {
		t.Fatalf("after the wait: %q", got)
	}
	// The file keeps the prints that are up, for the next deck program.
	var f cloudProjectsFile
	b, _ = os.ReadFile(filepath.Join(dir, "cloud-projects.json"))
	json.Unmarshal(b, &f)
	if cp := f["Garden"]; cp == nil || cp.ID != "proj1" || len(cp.Prints) != 3 {
		t.Fatalf("cloud-projects.json %s", b)
	}
}
