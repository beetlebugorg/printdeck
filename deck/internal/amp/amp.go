// Package amp controls the volume of the HiFiBerry Amp2 through the ALSA "Digital" control.
//
// The percent of the screen is the amplitude of the sound: 100% is 0 dB (full), 50% is -6 dB,
// 10% is -20 dB, 1% is -40 dB, and 0% is mute. This is not the "mapped" scale of amixer -M,
// which is a curve over the whole 103 dB range of the control, where 10% is close to silence.
// go-librespot must not change the volume as well (external_volume in its config).
package amp

import (
	"context"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// The Digital control has steps of 0.5 dB from -103.5 dB (mute) at step 0 to 0 dB at step 207.
const (
	maxStep = 207
	stepDB  = 0.5
)

// stepFor returns the control step for a percent.
func stepFor(p int) int {
	if p <= 0 {
		return 0
	}
	db := 20 * math.Log10(float64(p)/100)
	return max(1, min(maxStep, int(math.Round(float64(maxStep)+db/stepDB))))
}

// percentFor returns the percent for a control step.
func percentFor(step int) int {
	if step <= 0 {
		return 0
	}
	return int(math.Round(100 * math.Pow(10, float64(step-maxStep)*stepDB/20)))
}

type Amp struct {
	card string
	max  int

	mu     sync.Mutex
	volume int
	want   chan int
}

var raw = regexp.MustCompile(`Playback (\d+) \[`)

func New(card string, max int) *Amp {
	return &Amp{card: card, max: max, want: make(chan int, 1), volume: -1}
}

// Volume returns the last known volume in percent, or -1.
func (a *Amp) Volume() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.volume
}

func (a *Amp) Max() int { return a.max }

func (a *Amp) read() (int, error) {
	out, err := exec.Command("amixer", "-c", a.card, "sget", "Digital").Output()
	if err != nil {
		return 0, err
	}
	m := raw.FindSubmatch(out)
	if m == nil {
		return 0, fmt.Errorf("no volume in amixer output")
	}
	step, err := strconv.Atoi(string(m[1]))
	return percentFor(step), err
}

// Set changes the volume. Rapid calls are collapsed: only the newest value is written.
func (a *Amp) Set(p int) {
	p = max(0, min(a.max, p))
	a.mu.Lock()
	a.volume = p
	a.mu.Unlock()
	select {
	case <-a.want:
	default:
	}
	a.want <- p
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
		case p := <-a.want:
			exec.Command("amixer", "-c", a.card, "sset", "Digital", strconv.Itoa(stepFor(p))).Run()
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
