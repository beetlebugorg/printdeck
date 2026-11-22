package printer

import (
	"slices"
	"time"
)

// The filament changes of a printer, in the heat log. The deck records them from the reports that
// it already reads, and sends no command to the printer.

// FilamentChange is one filament change of a print. Seconds is the time from the first report of
// the change to the first report of normal printing. PlannedFlush is the flush in mm³ that the
// print file plans for the change, or nil when the file gives none. The printer reports no flow.
type FilamentChange struct {
	At           time.Time `json:"at"`
	FromTray     int       `json:"from_tray"`
	ToTray       int       `json:"to_tray"`
	NozzleChange bool      `json:"nozzle_change"`
	FromMaterial string    `json:"from_material,omitempty"`
	ToMaterial   string    `json:"to_material,omitempty"`
	Seconds      float64   `json:"s"`
	PlannedFlush *float64  `json:"planned_flush_mm3,omitempty"`
}

// AddFilamentChange keeps the sample s in the log.
func (h *HeatLog) AddFilamentChange(s FilamentChange) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.load()
	h.data.Changes = keepLast(append(h.data.Changes, s))
	h.save()
}

// FilamentChanges returns the samples of the log, oldest first.
func (h *HeatLog) FilamentChanges() []FilamentChange {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.load()
	return slices.Clone(h.data.Changes)
}

// ChangeTimes are the times of a group of filament changes: the median and the 25th and 75th
// percentile of the seconds. With no sample, only Samples is in the JSON.
type ChangeTimes struct {
	Samples int     `json:"samples"`
	Median  float64 `json:"median_s,omitempty"`
	Low     float64 `json:"p25_s,omitempty"`
	High    float64 `json:"p75_s,omitempty"`
	// PlannedFlush is the flush that the print files plan for the samples that have a value. It
	// is nil when no sample has one.
	PlannedFlush *PlannedFlush `json:"planned_flush,omitempty"`
}

// PlannedFlush is the spread of the planned flush of a group of changes, in mm³. Samples is the
// count of changes with a planned value.
type PlannedFlush struct {
	Samples int     `json:"samples"`
	Median  float64 `json:"median_mm3"`
	Low     float64 `json:"p25_mm3"`
	High    float64 `json:"p75_mm3"`
}

// FilamentChangeTimes is the summary of the filament changes for the screens and the slicers. The
// split groups are nil when the log has no sample of that kind.
type FilamentChangeTimes struct {
	ChangeTimes
	SameNozzle     *ChangeTimes `json:"same_nozzle,omitempty"`
	NozzleToNozzle *ChangeTimes `json:"nozzle_to_nozzle,omitempty"`
}

// computeChangeTimes returns the summary of the samples.
func computeChangeTimes(samples []FilamentChange) FilamentChangeTimes {
	out := FilamentChangeTimes{ChangeTimes: changeGroup(samples)}
	var same, nozzle []FilamentChange
	for _, s := range samples {
		if s.NozzleChange {
			nozzle = append(nozzle, s)
		} else {
			same = append(same, s)
		}
	}
	if len(same) > 0 {
		g := changeGroup(same)
		out.SameNozzle = &g
	}
	if len(nozzle) > 0 {
		g := changeGroup(nozzle)
		out.NozzleToNozzle = &g
	}
	return out
}

func changeGroup(samples []FilamentChange) ChangeTimes {
	if len(samples) == 0 {
		return ChangeTimes{}
	}
	var secs, flush []float64
	for _, s := range samples {
		secs = append(secs, s.Seconds)
		if s.PlannedFlush != nil {
			flush = append(flush, *s.PlannedFlush)
		}
	}
	g := ChangeTimes{Samples: len(secs), Median: median(secs), Low: percentile(secs, 25), High: percentile(secs, 75)}
	if len(flush) > 0 {
		g.PlannedFlush = &PlannedFlush{Samples: len(flush), Median: median(flush), Low: percentile(flush, 25), High: percentile(flush, 75)}
	}
	return g
}
