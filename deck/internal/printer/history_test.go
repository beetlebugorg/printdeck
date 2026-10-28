package printer

import (
	"path/filepath"
	"strings"
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

// OfSpool finds the prints of a roll by its ID, and an old record by the name of the roll.
func TestOfSpool(t *testing.T) {
	black := Spool{ID: "s1", Name: "Black"}
	recs := []Record{
		{File: "a.gcode", Spool: "Black", SpoolID: "s1"},
		{File: "b.gcode", Spool: "White", SpoolID: "s2"},
		{File: "c.gcode", Spool: "Black"},
		{File: "d.gcode", Spool: "Black", SpoolID: "s3"}, // a newer roll with the same name
		{File: "e.gcode"},
	}
	var got []string
	for _, r := range OfSpool(recs, black) {
		got = append(got, r.File)
	}
	if strings.Join(got, " ") != "a.gcode c.gcode" {
		t.Errorf("prints %v, want a.gcode c.gcode", got)
	}
}

// The video of a print goes to the record of the print. A video that comes before its record
// goes to the record when the deck adds it. A record from before the videos has none.
func TestHistoryVideo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	h := &History{Path: path}
	now := time.Date(2026, 10, 20, 16, 7, 0, 0, time.UTC)
	done := State{Finished: "complete", FinishedFile: "x.gcode", FinishedPath: "parts/x.gcode", FinishedTime: 62 * time.Minute}
	h.add(done, now, nil, 0, 0, nil)
	h.SetVideo("parts/x.gcode", "x-20261020-1607.mov", now.Add(5*time.Second))
	if r := h.Records(); r[0].Video != "x-20261020-1607.mov" {
		t.Fatalf("video %q", r[0].Video)
	}
	// The video stays in the file.
	if r := (&History{Path: path}).Records(); r[0].Video != "x-20261020-1607.mov" {
		t.Errorf("video in the file %q", r[0].Video)
	}

	// The next print of the file: the video comes first.
	next := done
	next.FinishedTime = 30 * time.Minute
	h.SetVideo("parts/x.gcode", "x-20261020-1800.mov", now.Add(2*time.Hour))
	h.add(next, now.Add(2*time.Hour+time.Second), nil, 0, 0, nil)
	r := h.Records()
	if len(r) != 2 || r[0].Video != "x-20261020-1800.mov" || r[1].Video != "x-20261020-1607.mov" {
		t.Fatalf("records %+v", r)
	}

	// An old record gets no video, and the next record does not get it later.
	h.SetVideo("parts/x.gcode", "x-late.mov", now.Add(5*time.Hour))
	other := done
	other.FinishedPath, other.FinishedFile = "y.gcode", "y.gcode"
	h.add(other, now.Add(5*time.Hour), nil, 0, 0, nil)
	for _, rec := range h.Records() {
		if rec.Video == "x-late.mov" {
			t.Errorf("record %s has the late video", rec.File)
		}
	}
	if r := h.Records(); r[0].Video != "" {
		t.Errorf("the record of another file has the video %q", r[0].Video)
	}
}

// The name of a print is the title in the file, or else the cleaned file name. A record from
// before the title has only the file.
func TestPrintNames(t *testing.T) {
	st := State{File: "TAZWH_hex_planter_v3.gcode", FinishedFile: "UMS3_bracket (1).gcode", FinishedTitle: "Wall bracket"}
	if st.Name() != "Hex planter" || st.FinishedName() != "Wall bracket" {
		t.Errorf("%q %q", st.Name(), st.FinishedName())
	}
	if r := (Record{File: "benchy.gcode"}); r.Name() != "Benchy" {
		t.Errorf("%q", r.Name())
	}
	if r := (Record{File: "benchy.gcode", Title: "Boat"}); r.Name() != "Boat" {
		t.Errorf("%q", r.Name())
	}
}
