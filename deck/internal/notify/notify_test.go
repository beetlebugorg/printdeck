package notify

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"printdeck/internal/printer"
)

func titles(ms []Message) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.Title)
	}
	return out
}

// A print: the first layer, a short roll once, a pause, and the end.
func TestWatcher(t *testing.T) {
	var w Watcher
	idle := printer.State{Reachable: true, Connected: true}
	if m := w.Step(idle, Roll{}); m != nil {
		t.Fatalf("first step: %v", titles(m))
	}
	st := printer.State{Reachable: true, Connected: true, Printing: true, File: "a.gcode", Layer: 1, Layers: 10}
	short := Roll{Name: "#3 Navy", Left: 10, Need: 30, Known: true}
	steps := []struct {
		change func()
		roll   Roll
		want   []string
	}{
		{func() {}, Roll{}, nil},
		{func() { st.Layer = 2 }, Roll{}, []string{"First layer done"}},
		{func() { st.Layer = 3 }, short, []string{"The roll runs short"}},
		{func() { st.Layer = 4 }, short, nil},
		{func() { st.Paused, st.PausePrompt = true, "Put the panel in." }, short, []string{"Print paused"}},
		{func() { st.Paused, st.PausePrompt = false, "" }, short, nil},
		{func() {
			st = printer.State{Reachable: true, Connected: true, Finished: "complete", FinishedFile: "a.gcode", FinishedTime: 102 * time.Minute}
		}, Roll{}, []string{"Print done"}},
		{func() {}, Roll{}, nil},
	}
	for i, s := range steps {
		s.change()
		got := titles(w.Step(st, s.roll))
		if len(got) != len(s.want) || (len(got) > 0 && got[0] != s.want[0]) {
			t.Errorf("step %d: %v, want %v", i, got, s.want)
		}
	}
}

// The deck can start during a print: the state then is no event.
func TestWatcherStartsInAPrint(t *testing.T) {
	var w Watcher
	st := printer.State{Reachable: true, Connected: true, Printing: true, File: "a.gcode", Layer: 5, Layers: 10}
	if m := w.Step(st, Roll{}); m != nil {
		t.Fatalf("%v", titles(m))
	}
	st.Layer = 6
	if m := w.Step(st, Roll{}); m != nil {
		t.Errorf("first layer again: %v", titles(m))
	}
}

func TestSend(t *testing.T) {
	var title, body, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		title, path = r.Header.Get("Title"), r.URL.Path
		b, _ := io.ReadAll(r.Body)
		body = string(b)
	}))
	defer srv.Close()
	if err := Send(context.Background(), srv.URL+"/deck", Message{Title: "Print done", Body: "a.gcode · 1h 42m"}); err != nil {
		t.Fatal(err)
	}
	if title != "Print done" || body != "a.gcode · 1h 42m" || path != "/deck" {
		t.Errorf("%q %q %q", title, body, path)
	}
	if URL("printdeck-7f3a") != "https://ntfy.sh/printdeck-7f3a" || URL("https://ntfy.example/x") != "https://ntfy.example/x" {
		t.Errorf("URL")
	}
}
