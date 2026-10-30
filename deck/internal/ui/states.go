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
		return printer.Record{File: st.FinishedFile, Title: st.FinishedTitle, Path: st.FinishedPath, Result: st.Finished,
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
	// Tall: the steps and Cancel under the display. Wide: the Main page, with the steps and the
	// model, and Cancel print and Help at the bottom.
	wide := W > H
	var area, cancel image.Rectangle
	if wide {
		title := "Starting the print"
		if end {
			title = "Finishing the print"
		}
		keysH := actH
		if end && a.Setup == nil {
			keysH = 0
		}
		area, cancel = a.statePage(c, st, title, keysH)
	} else {
		a.statusPanel(c, st)
		_, _, view := a.stateAreas()
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

	stepInfo := func(s step) (string, float64) {
		switch {
		case s.done:
			return "Done", 0
		case s.now && s.kind == "heat-bed" && st.BedTarget > 0:
			return fmt.Sprintf("%.0f° of %.0f°", st.Bed, st.BedTarget), st.Bed / st.BedTarget
		case s.now && s.kind == "heat-nozzle" && st.ToolTarget > 0:
			return fmt.Sprintf("%.0f° of %.0f°", st.Tool, st.ToolTarget), st.Tool / st.ToolTarget
		case s.now && s.kind == "cool":
			return fmt.Sprintf("%.0f°", st.Tool), 0
		case s.now && s.kind == "cool-bed" && st.HasBedCoolLeft:
			return fmt.Sprintf("%.0f°  %s left", st.Bed, shortWait(st.BedCoolLeft)), 0
		case s.now && s.kind == "cool-bed" && st.BedTarget > 0:
			return fmt.Sprintf("%.0f° to %.0f°", st.Bed, st.BedTarget), 0
		case s.now && s.kind == "cool-bed":
			return fmt.Sprintf("%.0f°", st.Bed), 0
		}
		return "", 0
	}
	infoF := gfx.Face(false, 16)
	// under is true for a step whose information goes under its name: with the large text, when
	// the name and the information do not fit in one line.
	under := func(s step, box image.Rectangle) bool {
		info, _ := stepInfo(s)
		return gfx.FinePrint() && info != "" &&
			gfx.Width(gfx.Face(true, 19), s.name)+12+gfx.Width(infoF, info) > box.Dx()-82
	}
	maxH := 48
	for _, s := range steps {
		if under(s, area) {
			maxH = 48 + stepInfoH
		}
	}
	rowH := min(maxH, (area.Dy()-30)/max(1, len(steps)))
	box := kit.Rows(area, 0, kit.Px(len(steps)*rowH+30))[0]
	a.region("Steps", box)
	head := "PRINT START"
	if end {
		head = "PRINT END"
	}
	panel(c, box, head+"  ·  "+strings.ToUpper(gfx.Fit(gfx.Face(true, 13), st.Name(), 300)))
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
		info, f := stepInfo(s)
		// The name gets the space that the information does not use.
		nameR := image.Rect(box.Min.X+60, y, box.Max.X-22, y+rowH-12)
		infoR := image.Rect(box.Max.X-190, y, box.Max.X-22, y+rowH-12)
		switch {
		case under(s, box):
			nameR.Max.Y = y + rowH - 12 - stepInfoH
			infoR = image.Rect(nameR.Min.X, nameR.Max.Y, nameR.Max.X, y+rowH-12)
		case info != "":
			nameR.Max.X = min(box.Max.X-160, infoR.Max.X-min(infoR.Dx(), gfx.Width(gfx.Shrink(infoF, info, infoR.Dx()), info))-12)
		}
		c.Text(nameR, s.name, gfx.Face(true, 19), col, gfx.Left)
		if under(s, box) {
			c.Text(infoR, info, infoF, orange, gfx.Left)
		} else {
			c.Text(infoR, info, infoF, orange, gfx.Right)
		}
		if f > 0 {
			// A thin bar under the name, with a gap: the segments of a meter crowd the line.
			bar := image.Rect(box.Min.X+60, y+rowH-8, box.Max.X-22, y+rowH-4)
			c.RoundRect(bar, 2, mix(surface, dim, 0.3))
			c.RoundRect(image.Rect(bar.Min.X, bar.Min.Y, bar.Min.X+int(float64(bar.Dx())*min(1, f)), bar.Max.Y), 2, orange)
		}
	}
	// The model under the steps: on a tall screen at the end, on a wide screen when it has the room.
	if model := image.Rect(area.Min.X, box.Max.Y+16, area.Max.X, area.Max.Y); end || wide && model.Dy() >= 120 {
		a.modelView(c, model, st)
	}
	if wide {
		// Cancel print, the destructive action, at the left, Help, and Share at the right. Main is
		// the print, so the page needs no Menu.
		share := !end && a.canShare()
		sizes := []kit.Size{kit.Fr(1), kit.Fr(1)}
		if share && a.Setup != nil {
			sizes = append(sizes, kit.Fr(1))
		}
		k := kit.Cols(cancel, 12, sizes...)
		if !end {
			a.button(c, k[0], "Cancel print", raised, red, a.askCancel)
		}
		if a.Setup != nil {
			a.button(c, k[1], "Help", raised, text, a.OpenHelp)
		}
		if share {
			a.shareKey(c, k[len(k)-1])
		}
		return
	}
	keysIn := func(widths ...kit.Size) []image.Rectangle { return kit.Cols(cancel, 12, widths...) }
	if end {
		// Menu leaves the screen while the end macro runs. The deck follows the cooling of the bed
		// all the same, and shows the done screen at the end of the print.
		k := keysIn(kit.Px(130), kit.Fr(1))
		if a.Setup != nil {
			a.button(c, k[0], "Help", raised, text, a.OpenHelp)
		}
		a.button(c, k[1], "Menu", raised, text, func() { a.leavePrint(st) })
		return
	}
	// Help, Share, Menu and Cancel print. Menu leaves the start of the print; the print goes on.
	sizes := []kit.Size{kit.Px(130), kit.Fr(1)}
	if a.Setup != nil {
		sizes = []kit.Size{kit.Px(110), kit.Px(110), kit.Fr(1)}
	}
	cancelLabel := "Cancel print"
	if a.canShare() {
		sizes = append([]kit.Size{kit.Px(110)}, sizes...)
		if a.Setup != nil {
			// Four keys of the same width: the short label of Cancel fits.
			sizes, cancelLabel = []kit.Size{kit.Fr(1), kit.Fr(1), kit.Fr(1), kit.Fr(1)}, "Cancel"
		}
	}
	k := keysIn(sizes...)
	if a.canShare() {
		a.shareKey(c, k[0])
		k = k[1:]
	}
	if a.Setup != nil {
		a.button(c, k[0], "Help", raised, text, a.OpenHelp)
		k = k[1:]
	}
	a.button(c, k[0], "Menu", raised, text, func() { a.leavePrint(st) })
	a.button(c, k[1], cancelLabel, raised, red, a.askCancel)
}

