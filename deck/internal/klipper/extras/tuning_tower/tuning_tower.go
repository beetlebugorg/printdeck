// Package tuning_tower changes a parameter at each Z height of a test print (TUNING_TOWER).
//
// Ported to Go from klippy/extras/tuning_tower.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2019  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package tuning_tower

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/gcode_move"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
)

const cancelZDelta = 2.0

// TuningTower is the move transform of a tuning test.
type TuningTower struct {
	printer          *host.Printer
	normalTransform  gcode_move.MoveTransform
	lastPosition     []float64
	lastZ            float64
	start, factor    float64
	band             float64
	stepDelta        float64
	stepHeight       float64
	skip             float64
	lastCommandValue *float64
	commandFmt       string
	gcodeMove        *gcode_move.GCodeMove
	gcode            *gcode.Dispatch
}

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	tt := &TuningTower{printer: printer, lastPosition: []float64{0., 0., 0., 0.}}
	tt.gcodeMove = printer.LoadObject(config, "gcode_move").(*gcode_move.GCodeMove)
	// Register command
	tt.gcode = gcode.Of(printer)
	tt.gcode.RegisterCommand("TUNING_TOWER", tt.cmdTuningTower, false, "Tool to adjust a parameter at each Z height")
	return tt
}

func init() { host.RegisterModule("tuning_tower", load, nil) }

func (tt *TuningTower) cmdTuningTower(gcmd *gcode.Command) {
	if tt.normalTransform != nil {
		tt.endTest()
	}
	// Get parameters
	command := gcmd.GetString("COMMAND")
	parameter := gcmd.GetString("PARAMETER")
	tt.start = gcmd.GetFloat("START", gcode.Default(0.))
	tt.factor = gcmd.GetFloat("FACTOR", gcode.Default(0.))
	tt.band = gcmd.GetFloat("BAND", gcode.Default(0.), gcode.Min(0.))
	tt.stepDelta = gcmd.GetFloat("STEP_DELTA", gcode.Default(0.))
	tt.stepHeight = gcmd.GetFloat("STEP_HEIGHT", gcode.Default(0.), gcode.Min(0.))
	tt.skip = gcmd.GetFloat("SKIP", gcode.Default(0.), gcode.Min(0.))
	if tt.factor != 0 && (tt.stepHeight != 0 || tt.stepDelta != 0) {
		panic(gcmd.Error("Cannot specify both FACTOR and STEP_DELTA/STEP_HEIGHT"))
	}
	if (tt.stepDelta != 0.) != (tt.stepHeight != 0.) {
		panic(gcmd.Error("Must specify both STEP_DELTA and STEP_HEIGHT"))
	}
	// Enable test mode
	if gcode.IsTraditionalGcode(command) {
		tt.commandFmt = command + " " + parameter + "%.9f"
	} else {
		tt.commandFmt = command + " " + parameter + "=%.9f"
	}
	tt.normalTransform = tt.gcodeMove.SetMoveTransform(tt, true)
	tt.lastZ = -99999999.9
	tt.lastCommandValue = nil
	tt.Position()
	parts := []string{fmt.Sprintf("start=%.6f", tt.start)}
	if tt.factor != 0 {
		parts = append(parts, fmt.Sprintf("factor=%.6f", tt.factor))
		if tt.band != 0 {
			parts = append(parts, fmt.Sprintf("band=%.6f", tt.band))
		}
	} else {
		parts = append(parts, fmt.Sprintf("step_delta=%.6f", tt.stepDelta), fmt.Sprintf("step_height=%.6f", tt.stepHeight))
	}
	if tt.skip != 0 {
		parts = append(parts, fmt.Sprintf("skip=%.6f", tt.skip))
	}
	gcmd.RespondInfo("Starting tuning test ("+strings.Join(parts, " ")+")", true)
}

// Position returns the position of the normal transform (gcode_move.MoveTransform).
func (tt *TuningTower) Position() []float64 {
	pos := tt.normalTransform.Position()
	tt.lastPosition = append([]float64(nil), pos...)
	return pos
}

func (tt *TuningTower) calcValue(z float64) float64 {
	if tt.skip != 0 {
		z = math.Max(0., z-tt.skip)
	}
	if tt.stepHeight != 0 {
		return tt.start + tt.stepDelta*math.Floor(z/tt.stepHeight)
	}
	if tt.band != 0 {
		z = (math.Floor(z/tt.band) + .5) * tt.band
	}
	return tt.start + z*tt.factor
}

// Move runs the command at a new Z height of an extrusion, and forwards the move
// (gcode_move.MoveTransform).
func (tt *TuningTower) Move(newpos []float64, speed float64) {
	normalTransform := tt.normalTransform
	if newpos[3] > tt.lastPosition[3] && newpos[2] != tt.lastZ && !slices.Equal(newpos[:3], tt.lastPosition[:3]) {
		// Extrusion move at new z height
		z := newpos[2]
		if z < tt.lastZ-cancelZDelta {
			// Extrude at a lower z height - probably start of new print
			tt.endTest()
		} else {
			// Process update
			gcodeZ := tt.gcodeMove.Status(0)["gcode_position"].(gcode.Coord).Z()
			newval := tt.calcValue(gcodeZ)
			tt.lastZ = z
			if tt.lastCommandValue == nil || newval != *tt.lastCommandValue {
				tt.lastCommandValue = &newval
				tt.gcode.RunScriptFromCommand(fmt.Sprintf(tt.commandFmt, newval))
			}
		}
	}
	// Forward move to actual handler
	tt.lastPosition = append(tt.lastPosition[:0], newpos...)
	normalTransform.Move(newpos, speed)
}

func (tt *TuningTower) endTest() {
	tt.gcode.RespondInfo("Ending tuning test mode", true)
	tt.gcodeMove.SetMoveTransform(tt.normalTransform, true)
	tt.normalTransform = nil
}

// IsActive reports that a test runs.
func (tt *TuningTower) IsActive() bool { return tt.normalTransform != nil }
