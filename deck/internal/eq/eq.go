// Package eq is the equalizer of the speaker: five filters and a preamp, on the samples that go to
// the amp. It keeps the setting and saves it.
//
// The filters are those that Printdeck used in CamillaDSP before: a low shelf at 80 Hz, peaks at
// 250 Hz, 1 kHz and 3.5 kHz, and a high shelf at 10 kHz. The
// coefficients and the processing are a port of CamillaDSP 4.1.3 (src/filters/biquad.rs and
// basicfilters.rs, GPL-3.0, Copyright (c) Henrik Enquist): the RBJ formulas with CamillaDSP's shelf
// slope, transposed direct form II in float64. The test compares the output with the output of the
// real CamillaDSP (tools/camilladsp-reference).
package eq

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// SampleRate is the rate of the samples that Process gets.
const SampleRate = 44100

type filterType int

const (
	lowshelf filterType = iota
	peaking
	highshelf
)

// Band is one slider.
type Band struct {
	Filter string // the name of the filter in the CamillaDSP config
	Label  string // the frequency, for the screen
	typ    filterType
	freq   float64
}

var Bands = []Band{
	{"eq_bass", "80", lowshelf, 80},
	{"eq_lowmid", "250", peaking, 250},
	{"eq_mid", "1k", peaking, 1000},
	{"eq_highmid", "3.5k", peaking, 3500},
	{"eq_treble", "10k", highshelf, 10000},
}

// The Q of the peaks and the slope of the shelves (dB per octave).
const (
	peakQ      = 1.0
	shelfSlope = 6.0
)

// MaxGain is the most that a band can boost or cut, in dB.
const MaxGain = 12.0

type Gains [5]float64

// Preset is a named setting.
type Preset struct {
	Name  string
	Gains Gains
}

var Presets = []Preset{
	{"Flat", Gains{}},
	{"Bass", Gains{6, 2, 0, 0, 0}},
	{"Loud", Gains{5, 1, 0, 1, 4}}, // more bass and treble, for low volumes
	{"Voice", Gains{-3, -1, 2, 3, 0}},
	{"Treble", Gains{0, 0, 0, 2, 5}},
}

// coeffs are the normalized coefficients of a biquad (BiquadCoefficients).
type coeffs struct{ a1, a2, b0, b1, b2 float64 }

func normalize(a0, a1, a2, b0, b1, b2 float64) coeffs {
	return coeffs{a1 / a0, a2 / a0, b0 / a0, b1 / a0, b2 / a0}
}

// biquadCoeffs is BiquadCoefficients::from_config of CamillaDSP for the three filter types.
func biquadCoeffs(typ filterType, freq, gain float64) coeffs {
	omega := 2.0 * math.Pi * freq / SampleRate
	sn := math.Sin(omega)
	cs := math.Cos(omega)
	ampl := math.Pow(10.0, gain/40.0)
	switch typ {
	case peaking:
		alpha := sn / (2.0 * peakQ)
		b0 := 1.0 + (alpha * ampl)
		b1 := -2.0 * cs
		b2 := 1.0 - (alpha * ampl)
		a0 := 1.0 + (alpha / ampl)
		a1 := -2.0 * cs
		a2 := 1.0 - (alpha / ampl)
		return normalize(a0, a1, a2, b0, b1, b2)
	case highshelf:
		alpha := sn / 2.0 * math.Sqrt((ampl+1.0/ampl)*(1.0/(shelfSlope/12.0)-1.0)+2.0)
		beta := 2.0 * math.Sqrt(ampl) * alpha
		b0 := ampl * ((ampl + 1.0) + (ampl-1.0)*cs + beta)
		b1 := -2.0 * ampl * ((ampl - 1.0) + (ampl+1.0)*cs)
		b2 := ampl * ((ampl + 1.0) + (ampl-1.0)*cs - beta)
		a0 := (ampl + 1.0) - (ampl-1.0)*cs + beta
		a1 := 2.0 * ((ampl - 1.0) - (ampl+1.0)*cs)
		a2 := (ampl + 1.0) - (ampl-1.0)*cs - beta
		return normalize(a0, a1, a2, b0, b1, b2)
	default: // lowshelf
		alpha := sn / 2.0 * math.Sqrt((ampl+1.0/ampl)*(1.0/(shelfSlope/12.0)-1.0)+2.0)
		beta := 2.0 * math.Sqrt(ampl) * alpha
		b0 := ampl * ((ampl + 1.0) - (ampl-1.0)*cs + beta)
		b1 := 2.0 * ampl * ((ampl - 1.0) - (ampl+1.0)*cs)
		b2 := ampl * ((ampl + 1.0) - (ampl-1.0)*cs - beta)
		a0 := (ampl + 1.0) + (ampl-1.0)*cs + beta
		a1 := -2.0 * ((ampl - 1.0) + (ampl+1.0)*cs)
		a2 := (ampl + 1.0) + (ampl-1.0)*cs - beta
		return normalize(a0, a1, a2, b0, b1, b2)
	}
}

