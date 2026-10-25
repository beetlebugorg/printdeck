// Package fan is the part cooling fan ([fan], M106, M107), and the fan object of the other fan
// modules.
//
// Ported to Go from klippy/extras/fan.py of Klipper (https://github.com/Klipper3d/klipper), commit
// 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f. The tachometer (pulse_counter.py) is not ported yet:
// a tachometer_pin is a config error.
//
//	Copyright (C) 2016-2024  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package fan

import (
	"math"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/output_pin"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/pins"
)

// Fan is a pwm fan.
type Fan struct {
	printer       *host.Printer
	lastFanValue  float64
	lastReqValue  float64
	maxPower      float64
	kickStartTime float64
	offBelow      float64
	mcuFan        mcu.PWMPin
	enablePin     mcu.DigitalOutPin
	gcrq          *output_pin.GCodeRequestQueue
}

// NewFan makes the fan of a config section.
func NewFan(config *configfile.Wrapper, defaultShutdownSpeed float64) *Fan {
	printer := host.Of(config)
	f := &Fan{printer: printer}
	// Read config
	f.maxPower = config.GetFloat("max_power", configfile.Default(1.), configfile.Above(0.), configfile.Max(1.))
	f.kickStartTime = config.GetFloat("kick_start_time", configfile.Default(0.1), configfile.Min(0.))
	f.offBelow = config.GetFloat("off_below", configfile.Default(0.), configfile.Min(0.), configfile.Max(1.))
	cycleTime := config.GetFloat("cycle_time", configfile.Default(0.010), configfile.Above(0.))
	hardwarePWM := config.GetBool("hardware_pwm", configfile.Default(false))
	shutdownSpeed := config.GetFloat("shutdown_speed", configfile.Default(defaultShutdownSpeed),
		configfile.Min(0.), configfile.Max(1.))
	// Setup pwm object
	ppins := printer.LookupObject("pins").(*pins.Pins)
	f.mcuFan = ppins.SetupPin("pwm", config.Get("pin")).(mcu.PWMPin)
	f.mcuFan.SetupMaxDuration(0.)
	f.mcuFan.SetupCycleTime(cycleTime, hardwarePWM)
	shutdownPower := math.Max(0., math.Min(f.maxPower, shutdownSpeed))
	f.mcuFan.SetupStartValue(0., shutdownPower)
	if enablePin, ok := config.GetOptional("enable_pin", configfile.Default(nil)); ok {
		f.enablePin = ppins.SetupPin("digital_out", enablePin).(mcu.DigitalOutPin)
		f.enablePin.SetupMaxDuration(0.)
	}
	// Create gcode request queue
	f.gcrq = output_pin.NewGCodeRequestQueue(config, f.mcuFan.MCU(), f.applySpeed)
	// Setup tachometer
	if _, ok := config.GetOptional("tachometer_pin", configfile.Default(nil)); ok {
		configfile.Raise("tachometer_pin in section '%s' is not supported yet", config.Name())
	}
	// Register callbacks
	printer.RegisterEventHandler("gcode:request_restart", func(args ...any) {
		f.SetSpeed(0., args[0].(float64))
	})
	return f
}

// MCU returns the board of the fan.
func (f *Fan) MCU() *mcu.MCU { return f.mcuFan.MCU() }

func (f *Fan) applySpeed(printTime, value float64) (string, float64) {
	if value < f.offBelow {
		value = 0.
	}
	value = math.Max(0., math.Min(f.maxPower, value*f.maxPower))
	if value == f.lastFanValue {
		return output_pin.ActionDiscard, 0.
	}
	if f.enablePin != nil {
		if value > 0 && f.lastFanValue == 0 {
			f.enablePin.SetDigital(printTime, true)
		} else if value == 0 && f.lastFanValue > 0 {
			f.enablePin.SetDigital(printTime, false)
		}
	}
	if value != 0 && f.kickStartTime != 0 && (f.lastFanValue == 0 || value-f.lastFanValue > .5) {
		// Run fan at full speed for specified kick_start_time
		f.lastReqValue = value
		f.lastFanValue = f.maxPower
		f.mcuFan.SetPWM(printTime, f.maxPower)
		return output_pin.ActionRepeat, printTime + f.kickStartTime
	}
	f.lastFanValue, f.lastReqValue = value, value
	f.mcuFan.SetPWM(printTime, value)
	return output_pin.ActionNone, 0
}

// SetSpeed sets the speed at a print time (NaN for now).
func (f *Fan) SetSpeed(value, printTime float64) { f.gcrq.SendAsyncRequest(value, printTime) }

// SetSpeedFromCommand sets the speed after the queued moves.
func (f *Fan) SetSpeedFromCommand(value float64) { f.gcrq.QueueGcodeRequest(value) }

// Status returns the speed, and the rpm (None with no tachometer).
func (f *Fan) Status(eventtime float64) map[string]any {
	return map[string]any{"speed": f.lastReqValue, "rpm": nil}
}

// PrinterFan is the part cooling fan of [fan].
type PrinterFan struct{ fan *Fan }

func load(config *configfile.Wrapper) any {
	pf := &PrinterFan{fan: NewFan(config, 0.)}
	g := gcode.Of(host.Of(config))
	g.RegisterCommand("M106", pf.cmdM106, false, "")
	g.RegisterCommand("M107", func(*gcode.Command) { pf.fan.SetSpeedFromCommand(0.) }, false, "")
	return pf
}

func init() { host.RegisterModule("fan", load, nil) }

func (pf *PrinterFan) Status(eventtime float64) map[string]any { return pf.fan.Status(eventtime) }

func (pf *PrinterFan) cmdM106(gcmd *gcode.Command) {
	// Set fan speed
	value := gcmd.GetFloat("S", gcode.Default(255.), gcode.Min(0.)) / 255.
	pf.fan.SetSpeedFromCommand(value)
}
