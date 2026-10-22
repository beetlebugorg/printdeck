// Package printdeck_pause pauses a print with a message for the user, for example to put a part in
// a pocket on the bed or an insert in the print. It is a module of Printdeck, not a port of a
// Klipper module. The config section is [printdeck_pause] (printdeck.cfg has it).
//
//	PRINTDECK_PAUSE [MESSAGE="Put the part in the pocket"] [CHECK_X=x CHECK_Y=y CHECK_Z=z]
//
// pauses the print (PAUSE), retracts, lifts the nozzle, and parks it, away from the print. The
// Pause key of the deck uses it with no message, so that a filament change purges beside the print. With CHECK_Z, the deck offers
// a height check: PRINTDECK_CHECK_HEIGHT moves the nozzle over CHECK_X, CHECK_Y of the part and
// starts a manual probe (the paper test). After ACCEPT, the difference between the measured
// height and CHECK_Z, the height of the face in the file, goes into the Z offset of the paused
// print. PRINTDECK_RESUME primes the nozzle and resumes the print.
//
//	PRINTDECK_PURGE [LENGTH=15]
//
// purges at the park position, then runs the wipe G-code of the section (wipe_gcode), for the start
// of a print on a part: the bed has a part, so the start has no prime line on it.
//
//	Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package printdeck_pause

import (
	"fmt"
	"math"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/gcode_move"
	"printdeck/internal/klipper/extras/manual_probe"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/toolhead"
)

// pauseState is the name of the saved G-code state of pause_resume.
const pauseState = "PAUSE_STATE"

// PrintdeckPause is the [printdeck_pause] section.
type PrintdeckPause struct {
	printer *host.Printer
	gcode   *gcode.Dispatch

	parkX, parkY   float64 // the park position; NaN is the back left corner of the axes
	lift, retract  float64 // mm
	wipe           string  // the G-code that wipes the nozzle after a purge, or ""
	active         bool    // a PRINTDECK_PAUSE waits for the user
	message        string
	check          bool // the pause has a height check
	checkX, checkY float64
	checkZ         float64 // the height of the face in the file
	checked        bool
	delta          float64 // the measured height less checkZ, in mm
}

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	p := &PrintdeckPause{printer: printer, gcode: gcode.Of(printer)}
	p.parkX = config.GetFloat("park_x", configfile.Default(math.NaN()))
	p.parkY = config.GetFloat("park_y", configfile.Default(math.NaN()))
	p.lift = config.GetFloat("lift", configfile.Default(10.), configfile.Min(0.))
	p.retract = config.GetFloat("retract", configfile.Default(2.), configfile.Min(0.))
	p.wipe = config.Get("wipe_gcode", configfile.Default(""))
	p.gcode.RegisterCommand("PRINTDECK_PAUSE", p.cmdPause, false, "Pause the print with a message, and park")
	p.gcode.RegisterCommand("PRINTDECK_CHECK_HEIGHT", p.cmdCheckHeight, false,
		"Check the height of the part with a paper test")
	p.gcode.RegisterCommand("PRINTDECK_RESUME", p.cmdResume, false, "Prime and resume a PRINTDECK_PAUSE")
	p.gcode.RegisterCommand("PRINTDECK_PURGE", p.cmdPurge, false, "Purge at the park position, and wipe")
	return p
}

func init() { host.RegisterModule("printdeck_pause", load, nil) }

// Status is the pause for the deck: its message, and the height check.
func (p *PrintdeckPause) Status(eventtime float64) map[string]any {
	return map[string]any{"active": p.active, "message": p.message, "check": p.check, "checked": p.checked,
		"delta": p.delta}
}

func (p *PrintdeckPause) cmdPause(gcmd *gcode.Command) {
	p.message = gcmd.GetString("MESSAGE", gcode.Default(""))
	z := gcmd.GetFloat("CHECK_Z", gcode.Default(math.NaN()))
	p.check = !math.IsNaN(z)
	p.checkX = gcmd.GetFloat("CHECK_X", gcode.Default(0.))
	p.checkY = gcmd.GetFloat("CHECK_Y", gcode.Default(0.))
	p.checkZ, p.checked, p.delta = z, false, 0
	p.gcode.RunScriptFromCommand("PAUSE")
	p.active = true

	// Park: retract, lift, and move to the park position, inside the limits of the axes.
	x, y, z := p.park()
	p.gcode.RunScriptFromCommand(fmt.Sprintf("M83\nG1 E-%.3f F1800\nG90\nG1 Z%.3f F600\nG1 X%.3f Y%.3f F6000\nM400",
		p.retract, z, x, y))
	if p.message != "" {
		p.gcode.RespondInfo("Paused: "+p.message, true)
	}
}

