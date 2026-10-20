// Package print_stats tracks the state, the durations and the filament use of a virtual sdcard
// print.
//
// Ported to Go from klippy/extras/print_stats.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2020  Eric Callahan <arksine.code@gmail.com>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package print_stats

import (
	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/gcode_move"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
)

// PrintStats is the print_stats object.
type PrintStats struct {
	gcodeMove         *gcode_move.GCodeMove
	reactor           *reactor.Reactor
	gcode             *gcode.Dispatch
	filename          string
	errorMessage      string
	state             string
	prevPauseDuration float64
	lastEpos          float64
	filamentUsed      float64
	totalDuration     float64
	printStartTime    *float64
	lastPauseTime     *float64
	initDuration      float64
	infoTotalLayer    *int
	infoCurrentLayer  *int
}

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	printer.LoadObject(config, "gcode_move")
	p := &PrintStats{gcodeMove: gcode_move.Of(printer), reactor: printer.Reactor()}
	p.Reset()
	// Register commands
	p.gcode = gcode.Of(printer)
	p.gcode.RegisterCommand("SET_PRINT_STATS_INFO", p.cmdSetPrintStatsInfo, false,
		"Pass slicer info like layer act and total to klipper")
	printer.RegisterEventHandler("extruder:activate_extruder", func(...any) { p.handleActivateExtruder() })
	return p
}

func init() { host.RegisterModule("print_stats", load, nil) }

// Load returns the print_stats object, and loads it the first time.
func Load(config *configfile.Wrapper) *PrintStats {
	return host.Of(config).LoadObject(config, "print_stats").(*PrintStats)
}

func ptr[T any](v T) *T { return &v }

func epos(status map[string]any) float64 { return status["position"].(gcode.Coord)[3] }

func (p *PrintStats) handleActivateExtruder() {
	p.lastEpos = epos(p.gcodeMove.Status(p.reactor.Monotonic()))
}

func (p *PrintStats) updateFilamentUsage(eventtime float64) {
	gcStatus := p.gcodeMove.Status(eventtime)
	curEpos := epos(gcStatus)
	p.filamentUsed += (curEpos - p.lastEpos) / gcStatus["extrude_factor"].(float64)
	p.lastEpos = curEpos
}

// SetCurrentFile resets the state for a new file.
func (p *PrintStats) SetCurrentFile(filename string) {
	p.Reset()
	p.filename = filename
}

// NoteStart notes the start or the resume of the print.
func (p *PrintStats) NoteStart() {
	curtime := p.reactor.Monotonic()
	if p.printStartTime == nil {
		p.printStartTime = ptr(curtime)
	} else if p.lastPauseTime != nil {
		// Update pause time duration
		pauseDuration := curtime - *p.lastPauseTime
		p.prevPauseDuration += pauseDuration
		p.lastPauseTime = nil
	}
	// Reset last e-position
	p.lastEpos = epos(p.gcodeMove.Status(curtime))
	p.state = "printing"
	p.errorMessage = ""
}

// NotePause notes a pause of the print.
func (p *PrintStats) NotePause() {
	if p.lastPauseTime == nil {
		curtime := p.reactor.Monotonic()
		p.lastPauseTime = ptr(curtime)
		// update filament usage
		p.updateFilamentUsage(curtime)
	}
	if p.state != "error" {
		p.state = "paused"
	}
}

// NoteComplete, NoteError and NoteCancel note the end of the print.
func (p *PrintStats) NoteComplete()            { p.noteFinish("complete", "") }
func (p *PrintStats) NoteError(message string) { p.noteFinish("error", message) }
func (p *PrintStats) NoteCancel()              { p.noteFinish("cancelled", "") }

func (p *PrintStats) noteFinish(state, errorMessage string) {
	if p.printStartTime == nil {
		return
	}
	p.state = state
	p.errorMessage = errorMessage
	eventtime := p.reactor.Monotonic()
	p.totalDuration = eventtime - *p.printStartTime
	if p.filamentUsed < 0.0000001 {
		// No positive extusion detected during print
		p.initDuration = p.totalDuration - p.prevPauseDuration
	}
	p.printStartTime = nil
}

// optionalInt is gcmd.get_int() with a default of None or a number, and a minimum of 0.
func optionalInt(gcmd *gcode.Command, name string, def *int) *int {
	if _, ok := gcmd.Params()[name]; !ok {
		return def
	}
	return ptr(gcmd.GetInt(name, configfile.Min(0)))
}

func (p *PrintStats) cmdSetPrintStatsInfo(gcmd *gcode.Command) {
	totalLayer := optionalInt(gcmd, "TOTAL_LAYER", p.infoTotalLayer)
	currentLayer := optionalInt(gcmd, "CURRENT_LAYER", p.infoCurrentLayer)
	if totalLayer != nil && *totalLayer == 0 {
		p.infoTotalLayer = nil
		p.infoCurrentLayer = nil
	} else if !intEqual(totalLayer, p.infoTotalLayer) {
		p.infoTotalLayer = totalLayer
		p.infoCurrentLayer = ptr(0)
	}
	if p.infoTotalLayer != nil && currentLayer != nil && !intEqual(currentLayer, p.infoCurrentLayer) {
		p.infoCurrentLayer = ptr(min(*currentLayer, *p.infoTotalLayer))
	}
}

func intEqual(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// Reset clears the state.
func (p *PrintStats) Reset() {
	p.filename, p.errorMessage = "", ""
	p.state = "standby"
	p.prevPauseDuration, p.lastEpos = 0., 0.
	p.filamentUsed, p.totalDuration = 0., 0.
	p.printStartTime, p.lastPauseTime = nil, nil
	p.initDuration = 0.
	p.infoTotalLayer = nil
	p.infoCurrentLayer = nil
}

func optional(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

// Status is the print state.
func (p *PrintStats) Status(eventtime float64) map[string]any {
	timePaused := p.prevPauseDuration
	if p.printStartTime != nil {
		if p.lastPauseTime != nil {
			// Calculate the total time spent paused during the print
			timePaused += eventtime - *p.lastPauseTime
		} else {
			// Accumulate filament if not paused
			p.updateFilamentUsage(eventtime)
		}
		p.totalDuration = eventtime - *p.printStartTime
		if p.filamentUsed < 0.0000001 {
			// Track duration prior to extrusion
			p.initDuration = p.totalDuration - timePaused
		}
	}
	printDuration := p.totalDuration - p.initDuration - timePaused
	return map[string]any{
		"filename":       p.filename,
		"total_duration": p.totalDuration,
		"print_duration": printDuration,
		"filament_used":  p.filamentUsed,
		"state":          p.state,
		"message":        p.errorMessage,
		"info": map[string]any{"total_layer": optional(p.infoTotalLayer),
			"current_layer": optional(p.infoCurrentLayer)},
	}
}
