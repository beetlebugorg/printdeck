package ui

import (
	"fmt"
	"image"
	"math"
	"printdeck/kit"

	"printdeck/internal/gfx"
)

// The tune sheet changes a running print: speed, flow, the height of the nozzle (baby steps), the fan,
// and the temperatures. The values come from the state of the printer, so each screen shows the same
// values.

func (a *App) drawTune(c *gfx.Canvas) {
	pr := a.Printer
	st := pr.State()
	f := a.pageBack(c, "Tune", "Back", func() { a.tune = false })
	a.helpKey(c, f.head)
	// Tall: the file, the speed, flow and Z rows, the fan, and the temperatures. Wide: the file and
	// the fan in the left column, under the display, which shows the temperatures (a tap on one
	// changes it); the rows at the right.
	var file, rows, zHint, fanRow, tHint, temps image.Rectangle
	if f.wide {
		l := kit.Rows(f.aside(), 10, kit.Px(22), kit.Px(52), kit.Px(40))
		file, fanRow, tHint = l[0], l[1], l[2]
		r := kit.Rows(f.body(), 8, kit.Fr(1), kit.Px(20))
		rows, zHint = r[0], r[1]
	} else {
		r := kit.Rows(kit.Inset(f.body(), 0, -4, 0, 8), 12, kit.Px(26), kit.Fr(1), kit.Px(20), kit.Px(56), kit.Px(20), kit.Px(100))
		file, rows, zHint, fanRow, tHint, temps = r[0], r[1], r[2], r[3], r[4], kit.Inset(r[5], 16, -4, 16, 4)
	}
	c.Text(file, st.File, gfx.Face(false, 17), dim, gfx.Left)

	labelW, keyW := 136, 80 // narrower in the right column of a wide screen
	if f.wide {
		labelW, keyW = 88, 64
	}
	rowH := min(110, (rows.Dy()+12)/3-12)
	s := kit.NewStack(rows, 12)
	// row draws a row with −, the value and +. With save, a Save key goes under the name.
	row := func(name, value string, minus, plus, save func()) {
		rr := s.Next(rowH)
		a.region(name, rr)
		r := kit.Cols(rr, 0, kit.Px(labelW), kit.Fr(1))
		if save != nil {
			l := kit.Rows(r[0], 4, kit.Fr(1), kit.Px(min(48, rowH/2)))
			label(c, l[0], name, gfx.Left)
			a.button(c, kit.Inset(l[1], 0, 0, 12, 0), "Save", raised, text, save)
		} else {
			label(c, r[0], name, gfx.Left)
		}
		k := kit.Cols(r[1], 8, kit.Px(keyW), kit.Fr(1), kit.Px(keyW))
		a.button(c, k[0], "−", raised, text, minus)
		ro := kit.Inset(k[1], 0, 8, 0, 8)
		window(c, ro)
		c.Text(ro, value, gfx.Face(true, 22), lcdInk, gfx.Center)
		a.button(c, k[2], "+", raised, text, plus)
	}
	setSpeed := func(v int) {
		v = max(10, min(300, v))
		a.do(func() error { return pr.SpeedFactor(v) })
	}
	setFlow := func(v int) {
		v = max(50, min(150, v))
		a.do(func() error { return pr.FlowFactor(v) })
	}
	z := func(d float64) { a.do(func() error { return pr.BabyStep(d) }) }
	speed, flow := st.SpeedPct, st.FlowPct
	row("Speed", fmt.Sprintf("%d%%", speed), func() { setSpeed(speed - 10) }, func() { setSpeed(speed + 10) }, nil)
	row("Flow", fmt.Sprintf("%d%%", flow), func() { setFlow(flow - 2) }, func() { setFlow(flow + 2) }, nil)
	row("Z offset", fmt.Sprintf("%+.3f mm", st.ZOffset), func() { z(-0.025) }, func() { z(0.025) }, func() { a.saveZOffset(st) })
	label(c, zHint, "Z offset −: nozzle closer to the bed", gfx.Left)

	// Fan
	a.region("Fan", fanRow)
	fan := kit.Cols(fanRow, 0, kit.Px(66), kit.Fr(1))
	label(c, fan[0], "Fan", gfx.Left)
	for i, r := range kit.Cols(kit.Inset(fan[1], 0, 2, 0, 2), 8, kit.Fr(1), kit.Fr(1), kit.Fr(1)) {
		p := []int{0, 50, 100}[i]
		fill, fg := keyOff, text
		if st.HasFan && math.Abs(st.Fan-float64(p)) < 1 {
			fill, fg = keyOn, keyOnInk
		}
		label := fmt.Sprintf("%d%%", p)
		if p == 0 {
			label = "Off"
		}
		pct := p
		a.button(c, r, label, fill, fg, func() { a.do(func() error { return pr.Fan(pct) }) })
	}

	switch {
	case f.wide:
		label(c, tHint, "Tap a temperature on the display", gfx.Left)
	default:
		label(c, tHint, "Tap a temperature to change it", gfx.Left)
		a.temps(c, temps, st.Tool, st.ToolTarget, st.Bed, st.BedTarget, st.Connected)
	}
}
