package printer

import (
	"encoding/json"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"
)

// The heat log of a printer: the time that each heater needs to heat up, and the time that the
// bed and the nozzles need to cool after a print. The backends feed it with the temperatures and
// the targets of the reports that they already read (Observe). The log sends no command to the
// printer and sets no temperature.
//
// A heat-up starts when the target of a heater goes up above its temperature. It ends when the
// temperature is within heatBand of the target for heatHold. Its time is the time from the start
// to the first report in the band. A change of the target, a target of 0, or a gap in the reports
// before the end drops it.
//
// A cool-down starts at the end of a print (PrintEnded). It ends when the bed is at its release
// temperature, or a nozzle at CoolNozzleTo. A target that goes up drops it.

// The heaters of the heat log.
const (
	HeatBed     = "bed"
	HeatNozzle  = "nozzle"
	HeatAux     = "aux" // the second nozzle, for example the aux nozzle of a Bambu Lab X2D
	HeatChamber = "chamber"
)

// HeaterNames is the order of the heaters on the screens and in the API.
var HeaterNames = []string{HeatBed, HeatNozzle, HeatAux, HeatChamber}

const (
	heatBand    = 1.0             // °C from the target
	heatHold    = 3 * time.Second // the time in the band
	heatKeep    = 200             // the samples of each heater that the log keeps, for each kind
	heatGap     = time.Minute     // a longer time between two reports drops a heat-up
	coolGap     = 10 * time.Minute
	heatLongest = time.Hour
	coolLongest = 4 * time.Hour
	// CoolBedTo is the bed temperature at the end of a cool-down, for a printer with no release
	// temperature of its own (HeatLog.Release). CoolNozzleTo is the temperature of a nozzle.
	CoolBedTo    = 40.0
	CoolNozzleTo = 50.0
)

// HeatSample is one heat-up of a heater: the temperature at the start, the target, and the
// seconds to the target. Room is the temperature of the room or the chamber at the start, or nil
// when the printer reports none.
type HeatSample struct {
	At      time.Time `json:"at"`
	From    float64   `json:"from"`
	Target  float64   `json:"target"`
	Seconds float64   `json:"s"`
	Room    *float64  `json:"room,omitempty"`
}

// CoolSample is one cool-down of a heater after a print: the temperature at the end of the print,
// the temperature at the end of the cool-down, and the seconds between them.
type CoolSample struct {
	At      time.Time `json:"at"`
	From    float64   `json:"from"`
	To      float64   `json:"to"`
	Seconds float64   `json:"s"`
}

// HeatLog is the heat log of one printer, in a JSON file.
type HeatLog struct {
	Path string // the file; "" keeps the log in memory only
	// Release is the bed temperature at which a part comes off, or 0 for CoolBedTo.
	Release float64

	mu     sync.Mutex
	loaded bool
	data   struct {
		Heat map[string][]HeatSample `json:"heat"`
		Cool map[string][]CoolSample `json:"cool"`
		// Changes are the filament changes of the printer (filamentchange.go), oldest first.
		Changes []FilamentChange `json:"filament_changes,omitempty"`
	}
	last    map[string]heaterLast
	runs    map[string]*heatRun
	cools   map[string]*coolRun
	room    float64
	hasRoom bool
	report  *HeatReport // the report of the samples; nil after a change
}

// heaterLast is the last report of a heater.
type heaterLast struct {
	at           time.Time
	temp, target float64
}

// heatRun is a heat-up that runs. within is the first report in the band, or zero.
type heatRun struct {
	start, within time.Time
	from, target  float64
	room          *float64
}

// coolRun is a cool-down that runs.
type coolRun struct {
	start    time.Time
	from, to float64
}

func (h *HeatLog) load() {
	if h.loaded {
		return
	}
	h.loaded = true
	h.last, h.runs, h.cools = map[string]heaterLast{}, map[string]*heatRun{}, map[string]*coolRun{}
	if h.Path != "" {
		if b, err := os.ReadFile(h.Path); err == nil {
			json.Unmarshal(b, &h.data)
		}
	}
	if h.data.Heat == nil {
		h.data.Heat = map[string][]HeatSample{}
	}
	if h.data.Cool == nil {
		h.data.Cool = map[string][]CoolSample{}
	}
}

