// Package touch has the touch events of the deck UI. On Linux, it reads the touch controller of
// the deck (the AR1100) from evdev.
package touch

import (
	"context"
	"log"
	"time"
)

type Kind int

const (
	Down Kind = iota
	Move
	Up
	// Wheel is the wheel of a mouse on a remote screen: it scrolls the list at X, Y by DY.
	Wheel
)

// Event has coordinates in the range 0..1 of the panel. Finger is 0 for the first finger on the
// panel, and above 0 for each other finger of a multi-touch panel (a pinch). DY is the movement
// of a Wheel event, as a part of the height of the panel (positive is down).
type Event struct {
	Kind   Kind
	X, Y   float64
	Finger int
	DY     float64
}

type Options struct {
	InvertX, InvertY, Swap bool
	Rotate                 int // rotation of the UI on the panel: 0, 90, 180 or 270
	// OffsetX and OffsetY move each touch in the UI, as a fraction of its width and height. They
	// correct a panel that reads the touches beside the finger.
	OffsetX, OffsetY float64
	// Log writes each sample of a touch to the log: the raw panel values and the position in the
	// UI (0..1). It is for the measurement of the calibration.
	Log bool
}

const (
	evKey, evAbs, evSyn = 1, 3, 0
	absX, absY          = 0, 1
	btnTouch            = 0x14a
	// The multi-touch protocol B: the slot of the values that follow, the tracking ID of the
	// finger in it (-1 when it lifts), and its position.
	absMTSlot, absMTTracking, absMTX, absMTY = 0x2f, 0x39, 0x35, 0x36
)

// mtSlot is a finger of a multi-touch panel.
type mtSlot struct {
	x, y                    float64
	active, down, up, moved bool
}

// debounce is how long the finger must be off the panel before the touch is over. The resistive panel
// loses contact for a few milliseconds, many times, during a swipe. Without this, each of them is a tap.
// (Measured: gaps of 4 to 25 ms inside a touch, and at least 485 ms between separate taps.)
const debounce = 80 * time.Millisecond

type rawEvent struct {
	typ, code uint16
	val       int32
}

