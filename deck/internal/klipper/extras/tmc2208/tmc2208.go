// Package tmc2208 is the TMC2208 stepper driver on a TMC uart. The TMC2209 uses its registers.
//
// Ported to Go from klippy/extras/tmc2208.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018-2019  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package tmc2208

import (
	"fmt"
	"maps"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/tmc"
	"printdeck/internal/klipper/extras/tmc2130"
	"printdeck/internal/klipper/extras/tmc_uart"
	"printdeck/internal/klipper/host"
)

const TMCFrequency = 12000000.

var Registers = map[string]int{
	"GCONF": 0x00, "GSTAT": 0x01, "IFCNT": 0x02, "SLAVECONF": 0x03,
	"OTP_PROG": 0x04, "OTP_READ": 0x05, "IOIN": 0x06, "FACTORY_CONF": 0x07,
	"IHOLD_IRUN": 0x10, "TPOWERDOWN": 0x11, "TSTEP": 0x12, "TPWMTHRS": 0x13,
	"VACTUAL": 0x22, "MSCNT": 0x6a, "MSCURACT": 0x6b, "CHOPCONF": 0x6c,
	"DRV_STATUS": 0x6f, "PWMCONF": 0x70, "PWM_SCALE": 0x71, "PWM_AUTO": 0x72,
}

var ReadRegisters = []string{
	"GCONF", "GSTAT", "IFCNT", "OTP_READ", "IOIN", "FACTORY_CONF", "TSTEP",
	"MSCNT", "MSCURACT", "CHOPCONF", "DRV_STATUS",
	"PWMCONF", "PWM_SCALE", "PWM_AUTO",
}

// FieldOrder is the order of the registers of Fields.
var FieldOrder = []string{"GCONF", "GSTAT", "IFCNT", "SLAVECONF", "OTP_PROG", "OTP_READ",
	"IOIN@TMC222x", "IOIN@TMC220x", "FACTORY_CONF", "IHOLD_IRUN", "TPOWERDOWN", "TSTEP", "TPWMTHRS",
	"VACTUAL", "MSCNT", "MSCURACT", "CHOPCONF", "DRV_STATUS", "PWMCONF", "PWM_SCALE", "PWM_AUTO"}

// Fields returns the fields of the registers. Each driver gets its own copy.
func Fields() map[string]map[string]int64 {
	f := map[string]map[string]int64{}
	f["GCONF"] = map[string]int64{
		"i_scale_analog":   0x01,
		"internal_rsense":  0x01 << 1,
		"en_spreadcycle":   0x01 << 2,
		"shaft":            0x01 << 3,
		"index_otpw":       0x01 << 4,
		"index_step":       0x01 << 5,
		"pdn_disable":      0x01 << 6,
		"mstep_reg_select": 0x01 << 7,
		"multistep_filt":   0x01 << 8,
		"test_mode":        0x01 << 9,
	}
	f["GSTAT"] = map[string]int64{
		"reset":   0x01,
		"drv_err": 0x01 << 1,
		"uv_cp":   0x01 << 2,
	}
	f["IFCNT"] = map[string]int64{
		"ifcnt": 0xff,
	}
	f["SLAVECONF"] = map[string]int64{
		"senddelay": 0x0f << 8,
	}
	f["OTP_PROG"] = map[string]int64{
		"otpbit":   0x07,
		"otpbyte":  0x03 << 4,
		"otpmagic": 0xff << 8,
	}
	f["OTP_READ"] = map[string]int64{
		"otp_fclktrim":       0x1f,
		"otp_ottrim":         0x01 << 5,
		"otp_internalrsense": 0x01 << 6,
		"otp_tbl":            0x01 << 7,
		"otp_pwm_grad":       0x0f << 8,
		"otp_pwm_autograd":   0x01 << 12,
		"otp_tpwmthrs":       0x07 << 13,
		"otp_pwm_ofs":        0x01 << 16,
		"otp_pwm_reg":        0x01 << 17,
		"otp_pwm_freq":       0x01 << 18,
		"otp_iholddelay":     0x03 << 19,
		"otp_ihold":          0x03 << 21,
		"otp_en_spreadcycle": 0x01 << 23,
	}
	// IOIN mapping depends on the driver type (SEL_A field)
	// TMC222x (SEL_A == 0)
	f["IOIN@TMC222x"] = map[string]int64{
		"pdn_uart": 0x01 << 1,
		"spread":   0x01 << 2,
		"dir":      0x01 << 3,
		"enn":      0x01 << 4,
		"step":     0x01 << 5,
		"ms1":      0x01 << 6,
		"ms2":      0x01 << 7,
		"sel_a":    0x01 << 8,
		"version":  0xff << 24,
	}
	// TMC220x (SEL_A == 1)
	f["IOIN@TMC220x"] = map[string]int64{
		"enn":      0x01,
		"ms1":      0x01 << 2,
		"ms2":      0x01 << 3,
		"diag":     0x01 << 4,
		"pdn_uart": 0x01 << 6,
		"step":     0x01 << 7,
		"sel_a":    0x01 << 8,
		"dir":      0x01 << 9,
		"version":  0xff << 24,
	}
	f["FACTORY_CONF"] = map[string]int64{
		"fclktrim": 0x1f,
		"ottrim":   0x03 << 8,
	}
	f["IHOLD_IRUN"] = map[string]int64{
		"ihold":      0x1f,
		"irun":       0x1f << 8,
		"iholddelay": 0x0f << 16,
	}
	f["TPOWERDOWN"] = map[string]int64{
		"tpowerdown": 0xff,
	}
	f["TSTEP"] = map[string]int64{
		"tstep": 0xfffff,
	}
	f["TPWMTHRS"] = map[string]int64{
		"tpwmthrs": 0xfffff,
	}
	f["VACTUAL"] = map[string]int64{
		"vactual": 0xffffff,
	}
	f["MSCNT"] = map[string]int64{
		"mscnt": 0x3ff,
	}
	f["MSCURACT"] = map[string]int64{
		"cur_a": 0x1ff,
		"cur_b": 0x1ff << 16,
	}
	f["CHOPCONF"] = map[string]int64{
		"toff":    0x0f,
		"hstrt":   0x07 << 4,
		"hend":    0x0f << 7,
		"tbl":     0x03 << 15,
		"vsense":  0x01 << 17,
		"mres":    0x0f << 24,
		"intpol":  0x01 << 28,
		"dedge":   0x01 << 29,
		"diss2g":  0x01 << 30,
		"diss2vs": 0x01 << 31,
	}
	f["DRV_STATUS"] = map[string]int64{
		"otpw":      0x01,
		"ot":        0x01 << 1,
		"s2ga":      0x01 << 2,
		"s2gb":      0x01 << 3,
		"s2vsa":     0x01 << 4,
		"s2vsb":     0x01 << 5,
		"ola":       0x01 << 6,
		"olb":       0x01 << 7,
		"t120":      0x01 << 8,
		"t143":      0x01 << 9,
		"t150":      0x01 << 10,
		"t157":      0x01 << 11,
		"cs_actual": 0x1f << 16,
		"stealth":   0x01 << 30,
		"stst":      0x01 << 31,
	}
	f["PWMCONF"] = map[string]int64{
		"pwm_ofs":       0xff,
		"pwm_grad":      0xff << 8,
		"pwm_freq":      0x03 << 16,
		"pwm_autoscale": 0x01 << 18,
		"pwm_autograd":  0x01 << 19,
		"freewheel":     0x03 << 20,
		"pwm_reg":       0xf << 24,
		"pwm_lim":       0xf << 28,
	}
	f["PWM_SCALE"] = map[string]int64{
		"pwm_scale_sum":  0xff,
		"pwm_scale_auto": 0x1ff << 16,
	}
	f["PWM_AUTO"] = map[string]int64{
		"pwm_ofs_auto":  0xff,
		"pwm_grad_auto": 0xff << 16,
	}
	return f
}

