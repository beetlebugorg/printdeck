package ui

import (
	"fmt"
	"image"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/kit"
)

// The status panel of the touch screen, in the place of the printer display: a row of three small
// readouts (the nozzle, the bed and the fan) and three lines of text over the progress bar. It is
// lower than the display, so the screens under it get more space. The browsers and the apps show
// the printer display (App.Remote).

// The heights of the status panel on a tall and on a wide screen, in the place of the display.
const (
	panelTallH = 132
	panelWideH = 132
)

// drawStatusBox draws the status panel in r. A tap on the nozzle or the bed sets its
// temperature. Away from the print, the taps wait for its end.
func (a *App) drawStatusBox(c *gfx.Canvas, st printer.State, r image.Rectangle) {
	c.RoundRect(r.Inset(-8), 8, edge)
	c.RoundRect(r.Inset(-6), 6, lcd)
	rows := kit.Rows(r, 0, kit.Px(46), kit.Px(8), kit.Px(24), kit.Px(20), kit.Px(20), kit.Px(6), kit.Fr(1))
	cells := kit.Cols(rows[0], 8, kit.Fr(1), kit.Fr(1), kit.Fr(1))
	fan := "off"
	if st.Fan > 0 {
		fan = fmt.Sprintf("%.0f%%", st.Fan)
	}
	if st.Reachable {
		readout(c, cells[0], "NOZZLE", fmt.Sprintf("%.0f°", st.Tool), targetText(st.ToolTarget), st.ToolPower, st.ToolTarget > 0)
		readout(c, cells[1], "BED", fmt.Sprintf("%.0f°", st.Bed), targetText(st.BedTarget), st.BedPower, st.BedTarget > 0)
		readout(c, cells[2], "FAN", fan, "", 0, false)
	} else {
		for i, l := range []string{"NOZZLE", "BED", "FAN"} {
			readout(c, cells[i], l, "--", "", 0, false)
		}
	}
	p := a.panelLines(st)
	line := func(r image.Rectangle) image.Rectangle { return kit.Inset(r, 4, 0, 4, 0) }
	l1 := line(rows[2])
	side, head := gfx.Face(true, 16), gfx.Face(true, 18)
	fits := func() bool { return gfx.Width(head, p.head)+gfx.Width(side, p.side)+12 <= l1.Dx() }
	if p.short != "" && !fits() {
		p.side = p.short
	}
	if p.side == clockText(a.Now(), a.Clock24) && !fits() {
		p.side = "" // the clock gives way to the state
	}
	sw := gfx.Width(side, p.side)
	c.Text(l1, gfx.Fit(head, p.head, l1.Dx()-sw-12), head, lcdInk, gfx.Left)
	c.Text(l1, p.side, side, lcdInk, gfx.Right)
	small := gfx.Face(false, 15)
	c.Text(line(rows[3]), gfx.Fit(small, p.msg, line(rows[3]).Dx()), small, lcdInk, gfx.Left)
	if p.shortDetail != "" && gfx.Width(small, p.detail) > line(rows[4]).Dx() {
		p.detail = p.shortDetail
	}
	c.Text(line(rows[4]), gfx.Fit(small, p.detail, line(rows[4]).Dx()), small, lcdDim, gfx.Left)
	if p.bar {
		bar := line(rows[6])
		c.RoundRect(bar, 3, lcdCell)
		if w := int(float64(bar.Dx()) * max(0, min(1, p.progress))); w > 0 {
			c.RoundRect(image.Rect(bar.Min.X, bar.Min.Y, bar.Min.X+w, bar.Max.Y), 3, lcdInk)
		}
	}

	a.region("Nozzle", cells[0])
	a.region("Bed", cells[1])
	a.region("Fan", cells[2])
	a.region("Status", image.Rect(r.Min.X, rows[2].Min.Y, r.Max.X, r.Max.Y))
	if !st.Reachable {
		return
	}
	pr := a.Printer
	nozzle := func() { a.editTemp("Nozzle temperature", int(st.ToolTarget), 300, toolPresets, pr.SetToolTemp) }
	bed := func() { a.editTemp("Bed temperature", int(st.BedTarget), 120, bedPresets, pr.SetBedTemp) }
	if a.bar {
		nozzle = func() { a.Toast(waitText) }
		bed = nozzle
	}
	a.addHit(kit.Hit{R: cells[0], Tap: nozzle})
	a.addHit(kit.Hit{R: cells[1], Tap: bed})
}

// targetText is the target of a heater in a readout: "→210", or "off".
func targetText(t float64) string {
	if t <= 0 {
		return "off"
	}
	return fmt.Sprintf("→%.0f", t)
}

// readout draws one readout of the status panel in r: the name at the top, the value, and the
// target at the right. A heater that is on has a bar of its power at the bottom.
func readout(c *gfx.Canvas, r image.Rectangle, name, value, target string, power float64, heater bool) {
	c.RoundRect(r, 6, lcdCell)
	in := kit.Inset(r, 8, 4, 8, 6)
	c.Text(image.Rect(in.Min.X, in.Min.Y, in.Max.X, in.Min.Y+14), name, gfx.Face(true, 11), lcdDim, gfx.Left)
	vr := image.Rect(in.Min.X, in.Min.Y+14, in.Max.X, in.Max.Y)
	tf := gfx.Face(false, 13)
	tw := 0
	if target != "" {
		tw = gfx.Width(tf, target) + 4
		c.Text(vr, target, tf, lcdDim, gfx.Right)
	}
	size := 22
	for size > 14 && gfx.Width(gfx.Face(true, size), value) > vr.Dx()-tw {
		size--
	}
	c.Text(vr, value, gfx.Face(true, size), lcdInk, gfx.Left)
	if heater && power > 0 {
		w := int(float64(r.Dx()-12) * min(1, power))
		c.Fill(image.Rect(r.Min.X+6, r.Max.Y-4, r.Min.X+6+w, r.Max.Y-2), orange)
	}
}

