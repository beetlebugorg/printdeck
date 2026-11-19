package ui

import (
	"fmt"
	"image"
	"math"
	"printdeck/kit"
	"strings"

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
	var file, rows, zHint, fanName, fanRow, tHint, temps image.Rectangle
	if f.wide {
		// The left column is narrow: the name of the fan goes above its keys.
		l := kit.Rows(f.aside(), 10, kit.Px(22), kit.Px(16), kit.Px(52), kit.Px(40))
		file, fanName, fanRow, tHint = l[0], l[1], l[2], l[3]
		r := kit.Rows(f.body(), 8, kit.Fr(1), kit.Px(keyMin))
		rows, zHint = r[0], r[1]
	} else {
		// The hint of the Z offset at the left of Save: three lines of the large text.
		zHintH := keyMin
		if gfx.FinePrint() {
			zHintH = 3 * gfx.Face(false, 16).Metrics().Height.Ceil()
		}
		r := kit.Rows(kit.Inset(f.body(), 0, -4, 0, 8), 12, kit.Px(26), kit.Fr(1), kit.Px(zHintH), kit.Px(56), kit.Px(20), kit.Px(100))
		file, rows, zHint, fanRow, tHint, temps = r[0], r[1], r[2], r[3], r[4], kit.Inset(r[5], 0, -4, 0, 4) // the cards at the margins of the page
	}
	c.Text(file, st.Name(), gfx.Face(false, 17), dim, gfx.Left)

	keyW := 80 // narrower in the right column of a wide screen
	switch {
	case f.wide:
		keyW = 64
	case gfx.FinePrint():
		keyW = 72
	}
	rowH := min(110, (rows.Dy()+12)/3-12)
	s := kit.NewStack(rows, 12)
	// row draws a row with −, the value and +. The name is at the top of the window of the value.
	row := func(name, value string, minus, plus func()) {
		rr := s.Next(rowH)
		a.region(name, rr)
		k := kit.Cols(rr, 8, kit.Px(keyW), kit.Fr(1), kit.Px(keyW))
		a.button(c, k[0], "−", raised, text, minus)
		window(c, k[1])
		nf := gfx.Face(true, 15)
		nh := nf.Metrics().Height.Ceil()
		in := kit.Inset(k[1], 12, 6, 12, 6)
		c.Text(image.Rect(in.Min.X, in.Min.Y, in.Max.X, in.Min.Y+nh), strings.ToUpper(name), nf, lcdDim, gfx.Left)
		// The value keeps 8 pixels from the sides of the window: a long value gets a smaller font.
		v := kit.Inset(in, 0, nh, 0, 0)
		c.Text(v, value, shrinkFace(true, 32, 16, value, v.Dx()), lcdInk, gfx.Center)
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
	row("Speed", fmt.Sprintf("%d%%", speed), func() { setSpeed(speed - 10) }, func() { setSpeed(speed + 10) })
	row("Flow", fmt.Sprintf("%d%%", flow), func() { setFlow(flow - 2) }, func() { setFlow(flow + 2) })
	row("Z offset", fmt.Sprintf("%+.3f mm", st.ZOffset), func() { z(-0.025) }, func() { z(0.025) })
	// The hint of the Z offset, and Save for the Z offset at its right.
	zr := kit.Cols(zHint, 12, kit.Fr(1), kit.Px(120))
	// The hint goes to more lines before it gets smaller.
	hf := gfx.Face(false, 16)
	hl := wrapText(hf, "Z offset −: nozzle closer to the bed", zr[0].Dx())
	lh := hf.Metrics().Height.Ceil()
	if len(hl)*lh > zr[0].Dy() {
		hl = []string{"Z offset −: nozzle closer to the bed"}
	}
	y := zr[0].Min.Y + (zr[0].Dy()-len(hl)*lh)/2
	for i, l := range hl {
		hintText(c, image.Rect(zr[0].Min.X, y+i*lh, zr[0].Max.X, y+(i+1)*lh), l, gfx.Left)
	}
	a.button(c, zr[1], "Save", raised, text, func() { a.saveZOffset(st) })

	// Fan
	a.region("Fan", fanRow)
	fanKeys := fanRow
	if fanName.Empty() {
		fan := kit.Cols(fanRow, 0, kit.Px(66), kit.Fr(1))
		label(c, fan[0], "Fan", gfx.Left)
		fanKeys = kit.Inset(fan[1], 0, 2, 0, 2)
	} else {
		label(c, fanName, "Fan", gfx.Left)
	}
	for i, r := range kit.Cols(fanKeys, 8, kit.Fr(1), kit.Fr(1), kit.Fr(1)) {
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
		hintText(c, tHint, "Tap a temperature on the display", gfx.Left)
	default:
		hintText(c, tHint, "Tap a temperature to change it", gfx.Left)
		a.temps(c, temps, st.Tool, st.ToolTarget, st.Bed, st.BedTarget, st.Connected)
	}
}
