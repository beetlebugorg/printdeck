package ui

import (
	"fmt"
	"image"
	"printdeck/kit"

	"printdeck/internal/gfx"
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
	f := a.pageBack(c, "Tune", "Back", func() { a.tune = false })
	// Tall: the file, the speed, flow and Z rows, the fan, and the temperatures. Wide: the file, the
	// fan and the temperatures in the left column; the rows at the right.
	var file, rows, zHint, fanRow, tHint, temps image.Rectangle
	if f.wide {
		l := kit.Rows(f.aside(), 10, kit.Px(22), kit.Px(52), kit.Fr(1))
		file, fanRow, temps = l[0], l[1], l[2]
		r := kit.Rows(f.body(), 8, kit.Fr(1), kit.Px(20))
		rows, zHint = r[0], r[1]
	} else {
		r := kit.Rows(kit.Inset(f.body(), 0, -4, 0, 8), 12, kit.Px(26), kit.Fr(1), kit.Px(20), kit.Px(56), kit.Px(20), kit.Px(100))
		file, rows, zHint, fanRow, tHint, temps = r[0], r[1], r[2], r[3], r[4], kit.Inset(r[5], 16, -4, 16, 4)
	}
	c.Text(file, gfx.Fit(gfx.Face(false, 17), st.File, file.Dx()), gfx.Face(false, 17), dim, gfx.Left)

	labelW, keyW := 136, 80 // narrower in the right column of a wide screen
	if f.wide {
		labelW, keyW = 88, 64
	}
	rowH := min(110, (rows.Dy()+12)/3-12)
	s := kit.NewStack(rows, 12)
	row := func(name, value string, minus, plus func()) {
		r := kit.Cols(s.Next(rowH), 0, kit.Px(labelW), kit.Fr(1))
		label(c, r[0], name, gfx.Left)
		k := kit.Cols(r[1], 8, kit.Px(keyW), kit.Fr(1), kit.Px(keyW))
		a.button(c, k[0], "−", raised, text, minus)
		ro := kit.Inset(k[1], 0, 8, 0, 8)
		window(c, ro)
		c.Text(ro, value, gfx.Face(true, 22), lcdInk, gfx.Center)
		a.button(c, k[2], "+", raised, text, plus)
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
	row("Speed", fmt.Sprintf("%d%%", a.tuneSpeed), func() { setSpeed(a.tuneSpeed - 10) }, func() { setSpeed(a.tuneSpeed + 10) })
	row("Flow", fmt.Sprintf("%d%%", a.tuneFlow), func() { setFlow(a.tuneFlow - 2) }, func() { setFlow(a.tuneFlow + 2) })
	row("Z offset", fmt.Sprintf("%+.2f mm", a.tuneZ), func() { z(-0.025) }, func() { z(0.025) })
	label(c, zHint, "Z offset −: nozzle closer to the bed", gfx.Left)

	// Fan
	fan := kit.Cols(fanRow, 0, kit.Px(66), kit.Fr(1))
	label(c, fan[0], "Fan", gfx.Left)
	for i, r := range kit.Cols(kit.Inset(fan[1], 0, 2, 0, 2), 8, kit.Fr(1), kit.Fr(1), kit.Fr(1)) {
		p := []int{0, 50, 100}[i]
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

	if !tHint.Empty() {
		label(c, tHint, "Tap a temperature to change it", gfx.Left)
	}
	a.temps(c, temps, st.Tool, st.ToolTarget, st.Bed, st.BedTarget, st.Connected)
}
