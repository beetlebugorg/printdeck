package ui

import (
	"fmt"
	"image"

	"speakerdeck/internal/eq"
	"speakerdeck/internal/gfx"
)

// Equalizer is the tone control of the speaker.
type Equalizer interface {
	Gains() eq.Gains
	SetGain(band int, db float64)
	SetPreset(name string)
	Preset() string
	Connected() bool
}

const (
	eqTop    = 252 // top of the sliders
	eqBottom = 616 // bottom of the sliders
)

func (a *App) drawEQ(c *gfx.Canvas) {
	e := a.Equalizer
	heading(c, "Equalizer")
	a.button(c, image.Rect(W-130, 66, W-24, 110), "Close", raised, text, func() { a.eqOpen = false })
	if !e.Connected() {
		c.Text(image.Rect(24, 108, W-24, 136), "The equalizer is not running (camilladsp).", gfx.Face(false, 18), red, gfx.Left)
	} else {
		c.Text(image.Rect(24, 108, W-24, 136), "Changes are heard at once.", gfx.Face(false, 18), dim, gfx.Left)
	}

	// Presets
	cur := e.Preset()
	n := len(eq.Presets)
	gap := 8
	w := (W - 48 - gap*(n-1)) / n
	for i, p := range eq.Presets {
		r := image.Rect(24+i*(w+gap), 146, 24+i*(w+gap)+w, 192)
		fill, fg := raised, text
		if p.Name == cur {
			fill, fg = green, onAcc
		}
		key(c, r, p.Name, fill, fg, a.isPressed(r))
		name := p.Name
		a.addHit(hit{r: r, tap: func() { e.SetPreset(name) }})
	}

	// One vertical slider for each band. The middle is 0 dB, the top +12, the bottom -12.
	g := e.Gains()
	col := (W - 48) / len(eq.Bands)
	h := eqBottom - eqTop
	yFor := func(db float64) int { return eqTop + int((eq.MaxGain-db)/(2*eq.MaxGain)*float64(h)) }
	zero := yFor(0)
	c.Fill(image.Rect(24, zero, W-24, zero+1), raised)
	for i, b := range eq.Bands {
		x0 := 24 + i*col
		cx := x0 + col/2
		hitR := image.Rect(x0, eqTop-16, x0+col, eqBottom+16)
		db := g[i]
		if a.dragging != nil && a.dragging.r == hitR {
			db = eq.MaxGain - a.dragFrac*2*eq.MaxGain
		}
		c.RoundRect(image.Rect(cx-5, eqTop, cx+5, eqBottom), 5, lcd)
		for t := -12; t <= 12; t += 6 { // scale marks
			ty := yFor(float64(t))
			c.Fill(image.Rect(cx-16, ty, cx-9, ty+1), edge)
			c.Fill(image.Rect(cx+9, ty, cx+16, ty+1), edge)
		}
		y := yFor(db)
		if db != 0 {
			c.RoundRect(image.Rect(cx-2, min(y, zero), cx+2, max(y, zero)), 2, orange)
		}
		faderCap(c, cx, y, true)

		val := fmt.Sprintf("%+.1f", db)
		if db == 0 {
			val = "0"
		}
		c.Text(image.Rect(x0, eqTop-48, x0+col, eqTop-20), val, gfx.Face(true, 18), text, gfx.Center)
		c.Text(image.Rect(x0, eqBottom+20, x0+col, eqBottom+46), b.Label, gfx.Face(true, 18), text, gfx.Center)
		c.Text(image.Rect(x0, eqBottom+44, x0+col, eqBottom+66), []string{"Bass", "Hz", "Hz", "Hz", "Treble"}[i], gfx.Face(false, 15), dim, gfx.Center)

		band := i
		// The slider covers a bit more than the track, so the ends are easy to reach.
		set := func(f float64) {
			ff := (f*float64(hitR.Dy()) - 16) / float64(h)
			e.SetGain(band, eq.MaxGain-max(0, min(1, ff))*2*eq.MaxGain)
		}
		a.addHit(hit{r: hitR, vertical: true, drag: set, end: set})
	}
}