// zSteps are the steps of the Z offset on the first layer screen, in mm.
var zSteps = []float64{0.01, 0.025, 0.05}

// drawFirstLayer is the first layer: the Z offset is the main control, to fix the height of the
// first layer while it prints, and the model view shows the first layer.
func (a *App) drawFirstLayer(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	if a.zBaby == 0 {
		a.zBaby = 0.025
	}
	step := a.zBaby
	// −, the Z offset, + and Save in one row; the steps of a tap; the model view with the first
	// layer; Pause and Cancel. A wide screen has them in the Main page, with Cancel at the left of
	// the keys at the bottom and Pause at the right.
	wide := W > H
	var zRow, stepRow, printRow, box image.Rectangle
	if wide {
		view, keys := a.statePage(c, st, "First layer", actH)
		r := kit.Rows(view, 10, kit.Px(keyRowH), kit.Px(44), kit.Fr(1))
		zRow, stepRow, box, printRow = r[0], r[1], r[2], keys
	} else {
		a.statusPanel(c, st)
		_, keys, _ := a.stateAreas()
		r := kit.Rows(keys, 8, kit.Px(keyRowH), kit.Px(44), kit.Px(60), kit.Fr(1))
		zRow, stepRow, printRow, box = r[0], r[1], r[2], r[3]
	}
	z := kit.Cols(zRow, 12, kit.Px(80), kit.Fr(1), kit.Px(80), kit.Px(80))
	a.button(c, z[0], "−", raised, text, func() { a.do(func() error { return pr.BabyStep(-step) }) })
	win := z[1]
	window(c, win)
	a.region("Z offset", win)
	a.region("− and +", z[0].Union(z[2]))
	label(c, image.Rect(win.Min.X+10, win.Min.Y+4, win.Max.X, win.Min.Y+22), "Z offset", gfx.Left)
	c.Text(image.Rect(win.Min.X, win.Min.Y+14, win.Max.X, win.Max.Y), fmt.Sprintf("%+.3f", st.ZOffset), gfx.Face(true, 30), lcdInk, gfx.Center)
	a.button(c, z[2], "+", raised, text, func() { a.do(func() error { return pr.BabyStep(step) }) })
	a.button(c, z[3], "Save", raised, text, func() { a.saveZOffset(st) })
	steps := kit.Cols(stepRow, 8, kit.Px(76), kit.Px(76), kit.Px(76), kit.Fr(1))
	c.Text(steps[3], "− is closer", gfx.Face(false, 15), dim, gfx.Right)
	a.region("Step", steps[0].Union(steps[2]))
	for i, s := range zSteps {
		fill, fg := keyOff, text
		if a.zBaby == s {
			fill, fg = keyOn, keyOnInk
		}
		v := s
		a.button(c, steps[i], fmt.Sprint(s), fill, fg, func() { a.zBaby = v })
	}
	if wide {
		// Cancel, Help, Share and Pause. Share goes in the place of Help with no Help.
		sizes := []kit.Size{kit.Fr(1), kit.Fr(1), kit.Fr(1)}
		if a.canShare() && a.Setup != nil {
			sizes = append(sizes, kit.Fr(1))
		}
		pc := kit.Cols(printRow, 12, sizes...)
		a.button(c, pc[0], "Cancel", raised, red, a.askCancel)
		mid := pc[1 : len(pc)-1]
		if a.Setup != nil {
			a.button(c, mid[0], "Help", raised, text, a.OpenHelp)
			mid = mid[1:]
		}
		if a.canShare() {
			a.shareKey(c, mid[0])
		}
		a.button(c, pc[len(pc)-1], "Pause", orange, onAcc, pr.Pause)
	} else {
		// Pause, Cancel, Menu, then Help and Share.
		sizes := []kit.Size{kit.Fr(1), kit.Fr(1), kit.Fr(1)}
		if a.Setup != nil {
			sizes = append(sizes, kit.Fr(1))
		}
		if a.canShare() {
			sizes = append(sizes, kit.Fr(1))
		}
		gap := 12
		if len(sizes) > 3 {
			gap = 8
		}
		pc := kit.Cols(printRow, gap, sizes...)
		extra := pc[3:]
		if a.Setup != nil {
			a.button(c, extra[0], "Help", raised, text, a.OpenHelp)
			extra = extra[1:]
		}
		if a.canShare() {
			a.shareKey(c, extra[0])
		}
		a.button(c, pc[0], "Pause", raised, text, pr.Pause)
		a.button(c, pc[1], "Cancel", raised, red, a.askCancel)
		a.button(c, pc[2], "Menu", raised, text, func() { a.leavePrint(st) })
	}
	a.modelView(c, box, st)
	layer := image.Rect(box.Min.X+14, box.Min.Y+8, box.Max.X-14, box.Min.Y+30)
	line := fmt.Sprintf("LAYER 1 OF %d", st.Layers)
	if left, ok := a.firstLayerLeft(st); ok {
		line += " · DONE IN " + strings.ToUpper(shortWait(left)) + " · " + strings.ToUpper(clockText(a.Now().Add(left), a.Clock24))
	}
	c.Text(layer, line, gfx.Face(true, 13), lcdDim, gfx.Left)
	a.textRegion("Layer", layer, gfx.Face(true, 13), line)
}

