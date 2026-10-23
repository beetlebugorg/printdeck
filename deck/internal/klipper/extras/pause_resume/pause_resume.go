// Package pause_resume pauses and resumes a print, with the capture and the restore of the
// position (PAUSE, RESUME, CLEAR_PAUSE, CANCEL_PRINT).
//
// Ported to Go from klippy/extras/pause_resume.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2019  Eric Callahan <arksine.code@gmail.com>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package pause_resume

import (
	"fmt"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/webhooks"
)

// virtualSD is the virtual_sdcard object.
type virtualSD interface {
	IsActive() bool
	DoPause()
	DoResume()
	DoCancel()
}

// PauseResume is the pause_resume object.
type PauseResume struct {
	printer          *host.Printer
	gcode            *gcode.Dispatch
	recoverVelocity  float64
	vSD              virtualSD
	isPaused         bool
	sdPaused         bool
	pauseCommandSent bool
}

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	p := &PauseResume{printer: printer, gcode: gcode.Of(printer)}
	p.recoverVelocity = config.GetFloat("recover_velocity", configfile.Default(50.))
	printer.RegisterEventHandler("klippy:connect", func(...any) { p.handleConnect() })
	p.gcode.RegisterCommand("PAUSE", p.cmdPause, false, "Pauses the current print")
	p.gcode.RegisterCommand("RESUME", p.cmdResume, false, "Resumes the print from a pause")
	p.gcode.RegisterCommand("CLEAR_PAUSE", p.cmdClearPause, false,
		"Clears the current paused state without resuming the print")
	p.gcode.RegisterCommand("CANCEL_PRINT", p.cmdCancelPrint, false, "Cancel the current print")
	wh := webhooks.Of(printer)
	// A cancel stops the command that runs now, such as the start macro (RunScriptAbort, Printdeck).
	wh.RegisterEndpoint("pause_resume/cancel", func(*webhooks.WebRequest) { p.gcode.RunScriptAbort("CANCEL_PRINT") })
	wh.RegisterEndpoint("pause_resume/pause", func(*webhooks.WebRequest) { p.gcode.RunScript("PAUSE") })
	wh.RegisterEndpoint("pause_resume/resume", func(*webhooks.WebRequest) { p.gcode.RunScript("RESUME") })
	return p
}

func init() { host.RegisterModule("pause_resume", load, nil) }

func (p *PauseResume) handleConnect() {
	p.vSD, _ = p.printer.TryLookupObject("virtual_sdcard").(virtualSD)
}

// Status is the pause state.
func (p *PauseResume) Status(eventtime float64) map[string]any {
	return map[string]any{"is_paused": p.isPaused}
}

func (p *PauseResume) isSDActive() bool { return p.vSD != nil && p.vSD.IsActive() }

// SendPauseCommand sends the pause command from an event. The module is not officially paused
// until the PAUSE gcode executes.
func (p *PauseResume) SendPauseCommand() {
	if !p.pauseCommandSent {
		if p.isSDActive() {
			// Printing from virtual sd, run pause command
			p.sdPaused = true
			p.vSD.DoPause()
		} else {
			p.sdPaused = false
			p.gcode.RespondInfo("action:paused", true)
		}
		p.pauseCommandSent = true
	}
}

func (p *PauseResume) cmdPause(gcmd *gcode.Command) {
	if p.isPaused {
		gcmd.RespondInfo("Print already paused", true)
		return
	}
	p.SendPauseCommand()
	p.gcode.RunScriptFromCommand("SAVE_GCODE_STATE NAME=PAUSE_STATE")
	p.isPaused = true
}

func (p *PauseResume) sendResumeCommand() {
	if p.sdPaused {
		// Printing from virtual sd, run pause command
		p.vSD.DoResume()
		p.sdPaused = false
	} else {
		p.gcode.RespondInfo("action:resumed", true)
	}
	p.pauseCommandSent = false
}

func (p *PauseResume) cmdResume(gcmd *gcode.Command) {
	if !p.isPaused {
		gcmd.RespondInfo("Print is not paused, resume aborted", true)
		return
	}
	velocity := gcmd.GetFloat("VELOCITY", configfile.Default(p.recoverVelocity))
	p.gcode.RunScriptFromCommand(fmt.Sprintf("RESTORE_GCODE_STATE NAME=PAUSE_STATE MOVE=1 MOVE_SPEED=%.4f", velocity))
	p.sendResumeCommand()
	p.isPaused = false
}

func (p *PauseResume) cmdClearPause(gcmd *gcode.Command) {
	p.isPaused, p.pauseCommandSent = false, false
}

func (p *PauseResume) cmdCancelPrint(gcmd *gcode.Command) {
	if p.isSDActive() || p.sdPaused {
		p.vSD.DoCancel()
	} else {
		gcmd.RespondInfo("action:cancel", true)
	}
	p.cmdClearPause(gcmd)
}
