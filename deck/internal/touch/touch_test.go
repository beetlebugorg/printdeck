package touch

import (
	"context"
	"math"
	"testing"
	"time"
)

func abs(ax uint16, v int32) rawEvent { return rawEvent{evAbs, ax, v} }
func key(v int32) rawEvent            { return rawEvent{evKey, btnTouch, v} }
func syn() rawEvent                   { return rawEvent{evSyn, 0, 0} }

// A swipe on the resistive panel: the contact is lost for a few milliseconds, many times.
// That must be one Down, the Moves, and one Up. A real lift of the finger must still end the touch.
func TestBounceIsOneTouch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	raw := make(chan rawEvent, 100)
	out := make(chan Event, 100)
	go process(ctx, raw, Options{}, [4]int32{0, 4095, 0, 4095}, out)

	send := func(es ...rawEvent) {
		for _, e := range es {
			raw <- e
		}
	}
	send(abs(absX, 1000), abs(absY, 2000), key(1), syn())
	for i := 0; i < 5; i++ { // five short losses of contact, 10 ms each, while the finger moves down
		send(key(0))
		time.Sleep(10 * time.Millisecond)
		send(key(1), abs(absX, 1000), abs(absY, int32(2100+i*100)), syn())
		time.Sleep(10 * time.Millisecond)
	}
	send(key(0)) // the real lift
	time.Sleep(200 * time.Millisecond)

	var kinds []Kind
	for len(out) > 0 {
		kinds = append(kinds, (<-out).Kind)
	}
	if len(kinds) < 3 || kinds[0] != Down || kinds[len(kinds)-1] != Up {
		t.Fatalf("events %v: want Down, Moves, Up", kinds)
	}
	for _, k := range kinds[1 : len(kinds)-1] {
		if k != Move {
			t.Fatalf("events %v: want only Moves between Down and Up", kinds)
		}
	}

	// A second touch after a real pause is a new touch.
	send(abs(absX, 500), abs(absY, 500), key(1), syn())
	time.Sleep(20 * time.Millisecond)
	send(key(0))
	time.Sleep(200 * time.Millisecond)
	kinds = nil
	for len(out) > 0 {
		kinds = append(kinds, (<-out).Kind)
	}
	if len(kinds) != 2 || kinds[0] != Down || kinds[1] != Up {
		t.Fatalf("second touch gave %v, want Down, Up", kinds)
	}
}

// The offset moves the touch in the UI, after the rotation, and the result stays on the screen.
func TestOffset(t *testing.T) {
	o := Options{Rotate: 270, OffsetX: 14. / 480, OffsetY: 11. / 800}
	// Raw (0.5, 0.25) is UI (0.75, 0.5) at rotation 270.
	e := adj(0.5, 0.25, o)
	if math.Abs(e.X-(0.75+14./480)) > 1e-12 || math.Abs(e.Y-(0.5+11./800)) > 1e-12 {
		t.Errorf("got %.6f,%.6f", e.X, e.Y)
	}
	// Raw x 1 is the bottom of the UI: the offset does not move it past the edge.
	if e := adj(1, 0.5, o); e.Y != 1 {
		t.Errorf("not clamped: %.6f", e.Y)
	}
}
