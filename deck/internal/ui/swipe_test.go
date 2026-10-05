package ui

import (
	"testing"
	"time"

	"speakerdeck/internal/gfx"
	"speakerdeck/internal/octoprint"
	"speakerdeck/internal/spotify"
	"speakerdeck/internal/touch"
)

type countingMusic struct {
	*DemoMusic
	next, prev int
}

func (m *countingMusic) Next()     { m.next++ }
func (m *countingMusic) Previous() { m.prev++ }

func swipe(a *App, x0, y0, x1, y1 int) {
	ev := func(k touch.Kind, x, y int) touch.Event {
		return touch.Event{Kind: k, X: float64(x) / W, Y: float64(y) / H}
	}
	a.handle(ev(touch.Down, x0, y0))
	a.handle(ev(touch.Move, (x0+x1)/2, (y0+y1)/2))
	a.handle(ev(touch.Up, x1, y1))
}

func TestSwipe(t *testing.T) {
	m := &countingMusic{DemoMusic: &DemoMusic{State_: spotify.State{Authorized: true, Active: true, Playing: true, Title: "t", URI: "spotify:track:x", Duration: time.Minute}}}
	a := New()
	a.Music, a.Printer, a.Vol = m, &DemoPrinter{State_: octoprint.State{}}, DemoVolume(10)
	cv := gfx.New(W, H)

	for _, c := range []struct {
		name           string
		x0, y0, x1, y1 int
		next, prev     int
	}{
		{"left is next", 380, 220, 200, 230, 1, 0},
		{"right is previous", 100, 220, 300, 215, 1, 1},
		{"short move does nothing", 240, 220, 270, 220, 1, 1},
		{"vertical does nothing", 240, 120, 250, 330, 1, 1},
		{"outside the cover area (progress bar) does not swipe", 400, 532, 100, 532, 1, 1},
	} {
		a.Draw(cv)
		swipe(a, c.x0, c.y0, c.x1, c.y1)
		if m.next != c.next || m.prev != c.prev {
			t.Errorf("%s: next=%d prev=%d, want %d %d", c.name, m.next, m.prev, c.next, c.prev)
		}
	}
}