func (h *HeatLog) save() {
	h.report = nil
	if h.Path == "" {
		return
	}
	b, _ := json.Marshal(h.data)
	os.MkdirAll(filepath.Dir(h.Path), 0o700)
	tmp := h.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err == nil {
		os.Rename(tmp, h.Path)
	}
}

// Observe feeds the temperatures and the targets of the state st at now to the log.
func (h *HeatLog) Observe(st State, now time.Time) {
	if h == nil || !st.Reachable {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.load()
	h.room, h.hasRoom = st.Chamber, st.HasChamber
	h.sample(now, HeatBed, st.Bed, st.BedTarget)
	h.sample(now, HeatNozzle, st.Tool, st.ToolTarget)
	if st.HasTool2 {
		h.sample(now, HeatAux, st.Tool2, st.Tool2Target)
	}
	if st.HasChamber {
		h.sample(now, HeatChamber, st.Chamber, st.ChamberTarget)
	}
}

// Sample feeds one report of a heater to the log: its temperature and its target at now.
func (h *HeatLog) Sample(now time.Time, heater string, temp, target float64) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.load()
	h.sample(now, heater, temp, target)
}

// SetRoom sets the temperature of the room or the chamber for the next heat-ups. ok is false when
// the printer reports none.
func (h *HeatLog) SetRoom(temp float64, ok bool) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.room, h.hasRoom = temp, ok
}

// sample is Sample. h.mu is locked.
func (h *HeatLog) sample(now time.Time, heater string, temp, target float64) {
	l, seen := h.last[heater]
	h.last[heater] = heaterLast{now, temp, target}
	heatGapped := seen && now.Sub(l.at) > heatGap
	if r := h.runs[heater]; r != nil {
		switch {
		case heatGapped || math.Abs(target-r.target) > 0.5 || now.Sub(r.start) > heatLongest:
			delete(h.runs, heater)
		case math.Abs(temp-target) <= heatBand:
			if r.within.IsZero() {
				r.within = now
			}
			if now.Sub(r.within) >= heatHold {
				h.addHeat(heater, HeatSample{At: r.start, From: r.from, Target: r.target, Seconds: r.within.Sub(r.start).Seconds(), Room: r.room})
				delete(h.runs, heater)
			}
		default:
			r.within = time.Time{}
		}
	}
	if h.runs[heater] == nil && seen && !heatGapped && target > l.target+0.5 && target > temp+heatBand {
		r := &heatRun{start: now, from: temp, target: target}
		if h.hasRoom && heater != HeatChamber {
			room := h.room
			r.room = &room
		}
		h.runs[heater] = r
	}
	if c := h.cools[heater]; c != nil {
		switch {
		case seen && now.Sub(l.at) > coolGap || target > l.target+0.5 || now.Sub(c.start) > coolLongest:
			delete(h.cools, heater)
		case temp <= c.to:
			h.addCool(heater, CoolSample{At: c.start, From: c.from, To: c.to, Seconds: now.Sub(c.start).Seconds()})
			delete(h.cools, heater)
		}
	}
}

// PrintEnded starts the cool-downs of the bed and the nozzles at the end of a print at now, from
// the temperatures of the last reports.
func (h *HeatLog) PrintEnded(now time.Time) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.load()
	for _, heater := range []string{HeatBed, HeatNozzle, HeatAux} {
		l, ok := h.last[heater]
		to := h.coolTo(heater)
		if !ok || l.temp <= to {
			delete(h.cools, heater)
			continue
		}
		h.cools[heater] = &coolRun{start: now, from: l.temp, to: to}
	}
}

// coolTo is the temperature at the end of a cool-down of the heater.
func (h *HeatLog) coolTo(heater string) float64 {
	if heater != HeatBed {
		return CoolNozzleTo
	}
	if h.Release > 0 {
		return h.Release
	}
	return CoolBedTo
}

func (h *HeatLog) addHeat(heater string, s HeatSample) {
	h.data.Heat[heater] = keepLast(append(h.data.Heat[heater], s))
	h.save()
}

func (h *HeatLog) addCool(heater string, s CoolSample) {
	h.data.Cool[heater] = keepLast(append(h.data.Cool[heater], s))
	h.save()
}

// keepLast returns the last heatKeep samples of v.
func keepLast[T any](v []T) []T {
	if len(v) > heatKeep {
		return slices.Clone(v[len(v)-heatKeep:])
	}
	return v
}

