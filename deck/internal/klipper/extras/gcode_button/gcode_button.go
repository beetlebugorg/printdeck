// Package gcode_button runs G-code when a button is pressed or released.
//
// Ported to Go from klippy/extras/gcode_button.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2019 Alec Plumb <alec@etherwalker.com>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package gcode_button

import (
	"log"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/buttons"
	"printdeck/internal/klipper/extras/gcode_macro"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
)

// GCodeButton is a [gcode_button] section.
type GCodeButton struct {
	printer         *host.Printer
	name            string
	pin             string
	lastState       int
	pressTemplate   *gcode_macro.TemplateWrapper
	releaseTemplate *gcode_macro.TemplateWrapper
	gcode           *gcode.Dispatch
}

func loadPrefix(config *configfile.Wrapper) any {
	parts := strings.Split(config.Name(), " ")
	b := &GCodeButton{printer: host.Of(config), name: parts[len(parts)-1]}
	b.pin = config.Get("pin")
	b.lastState = 0
	btns := buttons.Load(config)
	if _, ok := config.GetOptional("analog_range", configfile.Default(nil)); !ok {
		btns.RegisterDebounceButton(b.pin, b.buttonCallback, config)
	} else {
		r := config.GetFloatList("analog_range", ",", 2)
		pullup := config.GetFloat("analog_pullup_resistor", configfile.Default(4700.), configfile.Above(0.))
		btns.RegisterDebounceADCButton(b.pin, r[0], r[1], pullup, b.buttonCallback, config)
	}
	gm := gcode_macro.Load(config)
	b.pressTemplate = gm.LoadTemplate(config, "press_gcode", nil)
	empty := ""
	b.releaseTemplate = gm.LoadTemplate(config, "release_gcode", &empty)
	b.gcode = gcode.Of(b.printer)
	b.gcode.RegisterMuxCommand("QUERY_BUTTON", "BUTTON", &b.name, b.cmdQueryButton,
		"Report on the state of a button")
	return b
}

func init() { host.RegisterModule("gcode_button", nil, loadPrefix) }

func (b *GCodeButton) cmdQueryButton(gcmd *gcode.Command) {
	gcmd.RespondInfo(b.name+": "+b.Status(0)["state"].(string), true)
}

func (b *GCodeButton) buttonCallback(eventtime float64, state int) {
	b.lastState = state
	template := b.pressTemplate
	if state == 0 {
		template = b.releaseTemplate
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Script running error: %v", r)
		}
	}()
	commands := template.Render(nil)
	if strings.TrimSpace(commands) == "" {
		return
	}
	b.gcode.RunScript(commands)
}

func (b *GCodeButton) Status(eventtime float64) map[string]any {
	if b.lastState != 0 {
		return map[string]any{"state": "PRESSED"}
	}
	return map[string]any{"state": "RELEASED"}
}
