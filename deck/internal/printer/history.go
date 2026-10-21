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
}

// summary is a short line for the screen, for example "Done · 1:42 · 4.1 m".
func (r Record) Summary() string {
	res := map[string]string{"complete": "Done", "cancelled": "Cancelled", "error": "Failed"}[r.Result]
	m := int(r.Duration.Minutes())
	s := fmt.Sprintf("%s · %d:%02d", res, m/60, m%60)
	if r.Filament > 0 {
		s += fmt.Sprintf(" · %.1f m", r.Filament/1000)
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
// one, also after a new connection.
func (h *History) add(st State, now time.Time) {
	if st.Finished == "" {
		return
	}
	r := Record{File: st.FinishedFile, Path: st.FinishedPath, Result: st.Finished, Duration: st.FinishedTime,
		Filament: st.FinishedFilament, End: now}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.load()
	if key(r) == h.last {
		return
	}
	h.last = key(r)
	h.records = append([]Record{r}, h.records...)
	if len(h.records) > maxHistory {
		h.records = h.records[:maxHistory]
	}
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

// Totals are the sums of the history.
type Totals struct {
	Prints   int
	Time     time.Duration
	Filament float64 // mm
}

// Totals adds up the finished prints.
func (h *History) Totals() Totals {
	var t Totals
	for _, r := range h.Records() {
		t.Prints++
		t.Time += r.Duration
		t.Filament += r.Filament
	}
	return t
}