// Heats returns the heat-ups of the heater, oldest first.
func (h *HeatLog) Heats(heater string) []HeatSample {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.load()
	return slices.Clone(h.data.Heat[heater])
}

// Cools returns the cool-downs of the heater, oldest first.
func (h *HeatLog) Cools(heater string) []CoolSample {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.load()
	return slices.Clone(h.data.Cool[heater])
}

// HeatReport is the summary of the heat log, for the screens and for the slicers
// (/slicer/heat.json).
type HeatReport struct {
	Heaters []HeaterTimes `json:"heaters"`
	Cool    []CoolTimes   `json:"cool"`
	// FilamentChanges are the times of the filament changes. With no sample, it has samples 0
	// and no other key.
	FilamentChanges FilamentChangeTimes `json:"filament_changes"`
}

// HeaterTimes are the heat-ups of one heater: a fit for any start and target, and the times of
// each target.
type HeaterTimes struct {
	Heater  string        `json:"heater"`
	Samples int           `json:"samples"`
	Fit     *HeatFit      `json:"fit"` // nil with too few samples
	Targets []TargetTimes `json:"targets"`
	// Points are the target and the seconds of each sample, for the chart. They are not in the
	// API.
	Points [][2]float64 `json:"-"`
}

// HeatFit gives the seconds of a heat-up: Intercept + PerDegree × (target − start). Method is
// "theil-sen" for a fit of the two numbers to the samples, or "rate" for a median of the seconds
// for each degree with an Intercept of 0, when the samples have too little spread of degrees.
type HeatFit struct {
	Method    string  `json:"method"`
	Intercept float64 `json:"intercept_s"`
	PerDegree float64 `json:"per_degree_s"`
	Samples   int     `json:"samples"`
}

// Seconds is the fit for a heat-up from the temperature from to target.
func (f HeatFit) Seconds(from, target float64) float64 {
	if target <= from {
		return 0
	}
	return f.Intercept + f.PerDegree*(target-from)
}

// TargetTimes are the heat-ups of a heater to one target: the median of the seconds and the
// spread (the 25th and the 75th percentile), the median start temperature, and the median room
// temperature, or nil.
type TargetTimes struct {
	Target  float64  `json:"target"`
	Samples int      `json:"samples"`
	Median  float64  `json:"median_s"`
	Low     float64  `json:"p25_s"`
	High    float64  `json:"p75_s"`
	From    float64  `json:"from"`
	Room    *float64 `json:"room,omitempty"`
}

// CoolTimes are the cool-downs of one heater after a print, to the temperature To.
type CoolTimes struct {
	Heater  string  `json:"heater"`
	To      float64 `json:"to"`
	Samples int     `json:"samples"`
	Median  float64 `json:"median_s"`
	Low     float64 `json:"p25_s"`
	High    float64 `json:"p75_s"`
	From    float64 `json:"from"`
}

// fitMinDegrees is the least rise of a heat-up for the rate fit: a short rise is mostly the time
// to settle in the band.
const fitMinDegrees = 10.0

