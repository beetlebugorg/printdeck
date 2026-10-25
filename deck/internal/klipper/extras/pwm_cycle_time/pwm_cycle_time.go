// Package pwm_cycle_time is the [pwm_cycle_time] section: a software pwm output pin with a cycle
// time that SET_PIN can change, for example a beeper.
//
// Ported to Go from klippy/extras/pwm_cycle_time.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2017-2025  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package pwm_cycle_time

import (
	"fmt"
	"math"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/pins"
	"printdeck/internal/klipper/toolhead"
)

func b2i(v bool) int {
	if v {
		return 1
	}
	return 0
}

// MCUPWMCycle is a software pwm pin with a variable cycle time.
type MCUPWMCycle struct {
	mcu                       *mcu.MCU
	cycleTime                 float64
	oid                       int
	pin                       string
	invert                    bool
	startValue, shutdownValue float64
	lastClock, cycleTicks     int64
	setCmd, setCycleTicks     *mcu.CommandWrapper
}

func newMCUPWMCycle(pinParams *pins.Params, cycleTime, startValue, shutdownValue float64) *MCUPWMCycle {
	p := &MCUPWMCycle{mcu: pinParams.Chip.(*mcu.MCU), cycleTime: cycleTime, pin: pinParams.Pin,
		invert: pinParams.Invert != 0}
	p.mcu.RegisterConfigCallback(p.buildConfig)
	if p.invert {
		startValue = 1. - startValue
		shutdownValue = 1. - shutdownValue
	}
	p.startValue = math.Max(0., math.Min(1., startValue))
	p.shutdownValue = math.Max(0., math.Min(1., shutdownValue))
	return p
}

func (p *MCUPWMCycle) MCU() *mcu.MCU { return p.mcu }

func (p *MCUPWMCycle) buildConfig() {
	cmdQueue := p.mcu.AllocCommandQueue()
	curtime := p.mcu.Printer().Reactor().Monotonic()
	printtime := p.mcu.EstimatedPrintTime(curtime)
	p.lastClock = p.mcu.PrintTimeToClock(printtime + 0.200)
	cycleTicks := p.mcu.SecondsToClock(p.cycleTime)
	if p.shutdownValue != 0. && p.shutdownValue != 1. {
		configfile.Raise("shutdown value must be 0.0 or 1.0 on soft pwm")
	}
	if cycleTicks >= 1<<31 {
		configfile.Raise("PWM pin cycle time too large")
	}
	p.mcu.RequestMoveQueueSlot()
	p.oid = p.mcu.CreateOID()
	p.mcu.AddConfigCmd(fmt.Sprintf("config_digital_out oid=%d pin=%s value=%d"+
		" default_value=%d max_duration=%d", p.oid, p.pin, b2i(p.startValue >= 1.0),
		b2i(p.shutdownValue >= 0.5), 0), false, false)
	p.mcu.AddConfigCmd(fmt.Sprintf("set_digital_out_pwm_cycle oid=%d cycle_ticks=%d", p.oid, cycleTicks),
		false, false)
	p.cycleTicks = cycleTicks
	svalue := int64(p.startValue*float64(cycleTicks) + 0.5)
	p.mcu.AddConfigCmd(fmt.Sprintf("queue_digital_out oid=%d clock=%d on_ticks=%d", p.oid, p.lastClock, svalue),
		true, false)
	p.setCmd = p.mcu.LookupCommand("queue_digital_out oid=%c clock=%u on_ticks=%u", cmdQueue)
	p.setCycleTicks = p.mcu.LookupCommand("set_digital_out_pwm_cycle oid=%c cycle_ticks=%u", cmdQueue)
}

