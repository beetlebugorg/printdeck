// Package filament_motion_sensor is a filament sensor with an encoder: the filament runs out when
// the extruder moves and the encoder reports no motion.
//
// Ported to Go from klippy/extras/filament_motion_sensor.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2021 Joshua Wherrett <thejoshw.code@gmail.com>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package filament_motion_sensor

import (
	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/buttons"
	"printdeck/internal/klipper/extras/filament_switch_sensor"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/reactor"
)

const CheckRunoutTimeout = .250

type pastPositioner interface {
	FindPastPosition(printTime float64) float64
}

// EncoderSensor is a [filament_motion_sensor] section.
type EncoderSensor struct {
	printer                *host.Printer
	extruderName           string
	detectionLength        float64
	reactor                *reactor.Reactor
	runoutHelper           *filament_switch_sensor.RunoutHelper
	extruder               pastPositioner
	mcu                    *mcu.MCU
	filamentRunoutPos      float64
	extruderPosUpdateTimer *reactor.Timer
}

func loadPrefix(config *configfile.Wrapper) any {
	e := &EncoderSensor{printer: host.Of(config)}
	// Read config
	switchPin := config.Get("switch_pin")
	e.extruderName = config.Get("extruder")
	e.detectionLength = config.GetFloat("detection_length", configfile.Default(7.), configfile.Above(0.))
	// Configure pins
	btns := buttons.Load(config)
	btns.RegisterButtons([]string{switchPin}, e.encoderEvent)
	// Get printer objects
	e.reactor = e.printer.Reactor()
	e.runoutHelper = filament_switch_sensor.NewRunoutHelper(config)
	// Register commands and event handlers
	e.printer.RegisterEventHandler("klippy:ready", func(...any) { e.handleReady() })
	e.printer.RegisterEventHandler("idle_timeout:printing", func(...any) { e.handlePrinting() })
	e.printer.RegisterEventHandler("idle_timeout:ready", func(...any) { e.handleNotPrinting() })
	e.printer.RegisterEventHandler("idle_timeout:idle", func(...any) { e.handleNotPrinting() })
	return e
}

func init() { host.RegisterModule("filament_motion_sensor", nil, loadPrefix) }

func (e *EncoderSensor) Status(eventtime float64) map[string]any {
	return e.runoutHelper.Status(eventtime)
}

func (e *EncoderSensor) updateFilamentRunoutPos(eventtime float64) {
	e.filamentRunoutPos = e.extruderPos(eventtime) + e.detectionLength
}

func (e *EncoderSensor) handleReady() {
	e.extruder = e.printer.LookupObject(e.extruderName).(pastPositioner)
	e.mcu = e.printer.LookupObject("mcu").(*mcu.MCU)
	e.updateFilamentRunoutPos(e.reactor.Monotonic())
	e.extruderPosUpdateTimer = e.reactor.RegisterTimer(e.extruderPosUpdateEvent, reactor.Never)
}

func (e *EncoderSensor) handlePrinting() {
	e.reactor.UpdateTimer(e.extruderPosUpdateTimer, reactor.Now)
}

func (e *EncoderSensor) handleNotPrinting() {
	e.reactor.UpdateTimer(e.extruderPosUpdateTimer, reactor.Never)
}

func (e *EncoderSensor) extruderPos(eventtime float64) float64 {
	printTime := e.mcu.EstimatedPrintTime(eventtime)
	return e.extruder.FindPastPosition(printTime)
}

func (e *EncoderSensor) extruderPosUpdateEvent(eventtime float64) float64 {
	extruderPos := e.extruderPos(eventtime)
	// Check for filament runout
	e.runoutHelper.NoteFilamentPresent(eventtime, extruderPos < e.filamentRunoutPos)
	return eventtime + CheckRunoutTimeout
}

func (e *EncoderSensor) encoderEvent(eventtime float64, state int) {
	if e.extruder != nil {
		e.updateFilamentRunoutPos(eventtime)
		// Check for filament insertion
		// Filament is always assumed to be present on an encoder event
		e.runoutHelper.NoteFilamentPresent(eventtime, true)
	}
}