// Report returns the summary of the log.
func (h *HeatLog) Report() HeatReport {
	if h == nil {
		return HeatReport{Heaters: []HeaterTimes{}, Cool: []CoolTimes{}}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.load()
	if h.report == nil {
		r := computeHeatReport(h.data.Heat, h.data.Cool)
		r.FilamentChanges = computeChangeTimes(h.data.Changes)
		h.report = &r
	}
	return *h.report
}

func computeHeatReport(heat map[string][]HeatSample, cool map[string][]CoolSample) HeatReport {
	r := HeatReport{Heaters: []HeaterTimes{}, Cool: []CoolTimes{}}
	for _, name := range HeaterNames {
		samples := heat[name]
		if len(samples) == 0 {
			continue
		}
		ht := HeaterTimes{Heater: name, Samples: len(samples), Fit: fitSamples(samples)}
		byTarget := map[float64][]HeatSample{}
		for _, s := range samples {
			t := math.Round(s.Target)
			byTarget[t] = append(byTarget[t], s)
			ht.Points = append(ht.Points, [2]float64{s.Target, s.Seconds})
		}
		targets := make([]float64, 0, len(byTarget))
		for t := range byTarget {
			targets = append(targets, t)
		}
		sort.Float64s(targets)
		for _, t := range targets {
			var secs, from, rooms []float64
			for _, s := range byTarget[t] {
				secs, from = append(secs, s.Seconds), append(from, s.From)
				if s.Room != nil {
					rooms = append(rooms, *s.Room)
				}
			}
			tt := TargetTimes{Target: t, Samples: len(secs), Median: median(secs), Low: percentile(secs, 25),
				High: percentile(secs, 75), From: median(from)}
			if len(rooms) > 0 {
				room := median(rooms)
				tt.Room = &room
			}
			ht.Targets = append(ht.Targets, tt)
		}
		r.Heaters = append(r.Heaters, ht)
	}
	for _, name := range HeaterNames {
		samples := cool[name]
		if len(samples) == 0 {
			continue
		}
		// The cool-downs to the temperature of the newest sample: a change of the release
		// temperature starts new times.
		to := samples[len(samples)-1].To
		var secs, from []float64
		for _, s := range samples {
			if s.To == to {
				secs, from = append(secs, s.Seconds), append(from, s.From)
			}
		}
		r.Cool = append(r.Cool, CoolTimes{Heater: name, To: to, Samples: len(secs), Median: median(secs),
			Low: percentile(secs, 25), High: percentile(secs, 75), From: median(from)})
	}
	return r
}

// fitSamples fits the seconds of the heat-ups to their rise in degrees, or returns nil when no
// sample rises fitMinDegrees or more.
func fitSamples(samples []HeatSample) *HeatFit {
	var spans [][2]float64
	var rates []float64
	for _, s := range samples {
		d := s.Target - s.From
		if d >= 2 {
			spans = append(spans, [2]float64{d, s.Seconds})
		}
		if d >= fitMinDegrees {
			rates = append(rates, s.Seconds/d)
		}
	}
	if rate, settle, ok := fitHeat(spans); ok {
		return &HeatFit{Method: "theil-sen", Intercept: settle, PerDegree: rate, Samples: len(spans)}
	}
	if len(rates) == 0 {
		return nil
	}
	return &HeatFit{Method: "rate", PerDegree: median(rates), Samples: len(rates)}
}

// percentile is the p-th percentile of v (0 to 100), with a straight line between two values. It
// is 0 for none.
func percentile(v []float64, p float64) float64 {
	if len(v) == 0 {
		return 0
	}
	v = slices.Clone(v)
	sort.Float64s(v)
	x := p / 100 * float64(len(v)-1)
	i := int(x)
	if i >= len(v)-1 {
		return v[len(v)-1]
	}
	return v[i] + (x-float64(i))*(v[i+1]-v[i])
}

// heatRateKinds are the kinds of the heating steps of the start macro (Rates) for each heater.
var heatRateKinds = map[string]string{HeatBed: "heat-bed", HeatNozzle: "heat-nozzle"}

// rateMinSamples is the least number of samples of a fit that replaces the rate of the history.
const rateMinSamples = 3

// Apply returns the rates with the heating rates of the log. A "theil-sen" fit of a heater with
// rateMinSamples samples or more replaces the rate and the settle time from the history. A "rate"
// fit replaces the rate only when the history has no settle time.
func (r HeatReport) Apply(rates Rates) Rates {
	for _, ht := range r.Heaters {
		kind, ok := heatRateKinds[ht.Heater]
		if !ok || ht.Fit == nil || ht.Fit.Samples < rateMinSamples || ht.Fit.PerDegree <= 0 {
			continue
		}
		if ht.Fit.Method != "theil-sen" && rates.Settle[kind] > 0 {
			continue
		}
		// The maps of the rates of the history are shared: the change goes in copies.
		heat, settle := maps.Clone(rates.Heat), maps.Clone(rates.Settle)
		if heat == nil {
			heat = map[string]float64{}
		}
		if settle == nil {
			settle = map[string]float64{}
		}
		heat[kind], settle[kind] = ht.Fit.PerDegree, ht.Fit.Intercept
		rates.Heat, rates.Settle = heat, settle
	}
	return rates
}

// CoolOf returns the cool-down times of the heater, and false when the log has none.
func (r HeatReport) CoolOf(heater string) (CoolTimes, bool) {
	for _, c := range r.Cool {
		if c.Heater == heater {
			return c, true
		}
	}
	return CoolTimes{}, false
}
