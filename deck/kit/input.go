package kit

import (
	"image"
	"math"
	"time"
)

// Kind is the kind of a touch event.
type Kind int

const (
	Down Kind = iota // the finger touches the screen
	Move             // the finger moves
	Up               // the finger lifts
)

// Event is a touch of the finger at P, in the pixels of the frame, at the time At. An event with no
// time gives a list no fling.
type Event struct {
	Kind Kind
	P    image.Point
	At   time.Time
}

// Phase is the part of a touch that a pad gets.
type Phase int

const (
	PadDown Phase = iota
	PadMove
	PadUp
)

// Hit is a touch area of the frame. Set the field of one kind of area. A Hit with none of them
// blocks the touches under it.
type Hit struct {
	R Rect

	// Tap is a key: it runs when the finger lifts in the area, or near it.
	Tap func()

	// Drag and End are a slider: Drag runs while the finger moves, End when it lifts, with the
	// position from 0 to 1. Vertical makes the slider follow the finger up and down.
	Drag, End func(frac float64)
	Vertical  bool

	// Swipe runs after a horizontal swipe: 1 for a swipe to the left (next), -1 to the right.
	Swipe func(dir int)

	// Scroll is a list: it runs while the finger drags, with the movement in pixels (positive is
	// down). A touch that does not move runs TapAt with the position instead.
	Scroll func(dy int)
	TapAt  func(p image.Point)

	// Pad follows the finger from Down to Up, for a joystick.
	Pad func(p image.Point, phase Phase)
}

// The limits of the gestures, in pixels.
const (
	tapSlop    = 24 // a key still takes the tap when the finger lifts this far outside it
	scrollSlop = 12 // a list scrolls after the finger moves this far
	swipeMin   = 70 // the shortest swipe
	swipeShow  = 80 // the swipe offset that the program shows at most
)

// The fling of a list: a list that the finger flicks goes on, slower and slower.
const (
	flingMin   = 250                    // the least speed of a flick, in pixels a second
	flingMax   = 4000                   // the most speed of a fling
	flingStop  = 40                     // a fling stops under this speed
	flingDecay = 0.35                   // the time constant of the slowing, in seconds
	flingLift  = 150 * time.Millisecond // the longest time from the last move to the lift (the touch panel waits 80 ms)
)

// Input has the touch areas of the frame, and follows the finger on them. The program calls Reset
// at the start of each frame, Add for each area while it draws, and Handle for each event.
// The last area that it adds is on top.
type Input struct {
	hits []Hit

	downAt      image.Point
	pressed     *Hit // a key under the finger
	dragging    *Hit // a slider
	dragFrac    float64
	scrolling   *Hit // a list
	lastY       int
	scrollMoved bool
	lastAt      time.Time // the time of the last move of the list
	scrollV     float64   // the speed of the list, in pixels a second
	fling       *Hit      // a list that goes on after a flick
	flingV      float64
	flingAt     time.Time
	flingRest   float64 // the part of a pixel that the fling has not moved yet
	swiping     *Hit
	swipeDX     int
	padding     *Hit
}

// Reset removes all the areas, at the start of a frame, or under a dialog that blocks them.
func (in *Input) Reset() { in.hits = in.hits[:0] }

// Add adds a touch area on top of the others.
func (in *Input) Add(h Hit) { in.hits = append(in.hits, h) }

// Hits returns the areas of the frame, the top last.
func (in *Input) Hits() []Hit { return in.hits }

// Pressed reports that the finger is on the key with the rectangle r.
func (in *Input) Pressed(r Rect) bool { return in.pressed != nil && in.pressed.R == r }

// Dragging returns the position (0 to 1) of the finger on the slider with the rectangle r, while
// it drags.
func (in *Input) Dragging(r Rect) (float64, bool) {
	if in.dragging != nil && in.dragging.R == r {
		return in.dragFrac, true
	}
	return 0, false
}

// SwipeOffset is how far the finger is in a swipe now, from -80 to 80 pixels.
func (in *Input) SwipeOffset() int { return in.swipeDX }

