// Package homing_override runs a user G-code script in place of the G28 homing command.
//
// Ported to Go from klippy/extras/homing_override.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package homing_override

import (
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/gcode_macro"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/jinja"
	"printdeck/internal/klipper/toolhead"
)

// HomingOverride is the [homing_override] section.
type HomingOverride struct {
	printer  *host.Printer
	startPos [3]*float64
	axes     string
	template *gcode_macro.TemplateWrapper
	inScript bool
	gcode    *gcode.Dispatch
	prevG28  gcode.Handler
}

func load(config *configfile.Wrapper) any {
	h := &HomingOverride{printer: host.Of(config)}
	for i, a := range "xyz" {
		if v, ok := config.GetFloatOptional("set_position_"+string(a), configfile.Default(nil)); ok {
			h.startPos[i] = &v
		}
	}
	h.axes = strings.ToUpper(config.Get("axes", configfile.Default("XYZ")))
	gm := gcode_macro.Load(config)
	h.template = gm.LoadTemplate(config, "gcode", nil)
	h.inScript = false
	h.printer.LoadObject(config, "homing")
	h.gcode = gcode.Of(h.printer)
	h.prevG28 = h.gcode.UnregisterCommand("G28")
	h.gcode.RegisterCommand("G28", h.cmdG28, false, "")
	return h
}

func init() { host.RegisterModule("homing_override", load, nil) }

func (h *HomingOverride) cmdG28(gcmd *gcode.Command) {
	if h.inScript {
		// Was called recursively - invoke the real G28 command
		h.prevG28(gcmd)
		return
	}

	// if no axis is given as parameter we assume the override
	noAxis := true
	for _, axis := range "XYZ" {
		if _, ok := gcmd.Get(string(axis), gcode.Default(nil)); ok {
			noAxis = false
			break
		}
	}

	override := false
	if noAxis {
		override = true
	} else {
		// check if we home an axis which needs the override
		for _, axis := range h.axes {
			if _, ok := gcmd.Get(string(axis), gcode.Default(nil)); ok {
				override = true
			}
		}
	}

	if !override {
		h.prevG28(gcmd)
		return
	}

	// Calculate forced position (if configured)
	th := toolhead.Of(h.printer)
	pos := th.Position()
	homingAxes := ""
	for axis, loc := range h.startPos {
		if loc != nil {
			pos[axis] = *loc
			homingAxes += string("xyz"[axis])
		}
	}
	th.SetPosition(pos, homingAxes)
	// Perform homing
	context := h.template.CreateContext()
	params := jinja.NewDict()
	for _, k := range gcmd.ParamNames() {
		params.Set(k, gcmd.Params()[k])
	}
	context["params"] = params
	context["rawparams"] = gcmd.RawParams()
	h.inScript = true
	defer func() { h.inScript = false }()
	h.template.RunGcodeFromCommand(context)
}
