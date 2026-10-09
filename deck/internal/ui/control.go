package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"sync"
	"time"

	"speakerdeck/internal/gfx"
)

// The control screen: home, move the head with a joystick, Z, extruder, fan, motors, filament, stop.

const (
	joyCX, joyCY, joyR = 154, 344, 124 // the joystick pad
	joyDead            = 0.15          // the middle part that does not move
	joyTick            = 150 * time.Millisecond
	joyMinSpeed        = 2.0  // mm/s just outside the middle
	joyMaxSpeed        = 50.0 // mm/s at the edge
	minExtrudeTemp     = 170  // Klipper refuses to extrude below this
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
func (a *App) joyPad(p image.Point, phase int) {
	j := a.joy
	vx := float64(p.X-joyCX) / joyR
	vy := float64(joyCY-p.Y) / joyR
	if m := math.Hypot(vx, vy); m > 1 {
		vx, vy = vx/m, vy/m
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if phase == padUp {
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

func (a *App) drawControl(c *gfx.Canvas) {
	pr := a.Printer
	st := pr.State()
	c.Text(image.Rect(24, 64, W-150, 108), "Control", gfx.Face(true, 28), text, gfx.Left)
	a.button(c, image.Rect(W-130, 66, W-24, 110), "Back", raised, text, func() { a.control = false })

	moving := st.Connected && !st.Busy()
	hint := "Home after the printer starts, before you move it."
	switch {
	case !st.Connected:
		hint = "The printer is not connected."
	case st.Busy():
		hint = "Moving is off while a print runs."
	}
	c.Text(image.Rect(24, 108, W-24, 134), hint, gfx.Face(false, 17), dim, gfx.Left)

	btn := func(r image.Rectangle, label string, fill, fg color.RGBA, f func() error) {
		if !moving {
			c.RoundRect(r, 18, surface)
			c.Text(r, label, gfx.Face(true, 22), dim, gfx.Center)
			return
		}
		a.button(c, r, label, fill, fg, func() { a.do(f) })
	}

	// Home
	w := (W - 48 - 24) / 4
	btn(image.Rect(24, 142, 24+w+w/2, 198), "Home all", orange, onAcc, func() error { return pr.Home() })
	for i, ax := range []string{"X", "Y", "Z"} {
		x0 := 24 + w + w/2 + 8 + i*(w/2+w/3+8)
		axis := ax
		btn(image.Rect(x0, 142, x0+w/2+w/3, 198), axis, raised, text, func() error { return pr.Home(map[string]string{"X": "x", "Y": "y", "Z": "z"}[axis]) })
	}

	// Joystick for X and Y
	vx, vy, _ := a.joy.get()
	ring := surface
	c.RoundRect(image.Rect(joyCX-joyR, joyCY-joyR, joyCX+joyR, joyCY+joyR), joyR, ring)
	r := float64(joyR)
	dead := int(r * joyDead)
	c.RoundRect(image.Rect(joyCX-dead, joyCY-dead, joyCX+dead, joyCY+dead), dead, bg)
	c.Fill(image.Rect(joyCX-joyR+16, joyCY, joyCX+joyR-16, joyCY+1), raised)
	c.Fill(image.Rect(joyCX, joyCY-joyR+16, joyCX+1, joyCY+joyR-16), raised)
	for _, l := range []struct {
		s      string
		dx, dy int
	}{{"Y+", 0, -joyR + 18}, {"Y−", 0, joyR - 18}, {"X−", -joyR + 22, 0}, {"X+", joyR - 22, 0}} {
		c.Text(image.Rect(joyCX+l.dx-20, joyCY+l.dy-12, joyCX+l.dx+20, joyCY+l.dy+12), l.s, gfx.Face(true, 16), dim, gfx.Center)
	}
	kx, ky := joyCX+int(vx*joyR*0.85), joyCY-int(vy*joyR*0.85)
	knob := text
	if !moving {
		knob = raised
	} else if vx != 0 || vy != 0 {
		knob = green
	}
	c.RoundRect(image.Rect(kx-30, ky-30, kx+30, ky+30), 30, knob)
	if moving {
		speed := joySpeed(math.Hypot(vx, vy))
		if speed > 0 {
			c.Text(image.Rect(joyCX-joyR, joyCY+joyR+2, joyCX+joyR, joyCY+joyR+24), fmt.Sprintf("%.0f mm/s", speed), gfx.Face(false, 16), green, gfx.Center)
		}
		a.addHit(hit{r: image.Rect(joyCX-joyR, joyCY-joyR, joyCX+joyR, joyCY+joyR), pad: a.joyPad})
	}

	// Z
	zx0, zx1 := 300, W-24
	btn(image.Rect(zx0, 214, zx1, 290), "Z ▲", raised, text, func() error { return pr.Jog(0, 0, a.zStep, 600) })
	for i, s := range []float64{0.1, 1, 10} {
		pw := (zx1 - zx0 - 16) / 3
		r := image.Rect(zx0+i*(pw+8), 298, zx0+i*(pw+8)+pw, 342)
		fill, fg := raised, text
		if a.zStep == s {
			fill, fg = orange, onAcc
		}
		step := s
		c.RoundRect(r, 22, fill)
		c.Text(r, fmt.Sprint(s), gfx.Face(true, 18), fg, gfx.Center)
		a.addHit(hit{r: r, tap: func() { a.zStep = step }})
	}
	btn(image.Rect(zx0, 350, zx1, 426), "Z ▼", raised, text, func() error { return pr.Jog(0, 0, -a.zStep, 600) })
	c.Text(image.Rect(zx0, 430, zx1, 456), "mm per tap", gfx.Face(false, 15), dim, gfx.Center)

	// Extruder: needs a hot nozzle
	half := (W - 48 - 8) / 2
	cold := st.Tool < minExtrudeTemp
	ext := func(r image.Rectangle, label string, mm float64) {
		if moving && cold {
			c.RoundRect(r, 18, surface)
			c.Text(r, label, gfx.Face(true, 20), dim, gfx.Center)
			a.addHit(hit{r: r, tap: func() { a.Toast(fmt.Sprintf("Heat the nozzle to %d° first", minExtrudeTemp)) }})
			return
		}
		btn(r, label, raised, text, func() error { return pr.Extrude(mm, 300) })
	}
	ext(image.Rect(24, 490, 24+half, 548), "Extrude 10 mm", 10)
	ext(image.Rect(24+half+8, 490, W-24, 548), "Retract 10 mm", -10)

	// Fan: the printer does not report it, so the last choice is shown.
	c.Text(image.Rect(24, 558, 90, 614), "Fan", gfx.Face(true, 20), text, gfx.Left)
	for i, p := range []int{0, 50, 100} {
		fw := (W - 24 - 90 - 16) / 3
		r := image.Rect(90+i*(fw+8), 560, 90+i*(fw+8)+fw, 612)
		fill, fg := raised, text
		if a.fanSet == p {
			fill, fg = green, onAcc
		}
		label := fmt.Sprintf("%d%%", p)
		if p == 0 {
			label = "Off"
		}
		pct := p
		if !st.Connected {
			c.RoundRect(r, 18, surface)
			c.Text(r, label, gfx.Face(true, 20), dim, gfx.Center)
			continue
		}
		a.button(c, r, label, fill, fg, func() { a.fanSet = pct; a.do(func() error { return pr.Fan(pct) }) })
	}

	// Filament, motors, stop
	tw := (W - 48 - 16) / 3
	y := 628
	if moving {
		a.button(c, image.Rect(24, y, 24+tw, y+76), "Filament", orange, onAcc, func() { a.swap = &swapState{} })
	} else {
		btn(image.Rect(24, y, 24+tw, y+76), "Filament", orange, onAcc, nil)
	}
	btn(image.Rect(24+tw+8, y, 24+2*tw+8, y+76), "Motors off", raised, text, pr.MotorsOff)
	if st.Connected {
		a.button(c, image.Rect(W-24-tw, y, W-24, y+76), "Stop", red, onAcc, func() {
			a.confirm = &confirmBox{question: "Stop the printer now?", yes: "Stop", danger: true, onYes: func() { a.do(pr.EmergencyStop) }}
		})
	}
}
