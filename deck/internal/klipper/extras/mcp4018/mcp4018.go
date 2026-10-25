// Package mcp4018 is the MCP4018 digipot on I2C: it sets the current of a stepper driver.
//
// Ported to Go from klippy/extras/mcp4018.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2019-2025  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package mcp4018

import (
	"fmt"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/bus"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
)

// MCP4018 is an [mcp4018] section.
type MCP4018 struct {
	printer    *host.Printer
	i2c        *bus.MCUI2C
	scale      float64
	startValue float64
	name       string
}

func loadPrefix(config *configfile.Wrapper) any {
	m := &MCP4018{printer: host.Of(config)}
	m.i2c = bus.MCUI2CFromConfig(config, 0x2f, 100000, false)
	m.scale = config.GetFloat("scale", configfile.Default(1.), configfile.Above(0.))
	m.startValue = config.GetFloat("wiper", configfile.Min(0.), configfile.Max(m.scale))
	m.printer.RegisterEventHandler("klippy:connect", func(...any) { m.setDAC(m.startValue) })
	// Register commands
	m.name = strings.Fields(config.Name())[1]
	g := gcode.Of(m.printer)
	g.RegisterMuxCommand("SET_DIGIPOT", "DIGIPOT", &m.name, m.cmdSetDigipot, "Set digipot value")
	return m
}

func init() { host.RegisterModule("mcp4018", nil, loadPrefix) }

func (m *MCP4018) setDAC(value float64) {
	val := int(value*127./m.scale + .5)
	m.i2c.I2CWrite([]byte{byte(val)}, 0, 0, true)
}

func (m *MCP4018) cmdSetDigipot(gcmd *gcode.Command) {
	wiper := gcmd.GetFloat("WIPER", gcode.Min(0.), gcode.Max(m.scale))
	m.setDAC(wiper)
	gcmd.RespondInfo(fmt.Sprintf("New value for DIGIPOT = %s, wiper = %.2f", m.name, wiper), true)
}