var SignedFields = []string{"cur_a", "cur_b", "pwm_scale_auto"}

func flag(text string) func(int64) string {
	return func(v int64) string {
		if v != 0 {
			return text
		}
		return ""
	}
}

// FieldFormatters are the formatters of tmc2130 and the formatters of the TMC2208.
var FieldFormatters = func() map[string]func(int64) string {
	f := maps.Clone(tmc2130.FieldFormatters)
	f["sel_a"] = func(v int64) string { return fmt.Sprintf("%d(%s)", v, []string{"TMC222x", "TMC220x"}[v]) }
	f["s2vsa"] = flag("1(ShortToSupply_A!)")
	f["s2vsb"] = flag("1(ShortToSupply_B!)")
	return f
}()

// ######################################################################
// TMC2208 printer object
// ######################################################################

// TMC2208 is a [tmc2208] section.
type TMC2208 struct {
	fields    *tmc.FieldHelper
	mcuTMC    *tmc_uart.MCUTMCUart
	cmdhelper *tmc.TMCCommandHelper
}

func loadPrefix(config *configfile.Wrapper) any {
	t := &TMC2208{}
	// Setup mcu communication
	t.fields = tmc.NewFieldHelper(Fields(), FieldOrder, SignedFields, FieldFormatters, nil)
	t.mcuTMC = tmc_uart.NewMCUTMCUart(config, Registers, t.fields, 0, TMCFrequency)
	t.fields.SetField("pdn_disable", 1)
	// Register commands
	currentHelper := tmc2130.NewTMCCurrentHelper(config, t.mcuTMC)
	t.cmdhelper = tmc.NewTMCCommandHelper(config, t.mcuTMC, currentHelper)
	t.cmdhelper.SetupRegisterDump(ReadRegisters, t.readTranslate)
	// Setup basic register values
	t.fields.SetField("mstep_reg_select", 1)
	tmc.TMCStealthchopHelper(config, t.mcuTMC)
	// Allow other registers to be set from the config
	set := func(name string, def int64) { t.fields.SetConfigField(config, name, def) }
	// GCONF
	set("multistep_filt", 1)
	// CHOPCONF
	set("toff", 3)
	set("hstrt", 5)
	set("hend", 0)
	set("tbl", 2)
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
	return t
}

func init() { host.RegisterModule("tmc2208", nil, loadPrefix) }

func (t *TMC2208) readTranslate(regName string, val int64) (string, int64) {
	if regName == "IOIN" {
		drvType := t.fields.FieldOf("sel_a", val, "")
		if drvType != 0 {
			regName = "IOIN@TMC220x"
		} else {
			regName = "IOIN@TMC222x"
		}
	}
	return regName, val
}

// PhaseOffset returns the phase offset (nil for unknown) and the number of phases.
func (t *TMC2208) PhaseOffset() (*int64, int64) { return t.cmdhelper.PhaseOffset() }

func (t *TMC2208) Status(eventtime float64) map[string]any { return t.cmdhelper.Status(eventtime) }
