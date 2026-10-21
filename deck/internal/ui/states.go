package ui

import (
	"fmt"
	"image"
	"strings"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// The screens of the states of a print: preparing, the first layer, paused and done. The print
// itself is drawJob.

// finishedKey names the end of the last print, to remember that its done screen was closed.
func finishedKey(st printer.State) string {
	return fmt.Sprintf("%s|%s|%d", st.FinishedPath, st.Finished, int(st.FinishedTime.Seconds()))
}

// lastPrint is the newest print of the history, or the end of the last print that Klipper reports.
func (a *App) lastPrint(st printer.State) (printer.Record, bool) {
	if h := a.Printer.History(); len(h) > 0 {
		return h[0], true
	}
	if st.Finished != "" {
		return printer.Record{File: st.FinishedFile, Path: st.FinishedPath, Result: st.Finished,
			Duration: st.FinishedTime, Filament: st.FinishedFilament}, true
	}
	return printer.Record{}, false
}

// hm is a duration as hours and minutes, for example "1:42".
func hm(m int) string { return fmt.Sprintf("%d:%02d", m/60, m%60) }

// drawPreparing is the start of a print: the steps of the start, with the heating of the step that
// runs now.
func (a *App) drawPreparing(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	type step struct{ name, match string }
	steps := []step{{"Heat the bed", "Heating bed"}}
	if st.HasWipe {
		steps = append(steps, step{"Wipe the nozzle", "wipe"})
	}
	steps = append(steps, step{"Heat the nozzle", "Heating nozzle"}, step{"Probe and prime", "Probing"})
	now := -1
	for i, s := range steps {
		if strings.Contains(strings.ToLower(st.Activity), strings.ToLower(s.match)) {
			now = i
		}
	}
	if strings.HasPrefix(st.Activity, "Wiping") {
		now = 1
	}
	right := ""
	if now >= 0 {
		right = fmt.Sprintf("STEP %d/%d", now+1, len(steps))
	}
	charLCD(c, printLCD, lcd2("Preparing", right, strings.ReplaceAll(st.Activity, "  ", " "), ""), 20)

	box := image.Rect(24, 170, W-24, 170+len(steps)*62+24)
	panel(c, box, "PRINT START  ·  "+strings.ToUpper(gfx.Fit(gfx.Face(true, 13), st.File, 300)))
	for i, s := range steps {
		y := box.Min.Y + 24 + i*62
		done := now > i
		led(c, box.Min.X+34, y+24, i == now)
		if done {
			c.RoundRect(image.Rect(box.Min.X+28, y+18, box.Min.X+40, y+30), 6, dim)
		}
		col := text
		if !done && i != now {
			col = dim
		}
		c.Text(image.Rect(box.Min.X+60, y, box.Max.X-160, y+48), s.name, gfx.Face(true, 20), col, gfx.Left)
		info := ""
		switch {
		case done:
			info = "Done"
		case i == now && s.match == "Heating bed":
			info = fmt.Sprintf("%.0f° of %.0f°", st.Bed, st.BedTarget)
		case i == now && strings.HasPrefix(s.match, "Heating nozzle"):
			info = fmt.Sprintf("%.0f° of %.0f°", st.Tool, st.ToolTarget)
		}
		c.Text(image.Rect(box.Max.X-190, y, box.Max.X-22, y+48), info, gfx.Face(false, 17), orange, gfx.Right)
		if i == now {
			f := 0.0
			switch {
			case s.match == "Heating bed" && st.BedTarget > 0:
				f = st.Bed / st.BedTarget
			case strings.HasPrefix(s.match, "Heating nozzle") && st.ToolTarget > 0:
				f = st.Tool / st.ToolTarget
			}
			if f > 0 {
				segments(c, image.Rect(box.Min.X+60, y+44, box.Max.X-22, y+54), min(1, f))
			}
		}
	}
	a.temps(c, box.Max.Y+16, 76, st.Tool, st.ToolTarget, st.Bed, st.BedTarget, true)
	a.button(c, image.Rect(24, 636, W-24, 716), "Cancel print", raised, red, func() {
		a.confirm = &confirmBox{question: "Cancel this print?", yes: "Cancel print", danger: true, onYes: pr.Cancel}
	})
}

// zSteps are the steps of the Z offset on the first layer screen, in mm.
var zSteps = []float64{0.01, 0.025, 0.05}

// drawFirstLayer is the first layer: the Z offset is the main control, to fix the height of the
// first layer while it prints.
func (a *App) drawFirstLayer(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	right := fmt.Sprintf("%.0f%%", st.Completion)
	if st.HasTimeLeft {
		right += " " + hm(int(st.TimeLeft.Minutes()))
	}
	charLCD(c, printLCD, lcd2("First layer", right, "Z offset", fmt.Sprintf("%+.3fMM", st.ZOffset)), 20)

	z := image.Rect(24, 170, W-24, 420)
	panel(c, z, "01  Z OFFSET  ·  WATCH THE FIRST LINES")
	win := image.Rect(z.Min.X+110, z.Min.Y+34, z.Max.X-110, z.Min.Y+124)
	window(c, win)
	c.Text(win, fmt.Sprintf("%+.3f", st.ZOffset), gfx.Face(true, 48), lcdInk, gfx.Center)
	if a.zBaby == 0 {
		a.zBaby = 0.025
	}
	step := a.zBaby
	a.button(c, image.Rect(z.Min.X+18, z.Min.Y+34, z.Min.X+98, z.Min.Y+124), "−", raised, text, func() { a.do(func() error { return pr.BabyStep(-step) }) })
	a.button(c, image.Rect(z.Max.X-98, z.Min.Y+34, z.Max.X-18, z.Min.Y+124), "+", raised, text, func() { a.do(func() error { return pr.BabyStep(step) }) })
	label(c, image.Rect(z.Min.X+18, z.Min.Y+132, z.Max.X-18, z.Min.Y+154), "− moves the nozzle closer to the bed", gfx.Center)
	for i, s := range zSteps {
		r := image.Rect(z.Min.X+18+i*110, z.Min.Y+166, z.Min.X+18+i*110+100, z.Min.Y+226)
		fill, fg := raised, text
		if a.zBaby == s {
			fill, fg = orange, onAcc
		}
		v := s
		a.button(c, r, fmt.Sprint(s), fill, fg, func() { a.zBaby = v })
	}
	a.button(c, image.Rect(z.Max.X-102, z.Min.Y+166, z.Max.X-18, z.Min.Y+226), "Save", raised, text, func() {
		a.saveZOffset(st)
	})

	box := image.Rect(24, 432, W-24, 540)
	window(c, box)
	if img := pr.Preview(); img != nil {
		c.Clipped(box.Inset(2), func(in *gfx.Canvas) { in.Image(box, img) })
	}
	c.Text(image.Rect(box.Min.X+14, box.Min.Y+6, box.Max.X-14, box.Min.Y+28), fmt.Sprintf("LAYER 1 OF %d", st.Layers), gfx.Face(true, 13), lcdDim, gfx.Left)
	a.temps(c, 552, 70, st.Tool, st.ToolTarget, st.Bed, st.BedTarget, true)
	half := (W - 48 - 12) / 2
	a.button(c, image.Rect(24, 636, 24+half, 716), "Pause", raised, text, pr.Pause)
	a.button(c, image.Rect(W-24-half, 636, W-24, 716), "Cancel", raised, red, func() {
		a.confirm = &confirmBox{question: "Cancel this print?", yes: "Cancel print", danger: true, onYes: pr.Cancel}
	})
}

// saveZOffset writes the Z offset of the first layer into the config: into the probe offset with a
// probe, else into the Z endstop. SAVE_CONFIG then writes the file (Maintenance).
func (a *App) saveZOffset(st printer.State) {
	pr := a.Printer
	cmd := "Z_OFFSET_APPLY_ENDSTOP"
	if pr.Machine().Has("probe") {
		cmd = "Z_OFFSET_APPLY_PROBE"
	}
	a.confirm = &confirmBox{question: fmt.Sprintf("Save the Z offset %+.3f mm?", st.ZOffset), yes: "Save", onYes: func() {
		a.do(func() error {
			if err := pr.Gcode(cmd); err != nil {
				return err
			}
			a.Toast("Saved. Save config in Maintenance writes it.")
			return nil
		})
	}}
}

// drawPaused is a paused print: Resume first, then the actions that a pause is for.
func (a *App) drawPaused(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	charLCD(c, printLCD, lcd2("Paused", fmt.Sprintf("%.0f%%", st.Completion), st.Message, ""), 20)
	info := image.Rect(24, 170, W-24, 256)
	plate(c, info)
	where := "Paused"
	if st.Layers > 0 {
		where = fmt.Sprintf("Paused at layer %d of %d", st.Layer, st.Layers)
	}
	c.Text(image.Rect(info.Min.X+18, info.Min.Y+8, info.Max.X-18, info.Min.Y+40), where, gfx.Face(true, 19), text, gfx.Left)
	sub := "The heaters stay on until Klipper's idle timeout."
	if st.ToolTarget > 0 {
		sub = fmt.Sprintf("The nozzle stays at %.0f°.", st.ToolTarget)
	}
	c.Text(image.Rect(info.Min.X+18, info.Min.Y+40, info.Max.X-18, info.Max.Y-8), sub, gfx.Face(false, 16), dim, gfx.Left)
	a.button(c, image.Rect(24, 270, W-24, 360), "Resume", orange, onAcc, pr.Resume)
	half := (W - 48 - 12) / 2
	a.button(c, image.Rect(24, 374, 24+half, 444), "Change filament", raised, text, func() { a.swap = &swapState{} })
	a.button(c, image.Rect(W-24-half, 374, W-24, 444), "Move head", raised, text, func() { a.open(pageMove) })
	a.button(c, image.Rect(24, 456, 24+half, 526), "Tune", raised, text, func() { a.tune = true })
	a.button(c, image.Rect(W-24-half, 456, W-24, 526), "Cancel", raised, red, func() {
		a.confirm = &confirmBox{question: "Cancel this print?", yes: "Cancel print", danger: true, onYes: pr.Cancel}
	})
	a.temps(c, 546, 84, st.Tool, st.ToolTarget, st.Bed, st.BedTarget, true)
}

// releaseBed is the bed temperature below which a part comes off a PEI sheet easily.
const releaseBed = 35.0

// drawDone is the end of a print: the result, the bed until the part comes off easily, Reprint
// and Done.
func (a *App) drawDone(c *gfx.Canvas, st printer.State) {
	title := map[string]string{"complete": "Print done", "cancelled": "Cancelled", "error": "Print failed"}[st.Finished]
	right := ""
	if st.Finished == "complete" {
		right = "100%"
	}
	l2 := hm(int(st.FinishedTime.Minutes()))
	if st.FinishedFilament > 0 {
		l2 += fmt.Sprintf(" · %.1fM", st.FinishedFilament/1000)
	}
	charLCD(c, printLCD, lcd2(title, right, l2, a.Now().Format("15:04")), 20)

	box := image.Rect(90, 166, W-90, 400)
	window(c, box)
	if img := a.Printer.Preview(); img != nil {
		c.Clipped(box.Inset(2), func(in *gfx.Canvas) {
			b := img.Bounds()
			in.Image(image.Rect(box.Min.X+(box.Dx()-b.Dx())/2, box.Min.Y+(box.Dy()-b.Dy())/2, box.Max.X, box.Max.Y), img)
		})
	}
	c.Text(image.Rect(24, 410, W-24, 444), gfx.Fit(gfx.Face(true, 22), st.FinishedFile, W-48), gfx.Face(true, 22), text, gfx.Center)

	info := image.Rect(24, 458, W-24, 560)
	plate(c, info)
	if st.Finished == "error" && st.FinishedError != "" {
		c.Text(image.Rect(info.Min.X+18, info.Min.Y+8, info.Max.X-18, info.Min.Y+40), "What happened", gfx.Face(true, 19), text, gfx.Left)
		for i, l := range wrapText(gfx.Face(false, 16), st.FinishedError, info.Dx()-36) {
			if i < 2 {
				c.Text(image.Rect(info.Min.X+18, info.Min.Y+40+i*26, info.Max.X-18, info.Min.Y+66+i*26), l, gfx.Face(false, 16), dim, gfx.Left)
			}
		}
	} else {
		drawBed(c, info.Min.X+40, info.Min.Y+40, 16, orange)
		c.Text(image.Rect(info.Min.X+70, info.Min.Y+10, info.Max.X-18, info.Min.Y+44), fmt.Sprintf("Bed %.0f°", st.Bed), gfx.Face(true, 22), text, gfx.Left)
		msg := fmt.Sprintf("Easy to remove below %.0f°.", releaseBed)
		if st.Bed <= releaseBed {
			msg = "Cool enough to remove the part."
		}
		c.Text(image.Rect(info.Min.X+70, info.Min.Y+40, info.Max.X-18, info.Min.Y+66), msg, gfx.Face(false, 16), dim, gfx.Left)
		// The bar fills while the bed cools from the print temperature to the release temperature.
		f := 1.0
		if st.Bed > releaseBed {
			f = max(0, min(1, 1-(st.Bed-releaseBed)/40))
		}
		segments(c, image.Rect(info.Min.X+18, info.Min.Y+74, info.Max.X-18, info.Min.Y+88), f)
	}
	half := (W - 48 - 12) / 2
	key := finishedKey(st)
	path := st.FinishedPath
	a.button(c, image.Rect(24, 590, 24+half, 680), "Reprint", raised, text, func() { a.confirmPrint(path) })
	a.button(c, image.Rect(W-24-half, 590, W-24, 680), "Done", orange, onAcc, func() { a.doneSeen = key })
}
