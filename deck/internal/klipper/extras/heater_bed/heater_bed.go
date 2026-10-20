// Package heater_bed is the heated bed: its heater, and the M140 and M190 commands.
//
// Ported to Go from klippy/extras/heater_bed.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018-2019  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package heater_bed

import (
	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/heaters"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
)

// PrinterHeaterBed is the heated bed.
type PrinterHeaterBed struct {
	printer *host.Printer
	heater  *heaters.Heater
}

func load(config *configfile.Wrapper) any {
	b := &PrinterHeaterBed{printer: host.Of(config)}
	b.heater = heaters.Load(config).SetupHeater(config, "B")
	// Register commands
	g := gcode.Of(b.printer)
	g.RegisterCommand("M140", func(gcmd *gcode.Command) { b.cmdM140(gcmd, false) }, false, "")
	g.RegisterCommand("M190", func(gcmd *gcode.Command) { b.cmdM140(gcmd, true) }, false, "")
	return b
}

func init() { host.RegisterModule("heater_bed", load, nil) }

func (b *PrinterHeaterBed) Status(eventtime float64) map[string]any {
	return b.heater.Status(eventtime)
}
func (b *PrinterHeaterBed) Stats(eventtime float64) (bool, string) { return b.heater.Stats(eventtime) }

// Heater returns the heater of the bed.
func (b *PrinterHeaterBed) Heater() *heaters.Heater { return b.heater }

func (b *PrinterHeaterBed) cmdM140(gcmd *gcode.Command, wait bool) {
	// Set Bed Temperature
	temp := gcmd.GetFloat("S", gcode.Default(0.))
	heaters.Of(b.printer).SetTemperature(b.heater, temp, wait)
}
