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
		choices = append(choices, menuItem{"Measure on the cube", "Home, then touch the cube · about 2 minutes", dim, func() {
			a.startProbeCal("backlash", "Backlash")
		}})
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

// setBacklash sets the correction, and tells it.
func (a *App) setBacklash(x, y float64) error {
	if err := a.Printer.SetBacklash(x, y); err != nil {
		return err
	}
	a.Toast(fmt.Sprintf("Backlash correction X %.2f mm, Y %.2f mm, in printer.cfg", x, y))
	return nil
}

// askCubeResult reads the result of the cube step of the calibration, and asks what to do with it.
// A save of the z_offset goes on to the save step. Keep skips it.
func (a *App) askCubeResult(fl *probeCal) {
	pr := a.Printer
	a.do(func() error {
		x, y, z, err := pr.CubeResult()
		a.post(func() {
			if a.probeCal != fl {
				return
			}
			if err != nil {
				a.calFail(fl, err.Error())
				return
			}
			keep := func() { fl.at++; a.calNext(fl) } // past the save step
			if fl.steps[fl.at] == calCubeBacklash {
				y0 := pr.Machine().BacklashY
				set := func(x, y float64) func() {
					return func() {
						a.do(func() error {
							err := a.setBacklash(x, y)
							a.post(func() { keep() })
							return err
						})
					}
				}
				a.confirm = &confirmBox{question: fmt.Sprintf("Backlash on the cube: X %.2f mm, Y %.2f mm. Set the correction?", x, y),
					choices: []menuItem{
						{"X and Y", fmt.Sprintf("X %.2f, Y %.2f", x, y), dim, set(x, y)},
						{"X only", fmt.Sprintf("X %.2f, Y stays %.2f", x, y0), dim, set(x, y0)},
						{"Keep", backlashLine(pr.Machine()), dim, keep},
					}}
			} else {
				now := pr.Machine().ProbeOffset[2]
				a.confirm = &confirmBox{question: fmt.Sprintf("The cube gave z_offset %.3f mm (now %.3f, %+.3f). Save it? Klipper restarts.", z, now, z-now),
					choices: []menuItem{
						{"Save", fmt.Sprintf("z_offset %.3f", z), dim, func() {
							a.do(func() error {
								err := pr.Gcode("CUBE_SAVE_PROBE_OFFSET")
								a.post(func() {
									if err != nil {
										a.calFail(fl, err.Error())
										return
									}
									a.calNext(fl)
								})
								return nil
							})
						}},
						{"Keep", fmt.Sprintf("z_offset %.3f", now), dim, keep},
					}}
			}
			a.Redraw()
		})
		return nil
	})
}
