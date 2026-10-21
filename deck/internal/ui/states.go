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

// drawPreparing is the start of a print: the steps of the start macro, with the step that runs now.
// The steps come from the commands that the printer runs (State.Prep). With no command stack, the
// deck guesses them from the temperatures.
func (a *App) drawPreparing(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	a.statusPanel(c, st)
	type step struct {
		name      string
		done, now bool
		kind      string
	}
	var steps []step
	if st.PrepN > 0 {
		for _, p := range st.Prep[:st.PrepN] {
			steps = append(steps, step{p.Name, p.Done, p.Now, p.Kind})
		}
	} else {
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

	rowH := min(48, (contentY1-8-keyRowH-16-belowPanel-36)/max(1, len(steps)))
	box := image.Rect(24, belowPanel, W-24, belowPanel+len(steps)*rowH+30)
	panel(c, box, "PRINT START  ·  "+strings.ToUpper(gfx.Fit(gfx.Face(true, 13), st.File, 300)))
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
		c.Text(image.Rect(box.Min.X+60, y, box.Max.X-160, y+rowH-8), s.name, gfx.Face(true, 19), col, gfx.Left)
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
		}
		c.Text(image.Rect(box.Max.X-190, y, box.Max.X-22, y+rowH-8), info, gfx.Face(false, 16), orange, gfx.Right)
		if f > 0 {
			segments(c, image.Rect(box.Min.X+60, y+rowH-12, box.Max.X-22, y+rowH-4), min(1, f))
		}
	}
	a.button(c, image.Rect(24, contentY1-8-keyRowH, W-24, contentY1-8), "Cancel print", raised, red, func() {
		a.confirm = &confirmBox{question: "Cancel this print?", yes: "Cancel print", danger: true, onYes: pr.Cancel}
	})
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
	// −, the Z offset, + and Save in one row.
	y := belowPanel
	a.button(c, image.Rect(24, y, 104, y+keyRowH), "−", raised, text, func() { a.do(func() error { return pr.BabyStep(-step) }) })
	win := image.Rect(116, y, W-206, y+keyRowH)
	window(c, win)
	label(c, image.Rect(win.Min.X+10, win.Min.Y+4, win.Max.X, win.Min.Y+22), "Z OFFSET", gfx.Left)
	c.Text(image.Rect(win.Min.X, win.Min.Y+14, win.Max.X, win.Max.Y), fmt.Sprintf("%+.3f", st.ZOffset), gfx.Face(true, 34), lcdInk, gfx.Center)
	a.button(c, image.Rect(W-194, y, W-114, y+keyRowH), "+", raised, text, func() { a.do(func() error { return pr.BabyStep(step) }) })
	a.button(c, image.Rect(W-104, y, W-24, y+keyRowH), "Save", raised, text, func() { a.saveZOffset(st) })
	// The step of a tap, and the hint.
	y += keyRowH + 8
	for i, s := range zSteps {
		r := image.Rect(24+i*84, y, 24+i*84+76, y+44)
		fill, fg := raised, text
		if a.zBaby == s {
			fill, fg = orange, onAcc
		}
		v := s
		a.button(c, r, fmt.Sprint(s), fill, fg, func() { a.zBaby = v })
	}
	c.Text(image.Rect(24+3*84, y, W-24, y+44), "− is closer", gfx.Face(false, 15), dim, gfx.Right)
	// Pause and Cancel.
	y += 44 + 8
	half := (W - 48 - 12) / 2
	a.button(c, image.Rect(24, y, 24+half, y+60), "Pause", raised, text, pr.Pause)
	a.button(c, image.Rect(W-24-half, y, W-24, y+60), "Cancel", raised, red, func() {
		a.confirm = &confirmBox{question: "Cancel this print?", yes: "Cancel print", danger: true, onYes: pr.Cancel}
	})
	// The model view, with the first layer.
	box := image.Rect(24, y+60+12, W-24, contentY1-8)
	a.modelView(c, box, st)
	c.Text(image.Rect(box.Min.X+14, box.Min.Y+8, box.Max.X-14, box.Min.Y+30), fmt.Sprintf("LAYER 1 OF %d", st.Layers), gfx.Face(true, 13), lcdDim, gfx.Left)
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
	y := belowPanel
	info := image.Rect(24, y, W-24, y+76)
	plate(c, info)
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
	y += 76 + 12
	a.button(c, image.Rect(24, y, W-24, y+88), "Resume", orange, onAcc, pr.Resume)
	y += 88 + 12
	half := (W - 48 - 12) / 2
	a.button(c, image.Rect(24, y, 24+half, y+keyRowH), "Change filament", raised, text, func() { a.swap = &swapState{} })
	a.button(c, image.Rect(W-24-half, y, W-24, y+keyRowH), "Move head", raised, text, func() { a.open(pageMove) })
	y += keyRowH + 12
	a.button(c, image.Rect(24, y, 24+half, y+keyRowH), "Tune", raised, text, func() { a.tune = true })
	a.button(c, image.Rect(W-24-half, y, W-24, y+keyRowH), "Cancel", raised, red, func() {
		a.confirm = &confirmBox{question: "Cancel this print?", yes: "Cancel print", danger: true, onYes: pr.Cancel}
	})
}

// releaseBed is the bed temperature below which a part comes off a PEI sheet easily.
const releaseBed = 35.0

// drawDone is the end of a print: the result, the bed until the part comes off easily, Reprint
// and Done.
func (a *App) drawDone(c *gfx.Canvas, st printer.State) {
	a.statusPanel(c, st)
	box := image.Rect(24, belowPanel, W-24, 424)
	a.modelView(c, box, st)
	title := map[string]string{"complete": "Print done", "cancelled": "Cancelled", "error": "Print failed"}[st.Finished]
	line := title + " · " + st.FinishedFile + " · " + hm(int(st.FinishedTime.Minutes()))
	if st.FinishedFilament > 0 {
		line += fmt.Sprintf(" · %.1f m", st.FinishedFilament/1000)
	}
	c.Text(image.Rect(24, 430, W-24, 462), gfx.Fit(gfx.Face(true, 18), line, W-48), gfx.Face(true, 18), text, gfx.Center)

	info := image.Rect(24, 470, W-24, 572)
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
	a.button(c, image.Rect(24, contentY1-8-keyRowH, 24+half, contentY1-8), "Reprint", raised, text, func() { a.confirmPrint(path) })
	a.button(c, image.Rect(W-24-half, contentY1-8-keyRowH, W-24, contentY1-8), "Done", orange, onAcc, func() { a.doneSeen = key })
}
