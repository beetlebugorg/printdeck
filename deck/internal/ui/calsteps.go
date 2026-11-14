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

// The step screen of a calibration: the steps in a list, as the start of a print. A LED shows the
// step that runs, and Done each step before it. The screen stays from the first step to the end,
// also between two actions. At the end it shows Done, or the step that failed and why, until OK.
// The paper test has its own screen, and the questions of a step show over this screen.

// calEnd is the end of a calibration on the step screen, until OK.
type calEnd struct {
	fl  *probeCal
	err string // why it stopped, or "" when it is done
}

// calStepName is the name of a step in the list.
func calStepName(s calStep) string {
	switch s {
	case calHeat:
		return "Heat and level"
	case calWipe:
		return "Wipe the nozzle"
	case calZOffset:
		return "Paper test"
	case calSave:
		return "Save: Klipper restarts"
	case calTwistX:
		return "Twist along X"
	case calTwistY:
		return "Twist along Y"
	case calMap:
		return "Probe the bed map"
	case calCube:
		return "Cube: nozzle and probe"
	case calCubeBacklash:
		return "Touch the cube"
	}
	return "Cool down"
}

// calScreen draws the step screen while a calibration runs or after its end, and reports that it
// drew one.
func (a *App) calScreen(c *gfx.Canvas, st printer.State) bool {
	fl, end := a.probeCal, a.calEnded
	if fl == nil && end == nil {
		return false
	}
	if st.ManualProbe {
		return false // the paper test screen
	}
	// The actions of the steps show here: no toast and no error screen of their own.
	if st.LastAction.End.After(a.actionSeen) {
		a.actionSeen = st.LastAction.End
	}
	if fl == nil {
		fl = end.fl
	}
	area, key := a.takeover(c, fl.name)
	n := len(fl.steps)
	rowH := min(48, (area.Dy()-140)/max(1, n))
	box := kit.Rows(area, 0, kit.Px(n*rowH+30))[0]
	a.region("Steps", box)
	panel(c, box, "CALIBRATION  ·  "+strings.ToUpper(fl.name))
	infoF := gfx.Face(false, 16)
	for i, s := range fl.steps {
		y := box.Min.Y + 22 + i*rowH
		done := i < fl.at || end != nil && end.err == ""
		failed := end != nil && end.err != "" && i == fl.at
		now := end == nil && i == fl.at
		led(c, box.Min.X+34, y+rowH/2-4, now)
		if done {
			c.RoundRect(image.Rect(box.Min.X+28, y+rowH/2-10, box.Min.X+40, y+rowH/2+2), 6, dim)
		}
		col := text
		if !done && !now && !failed {
			col = dim
		}
		info, f := "", 0.0
		switch {
		case done:
			info = "Done"
		case failed:
			info, col = "Stopped", red
		case now:
			info, f = a.calStepInfo(fl, st)
		}
		nameR := image.Rect(box.Min.X+60, y, box.Max.X-22, y+rowH-12)
		infoR := image.Rect(box.Max.X-190, y, box.Max.X-22, y+rowH-12)
		if info != "" {
			nameR.Max.X = infoR.Max.X - gfx.Width(infoF, info) - 12
		}
		c.Text(nameR, gfx.Fit(gfx.Face(true, 19), calStepName(s), nameR.Dx()), gfx.Face(true, 19), col, gfx.Left)
		infoCol := orange
		if failed {
			infoCol = red
		}
		c.Text(infoR, info, infoF, infoCol, gfx.Right)
		if f > 0 {
			bar := image.Rect(box.Min.X+60, y+rowH-8, box.Max.X-22, y+rowH-4)
			c.RoundRect(bar, 2, mix(surface, dim, 0.3))
			c.RoundRect(image.Rect(bar.Min.X, bar.Min.Y, bar.Min.X+int(float64(bar.Dx())*min(1, f)), bar.Max.Y), 2, orange)
		}
	}
	s := kit.NewStack(image.Rect(area.Min.X, box.Max.Y+20, area.Max.X, area.Max.Y), 0)
	pr := a.Printer
	switch {
	case end != nil && end.err == "":
		c.Text(s.Next(36), "Done", gfx.Face(true, 24), text, gfx.Left)
		a.button(c, key, "OK", orange, onAcc, a.closeCalEnd)
	case end != nil:
		c.Text(s.Next(36), "It stopped", gfx.Face(true, 24), red, gfx.Left)
		s.Skip(10)
		paragraph(c, s, end.err, text)
		a.button(c, key, "OK", orange, onAcc, a.closeCalEnd)
	default:
		paragraph(c, s, "The other keys are off until the calibration ends.", dim)
		a.button(c, key, "Stop", redCap, onRed, func() {
			a.confirm = &confirmBox{question: "Stop the calibration?", choices: []menuItem{
				{"Stop the calibration", "The step that runs ends first", dim, func() {
					a.stopProbeCal("Stopped with the Stop key.")
				}},
				{"Stop the printer now", "An emergency stop. Then tap Firmware restart.", red, func() {
					a.stopProbeCal("Stopped with an emergency stop.")
					a.do(pr.EmergencyStop)
				}},
			}}
		})
	}
	return true
}

// calStepInfo is the information of the step that runs, and the part of it that is done (0 for
// none).
func (a *App) calStepInfo(fl *probeCal, st printer.State) (string, float64) {
	switch fl.steps[fl.at] {
	case calHeat:
		if st.BedTarget > 0 && st.Bed < st.BedTarget-1 {
			return fmt.Sprintf("Bed %.0f° of %.0f°", st.Bed, st.BedTarget), st.Bed / st.BedTarget
		}
	case calWipe:
		return fmt.Sprintf("Nozzle %.0f°", st.Tool), 0
	case calTwistX, calTwistY:
		if fl.probes > 0 {
			return strings.TrimPrefix(fl.stepText(), calStepName(fl.steps[fl.at])+": "), 0
		}
	}
	if st.Action != "" {
		sec := int(max(0, a.Now().Sub(st.ActionStart)) / time.Second)
		return fmt.Sprintf("%d:%02d", sec/60, sec%60), 0
	}
	return "", 0
}

// closeCalEnd closes the end of the step screen, and shows the Calibration page.
func (a *App) closeCalEnd() {
	a.calEnded = nil
	if a.topPage() != pageProbeCal {
		a.pages = []page{pageMachine, pageProbeCal}
	}
	a.Redraw()
}
