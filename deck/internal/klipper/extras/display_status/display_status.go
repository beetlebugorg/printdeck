// Package display_status has the progress and the message of the displays (M73, M117,
// SET_DISPLAY_TEXT).
//
// Ported to Go from klippy/extras/display_status.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018-2020  Kevin O'Connor <kevin@koconnor.net>
//	Copyright (C) 2018  Eric Callahan <arksine.code@gmail.com>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package display_status

import (
	"math"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
)

const m73Timeout = 5.

type statuser interface {
	Status(eventtime float64) map[string]any
}

// DisplayStatus is the progress and the message.
type DisplayStatus struct {
	printer        *host.Printer
	expireProgress float64
	progress       *float64
	message        *string
}

func load(config *configfile.Wrapper) any {
	d := &DisplayStatus{printer: host.Of(config)}
	g := gcode.Of(d.printer)
	g.RegisterCommand("M73", d.cmdM73, false, "")
	g.RegisterCommand("M117", d.cmdM117, false, "")
	g.RegisterCommand("SET_DISPLAY_TEXT", d.cmdSetDisplayText, false, "Set or clear the display message")
	return d
}

func init() { host.RegisterModule("display_status", load, nil) }

// Status returns the progress (0 to 1) and the message (None for no message).
func (d *DisplayStatus) Status(eventtime float64) map[string]any {
	if d.progress != nil && eventtime > d.expireProgress {
		info := d.printer.LookupObject("idle_timeout").(statuser).Status(eventtime)
		if info["state"] != "Printing" {
			d.progress = nil
		}
	}
	var progress any
	if d.progress != nil {
		progress = *d.progress
	} else {
		progress = 0.
		if sdcard, ok := d.printer.TryLookupObject("virtual_sdcard").(statuser); ok {
			progress = sdcard.Status(eventtime)["progress"]
		}
	}
	var message any
	if d.message != nil {
		message = *d.message
	}
	return map[string]any{"progress": progress, "message": message}
}

func (d *DisplayStatus) cmdM73(gcmd *gcode.Command) {
	if p, ok := gcmd.GetFloatOptional("P"); ok {
		v := math.Min(1., math.Max(0., p/100.))
		d.progress = &v
		d.expireProgress = d.printer.Reactor().Monotonic() + m73Timeout
	}
}

func (d *DisplayStatus) cmdM117(gcmd *gcode.Command) {
	if msg := gcmd.RawParams(); msg != "" {
		d.message = &msg
	} else {
		d.message = nil
	}
}

func (d *DisplayStatus) cmdSetDisplayText(gcmd *gcode.Command) {
	if msg, ok := gcmd.Get("MSG", gcode.Default(nil)); ok {
		d.message = &msg
	} else {
		d.message = nil
	}
}
