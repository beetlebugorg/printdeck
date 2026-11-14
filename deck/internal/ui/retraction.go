package ui

import (
	"fmt"

	"printdeck/internal/printer"
)

// The Retraction row of the Calibration page: the length of [firmware_retraction], from the
// retraction tower or by hand. Cura uses it when "Firmware Retraction" is on in the printer
// settings: the G-code then has G10 and G11.

// retractLine is the line of the Retraction row.
func retractLine(mach printer.Machine) string {
	if !mach.Has("firmware_retraction") {
		return "No firmware retraction · tap to add it"
	}
	return fmt.Sprintf("Now %.2f mm · set it from the tower", mach.RetractLength)
}

// askRetraction shows the choices of the Retraction row.
func (a *App) askRetraction(mach printer.Machine) {
	var choices []menuItem
	if mach.Has("firmware_retraction") {
		choices = append(choices, menuItem{"From the tower", "Type the height of the lowest band with no strings", dim, func() {
			a.editNumber("Height of the lowest clean band", 0, int(printer.RetractBand*printer.RetractBands), " mm", func(h int) {
				v := printer.RetractAt(float64(h))
				a.confirm = &confirmBox{question: fmt.Sprintf("Set the retraction to %.2f mm?", v), yes: "Set", onYes: func() {
					a.do(func() error { return a.setRetraction(v) })
				}}
			})
		}})
	}
	cur := mach.RetractLength
	if !mach.Has("firmware_retraction") {
		cur = 1.5
	}
	choices = append(choices, menuItem{"Set the length", fmt.Sprintf("Now %.2f mm", cur), dim, func() {
		a.editPosition("Retraction length", cur, 0, 5, func(v float64) error { return a.setRetraction(v) })
	}})
	q := "Retraction: " + retractLine(mach)
	if !mach.Has("firmware_retraction") {
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