func (p *MCUPWMCycle) setPWMCycle(printTime, value, cycleTime float64) {
	clock := p.mcu.PrintTimeToClock(printTime)
	minclock := p.lastClock
	// Send updated cycle_time if necessary
	cycleTicks := p.mcu.SecondsToClock(cycleTime)
	if cycleTicks != p.cycleTicks {
		if cycleTicks >= 1<<31 {
			panic(gcode.Errorf("PWM cycle time too large"))
		}
		p.setCycleTicks.Send([]any{p.oid, cycleTicks}, minclock, clock)
		p.cycleTicks = cycleTicks
	}
	// Send pwm update
	if p.invert {
		value = 1. - value
	}
	v := int64(math.Max(0., math.Min(1., value))*float64(p.cycleTicks) + 0.5)
	p.setCmd.Send([]any{p.oid, clock, v}, p.lastClock, clock)
	p.lastClock = clock
}

// PrinterOutputPWMCycle is a [pwm_cycle_time] section.
type PrinterOutputPWMCycle struct {
	printer          *host.Printer
	lastPrintTime    float64
	scale            float64
	lastValue        float64
	shutdownValue    float64
	mcuPin           *MCUPWMCycle
	lastCycleTime    float64
	defaultCycleTime float64
}

func loadPrefix(config *configfile.Wrapper) any {
	o := &PrinterOutputPWMCycle{printer: host.Of(config)}
	o.lastPrintTime = 0.
	// Determine start and shutdown values
	o.scale = config.GetFloat("scale", configfile.Default(1.), configfile.Above(0.))
	o.lastValue = config.GetFloat("value", configfile.Default(0.), configfile.Min(0.),
		configfile.Max(o.scale)) / o.scale
	o.shutdownValue = config.GetFloat("shutdown_value", configfile.Default(0.), configfile.Min(0.),
		configfile.Max(o.scale)) / o.scale
	// Create pwm pin object
	ppins := o.printer.LookupObject("pins").(*pins.Pins)
	pinParams := ppins.LookupPin(config.Get("pin"), true, false, "")
	maxDuration := pinParams.Chip.(*mcu.MCU).MaxNominalDuration()
	cycleTime := config.GetFloat("cycle_time", configfile.Default(0.100), configfile.Above(0.),
		configfile.Max(maxDuration))
	o.mcuPin = newMCUPWMCycle(pinParams, cycleTime, o.lastValue, o.shutdownValue)
	o.lastCycleTime, o.defaultCycleTime = cycleTime, cycleTime
	// Register commands
	pinName := strings.Fields(config.Name())[1]
	g := gcode.Of(o.printer)
	g.RegisterMuxCommand("SET_PIN", "PIN", &pinName, o.cmdSetPin, "Set the value of an output pin")
	return o
}

func init() { host.RegisterModule("pwm_cycle_time", nil, loadPrefix) }

func (o *PrinterOutputPWMCycle) Status(eventtime float64) map[string]any {
	return map[string]any{"value": o.lastValue}
}

func (o *PrinterOutputPWMCycle) setPin(printTime, value, cycleTime float64) {
	if value == o.lastValue && cycleTime == o.lastCycleTime {
		return
	}
	minSchedTime := o.mcuPin.MCU().MinScheduleTime()
	printTime = math.Max(printTime, o.lastPrintTime+minSchedTime)
	o.mcuPin.setPWMCycle(printTime, value, cycleTime)
	o.lastValue = value
	o.lastCycleTime = cycleTime
	o.lastPrintTime = printTime
}

func (o *PrinterOutputPWMCycle) cmdSetPin(gcmd *gcode.Command) {
	// Read requested value
	value := gcmd.GetFloat("VALUE", gcode.Min(0.), gcode.Max(o.scale))
	value /= o.scale
	maxDuration := o.mcuPin.MCU().MaxNominalDuration()
	cycleTime := gcmd.GetFloat("CYCLE_TIME", gcode.Default(o.defaultCycleTime), gcode.Above(0.),
		gcode.Max(maxDuration))
	// Obtain print_time and apply requested settings
	toolhead.Of(o.printer).RegisterLookaheadCallback(func(printTime float64) {
		o.setPin(printTime, value, cycleTime)
	})
}