// Preamp is the gain of the preamp in dB: it lowers everything by the largest boost, so that a
// boost cannot clip.
func Preamp(g Gains) float64 {
	boost := 0.0
	for _, v := range g {
		boost = max(boost, v)
	}
	return -boost
}

// setting is the filters of a setting: the preamp factor and the coefficients of the bands.
type setting struct {
	gain   float64
	coeffs [5]coeffs
}

func newSetting(g Gains) *setting {
	// db_to_linear of CamillaDSP
	s := &setting{gain: math.Pow(10.0, Preamp(g)/20.0)}
	for i, b := range Bands {
		s.coeffs[i] = biquadCoeffs(b.typ, b.freq, g[i])
	}
	return s
}

type EQ struct {
	path string // the saved setting

	mu    sync.Mutex
	gains Gains
	kick  chan struct{}

	cur atomic.Pointer[setting]
	// the state of the filters (s1, s2) for each channel and band; only Process uses it
	state [2][5][2]float64
}

func New(stateDir string) *EQ {
	e := &EQ{path: filepath.Join(stateDir, "eq.json"), kick: make(chan struct{}, 1)}
	if b, err := os.ReadFile(e.path); err == nil {
		json.Unmarshal(b, &e.gains)
	}
	e.cur.Store(newSetting(e.gains))
	return e
}

func (e *EQ) Gains() Gains {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.gains
}

// Connected reports that the equalizer works. It runs in this program, so it always does.
func (e *EQ) Connected() bool { return true }

// Preset returns the name of the preset that the gains are, or "".
func (e *EQ) Preset() string {
	g := e.Gains()
	for _, p := range Presets {
		if p.Gains == g {
			return p.Name
		}
	}
	return ""
}

// SetGain sets one band, in steps of 0.5 dB.
func (e *EQ) SetGain(band int, db float64) {
	if band < 0 || band >= len(Bands) {
		return
	}
	e.mu.Lock()
	e.gains[band] = math.Round(max(-MaxGain, min(MaxGain, db))*2) / 2
	g := e.gains
	e.mu.Unlock()
	e.changed(g)
}

func (e *EQ) SetPreset(name string) {
	for _, p := range Presets {
		if p.Name == name {
			e.mu.Lock()
			e.gains = p.Gains
			e.mu.Unlock()
			e.changed(p.Gains)
		}
	}
}

// changed takes the new gains into the filters at once, as CamillaDSP's update_parameters (the
// state of the filters stays), and asks Run to save them.
func (e *EQ) changed(g Gains) {
	e.cur.Store(newSetting(g))
	select {
	case e.kick <- struct{}{}:
	default:
	}
}

// Process filters interleaved stereo samples in place. Only one goroutine may call it.
func (e *EQ) Process(samples []float32) {
	s := e.cur.Load()
	for i := 0; i+1 < len(samples); i += 2 {
		for ch := 0; ch < 2; ch++ {
			x := float64(samples[i+ch]) * s.gain
			for b := range s.coeffs {
				c := &s.coeffs[b]
				st := &e.state[ch][b]
				out := st[0] + c.b0*x
				st[0] = st[1] + c.b1*x - c.a1*out
				st[1] = c.b2*x - c.a2*out
				x = out
			}
			samples[i+ch] = float32(x)
		}
	}
	// Flush stored subnormal numbers to zero (flush_subnormals)
	for ch := range e.state {
		for b := range e.state[ch] {
			for k, v := range e.state[ch][b] {
				if v != 0 && math.Abs(v) < 0x1p-1022 {
					e.state[ch][b][k] = 0
				}
			}
		}
	}
}

// Run saves the setting after each change, until ctx ends.
func (e *EQ) Run(ctx context.Context, notify func()) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.kick:
			e.save(e.Gains())
			notify()
		}
	}
}

func (e *EQ) save(g Gains) {
	b, _ := json.Marshal(g)
	os.MkdirAll(filepath.Dir(e.path), 0o700)
	tmp := e.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err == nil {
		os.Rename(tmp, e.path)
	}
}

// String is for logs.
func (g Gains) String() string { return fmt.Sprintf("%v dB", [5]float64(g)) }
