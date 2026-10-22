package ui

import (
	"fmt"
	"image"
	"strings"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/kit"
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

// shortWait is a short time to wait, for example "6 min", "<1 min" or "1 h 05".
func shortWait(d time.Duration) string {
	m := int(d.Round(time.Minute).Minutes())
	switch {
	case d < time.Minute:
		return "<1 min"
	case m < 60:
		return fmt.Sprintf("%d min", m)
	}
	return fmt.Sprintf("%d h %02d", m/60, m%60)
}

// drawPreparing is the start of a print: the steps of the start macro, with the step that runs now.
// The steps come from the commands that the printer runs (State.Prep). With no command stack, the
// deck guesses them from the temperatures.
func (a *App) drawPreparing(c *gfx.Canvas, st printer.State) { a.drawSteps(c, st, false) }

// drawFinishing is the end of a print: the steps of the end macro, such as the cooling of the bed.
func (a *App) drawFinishing(c *gfx.Canvas, st printer.State) { a.drawSteps(c, st, true) }

// drawSteps draws the steps of the start macro, or of the end macro when end is true.
func (a *App) drawSteps(c *gfx.Canvas, st printer.State, end bool) {
	a.statusPanel(c, st)
	// Tall: the steps and Cancel under the display. Wide: the steps in the left column; the model
	// and Cancel at the right.
	f, keys, view := a.stateAreas()
	area, cancel := keys, image.Rectangle{}
	if f.wide {
		r := kit.Rows(view, 12, kit.Fr(1), kit.Px(keyRowH))
		a.modelView(c, r[0], st)
		cancel = r[1]
	} else {
		r := kit.Rows(view, 16, kit.Fr(1), kit.Px(keyRowH))
		area, cancel = r[0], r[1]
	}
	type step struct {
		name      string
		done, now bool
		kind      string
	}
	var steps []step
	switch {
	case st.PrepN > 0:
		for _, p := range st.Prep[:st.PrepN] {
			steps = append(steps, step{p.Name, p.Done, p.Now, p.Kind})
		}
	case end:
		// No steps from the end macro: one step with what the printer does.
		steps = []step{{name: "Finish the print", now: true}}
	default:
		// A guess from the temperatures and the activity.
		names := []step{{name: "Heat the bed", kind: "heat-bed"}}
		if st.HasWipe {
			names = append(names, step{name: "Wipe the nozzle", kind: "wipe"})
		}
		names = append(names, step{name: "Heat the nozzle", kind: "heat-nozzle"}, step{name: "Probe and prime", kind: "probe"})
		now := 0
		act := strings.ToLower(st.Activity)
		for i, s := range names {
			if strings.Contains(act, strings.ToLower(strings.TrimPrefix(s.name, "Heat the "))) ||
				s.kind == "wipe" && strings.Contains(act, "wip") || s.kind == "probe" && strings.Contains(act, "prob") {
				now = i
			}
		}
		for i := range names {
			names[i].done, names[i].now = i < now, i == now
		}
		steps = names
	}

	rowH := min(48, (area.Dy()-30)/max(1, len(steps)))
	box := kit.Rows(area, 0, kit.Px(len(steps)*rowH+30))[0]
	a.region("Steps", box)
	head := "PRINT START"
	if end {
		head = "PRINT END"
	}
	panel(c, box, head+"  ·  "+strings.ToUpper(gfx.Fit(gfx.Face(true, 13), st.File, 300)))
	for i, s := range steps {
		y := box.Min.Y + 22 + i*rowH
		led(c, box.Min.X+34, y+rowH/2-4, s.now)
		if s.done {
			c.RoundRect(image.Rect(box.Min.X+28, y+rowH/2-10, box.Min.X+40, y+rowH/2+2), 6, dim)
		}
		col := text
		if !s.done && !s.now {
			col = dim
		}
		info, f := "", 0.0
		switch {
		case s.done:
			info = "Done"
		case s.now && s.kind == "heat-bed" && st.BedTarget > 0:
			info, f = fmt.Sprintf("%.0f° of %.0f°", st.Bed, st.BedTarget), st.Bed/st.BedTarget
		case s.now && s.kind == "heat-nozzle" && st.ToolTarget > 0:
			info, f = fmt.Sprintf("%.0f° of %.0f°", st.Tool, st.ToolTarget), st.Tool/st.ToolTarget
		case s.now && s.kind == "cool":
			info = fmt.Sprintf("%.0f°", st.Tool)
		case s.now && s.kind == "cool-bed" && st.HasBedCoolLeft:
			info = fmt.Sprintf("%.0f°  %s left", st.Bed, shortWait(st.BedCoolLeft))
		case s.now && s.kind == "cool-bed" && st.BedTarget > 0:
			info = fmt.Sprintf("%.0f° to %.0f°", st.Bed, st.BedTarget)
		case s.now && s.kind == "cool-bed":
			info = fmt.Sprintf("%.0f°", st.Bed)
		}
		// The name gets the space that the information does not use.
		nameR := image.Rect(box.Min.X+60, y, box.Max.X-22, y+rowH-8)
		if info != "" {
			nameR.Max.X = box.Max.X - 160
		}
		c.Text(nameR, s.name, gfx.Face(true, 19), col, gfx.Left)
		c.Text(image.Rect(box.Max.X-190, y, box.Max.X-22, y+rowH-8), info, gfx.Face(false, 16), orange, gfx.Right)
		if f > 0 {
			segments(c, image.Rect(box.Min.X+60, y+rowH-12, box.Max.X-22, y+rowH-4), min(1, f))
		}
	}
	if end {
		// The finished model under the steps on a tall screen. The print is done: only Help.
		if !f.wide {
			a.modelView(c, image.Rect(area.Min.X, box.Max.Y+16, area.Max.X, area.Max.Y), st)
		}
		// Home leaves the screen while the end macro runs. The deck follows the cooling of the bed
		// all the same, and shows the done screen at the end of the print.
		k := kit.Cols(cancel, 12, kit.Px(130), kit.Fr(1))
		if a.Setup != nil {
			a.button(c, k[0], "Help", raised, text, a.OpenHelp)
		}
		file := st.FilePath
		a.button(c, k[1], "Home", raised, text, func() { a.finishLeft, a.finishFile = true, file })
		return
	}
	if a.Setup != nil {
		k := kit.Cols(cancel, 12, kit.Px(130), kit.Fr(1))
		a.button(c, k[0], "Help", raised, text, a.OpenHelp)
		cancel = k[1]
	}
	a.button(c, cancel, "Cancel print", raised, red, a.askCancel)
}

// zSteps are the steps of the Z offset on the first layer screen, in mm.
var zSteps = []float64{0.01, 0.025, 0.05}

// drawFirstLayer is the first layer: the Z offset is the main control, to fix the height of the
// first layer while it prints, and the model view shows the first layer.
func (a *App) drawFirstLayer(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	a.statusPanel(c, st)
	if a.zBaby == 0 {
		a.zBaby = 0.025
	}
	step := a.zBaby
	// −, the Z offset, + and Save in one row; the steps of a tap; Pause and Cancel; the model view
	// with the first layer.
	f, keys, view := a.stateAreas()
	r := kit.Rows(keys, 8, kit.Px(keyRowH), kit.Px(44), kit.Px(60), kit.Fr(1))
	box := r[3]
	if f.wide {
		box = view
	}
	kw := 80 // the keys of the Z row; narrower in the left column of a wide screen
	if f.wide {
		kw = 64
	}
	z := kit.Cols(r[0], 12, kit.Px(kw), kit.Fr(1), kit.Px(kw), kit.Px(kw))
	a.button(c, z[0], "−", raised, text, func() { a.do(func() error { return pr.BabyStep(-step) }) })
	win := z[1]
	window(c, win)
	a.region("Z offset", win)
	a.region("− and +", z[0].Union(z[2]))
	label(c, image.Rect(win.Min.X+10, win.Min.Y+4, win.Max.X, win.Min.Y+22), "Z OFFSET", gfx.Left)
	c.Text(image.Rect(win.Min.X, win.Min.Y+14, win.Max.X, win.Max.Y), fmt.Sprintf("%+.3f", st.ZOffset), gfx.Face(true, 34), lcdInk, gfx.Center)
	a.button(c, z[2], "+", raised, text, func() { a.do(func() error { return pr.BabyStep(step) }) })
	a.button(c, z[3], "Save", raised, text, func() { a.saveZOffset(st) })
	steps := kit.Cols(r[1], 8, kit.Px(76), kit.Px(76), kit.Px(76), kit.Fr(1))
	a.region("Step", steps[0].Union(steps[2]))
	for i, s := range zSteps {
		fill, fg := raised, text
		if a.zBaby == s {
			fill, fg = orange, onAcc
		}
		v := s
		a.button(c, steps[i], fmt.Sprint(s), fill, fg, func() { a.zBaby = v })
	}
	c.Text(steps[3], "− is closer", gfx.Face(false, 15), dim, gfx.Right)
	pc := kit.Cols(r[2], 12, kit.Fr(1), kit.Fr(1))
	if a.Setup != nil {
		pc = kit.Cols(r[2], 12, kit.Fr(1), kit.Fr(1), kit.Fr(1))
		a.button(c, pc[2], "Help", raised, text, a.OpenHelp)
	}
	a.button(c, pc[0], "Pause", raised, text, pr.Pause)
	a.button(c, pc[1], "Cancel", raised, red, a.askCancel)
	a.modelView(c, box, st)
	layer := image.Rect(box.Min.X+14, box.Min.Y+8, box.Max.X-14, box.Min.Y+30)
	c.Text(layer, fmt.Sprintf("LAYER 1 OF %d", st.Layers), gfx.Face(true, 13), lcdDim, gfx.Left)
	a.textRegion("Layer", layer, gfx.Face(true, 13), fmt.Sprintf("LAYER 1 OF %d", st.Layers))
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
	a.statusPanel(c, st)
	// Tall: the pause, Resume, and two rows of keys. Wide: Resume and the keys at the left, the
	// pause and the model view at the right.
	f, keys, view := a.stateAreas()
	var info, resume image.Rectangle
	var grid []image.Rectangle
	infoH := 76
	if st.PausePrompt != "" {
		infoH = 112 // the message of the file, in two lines, and the height check
	}
	if f.wide {
		v := kit.Rows(view, 12, kit.Px(infoH), kit.Fr(1))
		info = v[0]
		a.modelView(c, v[1], st)
		k := kit.Rows(keys, 12, kit.Px(88), kit.Fr(1))
		resume, grid = k[0], kit.Grid(k[1], 2, 2, 12)
	} else {
		r := kit.Rows(view, 12, kit.Px(infoH), kit.Px(88), kit.Px(2*keyRowH+12), kit.Fr(1))
		info, resume, grid = r[0], r[1], kit.Grid(r[2], 2, 2, 12)
		a.modelView(c, r[3], st)
	}
	plate(c, info)
	a.region("Where", info)
	if st.PausePrompt != "" {
		a.drawPausePrompt(c, st, info, resume, grid)
		return
	}
	where := "Paused"
	if st.Layers > 0 {
		where = fmt.Sprintf("Paused at layer %d of %d", st.Layer, st.Layers)
	}
	c.Text(image.Rect(info.Min.X+18, info.Min.Y+6, info.Max.X-18, info.Min.Y+38), where, gfx.Face(true, 19), text, gfx.Left)
	sub := "The heaters stay on until Klipper's idle timeout."
	switch {
	case st.Message != "":
		sub = st.Message
	case st.ToolTarget > 0:
		sub = fmt.Sprintf("The nozzle stays at %.0f°.", st.ToolTarget)
	}
	c.Text(image.Rect(info.Min.X+18, info.Min.Y+38, info.Max.X-18, info.Max.Y-6), gfx.Fit(gfx.Face(false, 16), sub, info.Dx()-36), gfx.Face(false, 16), dim, gfx.Left)
	a.button(c, resume, "Resume", orange, onAcc, pr.Resume)
	a.button(c, grid[0], "Change filament", raised, text, func() { a.openSwap(swapChange) })
	a.button(c, grid[1], "Move head", raised, text, func() { a.open(pageMove) })
	a.button(c, grid[2], "Tune", raised, text, func() { a.tune = true })
	a.button(c, grid[3], "Cancel", raised, red, a.askCancel)
}

// drawDone is the end of a print: the result, the bed until the part comes off easily, Reprint
// and Done.
func (a *App) drawDone(c *gfx.Canvas, st printer.State) {
	a.statusPanel(c, st)
	// Tall: the model, the result, the bed, and the keys. Wide: the bed and the keys at the left,
	// the model and the result at the right.
	f, keys, view := a.stateAreas()
	var box, result, info, end image.Rectangle
	if f.wide {
		v := kit.Rows(view, 8, kit.Fr(1), kit.Px(32))
		box, result = v[0], v[1]
		k := kit.Rows(keys, 12, kit.Px(102), kit.Px(keyRowH))
		info, end = k[0], k[1]
	} else {
		r := kit.Rows(view, 8, kit.Px(190), kit.Px(32), kit.Px(102), kit.Fr(1), kit.Px(keyRowH))
		box, result, info, end = r[0], r[1], r[2], r[4]
	}
	a.modelView(c, box, st)
	title := map[string]string{"complete": "Print done", "cancelled": "Cancelled", "error": "Print failed"}[st.Finished]
	line := title + " · " + st.FinishedFile + " · " + hm(int(st.FinishedTime.Minutes()))
	if st.FinishedFilament > 0 {
		line += " · " + a.weight(st.FinishedFilament)
	}
	c.Text(result, gfx.Fit(gfx.Face(true, 18), line, result.Dx()), gfx.Face(true, 18), text, gfx.Center)
	a.region("Result", result)

	plate(c, info)
	a.region("Bed", info)
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
		msg := fmt.Sprintf("Easy to remove below %.0f°.", printer.ReleaseBed)
		if st.HasBedCoolLeft && st.BedCoolTo == printer.ReleaseBed {
			msg = "Easy to remove in about " + shortWait(st.BedCoolLeft) + "."
			if st.BedCoolLeft < time.Minute {
				msg = "Easy to remove in a minute."
			}
		}
		if st.Bed <= printer.ReleaseBed {
			msg = "Cool enough to remove the part."
		}
		c.Text(image.Rect(info.Min.X+70, info.Min.Y+40, info.Max.X-18, info.Min.Y+66), msg, gfx.Face(false, 16), dim, gfx.Left)
		// The bar fills while the bed cools from the print temperature to the release temperature.
		f := 1.0
		if st.Bed > printer.ReleaseBed {
			f = max(0, min(1, 1-(st.Bed-printer.ReleaseBed)/40))
		}
		segments(c, image.Rect(info.Min.X+18, info.Min.Y+74, info.Max.X-18, info.Min.Y+88), f)
	}
	key := finishedKey(st)
	path := st.FinishedPath
	k := kit.Cols(end, 12, kit.Fr(1), kit.Fr(1))
	a.button(c, k[0], "Reprint", raised, text, func() { a.confirmPrint(path) })
	a.button(c, k[1], "Done", orange, onAcc, func() { a.doneSeen = key })
}

