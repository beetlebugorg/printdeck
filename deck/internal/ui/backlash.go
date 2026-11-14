package ui

import (
	"fmt"

	"printdeck/internal/printer"
)

// The Backlash row of the Calibration page: the correction of [printdeck_backlash], a measure on
// the calibration cube of [printdeck_cube], and a change by hand.

// backlashLine is the line of the Backlash row.
func backlashLine(mach printer.Machine) string {
	if !mach.Has("printdeck_backlash") {
		return "No correction · measure on the cube"
	}
	if mach.BacklashX == 0 && mach.BacklashY == 0 {
		return "Correction off"
	}
	return fmt.Sprintf("Correction X %.2f mm, Y %.2f mm", mach.BacklashX, mach.BacklashY)
}

// askBacklash shows the choices of the Backlash row.
func (a *App) askBacklash() {
	pr := a.Printer
	mach := pr.Machine()
	var choices []menuItem
	if mach.Has("printdeck_cube") {
		choices = append(choices, menuItem{"Measure on the cube", "Home, then touch the cube · about 2 minutes", dim, a.measureBacklash})
	}
	choices = append(choices,
		menuItem{"Set X", fmt.Sprintf("Now %.2f mm", mach.BacklashX), dim, func() {
			a.editPosition("Backlash correction of X", mach.BacklashX, 0, 1, func(v float64) error {
				return a.setBacklash(v, mach.BacklashY)
			})
		}},
		menuItem{"Set Y", fmt.Sprintf("Now %.2f mm", mach.BacklashY), dim, func() {
			a.editPosition("Backlash correction of Y", mach.BacklashY, 0, 1, func(v float64) error {
				return a.setBacklash(mach.BacklashX, v)
			})
		}},
		menuItem{"Off", "X 0, Y 0", dim, func() { a.do(func() error { return a.setBacklash(0, 0) }) }})
	a.confirm = &confirmBox{question: "Backlash: " + backlashLine(mach), choices: choices}
}

// measureBacklash measures on the cube, then asks to set the correction to the result.
func (a *App) measureBacklash() {
	pr := a.Printer
	a.Toast("Homing, then the cube measure")
	a.do(func() error {
		x, y, err := pr.MeasureBacklash()
		if err != nil {
			return err
		}
		a.post(func() {
			a.confirm = &confirmBox{question: fmt.Sprintf("Backlash on the cube: X %.2f mm, Y %.2f mm. Set the correction?", x, y),
				choices: []menuItem{
					{"X and Y", fmt.Sprintf("X %.2f, Y %.2f", x, y), dim, func() { a.do(func() error { return a.setBacklash(x, y) }) }},
					{"X only", fmt.Sprintf("X %.2f, Y stays %.2f", x, pr.Machine().BacklashY), dim, func() {
						a.do(func() error { return a.setBacklash(x, pr.Machine().BacklashY) })
					}},
					{"Keep", backlashLine(pr.Machine()), dim, func() {}},
				}}
			a.Redraw()
		})
		return nil
	})
}

// setBacklash sets the correction, and tells it.
func (a *App) setBacklash(x, y float64) error {
	if err := a.Printer.SetBacklash(x, y); err != nil {
		return err
	}
	a.Toast(fmt.Sprintf("Backlash correction X %.2f mm, Y %.2f mm, in printer.cfg", x, y))
	return nil
}

// measureProbeOffset measures the z_offset on the cube at the temperatures of the paper test, then
// asks to save it.
func (a *App) measureProbeOffset() {
	pr := a.Printer
	a.Toast(fmt.Sprintf("Heating to %d° and %d°, then the cube", calBedTemp, calToolTemp))
	a.do(func() error {
		z, err := pr.MeasureProbeOffset(calBedTemp, calToolTemp)
		if err != nil {
			return err
		}
		now := pr.Machine().ProbeOffset[2]
		a.post(func() {
			a.confirm = &confirmBox{question: fmt.Sprintf("The cube gave z_offset %.3f mm (now %.3f, %+.3f). Save it? Klipper restarts.", z, now, z-now),
				yes: "Save", onYes: func() { a.do(pr.SaveProbeOffset) }}
			a.Redraw()
		})
		return nil
	})
}
