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

// The Move page: home, move the head with a joystick, Z, park and center, motors.

const (
	joyR           = 128  // the radius of the joystick pad of the Move page
	joyDead        = 0.15 // the middle part that does not move
	joyTick        = 150 * time.Millisecond
	joyMinSpeed    = 2.0  // mm/s just outside the middle
	joyMaxSpeed    = 50.0 // mm/s at the edge
	minExtrudeTemp = 170  // Klipper blocks extrusion below this temperature
)

// joystick moves the head while a finger is on the pad. A goroutine sends one short move per tick; the next
// move is sent when the last one is accepted, so moves cannot pile up behind a slow connection.
type joystick struct {
	mu      sync.Mutex
	vx, vy  float64 // -1..1, up is +1
	held    bool    // a finger is on the pad
	running bool    // the goroutine runs
}

func (j *joystick) get() (vx, vy float64, held bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.vx, j.vy, j.held
}

// joySpeed is the speed in mm/s for a deflection of 0..1; 0 in the middle.
func joySpeed(mag float64) float64 {
	if mag < joyDead {
		return 0
	}
	f := (min(mag, 1) - joyDead) / (1 - joyDead)
	return joyMinSpeed + f*f*(joyMaxSpeed-joyMinSpeed)
}

// joyPad follows the finger on the pad.
func (a *App) joyPad(p image.Point, phase kit.Phase) {
	j := a.joy
	pad := a.joyArea // the pad of the last frame
	cx, cy, r := pad.Min.X+pad.Dx()/2, pad.Min.Y+pad.Dy()/2, float64(pad.Dx()/2)
	vx := float64(p.X-cx) / r
	vy := float64(cy-p.Y) / r
	if m := math.Hypot(vx, vy); m > 1 {
		vx, vy = vx/m, vy/m
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if phase == kit.PadUp {
		j.vx, j.vy, j.held = 0, 0, false
		return
	}
	j.vx, j.vy, j.held = vx, vy, true
	if !j.running {
		j.running = true
		go a.joyLoop()
	}
}

func (a *App) joyLoop() {
	j := a.joy
	for {
		start := time.Now()
		j.mu.Lock()
		vx, vy, held := j.vx, j.vy, j.held
		if !held {
			j.running = false
			j.mu.Unlock()
			return
		}
		j.mu.Unlock()

		mag := math.Hypot(vx, vy)
		if v := joySpeed(mag); v > 0 {
			d := v * joyTick.Seconds()
			dy := vy / mag * d
			if a.JogInvertY {
				dy = -dy
			}
			if err := a.Printer.Jog(vx/mag*d, dy, 0, int(v*60)); err != nil {
				a.Toast(err.Error())
				j.mu.Lock()
				j.vx, j.vy, j.held, j.running = 0, 0, false, false
				j.mu.Unlock()
				return
			}
		}
		time.Sleep(joyTick - time.Since(start))
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

// drawMove is the Move page: the position and the homed axes, homing, the joystick for X and Y,
// Z, park and center, and the motors. A wide screen has the position and the keys in the left
// column, and the joystick and Z at the right.
func (a *App) drawMove(c *gfx.Canvas) {
	pr := a.Printer
	st := pr.State()
	f := a.page(c, "Move")
	moving := st.Connected && (!st.Busy() || st.Paused)

	// Tall: the position, Home, the joystick with the Z column, the park keys, and a hint. Wide: the
	// position, Home and a Z row in the left column; a large joystick and the park keys at the right.
	var pos, home, zArea, pad, parks, hint image.Rectangle
	if f.wide {
		k := kit.Rows(f.aside(), 10, kit.Px(44), kit.Px(52), kit.Px(52))
		pos, home, zArea = k[0], k[1], k[2]
		r := kit.Rows(f.body(), 8, kit.Fr(1), kit.Px(20), kit.Px(58))
		side := min(r[0].Dx(), r[0].Dy())
		pad, hint, parks = kit.Center(r[0], side, side), r[1], r[2]
	} else {
		r := kit.Rows(kit.Inset(f.body(), 0, 10, 0, 0), 12, kit.Px(54), kit.Px(58), kit.Fr(1), kit.Px(60), kit.Px(70), kit.Px(24))
		pos, home, zArea, parks, hint = r[0], r[1], r[3], r[4], r[5]
		// The joystick as large as the space allows, in the middle; the Z row under it.
		side := min(r[2].Dx(), r[2].Dy()-26)
		pad = kit.Center(kit.Rows(r[2], 0, kit.Px(side))[0], side, side)
	}

	// The position, with an LED for each homed axis.
	window(c, pos)
	a.region("Position", pos)
	axes := kit.Cols(kit.Inset(pos, 14, 0, 0, 0), 0, kit.Fr(1), kit.Fr(1), kit.Fr(1))
	for i, ax := range []struct {
		n string
		v float64
		f string
	}{{"X", st.X, "%.1f"}, {"Y", st.Y, "%.1f"}, {"Z", st.Z, "%.2f"}} {
		r := axes[i]
		led(c, r.Min.X+8, r.Min.Y+r.Dy()/2, strings.Contains(st.Homed, strings.ToLower(ax.n)))
		c.Text(kit.Inset(r, 22, 0, 0, 0), ax.n+" "+fmt.Sprintf(ax.f, ax.v), gfx.Face(true, 22), lcdInk, gfx.Left)
	}

	btn := func(r image.Rectangle, label string, fill, fg color.RGBA, f func() error) {
		if !moving {
			key(c, r, label, surface, dim, false)
			return
		}
		a.button(c, r, label, fill, fg, func() { a.do(f) })
	}
	// During a pause, Home, Center and Motors off are off: homing Z and Center move the nozzle
	// into the part, and Motors off loses the position that Resume goes back to.
	paused := st.Busy() && st.Paused
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
	h := kit.Cols(home, 8, kit.Fr(1), kit.Fr(1), kit.Fr(1), kit.Fr(1))
	notPaused(h[0], "Home all", orange, onAcc, func() error { return pr.Home() })
	for i, ax := range []string{"X", "Y", "Z"} {
		axis := strings.ToLower(ax)
		notPaused(h[i+1], ax, raised, text, func() error { return pr.Home(axis) })
	}

	// The joystick for X and Y.
	a.drawJoystick(c, pad, moving)
	a.region("Joystick", pad)
	if !f.wide {
		label(c, image.Rect(pad.Min.X, pad.Max.Y+6, pad.Max.X, pad.Max.Y+26), "XY  hold to move · Z mm per tap below", gfx.Center)
	}

	// Z: a row with the steps in the middle.
	z := kit.Cols(zArea, 6, kit.Fr(1.3), kit.Fr(1), kit.Fr(1), kit.Fr(1), kit.Fr(1.3))
	down, up, steps := z[0], z[4], image.Rect(z[1].Min.X, zArea.Min.Y, z[3].Max.X, zArea.Max.Y)
	btn(up, "Z ▲", raised, text, func() error { return pr.Jog(0, 0, a.zStep, 600) })
	for i, r := range kit.Cols(steps, 6, kit.Fr(1), kit.Fr(1), kit.Fr(1)) {
		s := []float64{0.1, 1, 10}[i]
		fill, fg := raised, text
		if a.zStep == s {
			fill, fg = orange, onAcc
		}
		key(c, r, fmt.Sprint(s), fill, fg, a.isPressed(r))
		a.addHit(kit.Hit{R: r, Tap: func() { a.zStep = s }})
	}
	btn(down, "Z ▼", raised, text, func() error { return pr.Jog(0, 0, -a.zStep, 600) })

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
	msg := "Motors off lets you move the head by hand."
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

// drawJoystick draws the XY pad in a display window, with the cross at the finger, and takes the
// touches of the pad while the head can move.
func (a *App) drawJoystick(c *gfx.Canvas, pad image.Rectangle, moving bool) {
	a.joyArea = pad
	vx, vy, _ := a.joy.get()
	window(c, pad)
	grid := color.RGBA{0x34, 0x30, 0x2c, 255}
	for i := 1; i < 8; i++ {
		g := pad.Min.X + i*pad.Dx()/8
		c.Fill(image.Rect(g, pad.Min.Y+6, g+1, pad.Max.Y-6), grid)
		g = pad.Min.Y + i*pad.Dy()/8
		c.Fill(image.Rect(pad.Min.X+6, g, pad.Max.X-6, g+1), grid)
	}
	cx, cy := pad.Min.X+pad.Dx()/2, pad.Min.Y+pad.Dy()/2
	r := float64(pad.Dx() / 2)
	R := pad.Dx() / 2
	dead := int(r * joyDead)
	c.RoundRect(image.Rect(cx-dead, cy-dead, cx+dead, cy+dead), dead, color.RGBA{0x2a, 0x26, 0x22, 255})
	for _, l := range []struct {
		s      string
		dx, dy int
	}{{"Y+", 0, -R + 16}, {"Y-", 0, R - 16}, {"X-", -R + 20, 0}, {"X+", R - 20, 0}} {
		c.Text(image.Rect(cx+l.dx-20, cy+l.dy-10, cx+l.dx+20, cy+l.dy+10), l.s, gfx.Face(true, 14), lcdDim, gfx.Center)
	}
	kx, ky := cx+int(vx*r*0.85), cy-int(vy*r*0.85)
	cross := lcdInk
	if !moving {
		cross = lcdDim
	}
	if vx != 0 || vy != 0 { // a line from the middle to the finger
		for t := 0.0; t < 1; t += 0.02 {
			x, y := cx+int(float64(kx-cx)*t), cy+int(float64(ky-cy)*t)
			c.Fill(image.Rect(x, y, x+2, y+2), lcdDim)
		}
	}
	c.Fill(image.Rect(kx-18, ky-1, kx+18, ky+1), cross)
	c.Fill(image.Rect(kx-1, ky-18, kx+1, ky+18), cross)
	c.RoundRect(image.Rect(kx-9, ky-9, kx+9, ky+9), 9, cross)
	c.RoundRect(image.Rect(kx-5, ky-5, kx+5, ky+5), 5, lcd)
	if moving {
		if speed := joySpeed(math.Hypot(vx, vy)); speed > 0 {
			c.Text(image.Rect(pad.Min.X, pad.Max.Y-24, pad.Max.X-8, pad.Max.Y-4), fmt.Sprintf("%.0f MM/S", speed), gfx.Face(true, 14), lcdInk, gfx.Right)
		}
		a.addHit(kit.Hit{R: pad, Pad: a.joyPad})
	}
}

// ParkGcode moves the head to the back left, 10 mm up, out of the way of the part.
func ParkGcode(st printer.State) []string {
	z := min(st.Z+10, st.Axis[1][2])
	return []string{"G90", fmt.Sprintf("G1 Z%.2f F600", z), fmt.Sprintf("G1 X%.1f Y%.1f F6000", st.Axis[0][0]+5, st.Axis[1][1]-5)}
}

// CenterGcode moves the head to the middle of the bed.
func CenterGcode(st printer.State) []string {
	return []string{"G90", fmt.Sprintf("G1 X%.1f Y%.1f F6000", (st.Axis[0][0]+st.Axis[1][0])/2, (st.Axis[0][1]+st.Axis[1][1])/2)}
}
