package ui

import (
	"fmt"

	"printdeck/internal/printer"
)

// The Pressure advance and Retraction rows of the Calibration page. Each row prints its test tower,
// and sets its value from the tower or by hand.
//
// The retraction is the length of [firmware_retraction]. Cura uses it when "Firmware Retraction"
// is on in the printer settings: the G-code then has G10 and G11.

// askPressureAdvance shows the choices of the Pressure advance row.
func (a *App) askPressureAdvance(mach printer.Machine) {
	pr := a.Printer
	m := a.loaded()
	choices := []menuItem{
		{"Print the tower", fmt.Sprintf("A 30 mm tower at %d° · about 15 minutes", m.Tool), dim, func() {
			a.do(func() error { return pr.PrintPATower(m.Tool, m.Bed, m.Name) })
		}},
		{"From the tower", "Type the height of the best corners", dim, func() {
			a.editNumber("Height of the best corners", 0, int(printer.PATowerHeight), " mm", func(h int) {
				v := printer.PATowerValue(float64(h))
				a.confirm = &confirmBox{question: fmt.Sprintf("Set the pressure advance to %.3f?", v), yes: "Set", onYes: func() {
					a.do(func() error {
						if err := pr.SetPressureAdvance(v); err != nil {
							return err
						}
						a.Toast(fmt.Sprintf("Pressure advance %.3f, in printer.cfg", v))
						return nil
					})
				}}
			})
		}},
	}
	a.confirm = &confirmBox{question: fmt.Sprintf("Pressure advance: now %.3f", mach.PressureAdvance), choices: choices}
}

// retractLine is the line of the Retraction row.
func retractLine(mach printer.Machine) string {
	if !mach.Has("firmware_retraction") {
		return "No firmware retraction · tap to add it"
	}
	return fmt.Sprintf("%.2f mm · test tower", mach.RetractLength)
}

// askRetraction shows the choices of the Retraction row. Without [firmware_retraction], the only
// choice adds it with a length.
func (a *App) askRetraction(mach printer.Machine) {
	has := mach.Has("firmware_retraction")
	var choices []menuItem
	if has {
		pr := a.Printer
		m := a.loaded()
		choices = append(choices,
			menuItem{"Print the tower", fmt.Sprintf("Two 30 mm towers at %d° · about 20 minutes", m.Tool), dim, func() {
				a.do(func() error { return pr.PrintRetractTower(m.Tool, m.Bed, m.Name) })
			}},
			menuItem{"From the tower", "Type the height of the lowest band with no strings", dim, func() {
				a.editNumber("Height of the lowest clean band", 0, int(printer.RetractBand*printer.RetractBands), " mm", func(h int) {
					v := printer.RetractAt(float64(h))
					a.confirm = &confirmBox{question: fmt.Sprintf("Set the retraction to %.2f mm?", v), yes: "Set", onYes: func() {
						a.do(func() error { return a.setRetraction(v) })
					}}
				})
			}})
	}
	cur := mach.RetractLength
	if !has {
		cur = 1.5
	}
	choices = append(choices, menuItem{"Set the length", fmt.Sprintf("Now %.2f mm", cur), dim, func() {
		a.editPosition("Retraction length", cur, 0, 5, func(v float64) error { return a.setRetraction(v) })
	}})
	q := fmt.Sprintf("Retraction: now %.2f mm", cur)
	if !has {
		q = "Add [firmware_retraction] to printer.cfg? Klipper restarts."
	}
	a.confirm = &confirmBox{question: q, choices: choices}
}

// setRetraction sets the length, and tells it.
func (a *App) setRetraction(v float64) error {
	if err := a.Printer.SetRetraction(v); err != nil {
		return err
	}
	a.Toast(fmt.Sprintf("Retraction %.2f mm, in printer.cfg", v))
	return nil
}
