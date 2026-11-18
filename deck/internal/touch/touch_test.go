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

// A busy UI does not lose the moves of a swipe: it gets the newest position when it reads again.
func TestBusyUIGetsTheNewestMove(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	raw := make(chan rawEvent, 100)
	out := make(chan Event) // no buffer: the UI reads only when it is ready
	go process(ctx, raw, Options{}, [4]int32{0, 4095, 0, 4095}, out)

	raw <- abs(absX, 1000)
	raw <- abs(absY, 1000)
	raw <- key(1)
	raw <- syn()
	if e := <-out; e.Kind != Down {
		t.Fatalf("first event %v, want Down", e.Kind)
	}
	// The UI draws a slow frame while the finger moves down the panel.
	for y := int32(1100); y <= 3000; y += 100 {
		raw <- abs(absY, y)
		raw <- syn()
	}
	time.Sleep(20 * time.Millisecond)
	e := <-out
	if e.Kind != Move || math.Abs(e.Y-3000./4095) > 1e-9 {
		t.Fatalf("after the slow frame: %v at y %.4f, want the Move at %.4f", e.Kind, e.Y, 3000./4095)
	}
	raw <- key(0)
	if e := <-out; e.Kind != Up {
		t.Fatalf("last event %v, want Up", e.Kind)
	}
}

// A pinch on a multi-touch panel: the first finger is the touch as before, and the second finger
// is Down, Move and Up events with Finger 2.
func TestPinch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	raw := make(chan rawEvent, 100)
	out := make(chan Event, 100)
	go process(ctx, raw, Options{}, [4]int32{0, 1000, 0, 1000}, out)
	send := func(es ...rawEvent) {
		for _, e := range es {
			raw <- e
		}
	}
	// The first finger, in slot 0, with the single-touch events too.
	send(abs(absMTSlot, 0), abs(absMTTracking, 7), abs(absMTX, 400), abs(absMTY, 500), abs(absX, 400), abs(absY, 500), key(1), syn())
	// The second finger, in slot 1.
	send(abs(absMTSlot, 1), abs(absMTTracking, 8), abs(absMTX, 600), abs(absMTY, 500), syn())
	// Both fingers move apart.
	send(abs(absMTSlot, 0), abs(absMTX, 300), abs(absX, 300), abs(absMTSlot, 1), abs(absMTX, 700), syn())
	// The second finger lifts, then the first.
	send(abs(absMTSlot, 1), abs(absMTTracking, -1), syn())
	send(abs(absMTSlot, 0), abs(absMTTracking, -1), key(0), syn())
	time.Sleep(200 * time.Millisecond)
	var got []Event
	for len(out) > 0 {
		got = append(got, <-out)
	}
	var first, second []Kind
	for _, e := range got {
		if e.Finger == 0 {
			first = append(first, e.Kind)
		} else if e.Finger == 2 {
			second = append(second, e.Kind)
		} else {
			t.Errorf("finger %d", e.Finger)
		}
	}
	if len(first) < 2 || first[0] != Down || first[len(first)-1] != Up {
		t.Errorf("first finger: %v", first)
	}
	if len(second) != 3 || second[0] != Down || second[1] != Move || second[2] != Up {
		t.Errorf("second finger: %v (all %+v)", second, got)
	}
	for _, e := range got {
		if e.Finger == 2 && e.Kind == Move && math.Abs(e.X-0.7) > 0.01 {
			t.Errorf("second finger at %.2f, want 0.7", e.X)
		}
	}
}
