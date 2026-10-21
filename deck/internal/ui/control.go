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
	joyCX, joyCY, joyR = 152, 346, 128 // the joystick pad of the Move page
	joyDead            = 0.15          // the middle part that does not move
	joyTick            = 150 * time.Millisecond
	joyMinSpeed        = 2.0  // mm/s just outside the middle
	joyMaxSpeed        = 50.0 // mm/s at the edge
	minExtrudeTemp     = 170  // Klipper blocks extrusion below this temperature
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
	vx := float64(p.X-joyCX) / joyR
	vy := float64(joyCY-p.Y) / joyR
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
// Z, park and center, and the motors.
func (a *App) drawMove(c *gfx.Canvas) {
	pr := a.Printer
	st := pr.State()
	heading(c, "Move")
	a.backButton(c)

	moving := st.Connected && (!st.Busy() || st.Paused)

	// The position, with an LED for each homed axis.
	pos := image.Rect(24, 76, W-24, 130)
	window(c, pos)
	for i, ax := range []struct {
		n string
		v float64
		f string
	}{{"X", st.X, "%.1f"}, {"Y", st.Y, "%.1f"}, {"Z", st.Z, "%.2f"}} {
		x := pos.Min.X + 14 + i*146
		led(c, x+8, pos.Min.Y+27, strings.Contains(st.Homed, strings.ToLower(ax.n)))
		c.Text(image.Rect(x+22, pos.Min.Y, x+146, pos.Max.Y), ax.n+" "+fmt.Sprintf(ax.f, ax.v), gfx.Face(true, 22), lcdInk, gfx.Left)
	}

	btn := func(r image.Rectangle, label string, fill, fg color.RGBA, f func() error) {
		if !moving {
			key(c, r, label, surface, dim, false)
			return
		}
		a.button(c, r, label, fill, fg, func() { a.do(f) })
	}

	// Home
	q := (W - 48 - 24) / 4
	btn(image.Rect(24, 142, 24+q, 200), "Home all", orange, onAcc, func() error { return pr.Home() })
	for i, ax := range []string{"X", "Y", "Z"} {
		axis := strings.ToLower(ax)
		btn(image.Rect(24+(i+1)*(q+8), 142, 24+(i+1)*(q+8)+q, 200), ax, raised, text, func() error { return pr.Home(axis) })
	}

	// The joystick for X and Y: an XY pad in a display window.
	vx, vy, _ := a.joy.get()
	pad := image.Rect(joyCX-joyR, joyCY-joyR, joyCX+joyR, joyCY+joyR)
	window(c, pad)
	grid := color.RGBA{0x34, 0x30, 0x2c, 255}
	for i := 1; i < 8; i++ {
		g := pad.Min.X + i*pad.Dx()/8
		c.Fill(image.Rect(g, pad.Min.Y+6, g+1, pad.Max.Y-6), grid)
		g = pad.Min.Y + i*pad.Dy()/8
		c.Fill(image.Rect(pad.Min.X+6, g, pad.Max.X-6, g+1), grid)
	}
	r := float64(joyR)
	dead := int(r * joyDead)
	c.RoundRect(image.Rect(joyCX-dead, joyCY-dead, joyCX+dead, joyCY+dead), dead, color.RGBA{0x2a, 0x26, 0x22, 255})
	for _, l := range []struct {
		s      string
		dx, dy int
	}{{"Y+", 0, -joyR + 16}, {"Y-", 0, joyR - 16}, {"X-", -joyR + 20, 0}, {"X+", joyR - 20, 0}} {
		c.Text(image.Rect(joyCX+l.dx-20, joyCY+l.dy-10, joyCX+l.dx+20, joyCY+l.dy+10), l.s, gfx.Face(true, 14), lcdDim, gfx.Center)
	}
	kx, ky := joyCX+int(vx*r*0.85), joyCY-int(vy*r*0.85)
	cross := lcdInk
	if !moving {
		cross = lcdDim
	}
	if vx != 0 || vy != 0 { // a line from the middle to the finger
		for t := 0.0; t < 1; t += 0.02 {
			x, y := joyCX+int(float64(kx-joyCX)*t), joyCY+int(float64(ky-joyCY)*t)
			c.Fill(image.Rect(x, y, x+2, y+2), lcdDim)
		}
	}
	c.Fill(image.Rect(kx-18, ky-1, kx+18, ky+1), cross)
	c.Fill(image.Rect(kx-1, ky-18, kx+1, ky+18), cross)
	c.RoundRect(image.Rect(kx-9, ky-9, kx+9, ky+9), 9, cross)
	c.RoundRect(image.Rect(kx-5, ky-5, kx+5, ky+5), 5, lcd)
	if moving {
		speed := joySpeed(math.Hypot(vx, vy))
		if speed > 0 {
			c.Text(image.Rect(pad.Min.X, pad.Max.Y-24, pad.Max.X-8, pad.Max.Y-4), fmt.Sprintf("%.0f MM/S", speed), gfx.Face(true, 14), lcdInk, gfx.Right)
		}
		a.addHit(kit.Hit{R: pad, Pad: a.joyPad})
	}
	label(c, image.Rect(pad.Min.X, pad.Max.Y+6, pad.Max.X, pad.Max.Y+26), "XY  hold to move", gfx.Center)

	// Z
	zx0, zx1 := pad.Max.X+12, W-24
	btn(image.Rect(zx0, pad.Min.Y, zx1, pad.Min.Y+82), "Z ▲", raised, text, func() error { return pr.Jog(0, 0, a.zStep, 600) })
	for i, s := range []float64{0.1, 1, 10} {
		pw := (zx1 - zx0 - 12) / 3
		r := image.Rect(zx0+i*(pw+6), pad.Min.Y+94, zx0+i*(pw+6)+pw, pad.Min.Y+146)
		fill, fg := raised, text
		if a.zStep == s {
			fill, fg = orange, onAcc
		}
		step := s
		key(c, r, fmt.Sprint(s), fill, fg, a.isPressed(r))
		a.addHit(kit.Hit{R: r, Tap: func() { a.zStep = step }})
	}
	btn(image.Rect(zx0, pad.Min.Y+158, zx1, pad.Min.Y+240), "Z ▼", raised, text, func() error { return pr.Jog(0, 0, -a.zStep, 600) })
	label(c, image.Rect(zx0, pad.Min.Y+248, zx1, pad.Min.Y+270), "Z mm per tap", gfx.Center)

	// Park and center need the homed axes. Motors off lets the head move by hand.
	third := (W - 48 - 16) / 3
	y := pad.Max.Y + 38
	homed := strings.Contains(st.Homed, "x") && strings.Contains(st.Homed, "y")
	park := func(r image.Rectangle, name string, g func() []string) {
		if !homed {
			btn(r, name, raised, text, func() error { return fmt.Errorf("Home X and Y first") })
			return
		}
		btn(r, name, raised, text, func() error { return pr.Gcode(g()...) })
	}
	park(image.Rect(24, y, 24+third, y+70), "Park", func() []string { return ParkGcode(st) })
	park(image.Rect(24+third+8, y, 24+2*third+8, y+70), "Center", func() []string { return CenterGcode(st) })
	btn(image.Rect(W-24-third, y, W-24, y+70), "Motors off", raised, text, pr.MotorsOff)
	hint := "Motors off lets you move the head by hand."
	switch {
	case !st.Connected:
		hint = "The printer is not connected."
	case st.Busy() && !st.Paused:
		hint = "Moving is off while a print runs."
	}
	label(c, image.Rect(24, y+80, W-24, y+104), hint, gfx.Center)
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
