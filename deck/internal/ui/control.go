package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"printdeck/kit"
	"strings"
	"sync"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// The Toolhead page: home, arrow keys for X, Y and Z with one step for all axes, park and
// center, motors.

const minExtrudeTemp = 170 // Klipper blocks extrusion below this temperature

// jogSteps are the steps of the arrow keys of the Toolhead page, in mm.
var jogSteps = []float64{0.1, 0.5, 1, 10}

// The feeds of the arrow keys, in mm/min, and the repeat of a held arrow key: the wait before
// the first repeat, and the shortest time between two repeats.
const (
	jogFeedXY, jogFeedZ       = 6000, 600
	jogRepeatDelay, jogRepeat = 400 * time.Millisecond, 150 * time.Millisecond
)

// jogHold is the arrow key that a finger holds. A tap sends one step. A held key sends one step
// more after jogRepeatDelay, then one step each jogRepeat, or each time that one step needs at
// its feed, if longer. A goroutine sends the steps one at a time, so the moves cannot pile up.
type jogHold struct {
	mu      sync.Mutex
	d       [3]float64 // the step in X, Y and Z
	feed    int
	since   time.Time
	pending bool // a step of the press waits
	held    bool
	running bool
}

// jogPad is the touch of an arrow key with the step d.
func (a *App) jogPad(d [3]float64, feed int) func(image.Point, kit.Phase) {
	return func(_ image.Point, phase kit.Phase) {
		h := a.jog
		h.mu.Lock()
		defer h.mu.Unlock()
		switch phase {
		case kit.PadUp:
			h.held = false
			return
		case kit.PadMove:
			return
		}
		h.d, h.feed, h.since, h.pending, h.held = d, feed, time.Now(), true, true
		if !h.running {
			h.running = true
			go a.jogLoop()
		}
	}
}

func (a *App) jogLoop() {
	h := a.jog
	for {
		h.mu.Lock()
		d, feed, since := h.d, h.feed, h.since
		send := h.pending
		h.pending = false
		if !send {
			if !h.held {
				h.running = false
				h.mu.Unlock()
				return
			}
			send = time.Since(since) >= jogRepeatDelay
		}
		h.mu.Unlock()
		wait := jogRepeat
		if send {
			if err := a.Printer.Jog(d[0], d[1], d[2], feed); err != nil {
				a.Toast(err.Error())
				h.mu.Lock()
				h.held, h.pending, h.running = false, false, false
				h.mu.Unlock()
				return
			}
			dist := math.Abs(d[0]) + math.Abs(d[1]) + math.Abs(d[2])
			wait = max(wait, time.Duration(dist/float64(feed)*float64(time.Minute)))
		}
		time.Sleep(wait)
	}
}

// do runs a printer command in the background and shows its error.
func (a *App) do(f func() error) {
	go func() {
		if err := f(); err != nil {
			a.Toast(err.Error())
		}
	}()
}

