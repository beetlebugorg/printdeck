// Package tmc2209 is the TMC2209 stepper driver on a TMC uart, with StallGuard for sensorless
// homing.
//
// Ported to Go from klippy/extras/tmc2209.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2019  Stephan Oelze <stephan.oelze@gmail.com>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package tmc2209

import (
	"maps"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/tmc"
	"printdeck/internal/klipper/extras/tmc2130"
	"printdeck/internal/klipper/extras/tmc2208"
	"printdeck/internal/klipper/extras/tmc_uart"
	"printdeck/internal/klipper/host"
)

const TMCFrequency = 12000000.

var Registers = func() map[string]int {
	r := maps.Clone(tmc2208.Registers)
	r["TCOOLTHRS"] = 0x14
	r["COOLCONF"] = 0x42
	r["SGTHRS"] = 0x40
	r["SG_RESULT"] = 0x41
	return r
}()

var ReadRegisters = append(append([]string(nil), tmc2208.ReadRegisters...), "SG_RESULT")

// FieldOrder is the order of the registers of Fields.
var FieldOrder = append(append([]string(nil), tmc2208.FieldOrder...),
	"COOLCONF", "IOIN", "SGTHRS", "SG_RESULT", "TCOOLTHRS")

// Fields returns the fields of the registers. Each driver gets its own copy.
func Fields() map[string]map[string]int64 {
	f := tmc2208.Fields()
	f["COOLCONF"] = map[string]int64{
		"semin":  0x0F << 0,
		"seup":   0x03 << 5,
		"semax":  0x0F << 8,
		"sedn":   0x03 << 13,
		"seimin": 0x01 << 15,
	}
	f["IOIN"] = map[string]int64{
		"enn":       0x01 << 0,
		"ms1":       0x01 << 2,
		"ms2":       0x01 << 3,
		"diag":      0x01 << 4,
		"pdn_uart":  0x01 << 6,
		"step":      0x01 << 7,
		"spread_en": 0x01 << 8,
		"dir":       0x01 << 9,
		"version":   0xff << 24,
	}
	f["SGTHRS"] = map[string]int64{
		"sgthrs": 0xFF << 0,
	}
	f["SG_RESULT"] = map[string]int64{
		"sg_result": 0x3FF << 0,
	}
	f["TCOOLTHRS"] = map[string]int64{
		"tcoolthrs": 0xfffff,
	}
	return f
}

var FieldFormatters = maps.Clone(tmc2208.FieldFormatters)

// ######################################################################
// TMC2209 printer object
// ######################################################################

// TMC2209 is a [tmc2209] section.
type TMC2209 struct {
	fields    *tmc.FieldHelper
	mcuTMC    *tmc_uart.MCUTMCUart
	cmdhelper *tmc.TMCCommandHelper
}

func loadPrefix(config *configfile.Wrapper) any {
	t := &TMC2209{}
	// Setup mcu communication
	t.fields = tmc.NewFieldHelper(Fields(), FieldOrder, tmc2208.SignedFields, FieldFormatters, nil)
	t.mcuTMC = tmc_uart.NewMCUTMCUart(config, Registers, t.fields, 3, TMCFrequency)
	// Setup fields for UART
	t.fields.SetField("pdn_disable", 1)
	t.fields.SetField("senddelay", 2) // Avoid tx errors on shared uart
	// Allow virtual pins to be created
	tmc.NewTMCVirtualPinHelper(config, t.mcuTMC)
	// Register commands
	currentHelper := tmc2130.NewTMCCurrentHelper(config, t.mcuTMC)
	t.cmdhelper = tmc.NewTMCCommandHelper(config, t.mcuTMC, currentHelper)
	t.cmdhelper.SetupRegisterDump(ReadRegisters, nil)
	// Setup basic register values
	t.fields.SetField("mstep_reg_select", 1)
	tmc.TMCStealthchopHelper(config, t.mcuTMC)
	tmc.TMCVcoolthrsHelper(config, t.mcuTMC)
	// Allow other registers to be set from the config
	set := func(name string, def int64) { t.fields.SetConfigField(config, name, def) }
	// GCONF
	set("multistep_filt", 1)
	// CHOPCONF
	set("toff", 3)
	set("hstrt", 5)
	set("hend", 0)
	set("tbl", 2)
	// COOLCONF
	set("semin", 0)
	set("seup", 0)
	set("semax", 0)
	set("sedn", 0)
	set("seimin", 0)
	// IHOLDIRUN
	set("iholddelay", 8)
	// PWMCONF
	set("pwm_ofs", 36)
	set("pwm_grad", 14)
	set("pwm_freq", 1)
	set("pwm_autoscale", 1)
	set("pwm_autograd", 1)
	set("freewheel", 0)
	set("pwm_reg", 8)
	set("pwm_lim", 12)
	// TPOWERDOWN
	set("tpowerdown", 20)
	// SGTHRS
	set("sgthrs", 0)
	return t
}

func init() { host.RegisterModule("tmc2209", nil, loadPrefix) }

// PhaseOffset returns the phase offset (nil for unknown) and the number of phases.
func (t *TMC2209) PhaseOffset() (*int64, int64) { return t.cmdhelper.PhaseOffset() }

func (t *TMC2209) Status(eventtime float64) map[string]any { return t.cmdhelper.Status(eventtime) }
