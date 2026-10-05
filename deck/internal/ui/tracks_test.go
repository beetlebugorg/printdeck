package ui

import (
	"testing"
	"time"

	"speakerdeck/internal/gfx"
	"speakerdeck/internal/octoprint"
	"speakerdeck/internal/spotify"
	"speakerdeck/internal/touch"
)

type trackMusic struct {
	*DemoMusic
	played string
}

func (m *trackMusic) PlayTrack(ctx, track string) error {
	m.played = track
	return nil
}

func newTrackApp() (*App, *trackMusic, *gfx.Canvas) {
	m := &trackMusic{DemoMusic: &DemoMusic{State_: spotify.State{Authorized: true, Active: true, Title: "t", URI: "spotify:track:0", Duration: time.Minute}}}
	a := New()
	a.Music, a.Printer, a.Vol = m, &DemoPrinter{State_: octoprint.State{}}, DemoVolume(10)
	a.OpenTracks(0)
	return a, m, gfx.New(W, H)
}

func ev(a *App, k touch.Kind, x, y int) {
	a.handle(touch.Event{Kind: k, X: float64(x) / W, Y: float64(y) / H})
}

func TestTrackListTap(t *testing.T) {
	a, m, cv := newTrackApp()
	a.Draw(cv)
	// A tap on the third row (top 150, rows of 64) plays song 3.
	ev(a, touch.Down, 200, listTop+2*trackRowH+20)
	ev(a, touch.Up, 200, listTop+2*trackRowH+20)
	for i := 0; i < 100 && m.played == ""; i++ { // the song starts in its own goroutine
		time.Sleep(10 * time.Millisecond)
	}
	if m.played != "spotify:track:2" {
		t.Errorf("played %q", m.played)
	}
	if a.tracks != nil {
		t.Error("the list should close after a song is chosen")
	}
}

func TestTrackListScrollAndScrub(t *testing.T) {
	a, m, cv := newTrackApp()
	a.Draw(cv)
	// Dragging up by 200 pixels scrolls down by 200, and does not play a song.
	ev(a, touch.Down, 200, 500)
	ev(a, touch.Move, 200, 400)
	ev(a, touch.Move, 200, 300)
	ev(a, touch.Up, 200, 300)
	if a.tracks.scroll != 200 || m.played != "" {
		t.Errorf("scroll %d, played %q", a.tracks.scroll, m.played)
	}
	// The strip on the right: the middle of it is the middle of the list.
	a.Draw(cv)
	mid := (listTop + listBottom) / 2
	ev(a, touch.Down, W-30, mid)
	ev(a, touch.Up, W-30, mid)
	want := a.tracks.maxScroll() / 2
	if d := a.tracks.scroll - want; d < -trackRowH/2 || d > trackRowH/2 {
		t.Errorf("scrub to the middle gave %d, want about %d", a.tracks.scroll, want)
	}
	// The end of the strip is the end of the list.
	a.Draw(cv)
	ev(a, touch.Down, W-30, listBottom-1)
	ev(a, touch.Up, W-30, listBottom-1)
	if a.tracks.scroll < a.tracks.maxScroll()-trackRowH {
		t.Errorf("scrub to the end gave %d of %d", a.tracks.scroll, a.tracks.maxScroll())
	}
}
