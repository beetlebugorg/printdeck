// Package filament_switch_sensor is a filament sensor on a switch: it pauses the print when the
// filament runs out.
//
// Ported to Go from klippy/extras/filament_switch_sensor.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2019  Eric Callahan <arksine.code@gmail.com>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package filament_switch_sensor

import (
	"fmt"
	"log"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/buttons"
	"printdeck/internal/klipper/extras/gcode_macro"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
)

type pauser interface{ SendPauseCommand() }

type statuser interface {
	Status(eventtime float64) map[string]any
}

// RunoutHelper runs the G-code of a runout or an insert of the filament.
type RunoutHelper struct {
	name            string
	printer         *host.Printer
	reactor         *reactor.Reactor
	gcode           *gcode.Dispatch
	runoutPause     bool
	runoutGcode     *gcode_macro.TemplateWrapper
	insertGcode     *gcode_macro.TemplateWrapper
	pauseDelay      float64
	eventDelay      float64
	minEventSystime float64
	filamentPresent bool
	sensorEnabled   bool
}

// NewRunoutHelper reads the runout options of a config section.
func NewRunoutHelper(config *configfile.Wrapper) *RunoutHelper {
	f := strings.Fields(config.Name())
	h := &RunoutHelper{name: f[len(f)-1], printer: host.Of(config)}
	h.reactor = h.printer.Reactor()
	h.gcode = gcode.Of(h.printer)
	// Read config
	h.runoutPause = config.GetBool("pause_on_runout", configfile.Default(true))
	if h.runoutPause {
		h.printer.LoadObject(config, "pause_resume")
	}
	gcodeMacro := gcode_macro.Load(config)
	if _, ok := config.GetOptional("runout_gcode", configfile.Default(nil)); h.runoutPause || ok {
		empty := ""
		h.runoutGcode = gcodeMacro.LoadTemplate(config, "runout_gcode", &empty)
	}
	if _, ok := config.GetOptional("insert_gcode", configfile.Default(nil)); ok {
		h.insertGcode = gcodeMacro.LoadTemplate(config, "insert_gcode", nil)
	}
	h.pauseDelay = config.GetFloat("pause_delay", configfile.Default(.5), configfile.Above(.0))
	h.eventDelay = config.GetFloat("event_delay", configfile.Default(3.), configfile.Min(.0))
	// Internal state
	h.minEventSystime = reactor.Never
	h.filamentPresent = false
	h.sensorEnabled = true
	// Register commands and event handlers
	h.printer.RegisterEventHandler("klippy:ready", func(...any) { h.handleReady() })
	h.gcode.RegisterMuxCommand("QUERY_FILAMENT_SENSOR", "SENSOR", &h.name,
		h.cmdQueryFilamentSensor, "Query the status of the Filament Sensor")
	h.gcode.RegisterMuxCommand("SET_FILAMENT_SENSOR", "SENSOR", &h.name,
		h.cmdSetFilamentSensor, "Sets the filament sensor on/off")
	return h
}

func (h *RunoutHelper) handleReady() {
	h.minEventSystime = h.reactor.Monotonic() + 2.
}

func (h *RunoutHelper) runoutEventHandler(eventtime float64) any {
	// Pausing from inside an event requires that the pause portion
	// of pause_resume execute immediately.
	pausePrefix := ""
	if h.runoutPause {
		h.printer.LookupObject("pause_resume").(pauser).SendPauseCommand()
		pausePrefix = "PAUSE\n"
		h.reactor.Pause(eventtime + h.pauseDelay)
	}
	h.execGcode(pausePrefix, h.runoutGcode)
	return nil
}

func (h *RunoutHelper) insertEventHandler(eventtime float64) any {
	h.execGcode("", h.insertGcode)
	return nil
}

func (h *RunoutHelper) execGcode(prefix string, template *gcode_macro.TemplateWrapper) {
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Script running error: %v", r)
			}
		}()
		h.gcode.RunScript(prefix + template.Render(nil) + "\nM400")
	}()
	h.minEventSystime = h.reactor.Monotonic() + h.eventDelay
}

// NoteFilamentPresent gets the state of the sensor.
func (h *RunoutHelper) NoteFilamentPresent(eventtime float64, isFilamentPresent bool) {
	if isFilamentPresent == h.filamentPresent {
		return
	}
	h.filamentPresent = isFilamentPresent

	if eventtime < h.minEventSystime || !h.sensorEnabled {
		// do not process during the initialization time, duplicates,
		// during the event delay time, while an event is running, or
		// when the sensor is disabled
		return
	}
	// Determine "printing" status
	now := h.reactor.Monotonic()
	idleTimeout := h.printer.LookupObject("idle_timeout").(statuser)
	isPrinting := idleTimeout.Status(now)["state"] == "Printing"
	// Perform filament action associated with status change (if any)
	if isFilamentPresent {
		if !isPrinting && h.insertGcode != nil {
			// insert detected
			h.minEventSystime = reactor.Never
			log.Printf("Filament Sensor %s: insert event detected, Time %.2f", h.name, now)
			h.reactor.RegisterCallback(h.insertEventHandler, reactor.Now)
		}
	} else if isPrinting && h.runoutGcode != nil {
		// runout detected
		h.minEventSystime = reactor.Never
		log.Printf("Filament Sensor %s: runout event detected, Time %.2f", h.name, now)
		h.reactor.RegisterCallback(h.runoutEventHandler, reactor.Now)
	}
}

func (h *RunoutHelper) Status(eventtime float64) map[string]any {
	return map[string]any{
		"filament_detected": h.filamentPresent,
		"enabled":           h.sensorEnabled}
}

func (h *RunoutHelper) cmdQueryFilamentSensor(gcmd *gcode.Command) {
	var msg string
	if h.filamentPresent {
		msg = fmt.Sprintf("Filament Sensor %s: filament detected", h.name)
	} else {
		msg = fmt.Sprintf("Filament Sensor %s: filament not detected", h.name)
	}
	gcmd.RespondInfo(msg, true)
}

func (h *RunoutHelper) cmdSetFilamentSensor(gcmd *gcode.Command) {
	h.sensorEnabled = gcmd.GetInt("ENABLE", gcode.Default(1)) != 0
}

// SwitchSensor is a [filament_switch_sensor] section.
type SwitchSensor struct {
	runoutHelper *RunoutHelper
}

func loadPrefix(config *configfile.Wrapper) any {
	s := &SwitchSensor{}
	b := buttons.Load(config)
	switchPin := config.Get("switch_pin")
	b.RegisterDebounceButton(switchPin, s.buttonHandler, config)
	s.runoutHelper = NewRunoutHelper(config)
	return s
}

func init() { host.RegisterModule("filament_switch_sensor", nil, loadPrefix) }

func (s *SwitchSensor) Status(eventtime float64) map[string]any {
	return s.runoutHelper.Status(eventtime)
}

func (s *SwitchSensor) buttonHandler(eventtime float64, state int) {
	s.runoutHelper.NoteFilamentPresent(eventtime, state != 0)
}