// drawMove is the Toolhead page: the position and the homed axes, homing, the arrow keys for X, Y
// and Z with their step, park and center, and the motors.
func (a *App) drawMove(c *gfx.Canvas) {
	pr := a.Printer
	st := pr.State()
	f := a.navPage(c, "Toolhead")
	moving := st.Connected && (!st.Busy() || st.Paused)

	// Tall: the position, Home, the arrow keys with the Z column, the steps, the park keys, and a
	// hint. Wide: the arrow keys at the left; Home, the steps and Z at their right.
	var pos, home, steps, cross, zUp, zDown, parks, hint image.Rectangle
	if f.wide {
		content, acts := f.split()
		r := kit.Rows(content, 10, kit.Px(44), kit.Fr(1), kit.Px(20))
		pos, parks, hint = r[0], acts, r[2]
		side := min(r[1].Dy(), r[1].Dx()/2)
		cols := kit.Cols(r[1], 16, kit.Px(side), kit.Fr(1))
		cross = cols[0]
		k := kit.Rows(cols[1], 10, kit.Px(52), kit.Px(52), kit.Px(52))
		home, steps = k[0], k[1]
		z := kit.Cols(k[2], 8, kit.Fr(1), kit.Fr(1))
		zDown, zUp = z[0], z[1]
	} else {
		r := kit.Rows(kit.Inset(f.body(), 0, 10, 0, 0), 12, kit.Px(54), kit.Px(58), kit.Fr(1), kit.Px(60), kit.Px(70), kit.Px(24))
		pos, home, steps, parks, hint = r[0], r[1], r[3], r[4], r[5]
		cols := kit.Cols(r[2], 16, kit.Fr(3), kit.Fr(1))
		cross = cols[0]
		z := kit.Rows(cols[1], 12, kit.Fr(1), kit.Fr(1))
		zUp, zDown = z[0], z[1]
	}

	// The position, with an LED for each homed axis.
	window(c, pos)
	a.region("Position", pos)
	axes := kit.Cols(kit.Inset(pos, 14, 0, 0, 0), 0, kit.Fr(1), kit.Fr(1), kit.Fr(1))
	paused := st.Busy() && st.Paused
	for i, ax := range []struct {
		n string
		v float64
		f string
	}{{"X", st.X, "%.1f"}, {"Y", st.Y, "%.1f"}, {"Z", st.Z, "%.2f"}} {
		r := axes[i]
		homedAx := strings.Contains(st.Homed, strings.ToLower(ax.n))
		led(c, r.Min.X+8, r.Min.Y+r.Dy()/2, homedAx)
		c.Text(kit.Inset(r, 22, 0, 0, 0), ax.n+" "+fmt.Sprintf(ax.f, ax.v), gfx.Face(true, 22), lcdInk, gfx.Left)
		// A tap on the axis opens a keypad for a new position of that axis.
		if !moving {
			continue
		}
		a.region("Position "+ax.n, r)
		a.addHit(kit.Hit{R: r, Tap: func() {
			switch {
			case paused:
				a.Toast("Not during a pause")
			case !homedAx:
				a.Toast("Home " + ax.n + " first")
			default:
				a.editPosition("Move "+ax.n+" to", ax.v, st.Axis[0][i], st.Axis[1][i], func(v float64) error {
					return pr.Gcode(MoveGcode(i, v)...)
				})
			}
		}})
	}

	btn := func(r image.Rectangle, label string, fill, fg color.RGBA, f func() error) {
		if !moving {
			key(c, r, label, surface, dim, false)
			return
		}
		a.button(c, r, label, fill, fg, func() { a.do(f) })
	}
	// During a pause, Home, Center, Motors off and a typed position are off: homing Z, Center
	// and a typed position can move the nozzle into the part, and Motors off loses the position
	// that Resume goes back to.
	notPaused := func(r image.Rectangle, label string, fill, fg color.RGBA, f func() error) {
		if !paused {
			btn(r, label, fill, fg, f)
			return
		}
		key(c, r, label, surface, dim, false)
		a.region(label, r)
		a.addHit(kit.Hit{R: r, Tap: func() { a.Toast("Not during a pause") }})
	}

	// Home
	h := labelCols(home, 8, "Home all", "X", "Y", "Z") // "Home all" needs the room
	notPaused(h[0], "Home all", orange, onAcc, func() error { return pr.Home() })
	for i, ax := range []string{"X", "Y", "Z"} {
		axis := strings.ToLower(ax)
		notPaused(h[i+1], ax, raised, text, func() error { return pr.Home(axis) })
	}

	// The arrow keys: X and Y in a cross, Z in a column, all with the step of the step row.
	a.drawArrows(c, cross, zUp, zDown, moving)

	// The step of the arrow keys.
	a.region("Steps", steps)
	for i, r := range kit.Cols(steps, 6, kit.Fr(1), kit.Fr(1), kit.Fr(1), kit.Fr(1)) {
		s := jogSteps[i]
		fill, fg := keyOff, text
		if a.jogStep == s {
			fill, fg = keyOn, keyOnInk
		}
		key(c, r, fmt.Sprint(s), fill, fg, a.isPressed(r))
		a.region(fmt.Sprint(s), r)
		a.addHit(kit.Hit{R: r, Tap: func() { a.jogStep = s }})
	}

	// Park and center need the homed axes. Motors off lets the head move by hand.
	homed := strings.Contains(st.Homed, "x") && strings.Contains(st.Homed, "y")
	park := func(r image.Rectangle, name string, g func() []string) {
		if !homed {
			btn(r, name, raised, text, func() error { return fmt.Errorf("Home X and Y first") })
			return
		}
		btn(r, name, raised, text, func() error { return pr.Gcode(g()...) })
	}
	p := kit.Cols(parks, 8, kit.Fr(1), kit.Fr(1), kit.Fr(1))
	park(p[0], "Park", func() []string { return ParkGcode(st) })
	if paused {
		notPaused(p[1], "Center", raised, text, nil)
	} else {
		park(p[1], "Center", func() []string { return CenterGcode(st) })
	}
	notPaused(p[2], "Motors off", raised, text, pr.MotorsOff)
	msg := "Tap X, Y or Z above to type a position."
	switch {
	case !st.Connected:
		msg = "The printer is not connected."
	case paused:
		msg = "Park lifts the nozzle 10 mm and moves it away."
	case st.Busy():
		msg = "Moving is off while a print runs."
	}
	label(c, hint, msg, gfx.Center)
}

