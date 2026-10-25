// Package fan_generic is a [fan_generic] section: a fan that SET_FAN_SPEED controls.
//
// Ported to Go from klippy/extras/fan_generic.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2016-2024  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package fan_generic

import (
	"log"
	"math"
	"strconv"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/fan"
	"printdeck/internal/klipper/extras/output_pin"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
)

// PrinterFanGeneric is a [fan_generic] section.
type PrinterFanGeneric struct {
	printer      *host.Printer
	fan          *fan.Fan
	fanName      string
	templateEval *output_pin.PrinterTemplateEvaluator
}

func loadPrefix(config *configfile.Wrapper) any {
	parts := strings.Fields(config.Name())
	f := &PrinterFanGeneric{printer: host.Of(config)}
	f.fan = fan.NewFan(config, 0.)
	f.fanName = parts[len(parts)-1]

	// Template handling
	f.templateEval = output_pin.LookupTemplateEval(config)

	g := gcode.Of(f.printer)
	g.RegisterMuxCommand("SET_FAN_SPEED", "FAN", &f.fanName, f.cmdSetFanSpeed, "Sets the speed of a fan")
	return f
}

func init() { host.RegisterModule("fan_generic", nil, loadPrefix) }

func (f *PrinterFanGeneric) Status(eventtime float64) map[string]any { return f.fan.Status(eventtime) }

func (f *PrinterFanGeneric) templateUpdate(text string) {
	value, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil {
		log.Printf("fan_generic template render error: %v", err)
		value = 0.
	}
	f.fan.SetSpeed(value, math.NaN())
}

func (f *PrinterFanGeneric) cmdSetFanSpeed(gcmd *gcode.Command) {
	speed, hasSpeed := gcmd.GetFloatOptional("SPEED", gcode.Min(0.))
	_, hasTemplate := gcmd.Get("TEMPLATE", gcode.Default(nil))
	if hasSpeed == hasTemplate {
		panic(gcmd.Error("SET_FAN_SPEED must specify SPEED or TEMPLATE"))
	}
	// Check for template setting
	if hasTemplate {
		f.templateEval.SetTemplate(gcmd, f, f.templateUpdate, nil)
		return
	}
	f.fan.SetSpeedFromCommand(speed)
}