// Handle follows one event. The areas are those of the last frame: an area that is gone when the
// finger lifts does nothing, except a pad, which always gets the end of the touch.
func (in *Input) Handle(ev Event) {
	p := ev.P
	switch ev.Kind {
	case Down:
		in.downAt = p
		in.fling = nil // the finger stops a fling
		in.scrollV, in.lastAt = 0, ev.At
		for i := len(in.hits) - 1; i >= 0; i-- {
			h := in.hits[i] // the list changes on each frame, so keep a copy
			if !p.In(h.R) {
				continue
			}
			switch {
			case h.Pad != nil:
				in.padding = &h
				h.Pad(p, PadDown)
			case h.Swipe != nil:
				in.swiping, in.swipeDX = &h, 0
			case h.Scroll != nil:
				in.scrolling, in.lastY, in.scrollMoved = &h, p.Y, false
			case h.Drag != nil:
				in.dragging, in.dragFrac = &h, frac(&h, p)
				h.Drag(in.dragFrac)
			default:
				in.pressed = &h
			}
			break
		}
	case Move:
		switch {
		case in.padding != nil:
			if h := in.find(in.padding.R); h != nil {
				h.Pad(p, PadMove)
			}
		case in.scrolling != nil:
			if abs(p.Y-in.downAt.Y) > scrollSlop {
				in.scrollMoved = true
			}
			if in.scrollMoved {
				if h := in.find(in.scrolling.R); h != nil {
					h.Scroll(in.lastY - p.Y)
				}
			}
			// The speed, smoothed over the last moves.
			if dt := ev.At.Sub(in.lastAt).Seconds(); !ev.At.IsZero() && !in.lastAt.IsZero() && dt > 0 {
				in.scrollV = 0.7*float64(in.lastY-p.Y)/dt + 0.3*in.scrollV
			}
			in.lastY, in.lastAt = p.Y, ev.At
		case in.swiping != nil:
			in.swipeDX = max(-swipeShow, min(swipeShow, p.X-in.downAt.X))
		case in.dragging != nil:
			in.dragFrac = frac(in.dragging, p)
			if h := in.find(in.dragging.R); h != nil {
				h.Drag(in.dragFrac)
			}
		case in.pressed != nil && !p.In(in.pressed.R.Inset(-tapSlop)):
			in.pressed = nil
		}
	case Up:
		switch {
		case in.padding != nil:
			// The finger may lift after the screen changed: the joystick must stop all the same.
			in.padding.Pad(p, PadUp)
			in.padding = nil
		case in.scrolling != nil:
			h, moved := in.find(in.scrolling.R), in.scrollMoved
			// The touch panel drops the moves while the program draws a slow frame. A finger that
			// lifts far from where it went down scrolled, even with no move: it is never a tap.
			if !moved && abs(p.Y-in.downAt.Y) > scrollSlop {
				moved = true
				if h != nil {
					h.Scroll(in.lastY - p.Y)
				}
				in.lastY = p.Y
			}
			if moved && !ev.At.IsZero() && ev.At.Sub(in.lastAt) < flingLift && math.Abs(in.scrollV) > flingMin {
				in.fling, in.flingV, in.flingAt, in.flingRest = in.scrolling, max(-flingMax, min(flingMax, in.scrollV)), ev.At, 0
			}
			in.scrolling = nil
			if h != nil && !moved && h.TapAt != nil {
				h.TapAt(p)
			}
		case in.swiping != nil:
			dx, dy := p.X-in.downAt.X, p.Y-in.downAt.Y
			h := in.find(in.swiping.R)
			in.swiping, in.swipeDX = nil, 0
			if h != nil && h.Swipe != nil && abs(dx) >= swipeMin && abs(dx) > 2*abs(dy) {
				if dx < 0 {
					h.Swipe(1)
				} else {
					h.Swipe(-1)
				}
			}
		case in.dragging != nil:
			if h := in.find(in.dragging.R); h != nil && h.End != nil {
				h.End(frac(h, p))
			}
			in.dragging, in.dragFrac = nil, 0
		case in.pressed != nil && p.In(in.pressed.R.Inset(-tapSlop)):
			h := in.find(in.pressed.R)
			in.pressed = nil
			if h != nil && h.Tap != nil {
				h.Tap()
			}
		}
		in.pressed = nil
	}
}

// Fling moves a list that the finger flicked, up to the time now, and slows it. It reports that the
// list still moves: the program then draws the next frame soon. Call it before Reset, so that it
// finds the list of the last frame.
func (in *Input) Fling(now time.Time) bool {
	if in.fling == nil {
		return false
	}
	h := in.find(in.fling.R)
	if h == nil || h.Scroll == nil {
		in.fling = nil
		return false
	}
	dt := now.Sub(in.flingAt).Seconds()
	if dt <= 0 {
		return true
	}
	in.flingAt = now
	in.flingRest += in.flingV * dt
	if step := int(in.flingRest); step != 0 {
		in.flingRest -= float64(step)
		h.Scroll(step)
	}
	in.flingV *= math.Exp(-dt / flingDecay)
	if math.Abs(in.flingV) < flingStop {
		in.fling = nil
		return false
	}
	return true
}

// find returns the area of the current frame with the rectangle r.
func (in *Input) find(r Rect) *Hit {
	for i := len(in.hits) - 1; i >= 0; i-- {
		if in.hits[i].R == r {
			return &in.hits[i]
		}
	}
	return nil
}

// frac is where the finger is on a slider, from 0 to 1.
func frac(h *Hit, p image.Point) float64 {
	if h.Vertical {
		return max(0, min(1, float64(p.Y-h.R.Min.Y)/float64(h.R.Dy())))
	}
	return max(0, min(1, float64(p.X-h.R.Min.X)/float64(h.R.Dx())))
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