// panelText is the text of the status panel: the state and the time at the right in the first
// line, the message in the second, the details in the third, and the progress bar. short and
// shortDetail are a shorter side and a shorter detail for a narrow panel, or "".
type panelText struct {
	head, side, short, msg, detail, shortDetail string
	bar                                         bool
	progress                                    float64
}

// panelLines returns the text of the status panel for the state st.
func (a *App) panelLines(st printer.State) panelText {
	now := a.Now()
	clock := clockText(now, a.Clock24)
	pct := fmt.Sprintf("%.0f%%", st.Completion)
	hm := func(d time.Duration) string {
		m := int(d.Minutes())
		return fmt.Sprintf("%d:%02d", m/60, m%60)
	}
	or := func(s, alt string) string {
		if s != "" {
			return s
		}
		return alt
	}
	switch {
	case !st.Reachable:
		return panelText{head: "Klipper error", side: clock, msg: st.Message}
	case st.ManualProbe:
		return panelText{head: "Paper test", side: fmt.Sprintf("Z %.3f", st.ManualZ), msg: "Lower the nozzle until the paper drags"}
	}
	if st.Busy() {
		p := panelText{side: clock, bar: true, progress: st.Completion / 100}
		ends := ""
		if st.HasTimeLeft {
			p.side, p.short = hm(st.TimeLeft)+" left", hm(st.TimeLeft)
			ends = "Ends " + clockText(now.Add(st.TimeLeft), a.Clock24) + " · "
		}
		p.msg = or(st.Message, ends+hm(st.Elapsed)+" so far")
		p.detail, p.shortDetail = a.layerDetail(st)
		switch {
		case st.Paused:
			p.head = "Paused · " + pct
			p.msg = or(st.Message, "Tap Resume to go on")
		case st.Preparing:
			p.head, p.side, p.short, p.bar = "Starting the print", clock, "", false
			p.msg, p.detail, p.shortDetail = or(st.Activity, "Preparing"), st.Name(), ""
		case st.Finishing && st.HasBedCoolLeft && st.BedCoolTo > 0:
			p.head, p.side, p.short = "Cooling the bed", shortWait(st.BedCoolLeft), ""
			p.msg = fmt.Sprintf("The part comes off at %.0f°", st.BedCoolTo)
			p.detail, p.shortDetail = st.Name(), ""
			p.progress = 0
			if span := st.BedCoolFrom - st.BedCoolTo; span > 0 {
				p.progress = (st.BedCoolFrom - st.Bed) / span
			}
		case st.Finishing:
			p.head, p.detail, p.shortDetail = "Finishing the print", st.Name(), ""
		case st.Layer == 1 && st.Layers > 1:
			p.head = "First layer · " + pct
		default:
			p.head = "Printing · " + pct
		}
		return p
	}
	switch st.Finished {
	case "complete":
		return panelText{head: "Print done", side: clock, msg: st.FinishedName()}
	case "cancelled":
		return panelText{head: "Cancelled", side: clock, msg: st.FinishedName()}
	case "error":
		return panelText{head: "Print failed", side: clock, msg: st.FinishedName(), detail: st.FinishedError}
	}
	p := panelText{head: "Ready", side: clock}
	homed := "Not homed"
	if st.Homed == "xyz" {
		homed = "XYZ homed"
	}
	p.msg = or(st.Message, homed)
	if st.FirmwareOld {
		p.msg = "Firmware is old"
	}
	if st.HasZ && st.Homed != "" {
		p.detail = fmt.Sprintf("X%.0f Y%.0f Z%.2f", st.X, st.Y, st.Z)
	}
	if a.Material != "" && p.detail != "" {
		p.detail += " · " + a.Material
	} else if a.Material != "" {
		p.detail = a.Material
	}
	return p
}

// layerDetail is the line of the layer of a print, "Layer 12 of 27 · next in 1:32 · Z 3.20", and
// its short form, "12/27 · next 1:32 · Z 3.20". The first layer has the Z offset in place of the
// height.
func (a *App) layerDetail(st printer.State) (string, string) {
	if st.Layers <= 0 || st.Layer <= 0 {
		return fmt.Sprintf("Z %.2f", st.Z), ""
	}
	long, short := fmt.Sprintf("Layer %d of %d", st.Layer, st.Layers), fmt.Sprintf("%d/%d", st.Layer, st.Layers)
	if left, ok := a.layerLeft(st, st.Layer); ok {
		long += " · next in " + mmss(left.Seconds())
		short += " · next " + mmss(left.Seconds())
	}
	z := fmt.Sprintf(" · Z %.2f", st.Z)
	if st.Layer == 1 && st.Layers > 1 {
		z = fmt.Sprintf(" · Z offset %+.3f", st.ZOffset)
	}
	return long + z, short + z
}
