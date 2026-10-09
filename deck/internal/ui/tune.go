package ui

import (
	"fmt"
	"image"

	"speakerdeck/internal/gfx"
)

// The tune sheet changes a running print: speed, flow, the height of the nozzle (baby steps), the fan,
// and the temperatures. The printer does not report speed, flow and offset, so the screen counts from
// the start of each print.

func (a *App) drawTune(c *gfx.Canvas) {
	pr := a.Printer
	st := pr.State()
	if st.FilePath != a.tuneFile { // a new print starts as sliced
		a.tuneFile, a.tuneSpeed, a.tuneFlow, a.tuneZ = st.FilePath, 100, 100, 0
	}
	c.Text(image.Rect(24, 64, W-150, 108), "Tune", gfx.Face(true, 28), text, gfx.Left)
	a.button(c, image.Rect(W-130, 66, W-24, 110), "Back", raised, text, func() { a.tune = false })
	c.Text(image.Rect(24, 108, W-24, 134), st.File, gfx.Face(false, 17), dim, gfx.Left)

	row := func(y int, name, value string, minus, plus func()) {
		c.Text(image.Rect(24, y, 160, y+72), name, gfx.Face(true, 22), text, gfx.Left)
		a.button(c, image.Rect(160, y, 240, y+72), "−", raised, text, minus)
		c.Text(image.Rect(240, y, W-104, y+72), value, gfx.Face(true, 28), text, gfx.Center)
		a.button(c, image.Rect(W-104, y, W-24, y+72), "+", raised, text, plus)
	}
	setSpeed := func(v int) {
		a.tuneSpeed = max(10, min(300, v))
		s := a.tuneSpeed
		a.do(func() error { return pr.SpeedFactor(s) })
	}
	setFlow := func(v int) {
		a.tuneFlow = max(50, min(150, v))
		f := a.tuneFlow
		a.do(func() error { return pr.FlowFactor(f) })
	}
	z := func(d float64) {
		a.tuneZ += d
		a.do(func() error { return pr.BabyStep(d) })
	}
	row(146, "Speed", fmt.Sprintf("%d%%", a.tuneSpeed), func() { setSpeed(a.tuneSpeed - 10) }, func() { setSpeed(a.tuneSpeed + 10) })
	row(230, "Flow", fmt.Sprintf("%d%%", a.tuneFlow), func() { setFlow(a.tuneFlow - 2) }, func() { setFlow(a.tuneFlow + 2) })
	row(314, "Z offset", fmt.Sprintf("%+.2f mm", a.tuneZ), func() { z(-0.025) }, func() { z(0.025) })
	c.Text(image.Rect(24, 388, W-24, 412), "Z offset: − brings the nozzle closer to the bed.", gfx.Face(false, 16), dim, gfx.Left)

	// Fan
	c.Text(image.Rect(24, 424, 90, 480), "Fan", gfx.Face(true, 22), text, gfx.Left)
	for i, p := range []int{0, 50, 100} {
		fw := (W - 24 - 90 - 16) / 3
		r := image.Rect(90+i*(fw+8), 426, 90+i*(fw+8)+fw, 478)
		fill, fg := raised, text
		if a.fanSet == p {
			fill, fg = green, onAcc
		}
		label := fmt.Sprintf("%d%%", p)
		if p == 0 {
			label = "Off"
		}
		pct := p
		a.button(c, r, label, fill, fg, func() { a.fanSet = pct; a.do(func() error { return pr.Fan(pct) }) })
	}

	c.Text(image.Rect(24, 494, W-24, 518), "Tap a temperature to change it.", gfx.Face(false, 16), dim, gfx.Left)
	a.temps(c, 524, 100, st.Tool, st.ToolTarget, st.Bed, st.BedTarget, st.Connected)
}
