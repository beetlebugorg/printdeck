package ui

import (
	"testing"
	"time"

	"printdeck/internal/touch"
)

// A held key of the keyboard types once at the touch, then repeats after the delay. The lift
// types nothing more. A short tap types once.
func TestKeyRepeat(t *testing.T) {
	a, _, cv := newPrintApp(idle)
	now := time.Unix(1_800_000_000, 0)
	a.Now = func() time.Time { return now }
	var got string
	a.textKB = &keyboard{title: "Test", max: 100, ok: "Save", cancel: "Back", back: func() {}, done: func(v string) { got = v }}
	a.Draw(cv)
	r, ok := a.KeyRect("Key a")
	if !ok {
		t.Fatal("no key a")
	}
	x, y := (r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2
	// A short tap.
	ev(a, touch.Down, x, y)
	a.Draw(cv)
	ev(a, touch.Up, x, y)
	if a.textKB.value != "a" {
		t.Fatalf("tap: %q", a.textKB.value)
	}
	// A hold of 1 s: repeats from 0.4 s, each 70 ms.
	ev(a, touch.Down, x, y)
	for range 50 {
		a.Draw(cv)
		now = now.Add(20 * time.Millisecond)
	}
	ev(a, touch.Up, x, y)
	a.Draw(cv)
	n := len(a.textKB.value) - 1
	if n < 7 || n > 10 {
		t.Errorf("a hold of 1 s typed %d letters: %q", n, a.textKB.value)
	}
	_ = got
}
