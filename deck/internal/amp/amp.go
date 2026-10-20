// Package amp controls the volume of the HiFiBerry Amp2 through the ALSA control "Digital Playback
// Volume" (the simple control "Digital" of amixer).
//
// The percent on the screen, and in the Spotify app, covers a window of decibels that is useful for
// the speakers: 0% is mute, 1% is just above the floor, 100% is the ceiling. Inside the window the
// percent is linear in decibels, which is close to how loudness is heard. The Digital control itself
// reaches from -103.5 dB to 0 dB, and most of that is either silent or too loud for small speakers.
// go-librespot must not change the volume as well (external_volume in its config).
package amp

import (
	"context"
	"log"
	"math"
	"sync"
	"time"

	"printdeck/internal/alsa"
)

// The Digital control has steps of 0.5 dB from -103.5 dB (mute) at step 0 to 0 dB at step 207.
const (
	maxStep = 207
	stepDB  = 0.5
)

// request is a volume to write.
type request struct {
	percent int
	local   bool // from the screen: Spotify is told
}

type Amp struct {
	card        string
	max         int
	floor, ceil float64 // decibels at 0% (just above) and at 100%
	// OnSet is called with the percent after the amp has been set from the screen.
	OnSet func(percent int)

	mu     sync.Mutex
	volume int
	want   chan request
}

// control is the name of the volume control of the amp.
const control = "Digital Playback Volume"

func New(card string, max int, floorDB, ceilDB float64) *Amp {
	return &Amp{card: card, max: max, floor: floorDB, ceil: ceilDB, want: make(chan request, 1), volume: -1}
}

// stepFor returns the control step for a percent.
func (a *Amp) stepFor(p int) int {
	if p <= 0 {
		return 0
	}
	db := a.floor + float64(min(p, 100))/100*(a.ceil-a.floor)
	return max(1, min(maxStep, int(math.Round(maxStep+db/stepDB))))
}

// percentFor returns the percent for a control step.
func (a *Amp) percentFor(step int) int {
	if step <= 0 {
		return 0
	}
	db := float64(step-maxStep) * stepDB
	return max(0, min(100, int(math.Round((db-a.floor)/(a.ceil-a.floor)*100))))
}

// Volume returns the last known volume in percent, or -1.
func (a *Amp) Volume() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.volume
}

func (a *Amp) Max() int { return a.max }

func (a *Amp) read() (int, error) {
	c, err := alsa.OpenControl(a.card, control)
	if err != nil {
		return 0, err
	}
	step, err := c.Get()
	if err != nil {
		return 0, err
	}
	return a.percentFor(step), nil
}

// write sets the control step on all channels.
func (a *Amp) write(step int) error {
	c, err := alsa.OpenControl(a.card, control)
	if err != nil {
		return err
	}
	return c.Set(step)
}

// Set changes the volume from the screen. OnSet tells Spotify.
func (a *Amp) Set(p int) { a.set(p, true) }

// SetRemote changes the volume after the Spotify app changed it. OnSet is not called.
func (a *Amp) SetRemote(p int) { a.set(p, false) }

// set writes the volume. Rapid calls are collapsed: only the newest value is written.
func (a *Amp) set(p int, local bool) {
	p = max(0, min(a.max, p))
	a.mu.Lock()
	a.volume = p
	a.mu.Unlock()
	select {
	case <-a.want:
	default:
	}
	a.want <- request{p, local}
}

// Run writes the requested volumes and follows changes made by other programs.
// notify is called when the volume changes.
func (a *Amp) Run(ctx context.Context, notify func()) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case r := <-a.want:
			if err := a.write(a.stepFor(r.percent)); err != nil {
				log.Printf("amp: %v", err)
			}
			if r.local && a.OnSet != nil {
				a.OnSet(r.percent)
			}
		case <-tick.C:
			if len(a.want) > 0 {
				continue
			}
			if v, err := a.read(); err == nil {
				a.mu.Lock()
				changed := a.volume != v
				a.volume = v
				a.mu.Unlock()
				if changed {
					notify()
				}
			}
		}
	}
}
