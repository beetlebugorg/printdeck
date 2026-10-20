// Package idle_timeout turns the heaters and motors off after a time with no moves, and tracks
// the printing state: "Idle", "Printing" or "Ready".
//
// Ported to Go from klippy/extras/idle_timeout.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package idle_timeout

import (
	"fmt"
	"log"
	"math"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/gcode_macro"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
	"printdeck/internal/klipper/toolhead"
)

const defaultIdleGcode = `
{% if 'heaters' in printer %}
   TURN_OFF_HEATERS
{% endif %}
M84
`

const (
	pinMinTime   = 0.100
	readyTimeout = .500
)

// IdleTimeout tracks the printing state.
type IdleTimeout struct {
	printer               *host.Printer
	reactor               *reactor.Reactor
	gcode                 *gcode.Dispatch
	toolhead              *toolhead.ToolHead
	timeoutTimer          *reactor.Timer
	idleTimeout           float64
	idleGcode             *gcode_macro.TemplateWrapper
	state                 string
	lastPrintStartSystime float64
}

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	it := &IdleTimeout{printer: printer, reactor: printer.Reactor(), gcode: gcode.Of(printer), state: "Idle"}
	printer.RegisterEventHandler("klippy:ready", func(...any) { it.handleReady() })
	it.idleTimeout = config.GetFloat("timeout", configfile.Default(600.), configfile.Above(0.))
	def := defaultIdleGcode
	it.idleGcode = gcode_macro.Load(config).LoadTemplate(config, "gcode", &def)
	it.gcode.RegisterCommand("SET_IDLE_TIMEOUT", it.cmdSetIdleTimeout, false, "Set the idle timeout in seconds")
	return it
}

func init() { host.RegisterModule("idle_timeout", load, nil) }

// Status returns the state, the printing time, and the timeout.
func (it *IdleTimeout) Status(eventtime float64) map[string]any {
	printingTime := 0.
	if it.state == "Printing" {
		printingTime = eventtime - it.lastPrintStartSystime
	}
	return map[string]any{"state": it.state, "printing_time": printingTime, "idle_timeout": it.idleTimeout}
}

func (it *IdleTimeout) handleReady() {
	it.toolhead = toolhead.Of(it.printer)
	it.timeoutTimer = it.reactor.RegisterTimer(it.timeoutHandler, reactor.Never)
	it.printer.RegisterEventHandler("toolhead:sync_print_time", func(args ...any) {
		it.handleSyncPrintTime(args[0].(float64), args[1].(float64), args[2].(float64))
	})
}

func (it *IdleTimeout) transitionIdleState(eventtime float64) float64 {
	it.state = "Printing"
	failed := func() (failed bool) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("idle timeout gcode execution: %v", r)
				failed = true
			}
		}()
		script := it.idleGcode.Render(nil)
		it.gcode.RunScript(script)
		return false
	}()
	if failed {
		it.state = "Ready"
		return eventtime + 1.
	}
	printTime := it.toolhead.LastMoveTime()
	it.state = "Idle"
	it.printer.SendEvent("idle_timeout:idle", printTime)
	return reactor.Never
}

func (it *IdleTimeout) checkIdleTimeout(eventtime float64) float64 {
	// Make sure toolhead class isn't busy
	printTime, estPrintTime, lookaheadEmpty := it.toolhead.CheckBusy(eventtime)
	idleTime := estPrintTime - printTime
	if !lookaheadEmpty || idleTime < 1. {
		// Toolhead is busy
		return eventtime + it.idleTimeout
	}
	if idleTime < it.idleTimeout {
		// Wait for idle timeout
		return eventtime + it.idleTimeout - idleTime
	}
	if it.gcode.Mutex().Test() {
		// Gcode class busy
		return eventtime + 1.
	}
	// Idle timeout has elapsed
	return it.transitionIdleState(eventtime)
}

func (it *IdleTimeout) timeoutHandler(eventtime float64) float64 {
	if it.printer.IsShutdown() {
		return reactor.Never
	}
	if it.state == "Ready" {
		return it.checkIdleTimeout(eventtime)
	}
	// Check if need to transition to "ready" state
	printTime, estPrintTime, lookaheadEmpty := it.toolhead.CheckBusy(eventtime)
	bufferTime := math.Min(2., printTime-estPrintTime)
	if !lookaheadEmpty {
		// Toolhead is busy
		return eventtime + readyTimeout + math.Max(0., bufferTime)
	}
	if bufferTime > -readyTimeout {
		// Wait for ready timeout
		return eventtime + readyTimeout + bufferTime
	}
	if it.gcode.Mutex().Test() {
		// Gcode class busy
		return eventtime + readyTimeout
	}
	// Transition to "ready" state
	it.state = "Ready"
	it.printer.SendEvent("idle_timeout:ready", estPrintTime+pinMinTime)
	return eventtime + it.idleTimeout
}

// handleSyncPrintTime gets the arguments of toolhead:sync_print_time in their order: the current
// time, the estimated print time, and the print time. Klipper names the last two the other way.
func (it *IdleTimeout) handleSyncPrintTime(curtime, printTime, estPrintTime float64) {
	if it.state == "Printing" {
		return
	}
	// Transition to "printing" state
	it.state = "Printing"
	it.lastPrintStartSystime = curtime
	checkTime := readyTimeout + printTime - estPrintTime
	it.reactor.UpdateTimer(it.timeoutTimer, curtime+checkTime)
	it.printer.SendEvent("idle_timeout:printing", estPrintTime+pinMinTime)
}

func (it *IdleTimeout) cmdSetIdleTimeout(gcmd *gcode.Command) {
	timeout := gcmd.GetFloat("TIMEOUT", gcode.Default(it.idleTimeout), gcode.Above(0.))
	it.idleTimeout = timeout
	gcmd.RespondInfo(fmt.Sprintf("idle_timeout: Timeout set to %.2f s", timeout), true)
	if it.state == "Ready" {
		it.reactor.UpdateTimer(it.timeoutTimer, it.reactor.Monotonic()+timeout)
	}
}
