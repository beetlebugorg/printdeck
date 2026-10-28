package printer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"printdeck/internal/gcodemeta"
)

// Record is one finished print of the history.
type Record struct {
	File     string        `json:"file"`
	Title    string        `json:"title,omitempty"` // the title in the file, or "": Name is the name for the screens
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
	// CoolFrom is the bed temperature at the end of the print, at the start of CoolBed, and
	// CoolRoom the room temperature then.
	CoolFrom float64 `json:"cool_from,omitempty"`
	CoolRoom float64 `json:"cool_room,omitempty"`
	Planned  float64 `json:"planned,omitempty"`
	// Pos is the bytes of the file that the printer read before the print ended, or 0. The
	// picture of a cancelled or failed print shows the model up to there.
	Pos int64 `json:"pos,omitempty"`
	// Material is the material that the slicer wrote in the file, for example "PLA", or "".
	Material string `json:"material,omitempty"`
	// The roll that was in the printer (Spool.Label and Spool.ID), the grams of filament that the
	// print used from it, and the cost of that filament. A record from before SpoolID has only
	// the name of the roll.
	Spool   string  `json:"spool,omitempty"`
	SpoolID string  `json:"spool_id,omitempty"`
	Grams   float64 `json:"grams,omitempty"`
	Cost    float64 `json:"cost,omitempty"`
	// Video is the name of the time lapse video of the print, or "". The deck keeps the last
	// videos only, so the file of an old record can be gone.
	Video string `json:"video,omitempty"`
}

// OfSpool is the records of the prints that used the roll, in the order of recs. A record with
// no SpoolID matches by the name of the roll.
func OfSpool(recs []Record, sp Spool) []Record {
	var out []Record
	for _, r := range recs {
		if r.SpoolID == sp.ID || r.SpoolID == "" && r.Spool != "" && r.Spool == sp.Label() {
			out = append(out, r)
		}
	}
	return out
}

// Name is the name of the print for the screens: the title in the file, or else the cleaned file
// name.
func (r Record) Name() string { return gcodemeta.Meta{Title: r.Title}.Name(r.File) }

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
	video   pendingVideo
}

// pendingVideo is a video for a print that has no record yet.
type pendingVideo struct {
	path, name string
	at         time.Time
}

// videoWindow is the longest time between the end of a print and the save of its video.
const videoWindow = 10 * time.Minute

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
// one, also after a new connection. phases and planned are the timings of the print, and pos is
// the last file position of the print. material gives the material of the file, for a new record.
// It returns the key of a new record, or "".
func (h *History) add(st State, now time.Time, phases []Phase, planned float64, pos int64, material func(path string) string) string {
	if st.Finished == "" {
		return ""
	}
	r := Record{File: st.FinishedFile, Title: st.FinishedTitle, Path: st.FinishedPath, Result: st.Finished, Duration: st.FinishedTime,
		Filament: st.FinishedFilament, End: now, Phases: phases, Planned: planned, Pos: pos}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.load()
	if key(r) == h.last {
		return ""
	}
	if material != nil {
		r.Material = material(r.Path)
	}
	if v := h.video; v.name != "" && v.path == r.Path && now.Sub(v.at) < videoWindow {
		r.Video = v.name
	}
	h.video = pendingVideo{}
	h.last = key(r)
	h.records = append([]Record{r}, h.records...)
	if len(h.records) > maxHistory {
		h.records = h.records[:maxHistory]
	}
	h.rates = nil
	h.save()
	return h.last
}

// setCool sets the cooling of the bed of the record with the key: its seconds, and the bed and
// room temperatures at its start.
func (h *History) setCool(k string, seconds, from, room float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.records {
		if key(h.records[i]) == k {
			h.records[i].CoolBed, h.records[i].CoolFrom, h.records[i].CoolRoom = seconds, from, room
			h.rates = nil
			h.save()
			return
		}
	}
}

// setSpool sets the roll, the grams from it, and the cost of the record with the key.
func (h *History) setSpool(k string, sp Spool, grams, cost float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.records {
		if key(h.records[i]) == k {
			r := &h.records[i]
			r.Spool, r.SpoolID, r.Grams, r.Cost = sp.Label(), sp.ID, grams, cost
			h.save()
			return
		}
	}
}

// SetVideo gives the video name to the newest record of the file path, when the print ended in
// the last minutes and the record has no video. A print with no record yet gets the video when
// its record is added.
func (h *History) SetVideo(path, name string, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.load()
	for i := range h.records {
		r := &h.records[i]
		if r.Path != path {
			continue
		}
		if r.Video == "" && now.Sub(r.End) < videoWindow {
			r.Video = name
			h.save()
			return
		}
		break
	}
	h.video = pendingVideo{path, name, now}
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
