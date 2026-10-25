// Package extruder_stepper is an [extruder_stepper] section: a second stepper of an extruder.
//
// Ported to Go from klippy/extras/extruder_stepper.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2019 Simo Apell <simo.apell@live.fi>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package extruder_stepper

import (
	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/kinematics/extruder"
)

// PrinterExtruderStepper is an [extruder_stepper] section.
type PrinterExtruderStepper struct {
	printer         *host.Printer
	extruderStepper *extruder.ExtruderStepper
	extruderName    string
}

func loadPrefix(config *configfile.Wrapper) any {
	p := &PrinterExtruderStepper{printer: host.Of(config)}
	p.extruderStepper = extruder.NewExtruderStepper(config)
	p.extruderName = config.Get("extruder")
	p.printer.RegisterEventHandler("klippy:connect", func(...any) {
		p.extruderStepper.SyncToExtruder(p.extruderName)
	})
	return p
}

func init() { host.RegisterModule("extruder_stepper", nil, loadPrefix) }

// FindPastPosition returns the position of the stepper at a past print time.
func (p *PrinterExtruderStepper) FindPastPosition(printTime float64) float64 {
	return p.extruderStepper.FindPastPosition(printTime)
}

func (p *PrinterExtruderStepper) Status(eventtime float64) map[string]any {
	return p.extruderStepper.Status(eventtime)
}
