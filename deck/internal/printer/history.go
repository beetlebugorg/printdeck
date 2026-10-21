package printer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Record is one finished print of the history.
type Record struct {
	File     string        `json:"file"`
	Path     string        `json:"path"`
	Result   string        `json:"result"` // "complete", "cancelled" or "error"
	Duration time.Duration `json:"duration"`
	Filament float64       `json:"filament"` // mm
	End      time.Time     `json:"end"`
	// The timings of the print, for better estimates: the steps of the start macro, the seconds
	// that the bed took to cool to ReleaseBed after the print, and the seconds of the moves in
	// the plan of the file.
	Phases  []Phase `json:"phases,omitempty"`
	CoolBed float64 `json:"cool_bed,omitempty"`
	Planned float64 `json:"planned,omitempty"`
}

// Summary is a short line for the screen, for example "Done · 1:42 · 12 g". grams gives the
// weight of a length of filament in mm.
func (r Record) Summary(grams func(mm float64) float64) string {
	res := map[string]string{"complete": "Done", "cancelled": "Cancelled", "error": "Failed"}[r.Result]
	m := int(r.Duration.Minutes())
	s := fmt.Sprintf("%s · %d:%02d", res, m/60, m%60)
	if r.Filament > 0 {
		s += " · " + Weight(grams(r.Filament))
	}
	return s
}

// maxHistory is the number of prints that the history keeps.
const maxHistory = 100

// History is the list of the finished prints, newest first, in a JSON file. Klipper keeps only the
// last print, so the deck records each one when it ends.
type History struct {
	Path string // the file; "" keeps the history in memory only

	mu      sync.Mutex
	records []Record
	loaded  bool
	last    string // the key of the last added print
	rates   *Rates // the timings from the records; nil after a change
}

func (h *History) load() {
	if h.loaded {
		return
	}
	h.loaded = true
	if h.Path == "" {
		return
	}
	if b, err := os.ReadFile(h.Path); err == nil {
		json.Unmarshal(b, &h.records)
	}
	if len(h.records) > 0 {
		h.last = key(h.records[0])
	}
}

func key(r Record) string {
	return fmt.Sprintf("%s|%s|%d", r.Path, r.Result, int(r.Duration.Seconds()))
}

// Records returns the history, newest first.
func (h *History) Records() []Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.load()
	return append([]Record(nil), h.records...)
}

// add records the end of a print once: the state of Klipper shows the last print until the next
// one, also after a new connection. phases and planned are the timings of the print. It returns
// the key of a new record, or "".
func (h *History) add(st State, now time.Time, phases []Phase, planned float64) string {
	if st.Finished == "" {
		return ""
	}
	r := Record{File: st.FinishedFile, Path: st.FinishedPath, Result: st.Finished, Duration: st.FinishedTime,
		Filament: st.FinishedFilament, End: now, Phases: phases, Planned: planned}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.load()
	if key(r) == h.last {
		return ""
	}
	h.last = key(r)
	h.records = append([]Record{r}, h.records...)
	if len(h.records) > maxHistory {
		h.records = h.records[:maxHistory]
	}
	h.rates = nil
	h.save()
	return h.last
}

// setCool sets the cooling time of the bed of the record with the key.
func (h *History) setCool(k string, seconds float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.records {
		if key(h.records[i]) == k {
			h.records[i].CoolBed = seconds
			h.rates = nil
			h.save()
			return
		}
	}
}

// Rates returns the timings of this printer from the history.
func (h *History) Rates() Rates {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.load()
	if h.rates == nil {
		r := ComputeRates(h.records)
		h.rates = &r
	}
	return *h.rates
}

// save writes the history to its file. The caller holds the lock.
func (h *History) save() {
	if h.Path == "" {
		return
	}
	b, _ := json.Marshal(h.records)
	os.MkdirAll(filepath.Dir(h.Path), 0o700)
	tmp := h.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err == nil {
		os.Rename(tmp, h.Path)
	}
}

// Stats are the statistics of the history.
type Stats struct {
	Prints, Completed, Cancelled, Failed int
	Time, Average, Longest               time.Duration
	Filament                             float64 // mm
	Last7, Last30                        int     // the prints of the last 7 and 30 days
	TopFile                              string  // the file that was printed most often
	TopCount                             int
	Since                                time.Time // the end of the oldest print of the history
}

// ComputeStats adds up the history at the time now.
func ComputeStats(recs []Record, now time.Time) Stats {
	var s Stats
	count := map[string]int{}
	for _, r := range recs {
		s.Prints++
		switch r.Result {
		case "complete":
			s.Completed++
		case "cancelled":
			s.Cancelled++
		default:
			s.Failed++
		}
		s.Time += r.Duration
		s.Filament += r.Filament
		s.Longest = max(s.Longest, r.Duration)
		if age := now.Sub(r.End); age < 7*24*time.Hour {
			s.Last7++
			s.Last30++
		} else if age < 30*24*time.Hour {
			s.Last30++
		}
		count[r.File]++
		if c := count[r.File]; c > s.TopCount || (c == s.TopCount && r.File < s.TopFile) {
			s.TopFile, s.TopCount = r.File, c
		}
		if s.Since.IsZero() || r.End.Before(s.Since) {
			s.Since = r.End
		}
	}
	if s.Prints > 0 {
		s.Average = s.Time / time.Duration(s.Prints)
	}
	return s
}
