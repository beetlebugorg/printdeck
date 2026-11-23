package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"printdeck/kit"
	"strings"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// The tune sheet changes a running print: speed, flow, the height of the nozzle (baby steps), the fan,
// and the temperatures. The values come from the state of the printer, so each screen shows the same
// values. A printer with speed levels (a Bambu Lab printer) has − and + for the levels, and no flow
// or Z offset, because it has no command for them.

// speedLeveler is a printer with named speed levels. The levels count from 1, and now is 0 when
// the level is not known.
type speedLeveler interface {
	SpeedLevels() (names []string, now int)
	SetSpeedLevel(level int) error
}

// fanKeyOn reports that the fan key of p percent shows the fan speed. A printer with speed levels
// reports the fan in 15 steps, so the key lights within half a step.
func fanKeyOn(stepped bool, fan float64, p int) bool {
	tolerance := 1.0
	if stepped {
		tolerance = 3.4
	}
	return math.Abs(fan-float64(p)) < tolerance
}

func (a *App) drawTune(c *gfx.Canvas) {
	pr := a.Printer
	st := pr.State()
	levels, stepped := pr.(speedLeveler)
	f := a.pageBack(c, "Tune", "Back", func() { a.tune = false })
	a.helpKey(c, f.head)
	// Tall: the file, the speed, flow and Z rows, the fan, and the temperatures. Wide: the file and
	// the fan in the left column, under the display, which shows the temperatures (a tap on one
	// changes it); the rows at the right.
	var file, rows, zHint, fanName, fanRow, tHint, temps, temps2 image.Rectangle
	if f.wide {
		// The left column is narrow: the name of the fan goes above its keys.
		l := kit.Rows(f.aside(), 10, kit.Px(22), kit.Px(16), kit.Px(52), kit.Px(40))
		file, fanName, fanRow, tHint = l[0], l[1], l[2], l[3]
		rows = f.body()
		if !stepped {
			r := kit.Rows(f.body(), 8, kit.Fr(1), kit.Px(keyMin))
			rows, zHint = r[0], r[1]
		}
	} else {
		// The hint of the Z offset at the left of Save: three lines of the large text.
		zHintH := keyMin
		if gfx.FinePrint() {
			zHintH = 3 * gfx.Face(false, 16).Metrics().Height.Ceil()
		}
		body := kit.Inset(f.body(), 0, -4, 0, 8)
		if stepped {
			// The second row of cards has the aux nozzle and the chamber.
			r := kit.Rows(body, 12, kit.Px(26), kit.Px(130), kit.Px(56), kit.Px(20), kit.Px(100), kit.Px(100), kit.Fr(1))
			file, rows, fanRow, tHint = r[0], r[1], r[2], r[3]
			temps, temps2 = kit.Inset(r[4], 0, -4, 0, 4), kit.Inset(r[5], 0, -4, 0, 4)
		} else {
			r := kit.Rows(body, 12, kit.Px(26), kit.Fr(1), kit.Px(zHintH), kit.Px(56), kit.Px(20), kit.Px(100))
			file, rows, zHint, fanRow, tHint, temps = r[0], r[1], r[2], r[3], r[4], kit.Inset(r[5], 0, -4, 0, 4) // the cards at the margins of the page
		}
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
	if stepped {
		rowH = min(130, rows.Dy())
	}
	s := kit.NewStack(rows, 12)
	// key draws − or +. A nil tap is the end of the range: the key is dim and sends no command.
	key := func(r image.Rectangle, label string, tap func()) {
		fg := text
		if tap == nil {
			fg, tap = darken(dim, 0.55), func() {}
		}
		a.button(c, r, label, raised, fg, tap)
	}
	// row draws a row with −, the value and +. The name is at the top of the window of the value.
	row := func(name, value string, minus, plus func()) {
		rr := s.Next(rowH)
		a.region(name, rr)
		k := kit.Cols(rr, 8, kit.Px(keyW), kit.Fr(1), kit.Px(keyW))
		key(k[0], "−", minus)
		window(c, k[1])
		nf := gfx.Face(true, 15)
		nh := nf.Metrics().Height.Ceil()
		in := kit.Inset(k[1], 12, 6, 12, 6)
		c.Text(image.Rect(in.Min.X, in.Min.Y, in.Max.X, in.Min.Y+nh), strings.ToUpper(name), nf, lcdDim, gfx.Left)
		// The value keeps 8 pixels from the sides of the window: a long value gets a smaller font.
		v := kit.Inset(in, 0, nh, 0, 0)
		c.Text(v, value, shrinkFace(true, 32, 16, value, v.Dx()), lcdInk, gfx.Center)
		key(k[2], "+", plus)
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
	if stepped {
		name, value, minus, plus := speedLevelRow(levels, speed, func(l int) {
			a.do(func() error { return levels.SetSpeedLevel(l) })
		})
		row(name, value, minus, plus)
		a.drawTuneFan(c, pr, st, stepped, fanName, fanRow)
		a.drawTuneTemps(c, st, f.wide, tHint, temps)
		if !f.wide {
			a.drawTuneTemps2(c, st, temps2)
		}
		return
	}
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

	a.drawTuneFan(c, pr, st, stepped, fanName, fanRow)
	a.drawTuneTemps(c, st, f.wide, tHint, temps)
}

// speedLevelRow returns the name and the value of the speed row of a printer with levels, and the
// taps of − and +. A tap is nil at the end of the levels, and when the level is not known.
func speedLevelRow(l speedLeveler, pct int, set func(level int)) (name, value string, minus, plus func()) {
	names, now := l.SpeedLevels()
	name, value = "Speed", fmt.Sprintf("%d%%", pct)
	if now < 1 || now > len(names) {
		return name, value, nil, nil
	}
	value = names[now-1]
	if pct > 0 {
		value = fmt.Sprintf("%s · %d%%", names[now-1], pct)
	}
	switch now {
	case 1:
		name = "Speed · slowest"
	case len(names):
		name = "Speed · fastest"
	}
	if now > 1 {
		minus = func() { set(now - 1) }
	}
	if now < len(names) {
		plus = func() { set(now + 1) }
	}
	return name, value, minus, plus
}

// drawTuneFan draws the name of the fan and its keys.
func (a *App) drawTuneFan(c *gfx.Canvas, pr Printer, st printer.State, stepped bool, fanName, fanRow image.Rectangle) {
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
		if st.HasFan && fanKeyOn(stepped, st.Fan, p) {
			fill, fg = keyOn, keyOnInk
		}
		label := fmt.Sprintf("%d%%", p)
		if p == 0 {
			label = "Off"
		}
		pct := p
		a.button(c, r, label, fill, fg, func() { a.do(func() error { return pr.Fan(pct) }) })
	}
}

// drawTuneTemps draws the hint of the temperatures, and their cards on a tall screen.
func (a *App) drawTuneTemps(c *gfx.Canvas, st printer.State, wide bool, tHint, temps image.Rectangle) {
	if wide {
		hintText(c, tHint, "Tap a temperature on the display", gfx.Left)
		return
	}
	hintText(c, tHint, "Tap a temperature to change it", gfx.Left)
	a.temps(c, temps, st.Tool, st.ToolTarget, st.Bed, st.BedTarget, st.Connected)
}

// drawTuneTemps2 draws the cards of the aux nozzle and the chamber, for a printer that has them.
func (a *App) drawTuneTemps2(c *gfx.Canvas, st printer.State, area image.Rectangle) {
	cards := kit.Cols(area, 16, kit.Fr(1), kit.Fr(1))
	if ts, ok := a.Printer.(tool2Setter); ok && st.HasTool2 {
		tempCard(c, cards[0], func(cx, cy, r int, col color.RGBA) { drawNozzle(c, cx, cy, r, col) }, "Aux", st.Tool2, st.Tool2Target, st.Connected)
		a.region("Aux", cards[0])
		if st.Connected {
			a.addHit(kit.Hit{R: cards[0], Tap: func() {
				a.editTemp("Aux nozzle temperature", int(st.Tool2Target), 300, toolPresets, ts.SetTool2Temp)
			}})
		}
	}
	if cs, ok := a.Printer.(chamberSetter); ok && st.HasChamber {
		tempCard(c, cards[1], nil, "Chamber", st.Chamber, st.ChamberTarget, st.Connected)
		a.region("Chamber", cards[1])
		if st.Connected {
			a.addHit(kit.Hit{R: cards[1], Tap: func() {
				a.editTemp("Chamber temperature", int(st.ChamberTarget), 65, chamberPresets, cs.SetChamberTemp)
			}})
		}
	}
}
