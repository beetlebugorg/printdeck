// Package bedcool cools the bed before the end of a print (Maintenance > Bed cool). After a print,
// PRINT_END waits for the bed to cool before the part comes off. With this setting, the deck sets
// the bed to a hold temperature a little before the end: the bed is at the hold temperature when
// the print ends, so the wait is shorter. The hold temperature is for each material: low enough to
// save time, high enough to keep the part on the bed. If the print runs longer than its estimate,
// the bed stays at the hold temperature.
//
// The time to cool comes from Newton's law of cooling: T(t) = room + (T0 - room) * exp(-t / tau).
// The deck learns tau from each cool of the bed that it sees.
//
// The deck does not cool the bed before half of the print, because the estimate of the time left
// is poor at the start. It does not cool the bed for a tall part on a small base: such a part can
// come off the bed, and the nozzle then hits it.
package bedcool

import (
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"
)

// Settings are the settings of the bed cool.
type Settings struct {
	On bool `json:"on"`
	// Hold is the hold temperature of each material, in °C. A material with no hold, or 0, has no
	// early cool.
	Hold map[string]int `json:"hold"`
	// Tau is the time constant of the cooling of the bed, in seconds, and Samples the number of samples
	// that it comes from: one for each 2 minutes of a cool. The deck learns them.
	Tau     float64 `json:"tau"`
	Samples int     `json:"samples"`
}

// Room is the temperature of the room in °C, for the cooling curve.
const Room = 22.0

// DefaultTau is the time constant of the cooling of a bed before the deck learns it, in seconds.
const DefaultTau = 900.0

// DefaultSettings are the settings of a new deck: on for PLA. PETG, ABS and TPU warp or let go
// when the bed cools, so they have no hold.
var DefaultSettings = Settings{On: true, Hold: map[string]int{"PLA": 50}, Tau: DefaultTau}

// Holds are the hold temperatures that the page offers for a material, in °C. 0 is off.
var Holds = []int{0, 40, 45, 50, 55, 60, 65, 70}

// ParseSettings reads the settings of the file. A bad file gives the defaults.
func ParseSettings(b []byte) Settings {
	s := DefaultSettings
	s.Hold = map[string]int{}
	if json.Unmarshal(b, &s) != nil {
		return DefaultSettings
	}
	if s.Hold == nil {
		s.Hold = map[string]int{}
	}
	if s.Tau <= 0 {
		s.Tau = DefaultTau
	}
	return s
}

// Bytes is the file of the settings.
func (s Settings) Bytes() []byte {
	b, _ := json.Marshal(s)
	return b
}

// CoolTime is the time for the bed to cool from the temperature from to to, in °C.
func CoolTime(tau, from, to float64) time.Duration {
	if to >= from || to <= Room {
		return 0
	}
	return time.Duration(tau * math.Log((from-Room)/(to-Room)) * float64(time.Second))
}

// Print is a print now, for the runner.
type Print struct {
	Busy, Paused      bool
	File              string
	Bed, BedTarget    float64 // °C
	Elapsed, TimeLeft time.Duration
	HasTimeLeft       bool
	Material          string     // the loaded material, for example "PLA"
	Part              [3]float64 // the width, the depth and the height of the part, in mm
	PartOK            bool
}

// Info is what the runner did, for the page.
type Info struct {
	Last string // the last decision, or ""
}

// margin is the time that the cool starts before it must: the estimate of the time left can be
// short.
const margin = 30 * time.Second

// tallRatio is the most height of a part for the width of its base, for the cool.
const tallRatio = 2.0

// Runner follows the prints and cools the bed.
type Runner struct {
	mu   sync.Mutex
	s    Settings
	info Info
	done string // the file of the print that the runner decided on
	// The cooling now: its start, or a zero time.
	coolAt   time.Time
	coolFrom float64
	// Send sends G-code to the printer.
	Send func(line string) error
	// OnSettings is told when the settings change (the page, or a new tau).
	OnSettings func(Settings)
}

// NewRunner returns a runner with the settings s.
func NewRunner(s Settings) *Runner { return &Runner{s: s} }

// Settings are the settings now.
func (r *Runner) Settings() Settings {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.s
}

// SetSettings sets the settings.
func (r *Runner) SetSettings(s Settings) {
	r.mu.Lock()
	r.s = s
	on := r.OnSettings
	r.mu.Unlock()
	if on != nil {
		on(s)
	}
}

// Info is what the runner did.
func (r *Runner) Info() Info {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.info
}

// Step looks at the print p at the time now: it learns the cooling, and cools the bed when it is
// time.
func (r *Runner) Step(p Print, now time.Time) {
	r.learn(p, now)
	r.mu.Lock()
	line, last := r.decide(p)
	if last != "" {
		r.info.Last = last
	}
	send := r.Send
	r.mu.Unlock()
	if line != "" && send != nil {
		if err := send(line); err != nil {
			r.mu.Lock()
			r.info.Last = "The bed cool failed: " + err.Error()
			r.mu.Unlock()
		}
	}
}

// decide returns the G-code to send now, and the text of a new decision. r.mu is locked.
func (r *Runner) decide(p Print) (line, last string) {
	if !p.Busy {
		r.done = ""
		return "", ""
	}
	if p.Paused || r.done == p.File || !r.s.On {
		return "", ""
	}
	hold := float64(r.s.Hold[p.Material])
	if hold <= 0 || !p.HasTimeLeft || p.Elapsed < p.TimeLeft || p.BedTarget <= hold {
		return "", ""
	}
	if p.PartOK && p.Part[2] > tallRatio*math.Min(p.Part[0], p.Part[1]) {
		r.done = p.File
		return "", fmt.Sprintf("No early cool: the part is %.0f mm high on a %.0f mm base", p.Part[2], math.Min(p.Part[0], p.Part[1]))
	}
	need := CoolTime(r.s.Tau, p.BedTarget, hold) + margin
	if p.TimeLeft > need {
		return "", ""
	}
	r.done = p.File
	return fmt.Sprintf("M140 S%d", int(hold)), fmt.Sprintf("The bed went to %.0f° %s before the end", hold, minSec(p.TimeLeft))
}

// learn follows a cooling of the bed: the target under the temperature, and the temperature falls.
// After 2 minutes and a fall of 3 °C or more, it updates tau.
func (r *Runner) learn(p Print, now time.Time) {
	r.mu.Lock()
	cooling := p.BedTarget < p.Bed-3 && p.Bed > Room+8
	if !cooling {
		r.coolAt = time.Time{}
		r.mu.Unlock()
		return
	}
	if r.coolAt.IsZero() {
		r.coolAt, r.coolFrom = now, p.Bed
		r.mu.Unlock()
		return
	}
	dt := now.Sub(r.coolAt).Seconds()
	if dt < 120 || r.coolFrom-p.Bed < 3 {
		r.mu.Unlock()
		return
	}
	tau := dt / math.Log((r.coolFrom-Room)/(p.Bed-Room))
	r.coolAt, r.coolFrom = now, p.Bed
	if tau < 60 || tau > 7200 {
		r.mu.Unlock()
		return
	}
	// The mean of the samples, with more weight on the new ones after 10.
	w := 1 / float64(min(r.s.Samples+1, 10))
	r.s.Tau = r.s.Tau*(1-w) + tau*w
	if r.s.Samples == 0 {
		r.s.Tau = tau
	}
	r.s.Samples++
	s, on := r.s, r.OnSettings
	r.mu.Unlock()
	if on != nil {
		on(s)
	}
}

// minSec is a time as 4:05.
func minSec(d time.Duration) string {
	s := int(d.Round(time.Second) / time.Second)
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