// park is the park position as G-code coordinates: the park_x and park_y of the section (or the
// back left corner), and lift mm over the nozzle now.
func (p *PrintdeckPause) park() (x, y, z float64) {
	th := toolhead.Of(p.printer)
	pos := th.Position()
	lo, hi := axisLimits(th.Status(0))
	x, y = p.parkX, p.parkY
	if math.IsNaN(x) {
		x = lo[0] + 5
	}
	if math.IsNaN(y) {
		y = hi[1] - 5
	}
	z = math.Min(pos[2]+p.lift, hi[2]) - gcode_move.Of(p.printer).HomingZ()
	return x, y, z
}

func (p *PrintdeckPause) cmdPurge(gcmd *gcode.Command) {
	length := gcmd.GetFloat("LENGTH", gcode.Default(15.), gcode.Above(0.))
	x, y, _ := p.park()
	p.gcode.RunScriptFromCommand(fmt.Sprintf(
		"SAVE_GCODE_STATE NAME=printdeck_purge\nG90\nG1 X%.3f Y%.3f F6000\nM83\nG1 E%.3f F150\nRESTORE_GCODE_STATE NAME=printdeck_purge",
		x, y, length))
	if p.wipe != "" {
		p.gcode.RunScriptFromCommand(p.wipe)
	}
}

func (p *PrintdeckPause) cmdCheckHeight(gcmd *gcode.Command) {
	if !p.active || !p.check {
		panic(gcmd.Error("No height check: the print is not paused by PRINTDECK_PAUSE with CHECK_Z"))
	}
	// Over the point of the part, high, then 1 mm over the face.
	p.gcode.RunScriptFromCommand(fmt.Sprintf("G90\nG1 Z%.3f F600\nG1 X%.3f Y%.3f F6000\nG1 Z%.3f F300\nM400",
		p.checkZ+10, p.checkX, p.checkY, p.checkZ+1))
	offset := gcode_move.Of(p.printer).HomingZ()
	manual_probe.NewManualProbeHelper(p.printer, gcmd, func(r *manual_probe.ProbeResult) {
		if r == nil {
			return // abort
		}
		// The Z of the toolhead where the paper drags, as a Z of the file.
		p.delta = r.BedZ_ - offset - p.checkZ
		p.checked = true
		p.gcode.RespondInfo(fmt.Sprintf("The part is %.3f mm %s than in the file. The print goes on from the measured height.",
			math.Abs(p.delta), map[bool]string{true: "higher", false: "lower"}[p.delta >= 0]), true)
		p.gcode.RunScriptFromCommand(fmt.Sprintf("G90\nG1 Z%.3f F600", p.checkZ+10))
	})
}

func (p *PrintdeckPause) cmdResume(gcmd *gcode.Command) {
	if !p.active {
		p.gcode.RunScriptFromCommand("RESUME")
		return
	}
	if p.checked && p.delta != 0 {
		gcode_move.Of(p.printer).AdjustSavedZ(pauseState, p.delta)
	}
	// Prime the retract back. RESUME restores the extrusion mode and the E position of the print.
	p.gcode.RunScriptFromCommand(fmt.Sprintf("M83\nG1 E%.3f F300\nRESUME", p.retract))
	p.active, p.message, p.check, p.checked = false, "", false, false
}

// axisLimits are the minimum and the maximum of the axes, from the status of the toolhead.
func axisLimits(st map[string]any) (lo, hi [3]float64) {
	lo, hi = [3]float64{0, 0, 0}, [3]float64{300, 300, 300}
	if c, ok := st["axis_minimum"].(gcode.Coord); ok {
		lo = [3]float64{c.X(), c.Y(), c.Z()}
	}
	if c, ok := st["axis_maximum"].(gcode.Coord); ok {
		hi = [3]float64{c.X(), c.Y(), c.Z()}
	}
	return lo, hi
}
