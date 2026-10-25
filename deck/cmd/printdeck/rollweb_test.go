package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/internal/remote"
	"printdeck/internal/ui"
)

type demoBackend struct{ *ui.DemoPrinter }

func (demoBackend) Run(context.Context, func()) {}

// The page of a roll asks to load it, and Load opens the filament swap on the deck. The label
// sheet has a QR code for each roll, from the place "from".
func TestRollPages(t *testing.T) {
	rolls := &printer.Spools{}
	navy := rolls.Add("Navy", "PLA", 1180, 180, 22, time.Now())
	rolls.SetColor(navy.ID, "#1f2a5a")
	rolls.Add("White", "PLA", 1180, 180, 22, time.Now())
	pr := demoBackend{&ui.DemoPrinter{View: -1, Spools_: rolls,
		State_: printer.State{Reachable: true, Connected: true, Text: "Operational"}}}
	app := ui.New()
	app.Printer = pr
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go app.Run(ctx, nil, func(*gfx.Canvas) {})
	rs := remote.New()
	rs.NoLogin = true
	mux := http.NewServeMux()
	registerRollPages(mux, rs, pr, app)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	get := func(path string) string {
		r, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		if dir := os.Getenv("OUT"); dir != "" {
			os.WriteFile(filepath.Join(dir, strings.NewReplacer("/", "_", "?", "_").Replace(path)+".html"), b, 0o644)
		}
		return string(b)
	}
	if p := get("/roll/1"); !strings.Contains(p, "Load the Navy?") || !strings.Contains(p, "#1f2a5a") || !strings.Contains(p, "ROLL #1") {
		t.Errorf("roll page:\n%s", p)
	}
	r, err := http.PostForm(srv.URL+"/roll/1/load", url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if !strings.Contains(string(b), "heats the nozzle for PLA") {
		t.Errorf("load:\n%s", b)
	}
	// The roll goes in the printer only at the end of the filament change on the deck.
	if in, _ := rolls.In(); in.ID != "2" {
		t.Errorf("roll in the printer %q, want 2", in.ID)
	}
	// A second Load while the swap is open waits for it.
	r, _ = http.PostForm(srv.URL+"/roll/2/load", url.Values{})
	b, _ = io.ReadAll(r.Body)
	r.Body.Close()
	if !strings.Contains(string(b), "filament change open") {
		t.Errorf("second load:\n%s", b)
	}
	p := get("/rolls/labels?from=5")
	if n := strings.Count(p, "<svg"); n != 2 {
		t.Errorf("labels: %d QR codes", n)
	}
	// The first label is at place 5: the middle of the second row.
	if !strings.Contains(p, "left: 3.25in; top: 3.208") {
		t.Errorf("place 5 is not at its place")
	}
}