// drawPausePrompt is a pause of the file (PRINTDECK_PAUSE): its message, and the height check on
// the part. Resume asks first when the height is not checked.
func (a *App) drawPausePrompt(c *gfx.Canvas, st printer.State, info, resume image.Rectangle, grid []image.Rectangle) {
	pr := a.Printer
	face := gfx.Face(true, 17)
	lines := wrapText(face, st.PausePrompt, info.Dx()-36)
	sub := ""
	switch {
	case st.PauseChecked:
		sub = fmt.Sprintf("Height checked: %+.2f mm from the file.", st.PauseDelta)
	case st.PauseCheck:
		sub = "Then check the height of the part."
	}
	y := info.Min.Y + 6
	for i, l := range lines {
		if i == 2 {
			break
		}
		c.Text(image.Rect(info.Min.X+18, y, info.Max.X-18, y+28), l, face, text, gfx.Left)
		y += 28
	}
	if sub != "" {
		c.Text(image.Rect(info.Min.X+18, y+2, info.Max.X-18, info.Max.Y-6), gfx.Fit(gfx.Face(false, 16), sub, info.Dx()-36), gfx.Face(false, 16), dim, gfx.Left)
	}
	a.button(c, resume, "Resume", orange, onAcc, func() {
		if st.PauseCheck && !st.PauseChecked {
			a.confirm = &confirmBox{question: "Resume with no height check on the part?", yes: "Resume", onYes: pr.Resume}
			return
		}
		pr.Resume()
	})
	check := func() { a.do(func() error { return pr.Gcode("PRINTDECK_CHECK_HEIGHT") }) }
	if st.PauseCheck {
		fill, ink := raised, text
		if !st.PauseChecked {
			fill, ink = orange, onAcc
		}
		a.button(c, grid[0], "Check the height", fill, ink, check)
	} else {
		a.button(c, grid[0], "Change filament", raised, text, func() { a.openSwap(swapChange) })
	}
	a.button(c, grid[1], "Move head", raised, text, func() { a.open(pageMove) })
	a.button(c, grid[2], "Tune", raised, text, func() { a.tune = true })
	a.button(c, grid[3], "Cancel", raised, red, a.askCancel)
}
