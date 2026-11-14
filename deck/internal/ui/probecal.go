package ui

import (
	"fmt"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// The calibration of the probe (Machine > Calibration). It runs the steps that a new place of the
// probe needs, in this order:
//
//	Z offset  heat, home and level the gantry, then the paper test with the pin over the middle of
//	          the bed (PROBE_CALIBRATE), then SAVE_CONFIG.
//	Twist     heat, home and level, then the paper test at each point of [axis_twist_compensation]
//	          along X and along Y, then SAVE_CONFIG.
//	Map       heat, home and level, then BED_MESH_CALIBRATE: the saved map of PRINTDECK_MESH.
//
// Run all does the three. Each row of the page does one. The bed and the nozzle are at the
// temperatures of the probe at the start of a print, because the probe reads differently when it
// is warm. SAVE_CONFIG restarts Klipper and turns the heaters off, so the next step heats again.
//
// The paper test starts close to the bed: the deck lowers the nozzle when the paper screen opens.
// PROBE_CALIBRATE starts 5 mm over the height where the pin touched, and the deck lowers it 4.5
// mm: the nozzle is then over the bed, whatever the old z_offset. The twist calibration starts at
// horizontal_move_z. Right after a new z_offset, the deck lowers it to Z 1. Alone, it lowers it to
// Z 3, because an old z_offset can be wrong by some millimetres.

// calStep is a step of the calibration.
type calStep int

const (
	calHeat         calStep = iota // heat, home, and level the gantry (Z_TILT_ADJUST)
	calZOffset                     // the paper test of the probe (PROBE_CALIBRATE)
	calSave                        // SAVE_CONFIG: Klipper restarts
	calTwistX                      // the paper test at each point along X (AXIS_TWIST_COMPENSATION_CALIBRATE)
	calTwistY                      // the paper test at each point along Y
	calMap                         // BED_MESH_CALIBRATE
	calCool                        // the heaters off
	calCube                        // the nozzle and the probe touch the cube (CUBE_PROBE_OFFSET)
	calCubeBacklash                // home, then the backlash on the cube (CUBE_MEASURE)
)

// The temperatures of the calibration: PRINT_START probes with the bed at 60 and the nozzle at the
// wipe temperature of PLA.
const (
	calBedTemp  = 60
	calToolTemp = 150
)

// The points of the twist calibration along each axis.
const (
	calTwistXPoints = 5
	calTwistYPoints = 3
)

// The start of the paper test.
const (
	calProbeDrop   = 4.5 // mm down from the start of PROBE_CALIBRATE
	calTwistZFresh = 1.0 // the Z of a twist point after a new z_offset
	calTwistZ      = 3.0 // the Z of a twist point with an older z_offset
)

// The longest waits. The tests make them short.
var (
	calProbeWait   = 90 * time.Second // from the end of an action to the paper screen
	calGapWait     = 60 * time.Second // between two points of the twist calibration
	calRestartWait = 90 * time.Second // for Klipper to come back after SAVE_CONFIG
	calPendingWait = 5 * time.Second  // for the result of a paper test to show as unsaved
)

// probeCal is a calibration that runs.
type probeCal struct {
	name    string // what runs: "Calibration", "Z offset", "Twist" or "Bed map"
	steps   []calStep
	at      int
	sent    bool      // the step sent its commands
	started time.Time // when the step sent its commands
	probes  int       // the paper tests of the step that opened
	open    bool      // the paper screen is open
	closed  time.Time // when the last paper screen closed
	fresh   bool      // a new z_offset was saved in this calibration
	asked   bool      // the result of a cube step is on the screen
}

// calSteps returns the steps of a calibration: "all", "zoffset", "twist" or "map".
func calSteps(kind string, mach printer.Machine) []calStep {
	twist := []calStep{calHeat, calTwistX}
	if mach.HasTwistY {
		twist = append(twist, calTwistY)
	}
	twist = append(twist, calSave)
	switch kind {
	case "zoffset":
		return []calStep{calHeat, calZOffset, calSave, calCool}
	case "twist":
		return append(twist, calCool)
	case "map":
		return []calStep{calHeat, calMap, calCool}
	case "cube":
		return []calStep{calHeat, calCube, calSave, calCool}
	case "backlash":
		return []calStep{calCubeBacklash, calCool}
	}
	s := []calStep{calHeat, calZOffset, calSave}
	if mach.HasTwist {
		s = append(s, twist...)
	}
	return append(s, calHeat, calMap, calCool)
}

// startProbeCal starts a calibration, after the question.
func (a *App) startProbeCal(kind, name string) {
	mach := a.Printer.Machine()
	a.probeCal = &probeCal{name: name, steps: calSteps(kind, mach)}
	a.calMsg = ""
	a.Redraw()
}

// calStepText is the line of a step on the page.
func (fl *probeCal) stepText() string {
	switch fl.steps[fl.at] {
	case calHeat:
		return "Heating, homing and leveling"
	case calZOffset:
		return "Paper test at the middle of the bed"
	case calSave:
		return "Saving: Klipper restarts"
	case calTwistX:
		return fmt.Sprintf("Twist along X: point %d of %d", max(fl.probes, 1), calTwistXPoints)
	case calTwistY:
		return fmt.Sprintf("Twist along Y: point %d of %d", max(fl.probes, 1), calTwistYPoints)
	case calMap:
		return "Probing the map of the bed"
	case calCube:
		return "Nozzle and probe on the cube"
	case calCubeBacklash:
		return "Home, then touch the cube"
	}
	return "Cooling down"
}

// calCenter returns the nozzle place that puts the pin of the probe over the middle of the bed.
func calCenter(mach printer.Machine) (x, y float64) {
	switch {
	case mach.HasMesh:
		x, y = (mach.MeshMin[0]+mach.MeshMax[0])/2, (mach.MeshMin[1]+mach.MeshMax[1])/2
	case mach.MaxX > mach.MinX:
		x, y = (mach.MinX+mach.MaxX)/2, (mach.MinY+mach.MaxY)/2
	}
	x, y = x-mach.ProbeOffset[0], y-mach.ProbeOffset[1]
	if mach.MaxX > mach.MinX {
		x = min(max(x, mach.MinX), mach.MaxX)
		y = min(max(y, mach.MinY), mach.MaxY)
	}
	return x, y
}

// calStart sends the commands of the step now.
func (a *App) calStart(fl *probeCal, st printer.State) {
	pr := a.Printer
	mach := pr.Machine()
	fl.sent, fl.started, fl.probes, fl.open, fl.closed = true, a.Now(), 0, false, time.Time{}
	action := fl.name + ": " + fl.stepText()
	switch fl.steps[fl.at] {
	case calHeat:
		lines := []string{fmt.Sprintf("M140 S%d", calBedTemp), fmt.Sprintf("M104 S%d", calToolTemp)}
		if st.Homed != "xyz" {
			lines = append(lines, "G28")
		}
		lines = append(lines, fmt.Sprintf("TEMPERATURE_WAIT SENSOR=heater_bed MINIMUM=%d", calBedTemp-1),
			fmt.Sprintf("TEMPERATURE_WAIT SENSOR=extruder MINIMUM=%d", calToolTemp-2))
		if mach.Has("z_tilt") {
			lines = append(lines, "Z_TILT_ADJUST")
		}
		a.runAction(action, lines...)
	case calZOffset:
		x, y := calCenter(mach)
		a.runAction(action, "BED_MESH_CLEAR", "G90", "G1 Z15 F600", fmt.Sprintf("G1 X%.1f Y%.1f F6000", x, y), "PROBE_CALIBRATE")
	case calTwistX:
		a.runAction(action, "BED_MESH_CLEAR", fmt.Sprintf("AXIS_TWIST_COMPENSATION_CALIBRATE AXIS=X SAMPLE_COUNT=%d", calTwistXPoints))
	case calTwistY:
		a.runAction(action, "BED_MESH_CLEAR", fmt.Sprintf("AXIS_TWIST_COMPENSATION_CALIBRATE AXIS=Y SAMPLE_COUNT=%d", calTwistYPoints))
	case calMap:
		a.runAction(action, "BED_MESH_CALIBRATE")
	case calCube:
		a.runAction(action, "CUBE_PROBE_OFFSET")
	case calCubeBacklash:
		if st.Homed != "xyz" {
			a.runAction(action, "G28", "CUBE_MEASURE")
		} else {
			a.runAction(action, "CUBE_MEASURE")
		}
	case calSave:
		// SAVE_CONFIG ends with a restart of Klipper, so its call can fail: the next state shows the end.
		go pr.Gcode("SAVE_CONFIG")
	case calCool:
		a.do(func() error { return pr.Gcode("M104 S0", "M140 S0") })
	}
}

// calNext goes to the next step, or ends the calibration.
func (a *App) calNext(fl *probeCal) {
	if fl.steps[fl.at] == calSave && fl.at > 0 && fl.steps[fl.at-1] == calZOffset {
		fl.fresh = true
	}
	fl.at++
	fl.sent, fl.asked = false, false
	if fl.at >= len(fl.steps) {
		a.probeCal = nil
		a.Toast(fl.name + " is done")
	}
	a.Redraw()
}

// calFail ends the calibration with a message, and turns the heaters off.
func (a *App) calFail(fl *probeCal, msg string) {
	if a.probeCal != fl {
		return
	}
	a.probeCal = nil
	a.calMsg = msg
	pr := a.Printer
	a.do(func() error { return pr.Gcode("M104 S0", "M140 S0") })
	if a.topPage() != pageProbeCal {
		a.pages = []page{pageMachine, pageProbeCal}
	}
	a.Redraw()
}

// stopProbeCal stops the calibration: the Stop key of the page, or Abort on the paper screen.
func (a *App) stopProbeCal(msg string) {
	if fl := a.probeCal; fl != nil {
		a.calFail(fl, msg)
	}
}

// calActionEnd reports the end of the action of the step, and its error.
func (a *App) calActionEnd(fl *probeCal, st printer.State) (ended bool, err string) {
	r := st.LastAction
	if st.Action != "" || r.End.Before(fl.started) {
		return false, ""
	}
	return true, r.Err
}

// calLower lowers the nozzle at the start of a paper test.
func (a *App) calLower(fl *probeCal, st printer.State) {
	var dz float64
	switch fl.steps[fl.at] {
	case calZOffset:
		dz = -calProbeDrop
	case calTwistX, calTwistY:
		z := calTwistZ
		if fl.fresh {
			z = calTwistZFresh
		}
		dz = z - st.ManualZ
	}
	// Only down, and not more than the start height of a paper test can be.
	if dz > -0.05 || dz < -12 {
		return
	}
	pr := a.Printer
	a.do(func() error { return pr.Gcode(fmt.Sprintf("TESTZ Z=%.3f", dz)) })
}

// followProbeCal follows the calibration that runs: it sends the commands of a step, and goes to
// the next step at the end of the step. It runs at each frame, also under the screens of an action
// and of the paper test.
func (a *App) followProbeCal(st printer.State) {
	fl := a.probeCal
	if fl == nil {
		return
	}
	now := a.Now()
	step := fl.steps[fl.at]
	if !fl.sent {
		// After SAVE_CONFIG, Klipper must be back and idle before the next step.
		if !st.Connected || st.Action != "" || (st.Busy() && !st.ManualProbe) {
			return
		}
		a.calStart(fl, st)
		return
	}
	switch step {
	case calHeat, calMap:
		if ended, err := a.calActionEnd(fl, st); ended {
			if err != "" {
				a.calFail(fl, err)
				return
			}
			a.calNext(fl)
		}
	case calZOffset, calTwistX, calTwistY:
		want := 1
		switch step {
		case calTwistX:
			want = calTwistXPoints
		case calTwistY:
			want = calTwistYPoints
		}
		if st.ManualProbe {
			if !fl.open {
				fl.open = true
				fl.probes++
				a.calLower(fl, st)
				a.Redraw()
			}
			return
		}
		if fl.open {
			fl.open, fl.closed = false, now
		}
		if fl.probes >= want && !fl.closed.IsZero() {
			// The last paper test closed: Accept leaves a result to save, Abort none.
			if st.SaveConfigPending {
				a.calNext(fl)
			} else if now.Sub(fl.closed) > calPendingWait {
				a.calFail(fl, "The paper test ended with no result. Run the calibration again.")
			}
			return
		}
		if fl.probes > 0 {
			if now.Sub(fl.closed) > calGapWait {
				a.calFail(fl, fmt.Sprintf("The twist calibration stopped after point %d of %d.", fl.probes, want))
			}
			return
		}
		if ended, err := a.calActionEnd(fl, st); ended {
			if err != "" {
				a.calFail(fl, err)
			} else if now.Sub(st.LastAction.End) > calProbeWait {
				a.calFail(fl, "The paper test did not start.")
			}
		}
	case calCube, calCubeBacklash:
		if fl.asked {
			return
		}
		if ended, err := a.calActionEnd(fl, st); ended {
			if err != "" {
				a.calFail(fl, err)
				return
			}
			fl.asked = true
			a.askCubeResult(fl)
		}
	case calSave:
		if st.Connected && !st.SaveConfigPending && now.Sub(fl.started) > 3*time.Second {
			a.calNext(fl)
		} else if now.Sub(fl.started) > calRestartWait {
			a.calFail(fl, "Klipper did not come back after SAVE_CONFIG.")
		}
	case calCool:
		a.calNext(fl)
	}
}

// calibrationLine is the line of the Calibration key of the Machine page.
func (a *App) calibrationLine(mach printer.Machine) (string, bool) {
	if fl := a.probeCal; fl != nil {
		return fl.stepText(), true
	}
	if a.calMsg != "" {
		return "Stopped: " + a.calMsg, true
	}
	return fmt.Sprintf("Z offset %.3f, twist, map", mach.ProbeOffset[2]), false
}

// drawProbeCal is the Calibration page: Run all in the head, and a row for each calibration with
// its value now.
func (a *App) drawProbeCal(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	mach := pr.Machine()
	f := a.navPage(c, "Calibration")
	idle := st.Connected && !st.Busy()
	ask := func(kind, name, question string) func() {
		return func() {
			if a.probeCal != nil {
				a.Toast("A calibration runs. Stop it first.")
				return
			}
			if !idle {
				a.Toast("Wait until the print ends")
				return
			}
			a.confirm = &confirmBox{question: question, yes: "Start", onYes: func() { a.startProbeCal(kind, name) }}
		}
	}
	if fl := a.probeCal; fl != nil {
		a.headKey(c, f.head, "Stop", func() {
			a.confirm = &confirmBox{question: "Stop the calibration?", yes: "Stop", danger: true, onYes: func() {
				if st := pr.State(); st.ManualProbe {
					a.do(func() error { return pr.Gcode("ABORT") })
				}
				a.stopProbeCal("Stopped with the Stop key.")
			}}
		})
	} else {
		a.headKey(c, f.head, "Run all", ask("all", "Calibration",
			"Heat, then the Z offset, the twist and the map? About 25 minutes, with paper tests."))
	}
	var items []menuItem
	if fl := a.probeCal; fl != nil {
		items = append(items, menuItem{fmt.Sprintf("Step %d of %d", fl.at+1, len(fl.steps)), fl.stepText(), orange, nil})
	} else if a.calMsg != "" {
		items = append(items, menuItem{"Stopped", a.calMsg, orange, nil})
	}
	paper := ask("zoffset", "Z offset", "Heat, then the paper test? About 5 minutes.")
	if mach.Has("printdeck_cube") {
		items = append(items, menuItem{"Z offset", fmt.Sprintf("%.3f mm · paper test or the cube", mach.ProbeOffset[2]), dim, func() {
			if a.probeCal != nil || !idle {
				paper() // it tells why it cannot start
				return
			}
			a.confirm = &confirmBox{question: fmt.Sprintf("Z offset: now %.3f mm. How?", mach.ProbeOffset[2]), choices: []menuItem{
				{"On the cube", "Heat, then the nozzle and the probe touch the cube · about 8 minutes", dim, func() {
					a.startProbeCal("cube", "Z offset")
				}},
				{"Paper test", "Heat, then a paper test at the middle of the bed · about 5 minutes", dim, func() {
					a.startProbeCal("zoffset", "Z offset")
				}},
			}}
		}})
	} else {
		items = append(items, menuItem{"Z offset", fmt.Sprintf("%.3f mm · paper test at the middle of the bed", mach.ProbeOffset[2]), dim, paper})
	}
	if mach.HasTwist {
		sub := "Not calibrated · a paper test at each point"
		if len(mach.TwistX) > 0 {
			sub = fmt.Sprintf("X %.2f mm", printer.TwistRange(mach.TwistX))
			if len(mach.TwistY) > 0 {
				sub += fmt.Sprintf(", Y %.2f mm", printer.TwistRange(mach.TwistY))
			}
			sub += " along the gantry"
		}
		n := calTwistXPoints
		if mach.HasTwistY {
			n += calTwistYPoints
		}
		items = append(items, menuItem{"Twist", sub, dim,
			ask("twist", "Twist", fmt.Sprintf("Heat, then %d paper tests? About 10 minutes.", n))})
	}
	if mach.Has("bed_mesh") {
		sub := "No saved map yet"
		if st.ProbeMapPoints > 0 {
			sub = fmt.Sprintf("%d points · %s", st.ProbeMapPoints, fileDate(st.ProbeMapTime.Local(), a.Now(), a.Clock24))
		}
		items = append(items, menuItem{"Bed map", sub, dim, ask("map", "Bed map", "Heat, then probe the full bed? About 10 minutes.")})
	}
	if mach.Has("extruder") {
		items = append(items, menuItem{"Input shaping", shaperLine(mach), dim, func() { a.open(pageShaper) }})
	}
	if mach.Has("printdeck_backlash") || mach.Has("printdeck_cube") {
		items = append(items, menuItem{"Backlash", backlashLine(mach), dim, func() {
			if a.probeCal != nil {
				a.Toast("A calibration runs. Stop it first.")
				return
			}
			if !idle {
				a.Toast("Wait until the print ends")
				return
			}
			a.askBacklash()
		}})
	}
	a.menuRowsWait(c, a.listArea(f), f.wide, 86, 12, items, st.Busy())
}
