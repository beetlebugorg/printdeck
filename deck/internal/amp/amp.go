// Package amp controls the volume of the HiFiBerry Amp2 through the ALSA "Digital" control.
// raspotify uses the same control, so the Spotify app and the touch screen move the same slider.
package amp

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"sync"
	"time"
)

type Amp struct {
	card string
	max  int

	mu     sync.Mutex
	volume int
	want   chan int
}

var pct = regexp.MustCompile(`\[(\d+)%\]`)

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
	out, err := exec.Command("amixer", "-c", a.card, "-M", "sget", "Digital").Output()
	if err != nil {
		return 0, err
	}
	m := pct.FindSubmatch(out)
	if m == nil {
		return 0, fmt.Errorf("no volume in amixer output")
	}
	return strconv.Atoi(string(m[1]))
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
			exec.Command("amixer", "-c", a.card, "-M", "sset", "Digital", fmt.Sprintf("%d%%", p)).Run()
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