// drawArrows draws the arrow keys of X and Y in a cross in the square in the middle of cross,
// and the keys Z up and Z down. The step is in the middle of the cross. A key takes touches
// only while the head can move. Up on the screen is Y+, or Y- with JogInvertY.
func (a *App) drawArrows(c *gfx.Canvas, cross, zUp, zDown image.Rectangle, moving bool) {
	side := min(cross.Dx(), cross.Dy())
	sq := kit.Center(cross, side, side)
	g := kit.Grid(sq, 3, 3, 8)
	up, down := "Y+", "Y−"
	dy := a.jogStep
	if a.JogInvertY {
		up, down, dy = down, up, -dy
	}
	s := a.jogStep
	for _, k := range []struct {
		r           image.Rectangle
		label, name string
		d           [3]float64
		feed        int
	}{
		{g[1], up, up, [3]float64{0, dy, 0}, jogFeedXY},
		{g[3], "X−", "X−", [3]float64{-s, 0, 0}, jogFeedXY},
		{g[5], "X+", "X+", [3]float64{s, 0, 0}, jogFeedXY},
		{g[7], down, down, [3]float64{0, -dy, 0}, jogFeedXY},
		{zUp, "Z ▲", "Z up", [3]float64{0, 0, s}, jogFeedZ},
		{zDown, "Z ▼", "Z down", [3]float64{0, 0, -s}, jogFeedZ},
	} {
		if !moving {
			key(c, k.r, k.label, surface, dim, false)
			continue
		}
		key(c, k.r, k.label, raised, text, a.isPressed(k.r))
		a.region(k.name, k.r)
		a.addHit(kit.Hit{R: k.r, Pad: a.jogPad(k.d, k.feed)})
	}
	label(c, g[4], fmt.Sprint(s)+" mm", gfx.Center)
}

// ParkGcode moves the head to the back left, 10 mm up, out of the way of the part.
func ParkGcode(st printer.State) []string {
	z := min(st.Z+10, st.Axis[1][2])
	return []string{"G90", fmt.Sprintf("G1 Z%.2f F600", z), fmt.Sprintf("G1 X%.1f Y%.1f F6000", st.Axis[0][0]+5, st.Axis[1][1]-5)}
}

// MoveGcode moves one axis (0 X, 1 Y, 2 Z) to the position v in mm.
func MoveGcode(axis int, v float64) []string {
	feed := []int{6000, 6000, 600}[axis]
	return []string{"G90", fmt.Sprintf("G1 %s%.2f F%d", []string{"X", "Y", "Z"}[axis], v, feed)}
}

// CenterGcode moves the head to the middle of the bed.
func CenterGcode(st printer.State) []string {
	return []string{"G90", fmt.Sprintf("G1 X%.1f Y%.1f F6000", (st.Axis[0][0]+st.Axis[1][0])/2, (st.Axis[0][1]+st.Axis[1][1])/2)}
}
