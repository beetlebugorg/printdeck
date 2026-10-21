package printer

import (
	"path/filepath"
	"testing"
	"time"
)

func TestHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	h := &History{Path: path}
	now := time.Date(2026, 10, 20, 16, 7, 0, 0, time.UTC)
	done := State{Finished: "complete", FinishedFile: "x.gcode", FinishedPath: "parts/x.gcode",
		FinishedTime: 62 * time.Minute, FinishedFilament: 4100}
	h.add(State{}, now) // no print ended
	h.add(done, now)
	h.add(done, now.Add(time.Minute)) // the same print, seen again
	r := h.Records()
	if len(r) != 1 || r[0].File != "x.gcode" || r[0].Result != "complete" || !r[0].End.Equal(now) {
		t.Fatalf("records %+v", r)
	}
	if s := r[0].Summary(); s != "Done · 1:02 · 4.1 m" {
		t.Errorf("summary %q", s)
	}
	// A new history from the file has the print, and does not add it again.
	h2 := &History{Path: path}
	h2.add(done, now.Add(time.Hour))
	cancelled := done
	cancelled.Finished, cancelled.FinishedTime = "cancelled", 5*time.Minute
	h2.add(cancelled, now.Add(2*time.Hour))
	if r := h2.Records(); len(r) != 2 || r[0].Result != "cancelled" {
		t.Errorf("records %+v", r)
	}
	if tot := h2.Totals(); tot.Prints != 2 || tot.Time != 67*time.Minute || tot.Filament != 8200 {
		t.Errorf("totals %+v", tot)
	}
}
