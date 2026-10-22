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
	h.add(State{}, now, nil, 0, 0, nil) // no print ended
	h.add(done, now, nil, 0, 0, nil)
	h.add(done, now.Add(time.Minute), nil, 0, 0, nil) // the same print, seen again
	r := h.Records()
	if len(r) != 1 || r[0].File != "x.gcode" || r[0].Result != "complete" || !r[0].End.Equal(now) {
		t.Fatalf("records %+v", r)
	}
	grams := func(mm float64) float64 { return FilamentGrams(mm, 2.85, "PLA") }
	if s := r[0].Summary(grams); s != "Done · 1:02 · 32 g" {
		t.Errorf("summary %q", s)
	}
	// A new history from the file has the print, and does not add it again.
	h2 := &History{Path: path}
	h2.add(done, now.Add(time.Hour), nil, 0, 0, nil)
	cancelled := done
	cancelled.Finished, cancelled.FinishedTime = "cancelled", 5*time.Minute
	h2.add(cancelled, now.Add(2*time.Hour), nil, 0, 0, nil)
	if r := h2.Records(); len(r) != 2 || r[0].Result != "cancelled" {
		t.Errorf("records %+v", r)
	}
	st := ComputeStats(h2.Records(), now.Add(3*time.Hour))
	if st.Prints != 2 || st.Completed != 1 || st.Cancelled != 1 || st.Time != 67*time.Minute ||
		st.Filament != 8200 || st.Longest != 62*time.Minute || st.Last7 != 2 || st.TopFile != "x.gcode" || st.TopCount != 2 {
		t.Errorf("stats %+v", st)
	}
}
