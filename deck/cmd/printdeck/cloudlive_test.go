package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"printdeck/internal/cloud"
	"printdeck/internal/gcodestore"
	"printdeck/internal/printer"
)

// The deck sends the progress of a print of a project each liveEvery and at a pause, and its
// result at the end.
func TestSendLive(t *testing.T) {
	var mu sync.Mutex
	var sends []string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v1/projects":
			json.NewEncoder(w).Encode(cloud.ProjectSummary{ID: "proj1"})
		case r.Method == "POST" && r.URL.Path == "/api/v1/projects/proj1/live":
			var in cloud.LiveRequest
			json.Unmarshal(body, &in)
			s := fmt.Sprintf("%s %s %g", in.Key, in.Name, in.Percent)
			if in.Paused {
				s += " paused"
			}
			if in.Result != "" {
				s += " " + in.Result
			}
			sends = append(sends, s)
			json.NewEncoder(w).Encode(cloud.LiveReply{})
		case r.Method == "DELETE" && r.URL.Path == "/api/v1/projects/proj1/live":
			sends = append(sends, "end")
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
	now := time.Now().Truncate(time.Second)
	recs := []printer.Record{{File: "leg.gcode", Path: "leg.gcode", Result: "complete", End: now.Add(2 * time.Hour), Project: "Dog"}}
	b, _ = json.Marshal(recs)
	os.WriteFile(filepath.Join(dir, "history.json"), b, 0o600)
	k := printer.NewKlippy(filepath.Join(dir, "none.sock"), &gcodestore.Store{Dir: dir})
	k.Hist = &printer.History{Path: filepath.Join(dir, "history.json")}
	p := &projectLink{Link: &cloud.Link{Path: filepath.Join(dir, "cloud.json")}, k: k, stateDir: dir}
	got := func() string {
		mu.Lock()
		defer mu.Unlock()
		out := strings.Join(sends, ", ")
		sends = nil
		return out
	}
	ctx := context.Background()
	st := printer.State{Printing: true, File: "leg.gcode", FilePath: "leg.gcode", Elapsed: time.Hour, Completion: 40}
	l := p.liveStep(ctx, nil, st, "Dog", now)
	key := "leg.gcode@" + strconv.FormatInt(now.Add(-time.Hour).Unix(), 10)
	if s := got(); s != key+" Leg 40" {
		t.Fatalf("the first send: %q", s)
	}
	st.Completion, st.Elapsed = 41, time.Hour+30*time.Second
	l = p.liveStep(ctx, l, st, "Dog", now.Add(30*time.Second))
	if s := got(); s != "" {
		t.Fatalf("a send before liveEvery: %q", s)
	}
	st.Paused = true
	l = p.liveStep(ctx, l, st, "Dog", now.Add(40*time.Second))
	if s := got(); s != key+" Leg 41 paused" {
		t.Fatalf("the send at the pause: %q", s)
	}
	st.Paused, st.Completion = false, 50
	l = p.liveStep(ctx, l, st, "Dog", now.Add(2*time.Minute))
	if s := got(); s != key+" Leg 50" {
		t.Fatalf("the send after liveEvery: %q", s)
	}
	// The end: the result goes to the project.
	l = p.liveStep(ctx, l, printer.State{}, "", now.Add(3*time.Minute))
	if s := got(); l != nil || s != key+" Leg 100 complete" {
		t.Fatalf("the send at the end: %q %v", s, l)
	}
	// A print that leaves the project.
	l = p.liveStep(ctx, nil, st, "Dog", now.Add(4*time.Minute))
	got()
	l = p.liveStep(ctx, l, st, "", now.Add(5*time.Minute))
	if s := got(); l != nil || s != "end" {
		t.Fatalf("the send when the print left the project: %q %v", s, l)
	}
}
