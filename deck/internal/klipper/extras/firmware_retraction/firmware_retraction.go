// Package firmware_retraction is the Marlin/Smoothie/Reprap style firmware retraction with G10
// and G11.
//
// Ported to Go from klippy/extras/firmware_retraction.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2019  Len Trigg <lenbok@gmail.com>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package firmware_retraction

import (
	"fmt"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
)

// FirmwareRetraction is the [firmware_retraction] section.
type FirmwareRetraction struct {
	printer              *host.Printer
	retractLength        float64
	retractSpeed         float64
	unretractExtraLength float64
	unretractSpeed       float64
	unretractLength      float64
	isRetracted          bool
	gcode                *gcode.Dispatch
}

func load(config *configfile.Wrapper) any {
	f := &FirmwareRetraction{printer: host.Of(config)}
	f.retractLength = config.GetFloat("retract_length", configfile.Default(0.), configfile.Min(0.))
	f.retractSpeed = config.GetFloat("retract_speed", configfile.Default(20.), configfile.Min(1))
	f.unretractExtraLength = config.GetFloat("unretract_extra_length", configfile.Default(0.),
		configfile.Min(0.))
	f.unretractSpeed = config.GetFloat("unretract_speed", configfile.Default(10.), configfile.Min(1))
	f.unretractLength = f.retractLength + f.unretractExtraLength
	f.isRetracted = false
	f.gcode = gcode.Of(f.printer)
	f.gcode.RegisterCommand("SET_RETRACTION", f.cmdSetRetraction, false, "Set firmware retraction parameters")
	f.gcode.RegisterCommand("GET_RETRACTION", f.cmdGetRetraction, false, "Report firmware retraction parameters")
	f.gcode.RegisterCommand("G10", f.cmdG10, false, "")
	f.gcode.RegisterCommand("G11", f.cmdG11, false, "")
	return f
}

func init() { host.RegisterModule("firmware_retraction", load, nil) }

func (f *FirmwareRetraction) Status(eventtime float64) map[string]any {
	return map[string]any{
		"retract_length":         f.retractLength,
		"retract_speed":          f.retractSpeed,
		"unretract_extra_length": f.unretractExtraLength,
		"unretract_speed":        f.unretractSpeed,
	}
}

func (f *FirmwareRetraction) cmdSetRetraction(gcmd *gcode.Command) {
	f.retractLength = gcmd.GetFloat("RETRACT_LENGTH", gcode.Default(f.retractLength), gcode.Min(0.))
	f.retractSpeed = gcmd.GetFloat("RETRACT_SPEED", gcode.Default(f.retractSpeed), gcode.Min(1))
	f.unretractExtraLength = gcmd.GetFloat("UNRETRACT_EXTRA_LENGTH", gcode.Default(f.unretractExtraLength),
		gcode.Min(0.))
	f.unretractSpeed = gcmd.GetFloat("UNRETRACT_SPEED", gcode.Default(f.unretractSpeed), gcode.Min(1))
	f.unretractLength = f.retractLength + f.unretractExtraLength
	f.isRetracted = false
}

func (f *FirmwareRetraction) cmdGetRetraction(gcmd *gcode.Command) {
	gcmd.RespondInfo(fmt.Sprintf("RETRACT_LENGTH=%.5f RETRACT_SPEED=%.5f"+
		" UNRETRACT_EXTRA_LENGTH=%.5f UNRETRACT_SPEED=%.5f",
		f.retractLength, f.retractSpeed, f.unretractExtraLength, f.unretractSpeed), true)
}

func (f *FirmwareRetraction) cmdG10(gcmd *gcode.Command) {
	if !f.isRetracted {
		f.gcode.RunScriptFromCommand(fmt.Sprintf("SAVE_GCODE_STATE NAME=_retract_state\n"+
			"G91\n"+
			"G1 E-%.5f F%d\n"+
			"RESTORE_GCODE_STATE NAME=_retract_state", f.retractLength, int64(f.retractSpeed*60)))
		f.isRetracted = true
	}
}

func (f *FirmwareRetraction) cmdG11(gcmd *gcode.Command) {
	if f.isRetracted {
		f.gcode.RunScriptFromCommand(fmt.Sprintf("SAVE_GCODE_STATE NAME=_retract_state\n"+
			"G91\n"+
			"G1 E%.5f F%d\n"+
			"RESTORE_GCODE_STATE NAME=_retract_state", f.unretractLength, int64(f.unretractSpeed*60)))
		f.isRetracted = false
	}
}
