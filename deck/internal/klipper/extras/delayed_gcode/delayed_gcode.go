// Package delayed_gcode is a [delayed_gcode] section: a timer that runs a G-code template.
//
// Ported to Go from klippy/extras/delayed_gcode.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2019  Eric Callahan <arksine.code@gmail.com>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package delayed_gcode

import (
	"log"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/gcode_macro"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
)

// DelayedGcode is a [delayed_gcode] section.
type DelayedGcode struct {
	printer      *host.Printer
	reactor      *reactor.Reactor
	name         string
	gcode        *gcode.Dispatch
	timerGcode   *gcode_macro.TemplateWrapper
	duration     float64
	timerHandler *reactor.Timer
	insideTimer  bool
	repeat       bool
}

func loadPrefix(config *configfile.Wrapper) any {
	d := &DelayedGcode{printer: host.Of(config)}
	d.reactor = d.printer.Reactor()
	d.name = strings.Fields(config.Name())[1]
	d.gcode = gcode.Of(d.printer)
	gm := gcode_macro.Load(config)
	d.timerGcode = gm.LoadTemplate(config, "gcode", nil)
	d.duration = config.GetFloat("initial_duration", configfile.Default(0.), configfile.Min(0.))
	d.printer.RegisterEventHandler("klippy:ready", func(...any) { d.handleReady() })
	d.gcode.RegisterMuxCommand("UPDATE_DELAYED_GCODE", "ID", &d.name, d.cmdUpdateDelayedGcode,
		"Update the duration of a delayed_gcode")
	return d
}

func init() { host.RegisterModule("delayed_gcode", nil, loadPrefix) }

func (d *DelayedGcode) handleReady() {
	waketime := reactor.Never
	if d.duration != 0 {
		waketime = d.reactor.Monotonic() + d.duration
	}
	d.timerHandler = d.reactor.RegisterTimer(d.gcodeTimerEvent, waketime)
}

func (d *DelayedGcode) gcodeTimerEvent(eventtime float64) float64 {
	d.insideTimer = true
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Script running error: %v", r)
			}
		}()
		d.gcode.RunScript(d.timerGcode.Render(nil))
	}()
	nextwake := reactor.Never
	if d.repeat {
		nextwake = eventtime + d.duration
	}
	d.insideTimer, d.repeat = false, false
	return nextwake
}

func (d *DelayedGcode) cmdUpdateDelayedGcode(gcmd *gcode.Command) {
	d.duration = gcmd.GetFloat("DURATION", gcode.Min(0.))
	if d.insideTimer {
		d.repeat = d.duration != 0.
	} else {
		waketime := reactor.Never
		if d.duration != 0 {
			waketime = d.reactor.Monotonic() + d.duration
		}
		d.reactor.UpdateTimer(d.timerHandler, waketime)
	}
}