// firstLayerLeft is the time until the end of the first layer: the plan of the slicer from the
// file position to the second layer, scaled by the time left of the print over its plan, so that
// the speed of the print now counts.
func (a *App) firstLayerLeft(st printer.State) (time.Duration, bool) { return a.layerLeft(st, 1) }

// layerLeft is the time until the end of the layer of the print st, as firstLayerLeft.
func (a *App) layerLeft(st printer.State, layer int) (time.Duration, bool) {
	lp, ok := a.Printer.(interface {
		LayerLeft(pos int64, layer int) (toLayerEnd, toEnd float64, ok bool)
	})
	if !ok {
		return 0, false
	}
	toLayer, toEnd, ok := lp.LayerLeft(st.FilePos, layer)
	if !ok || toLayer <= 0 {
		return 0, false
	}
	f := 1.0
	if st.HasTimeLeft && toEnd > 0 {
		f = max(0.5, min(3, st.TimeLeft.Seconds()/toEnd))
	}
	return time.Duration(toLayer * f * float64(time.Second)), true
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
	// Tall: the pause, Resume, and two rows of keys. Wide: the Main page, with the pause and the
	// model, and two rows of keys at the bottom: Change filament, Move head and Tune; then Cancel at
	// the left and Resume at the right.
	var info, resume image.Rectangle
	var grid []image.Rectangle
	infoH := 76
	if st.PausePrompt != "" {
		infoH = 112 // the message of the file, in two lines, and the height check
	}
	if W > H {
		view, keys := a.statePage(c, st, "Paused", 2*actH+12)
		v := kit.Rows(view, 12, kit.Px(infoH), kit.Fr(1))
		info = v[0]
		a.modelView(c, v[1], st)
		k := kit.Rows(keys, 12, kit.Fr(1), kit.Fr(1))
		top := kit.Cols(k[0], 12, kit.Fr(3), kit.Fr(2), kit.Fr(2)) // Change filament is the longest
		bottom := kit.Cols(k[1], 12, kit.Fr(1), kit.Fr(2))
		grid, resume = []image.Rectangle{top[0], top[1], top[2], bottom[0]}, bottom[1]
	} else {
		a.statusPanel(c, st)
		_, _, view := a.stateAreas()
		if gfx.FinePrint() {
			infoH = pausedInfoH(st, view.Dx()-36)
		}
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
	sub := pausedSub(st)
	if gfx.FinePrint() {
		for i, l := range subLines(sub, info.Dx()-36) {
			y := info.Min.Y + 38 + i*largeSubH
			c.Text(image.Rect(info.Min.X+18, y, info.Max.X-18, y+largeSubH), l, gfx.Face(false, 16), dim, gfx.Left)
		}
	} else {
		c.Text(image.Rect(info.Min.X+18, info.Min.Y+38, info.Max.X-18, info.Max.Y-6), sub, gfx.Face(false, 16), dim, gfx.Left)
	}
	a.button(c, resume, "Resume", orange, onAcc, pr.Resume)
	a.button(c, grid[0], "Change filament", raised, text, func() { a.openSwap(swapChange) })
	a.button(c, grid[1], "Move head", raised, text, func() { a.open(pageMove) })
	a.button(c, grid[2], "Tune", raised, text, func() { a.tune = true })
	a.button(c, grid[3], "Cancel", raised, red, a.askCancel)
}

// stepInfoH is the height of the information of a step under its name, with the large text.
const stepInfoH = 26

// pausedSub is the line under the title of a pause.
func pausedSub(st printer.State) string {
	switch {
	case st.Message != "":
		return st.Message
	case st.ToolTarget > 0:
		return fmt.Sprintf("The nozzle stays at %.0f°.", st.ToolTarget)
	}
	return "The heaters stay on until Klipper's idle timeout."
}

// pausePromptSub is the line under the message of the file in a pause.
func pausePromptSub(st printer.State) string {
	switch {
	case st.PauseChecked:
		return fmt.Sprintf("Height checked: %+.2f mm from the file.", st.PauseDelta)
	case st.PauseCheck:
		return "Then check the height of the part."
	}
	return ""
}

// pausedInfoH is the height of the plate of a pause with the large text, for text of the width w.
func pausedInfoH(st printer.State, w int) int {
	if st.PausePrompt == "" {
		return 6 + 32 + len(subLines(pausedSub(st), w))*largeSubH + 6
	}
	h := 6 + min(2, len(wrapText(gfx.Face(true, 17), st.PausePrompt, w)))*28 + 6
	if sub := pausePromptSub(st); sub != "" {
		h += 2 + len(subLines(sub, w))*largeSubH
	}
	return h
}

// finishedView draws the finished print in box: the model as the print left it. The model view of
// a running print (modelView) has no picture after the end of the print.
func (a *App) finishedView(c *gfx.Canvas, box image.Rectangle, st printer.State) {
	window(c, box)
	a.region("Model", box)
	r, ok := a.lastPrint(st)
	if !ok {
		return
	}
	img, loading := a.Printer.RecordPicture(r)
	switch {
	case img != nil:
		picture(c, box, img)
	case loading:
		c.Text(box, "LOADING MODEL…", gfx.Face(true, 18), lcdDim, gfx.Center)
		a.redrawSoon(250 * time.Millisecond) // the picture is drawn in the background
	default:
		c.Text(box, "NO PICTURE OF THE MODEL", gfx.Face(true, 18), lcdDim, gfx.Center)
	}
}

// drawDone is the end of a print: the result, the bed until the part comes off easily, Reprint
// and Done.
func (a *App) drawDone(c *gfx.Canvas, st printer.State) {
	// Tall: the model, the result, the bed, and the keys. Wide: the Main page, with the model, the
	// result and the bed, and Reprint and Done at the bottom.
	title := map[string]string{"complete": "Print done", "cancelled": "Cancelled", "error": "Print failed"}[st.Finished]
	var box, result, info, end image.Rectangle
	if W > H {
		view, keys := a.statePage(c, st, title, actH)
		v := kit.Rows(view, 8, kit.Fr(1), kit.Px(32), kit.Px(102))
		box, result, info, end = v[0], v[1], v[2], keys
	} else {
		a.statusPanel(c, st)
		_, _, view := a.stateAreas()
		// The model gets the height that is free: a taller screen shows a larger model.
		resultH := 32
		if gfx.FinePrint() {
			resultH = 60 // two lines
		}
		infoH := 102
		if gfx.FinePrint() && st.Finished == "error" && st.FinishedError != "" {
			infoH = 128 // three lines of the error
		}
		r := kit.Rows(view, 8, kit.Fr(1), kit.Px(resultH), kit.Px(infoH), kit.Px(keyRowH))
		box, result, info, end = r[0], r[1], r[2], r[3]
	}
	a.finishedView(c, box, st)
	what := hm(int(st.FinishedTime.Minutes()))
	if st.FinishedFilament > 0 {
		what += " · " + a.weight(st.FinishedFilament)
	}
	line := title + " · " + st.FinishedName() + " · " + what
	if gfx.FinePrint() && result.Dy() >= 60 {
		// The large text: the result and the file, and under them the time and the filament.
		lines := kit.Rows(result, 0, kit.Fr(1), kit.Fr(1))
		c.Text(lines[0], title+" · "+st.FinishedName(), gfx.Face(true, 18), text, gfx.Center)
		c.Text(lines[1], what, gfx.Face(true, 18), text, gfx.Center)
	} else {
		c.Text(result, line, gfx.Face(true, 18), text, gfx.Center)
	}
	a.region("Result", result)

	plate(c, info)
	a.region("Bed", info)
	if st.Finished == "error" && st.FinishedError != "" {
		c.Text(image.Rect(info.Min.X+18, info.Min.Y+8, info.Max.X-18, info.Min.Y+40), "What happened", gfx.Face(true, 19), text, gfx.Left)
		for i, l := range wrapText(gfx.Face(false, 16), st.FinishedError, info.Dx()-36) {
			if i < 2 || i < 3 && info.Dy() >= 128 {
				c.Text(image.Rect(info.Min.X+18, info.Min.Y+40+i*26, info.Max.X-18, info.Min.Y+66+i*26), l, gfx.Face(false, 16), dim, gfx.Left)
			}
		}
	} else {
		drawBed(c, info.Min.X+40, info.Min.Y+40, 16, orange)
		c.Text(image.Rect(info.Min.X+70, info.Min.Y+10, info.Max.X-18, info.Min.Y+44), fmt.Sprintf("Bed %.0f°", st.Bed), gfx.Face(true, 22), text, gfx.Left)
		msg := fmt.Sprintf("Easy to remove below %.0f°.", printer.ReleaseBed)
		if st.HasBedCoolLeft && st.BedCoolTo == printer.ReleaseBed {
			msg = "Easy to remove in " + shortWait(st.BedCoolLeft) + "."
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
	seen := finishedKey(st)
	path := st.FinishedPath
	video, saving := a.doneVideo(st)
	if a.canShare() {
		// Share is in the place of the Reprint key's neighbor: Reprint, Share, the time lapse, Done.
		sizes := []kit.Size{kit.Fr(1), kit.Fr(1), kit.Fr(1)}
		if video != "" || saving {
			sizes = append(sizes, kit.Fr(1))
		}
		k := kit.Cols(end, 12, sizes...)
		a.button(c, k[0], "Reprint", raised, text, func() { a.confirmPrint(path) })
		a.shareKey(c, k[1])
		if len(k) == 4 {
			a.lapseKey(c, k[2], st, video, saving)
		}
		a.button(c, k[len(k)-1], "Done", orange, onAcc, func() { a.doneSeen = seen })
		return
	}
	if video == "" && !saving {
		k := kit.Cols(end, 12, kit.Fr(1), kit.Fr(1))
		a.button(c, k[0], "Reprint", raised, text, func() { a.confirmPrint(path) })
		a.button(c, k[1], "Done", orange, onAcc, func() { a.doneSeen = seen })
		return
	}
	k := kit.Cols(end, 12, kit.Fr(1), kit.Fr(1), kit.Fr(1))
	a.button(c, k[0], "Reprint", raised, text, func() { a.confirmPrint(path) })
	a.lapseKey(c, k[1], st, video, saving)
	a.button(c, k[2], "Done", orange, onAcc, func() { a.doneSeen = seen })
}

// lapseKey draws the Time lapse key of the done page, or Saving… while the deck writes the video.
func (a *App) lapseKey(c *gfx.Canvas, r image.Rectangle, st printer.State, video string, saving bool) {
	if saving {
		// The deck writes the video at the end of the print: a few seconds.
		key(c, r, "Saving…", surface, dim, false)
		a.region("Time lapse", r)
		a.redrawSoon(500 * time.Millisecond)
		return
	}
	name := st.FinishedName()
	a.button(c, r, "Time lapse", raised, text, func() { a.openPlayer(video, name) })
}

// doneVideo is the time lapse video of the print that ended: its name, or saving while the deck
// writes the video. A print with no time lapse, or a video that the deck removed, has neither.
func (a *App) doneVideo(st printer.State) (name string, saving bool) {
	if a.TimeLapse == nil {
		return "", false
	}
	if h := a.Printer.History(); len(h) > 0 {
		r := h[0]
		if r.Path == st.FinishedPath && r.Result == st.Finished && int(r.Duration.Seconds()) == int(st.FinishedTime.Seconds()) && r.Video != "" {
			if _, ok := a.TimeLapse.VideoPath(r.Video); ok {
				return r.Video, false
			}
			return "", false
		}
	}
	in := a.TimeLapse.Info()
	return "", (in.Saving || in.Recording) && in.Frames > 0
}

// drawPausePrompt is a pause of the file (PRINTDECK_PAUSE): its message, and the height check on
// the part. Resume asks first when the height is not checked.
func (a *App) drawPausePrompt(c *gfx.Canvas, st printer.State, info, resume image.Rectangle, grid []image.Rectangle) {
	pr := a.Printer
	face := gfx.Face(true, 17)
	lines := wrapText(face, st.PausePrompt, info.Dx()-36)
	sub := pausePromptSub(st)
	y := info.Min.Y + 6
	for i, l := range lines {
		if i == 2 {
			break
		}
		c.Text(image.Rect(info.Min.X+18, y, info.Max.X-18, y+28), l, face, text, gfx.Left)
		y += 28
	}
	switch {
	case sub != "" && gfx.FinePrint():
		for i, l := range subLines(sub, info.Dx()-36) {
			ly := y + 2 + i*largeSubH
			c.Text(image.Rect(info.Min.X+18, ly, info.Max.X-18, ly+largeSubH), l, gfx.Face(false, 16), dim, gfx.Left)
		}
	case sub != "":
		c.Text(image.Rect(info.Min.X+18, y+2, info.Max.X-18, info.Max.Y-6), sub, gfx.Face(false, 16), dim, gfx.Left)
	}
	a.button(c, resume, "Resume", orange, onAcc, func() {
		if st.PauseCheck && !st.PauseChecked {
			a.confirm = &confirmBox{question: "Resume with no height check on the part?", yes: "Resume", onYes: pr.Resume}
			return
		}
		pr.Resume()
	})
	check := func() { a.do(func() error { return pr.Gcode("PRINTDECK_CHECK_HEIGHT") }) }
	switch {
	case st.PauseRoll != "":
		// A color change of the file: the filament change has the roll of the file.
		roll := st.PauseRoll
		a.button(c, grid[0], "Change filament", orange, onAcc, func() {
			a.openSwap(swapChange)
			a.swap.roll = roll
		})
	case st.PauseCheck:
		fill, ink := raised, text
		if !st.PauseChecked {
			fill, ink = orange, onAcc
		}
		a.button(c, grid[0], "Check the height", fill, ink, check)
	default:
		a.button(c, grid[0], "Change filament", raised, text, func() { a.openSwap(swapChange) })
	}
	a.button(c, grid[1], "Move head", raised, text, func() { a.open(pageMove) })
	a.button(c, grid[2], "Tune", raised, text, func() { a.tune = true })
	a.button(c, grid[3], "Cancel", raised, red, a.askCancel)
}
