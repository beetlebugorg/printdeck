// Package printdeck_backlash is the [printdeck_backlash] section, a module of Printdeck: Klipper has
// no such module. It corrects the backlash of X and Y, as M425 of Marlin does.
//
//	[printdeck_backlash]
//	x: 0.2   # the backlash of X in mm (CUBE_MEASURE measures it)
//	y: 0     # the backlash of Y in mm
//
// When an axis turns back, the motor turns through the play before the carriage moves. The module
// adds the backlash to the first move after a reversal, so the carriage gets to its place. A move
// in the homing direction of the axis has no correction. So the places of the homing, the probe and
// CUBE_MEASURE stay the same. The correction stays in the position limits of the axis.
//
// Moves of other modules (homing, probing, CUBE_MEASURE) go to the toolhead directly, with no
// correction. After such a move, the module starts again with no correction.
//
// SET_BACKLASH X=<mm> Y=<mm> changes the values until a restart. SET_BACKLASH with no values
// reports them.
//
//	Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package printdeck_backlash

import (
	"fmt"
	"math"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/gcode_move"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/stepper"
	"printdeck/internal/klipper/toolhead"
)

const maxBacklash = 1.

// eps is the smallest change of an axis that sets its direction.
const eps = 1e-6

// Backlash is the [printdeck_backlash] section and its move transform.
type Backlash struct {
	printer *host.Printer
	next    gcode_move.MoveTransform
	comp    Comp
}

// Comp is the correction of X and Y. It is a separate type for the tests.
type Comp struct {
	Backlash [2]float64 // mm
	HomeDir  [2]float64 // +1 or -1: the direction of the last homing move
	Min, Max [2]float64 // the position limits

	dir    [2]float64 // the direction of the last move: +1, -1, or 0 for the homing direction
	off    [2]float64 // the correction in the last move
	target []float64  // the last position without the correction
	last   []float64  // the last position given to the next transform, nil after a reset
}

// Reset starts with no correction, as after a homing move.
func (c *Comp) Reset() {
	c.dir, c.off, c.target, c.last = [2]float64{}, [2]float64{}, nil, nil
}

// Apply returns the position for the next transform. to is the new position. Call Position
// before the first Apply after a reset.
func (c *Comp) Apply(to []float64) []float64 {
	out := append([]float64(nil), to...)
	for a := 0; a < 2; a++ {
		d := to[a] - c.target[a]
		if math.Abs(d) > eps {
			dir := math.Copysign(1, d)
			if dir == c.HomeDir[a] {
				c.dir[a], c.off[a] = 0, 0
			} else if c.dir[a] != dir {
				c.dir[a], c.off[a] = dir, dir*c.Backlash[a]
			}
		}
		out[a] = math.Max(c.Min[a], math.Min(c.Max[a], to[a]+c.off[a]))
	}
	c.target = append(c.target[:0], to...)
	c.last = append(c.last[:0], out...)
	return out
}

// Position returns the position without the correction. pos is the position of the next
// transform. A change of it by another module resets the correction.
func (c *Comp) Position(pos []float64) []float64 {
	if c.last == nil || math.Abs(pos[0]-c.last[0]) > eps || math.Abs(pos[1]-c.last[1]) > eps {
		c.Reset()
		c.target = append([]float64(nil), pos...)
		c.last = append([]float64(nil), pos...)
		return pos
	}
	out := append([]float64(nil), pos...)
	for a := 0; a < 2; a++ {
		out[a] -= c.off[a]
	}
	return out
}

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	b := &Backlash{printer: printer}
	b.comp.Backlash[0] = config.GetFloat("x", configfile.Default(0.), configfile.Min(0.), configfile.Max(maxBacklash))
	b.comp.Backlash[1] = config.GetFloat("y", configfile.Default(0.), configfile.Min(0.), configfile.Max(maxBacklash))
	gm := printer.LoadObject(config, "gcode_move").(*gcode_move.GCodeMove)
	// At connect, all the other transforms (bed_mesh, bed_tilt) are set. This transform goes
	// after them, next to the toolhead.
	printer.RegisterEventHandler("klippy:connect", func(...any) {
		kin, ok := toolhead.Of(printer).Kin().(interface{ Rails() []*stepper.Rail })
		if !ok || len(kin.Rails()) < 2 {
			configfile.Raise("[printdeck_backlash] needs cartesian or corexy kinematics")
		}
		for a, r := range kin.Rails()[:2] {
			b.comp.Min[a], b.comp.Max[a] = r.Range()
			b.comp.HomeDir[a] = -1
			if r.HomingInfo().PositiveDir {
				b.comp.HomeDir[a] = 1
			}
		}
		b.next = gm.SetMoveTransform(b, true)
	})
	gcode.Of(printer).RegisterCommand("SET_BACKLASH", b.cmdSet, false,
		"Set or report the backlash correction of X and Y")
	return b
}

func init() { host.RegisterModule("printdeck_backlash", load, nil) }

// Move adds the correction and gives the move to the next transform.
func (b *Backlash) Move(newpos []float64, speed float64) {
	if b.comp.target == nil {
		b.Position()
	}
	b.next.Move(b.comp.Apply(newpos), speed)
}

// Position is the position of the next transform without the correction.
func (b *Backlash) Position() []float64 {
	return b.comp.Position(b.next.Position())
}

// Status reports the values: x and y.
func (b *Backlash) Status(eventtime float64) map[string]any {
	return map[string]any{"x": b.comp.Backlash[0], "y": b.comp.Backlash[1]}
}

func (b *Backlash) cmdSet(gcmd *gcode.Command) {
	b.comp.Backlash[0] = gcmd.GetFloat("X", gcode.Default(b.comp.Backlash[0]), gcode.Min(0.), gcode.Max(maxBacklash))
	b.comp.Backlash[1] = gcmd.GetFloat("Y", gcode.Default(b.comp.Backlash[1]), gcode.Min(0.), gcode.Max(maxBacklash))
	gcmd.RespondInfo(fmt.Sprintf("Backlash correction: X %.3f mm, Y %.3f mm", b.comp.Backlash[0], b.comp.Backlash[1]), true)
}