// process turns the raw events of the panel into Down, Move, and Up events. rng is the range of the axes:
// minX, maxX, minY, maxY.
func process(ctx context.Context, raw <-chan rawEvent, o Options, rng [4]int32, out chan<- Event) {
	minX, maxX, minY, maxY := rng[0], rng[1], rng[2], rng[3]
	var x, y float64
	var rawX, rawY int32
	touching, moved, pendingDown := false, false, false
	logSample := func(kind string, p Event) {
		if o.Log {
			log.Printf("touch %s raw=%d,%d ui=%.4f,%.4f", kind, rawX, rawY, p.X, p.Y)
		}
	}
	var release *time.Timer // running while the finger is off the panel, but the touch is not over yet
	var releaseC <-chan time.Time
	// The newest move that the UI did not take yet. A busy UI gets the last position when it is
	// ready. It does not get each sample. A list then follows the finger, and a swipe is kept.
	var waiting Event
	var waitC chan<- Event
	// The fingers of a multi-touch panel. The first finger is the touch of the single-touch
	// events above; each other finger is an Event with Finger above 0.
	slots := map[int32]*mtSlot{}
	var cur int32
	primary := int32(-1)
	slot := func() *mtSlot {
		if slots[cur] == nil {
			slots[cur] = &mtSlot{}
		}
		return slots[cur]
	}
	for {
		select {
		case e, ok := <-raw:
			if !ok {
				return
			}
			switch {
			case e.typ == evAbs && e.code == absX:
				x, moved, rawX = float64(e.val-minX)/float64(max(1, maxX-minX)), true, e.val
			case e.typ == evAbs && e.code == absY:
				y, moved, rawY = float64(e.val-minY)/float64(max(1, maxY-minY)), true, e.val
			case e.typ == evAbs && e.code == absMTSlot:
				cur = e.val
			case e.typ == evAbs && e.code == absMTTracking:
				sl := slot()
				if e.val >= 0 {
					sl.active, sl.down = true, true
					if primary < 0 {
						primary = cur
					}
				} else {
					sl.active, sl.up = false, true
					if cur == primary {
						primary = -1 // the next finger down is the first finger
						delete(slots, cur)
					}
				}
			case e.typ == evAbs && e.code == absMTX:
				sl := slot()
				sl.x, sl.moved = float64(e.val-minX)/float64(max(1, maxX-minX)), true
			case e.typ == evAbs && e.code == absMTY:
				sl := slot()
				sl.y, sl.moved = float64(e.val-minY)/float64(max(1, maxY-minY)), true
			case e.typ == evKey && e.code == btnTouch:
				if e.val == 1 {
					if releaseC != nil { // the finger came back: it is the same touch
						release.Stop()
						release, releaseC = nil, nil
					} else if !touching {
						// The position arrives before the SYN event, so Down is sent there.
						touching, moved, pendingDown = true, true, true
					}
				} else if touching && releaseC == nil {
					release = time.NewTimer(debounce)
					releaseC = release.C
				}
			case e.typ == evSyn && e.code == 0 && len(slots) > 0 && multiTouch(slots, primary, o, out):
				// The other fingers of a multi-touch panel went out; the first finger follows.
				if moved && touching {
					p := adj(x, y, o)
					select {
					case out <- Event{Kind: Move, X: p.X, Y: p.Y}:
					default:
					}
					moved = false
				}
			case e.typ == evSyn && e.code == 0 && moved && touching:
				p := adj(x, y, o)
				if pendingDown {
					pendingDown = false
					logSample("down", p)
					out <- Event{Kind: Down, X: p.X, Y: p.Y}
				} else {
					logSample("move", p)
					select {
					case out <- Event{Kind: Move, X: p.X, Y: p.Y}:
						waitC = nil
					default: // the UI is busy: keep the newest move for later
						waiting, waitC = Event{Kind: Move, X: p.X, Y: p.Y}, out
					}
				}
				moved = false
			}
		case waitC <- waiting:
			waitC = nil
		case <-releaseC:
			touching, release, releaseC, waitC = false, nil, nil, nil // Up has the last position
			p := adj(x, y, o)
			logSample("up", p)
			out <- Event{Kind: Up, X: p.X, Y: p.Y}
		case <-ctx.Done():
			return
		}
	}
}

func adj(x, y float64, o Options) Event {
	if o.Swap {
		x, y = y, x
	}
	if o.InvertX {
		x = 1 - x
	}
	if o.InvertY {
		y = 1 - y
	}
	// The raw position is in the orientation of the panel. Turn it to the UI orientation.
	switch o.Rotate {
	case 90:
		x, y = y, 1-x
	case 270:
		x, y = 1-y, x
	case 180:
		x, y = 1-x, 1-y
	}
	x += o.OffsetX
	y += o.OffsetY
	return Event{X: max(0, min(1, x)), Y: max(0, min(1, y))}
}

// multiTouch sends the events of the fingers of a multi-touch panel that are not the first
// finger, at the end of a frame of the panel (SYN). It reports true when it sent one.
func multiTouch(slots map[int32]*mtSlot, primary int32, o Options, out chan<- Event) bool {
	sent := false
	for id, sl := range slots {
		if id == primary {
			sl.down, sl.up, sl.moved = false, false, false
			continue
		}
		p := adj(sl.x, sl.y, o)
		finger := int(id) + 1
		switch {
		case sl.down:
			out <- Event{Kind: Down, X: p.X, Y: p.Y, Finger: finger}
			sent = true
		case sl.up:
			out <- Event{Kind: Up, X: p.X, Y: p.Y, Finger: finger}
			sent = true
		case sl.moved && sl.active:
			select {
			case out <- Event{Kind: Move, X: p.X, Y: p.Y, Finger: finger}:
			default: // the UI is busy: the next move has the newer place
			}
			sent = true
		}
		sl.down, sl.up, sl.moved = false, false, false
		if !sl.active {
			delete(slots, id)
		}
	}
	return sent
}
