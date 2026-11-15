package ui

import (
	"fmt"

	"printdeck/internal/printer"
)

// The Probe offset row of the Calibration page: the deck prints a pad, and the pin finds its
// edges (PAD_FIND_OFFSET of [printdeck_pad]). The middle of the edges gives x_offset and y_offset.

// probeOffsetLine is the line of the Probe offset row.
func probeOffsetLine(mach printer.Machine) string {
	return fmt.Sprintf("X %.2f, Y %.2f mm · find on a printed pad", mach.ProbeOffset[0], mach.ProbeOffset[1])
}

// askProbeOffset shows the choices of the Probe offset row: print the pad, or set the offsets
// from the last pad.
func (a *App) askProbeOffset(mach printer.Machine) {
	pr := a.Printer
	m := a.loaded()
	a.do(func() error {
		x, y, ok, err := pr.FoundProbeOffset()
		if err != nil {
			return err
		}
		a.post(func() {
			choices := []menuItem{{"Print the pad and find it",
				fmt.Sprintf("A 20 mm pad at %d°, then the pin finds its edges · about 15 minutes", m.Tool), dim, func() {
					a.do(func() error { return pr.PrintProbePad(m.Tool, m.Bed, m.Name) })
				}}}
			if ok {
				choices = append(choices, menuItem{"Set from the last pad",
					fmt.Sprintf("X %.2f, Y %.2f mm · Klipper restarts", x, y), dim, func() {
						a.do(func() error {
							if err := pr.SetProbeXYOffset(x, y); err != nil {
								return err
							}
							a.Toast(fmt.Sprintf("Probe offset X %.2f, Y %.2f, in printer.cfg. Do the twist and the bed map again.", x, y))
							return nil
						})
					}})
			}
			a.confirm = &confirmBox{question: "Probe offset: " + probeOffsetLine(mach), choices: choices}
		})
		return nil
	})
}
