package ui

import (
	"context"
	"fmt"
	"sync"

	"printdeck/internal/gfx"
	"printdeck/internal/touch"
)

// Switcher shows the App of one printer of a deck on the touch screen at a time
// (specs/many-printers-on-one-deck.md). Each App keeps its pages, so a switch back shows the
// page as it was. The Apps share one wake channel and one queue of changes, so a change of any
// printer draws a new frame. The Switcher keeps the messages of the printers that are not on the
// screen: the end of a print, a pause, an error.
type Switcher struct {
	apps []*App
	ids  []Identity

	mu     sync.Mutex
	active int
	dir    int             // the side of the last switch, for the slide: -1 or 1, 0 when taken
	seen   map[string]bool // the messages that the user saw or closed
}

// NewSwitcher makes the Switcher of the printers ids, with the App of each one in apps. The
// first printer is on the screen.
func NewSwitcher(apps []*App, ids []Identity) *Switcher {
	s := &Switcher{apps: apps, ids: ids, seen: map[string]bool{}}
	wake, posted := make(chan struct{}, 1), make(chan func(), 64)
	for i, a := range apps {
		a.Fleet = &fleetView{s: s, i: i}
		a.wake, a.posted = wake, posted
	}
	// The ends of the prints from before the start show no banner.
	for i := range apps {
		for _, n := range s.notesOf(i) {
			s.seen[n.Key] = true
		}
	}
	return s
}

// Active returns the App on the screen.
func (s *Switcher) Active() *App {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.apps[s.active]
}

// Apps returns the App of each printer.
func (s *Switcher) Apps() []*App { return s.apps }

// Show shows the printer id on the screen.
func (s *Switcher) Show(id string) {
	for i, x := range s.ids {
		if x.ID == id {
			s.switchTo(i)
		}
	}
}

func (s *Switcher) switchTo(i int) {
	s.mu.Lock()
	if i == s.active {
		s.mu.Unlock()
		return
	}
	s.dir = 1
	if i < s.active {
		s.dir = -1
	}
	s.active = i
	s.mu.Unlock()
	// The messages of the printer that comes on the screen are seen.
	for _, n := range s.notesOf(i) {
		s.dismiss(n.Key)
	}
	s.apps[i].Redraw()
}

// takeDir returns the side of the last switch once.
func (s *Switcher) takeDir() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.dir
	s.dir = 0
	return d
}

func (s *Switcher) dismiss(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen[key] = true
}

func (s *Switcher) isSeen(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seen[key]
}

// notesOf returns the messages of the printer i, seen or not.
func (s *Switcher) notesOf(i int) []FleetNote {
	pr := s.apps[i].Printer
	if pr == nil {
		return nil
	}
	st := pr.State()
	id := s.ids[i].ID
	var out []FleetNote
	switch {
	case st.Err != "":
		out = append(out, FleetNote{ID: id, Key: id + ":error:" + st.Err, Text: "Error · " + st.Err})
	case st.Paused:
		out = append(out, FleetNote{ID: id, Key: fmt.Sprintf("%s:paused:%s:%d", id, st.FilePath, st.FilePos), Text: "Paused · " + st.Name()})
	case !st.Busy() && st.Finished == "complete":
		out = append(out, FleetNote{ID: id, Key: fmt.Sprintf("%s:done:%s:%d", id, st.FinishedPath, st.FinishedTime), Text: "Print done · " + st.FinishedName()})
	case !st.Busy() && st.Finished == "error":
		out = append(out, FleetNote{ID: id, Key: fmt.Sprintf("%s:failed:%s:%d", id, st.FinishedPath, st.FinishedTime), Text: "Print failed · " + st.FinishedName()})
	}
	return out
}

// Run draws the App on the screen and gives it the input, until ctx ends. A switch slides the
// new printer in from the side of its place in the list.
func (s *Switcher) Run(ctx context.Context, events <-chan touch.Event, present func(*gfx.Canvas)) {
	for _, a := range s.apps {
		a.hub.join(a)
		defer a.hub.leave(a)
	}
	runFrames(ctx, events, present, s.Active, s.takeDir)
}

// fleetView is the Fleet of the App of the printer i.
type fleetView struct {
	s *Switcher
	i int
}

func (v *fleetView) Self() Identity { return v.s.ids[v.i] }

func (v *fleetView) Cards() []FleetCard {
	out := make([]FleetCard, len(v.s.ids))
	for j, id := range v.s.ids {
		out[j] = FleetCard{Identity: id, Active: j == v.i}
		if pr := v.s.apps[j].Printer; pr != nil {
			out[j].State = pr.State()
		}
		for _, n := range v.s.notesOf(j) {
			if j != v.i && !v.s.isSeen(n.Key) {
				out[j].Note = n.Text
			}
		}
	}
	return out
}

func (v *fleetView) Notes() []FleetNote {
	var out []FleetNote
	for j := range v.s.ids {
		if j == v.i {
			continue
		}
		for _, n := range v.s.notesOf(j) {
			if !v.s.isSeen(n.Key) {
				out = append(out, n)
			}
		}
	}
	return out
}

func (v *fleetView) Dismiss(key string) { v.s.dismiss(key) }

func (v *fleetView) Switch(id string) { v.s.Show(id) }
