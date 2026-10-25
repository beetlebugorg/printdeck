// Package safe_z_home homes Z at a set XY position: it replaces the G28 command.
//
// Ported to Go from klippy/extras/safe_z_home.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2019  Florian Heilmann <Florian.Heilmann@gmx.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package safe_z_home

import (
	"math"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/manual_probe"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/toolhead"
)

type kinematics interface {
	Status(eventtime float64) map[string]any
	ClearHomingState(axes string)
}

// SafeZHoming is the [safe_z_home] section.
type SafeZHoming struct {
	printer            *host.Printer
	homeXPos, homeYPos float64
	zHop, zHopSpeed    float64
	maxZ               float64
	speed              float64
	moveToPrevious     bool
	gcode              *gcode.Dispatch
	prevG28            gcode.Handler
}

func load(config *configfile.Wrapper) any {
	s := &SafeZHoming{printer: host.Of(config)}
	pos := config.GetFloatList("home_xy_position", ",", 2)
	s.homeXPos, s.homeYPos = pos[0], pos[1]
	s.zHop = config.GetFloat("z_hop", configfile.Default(0.0))
	s.zHopSpeed = config.GetFloat("z_hop_speed", configfile.Default(15.), configfile.Above(0.))
	zconfig := manual_probe.LookupZEndstopConfig(config)
	if zconfig == nil {
		configfile.Raise("Missing Z endstop config for safe_z_homing")
	}
	s.maxZ = zconfig.GetFloat("position_max", configfile.NoteValid(false))
	s.speed = config.GetFloat("speed", configfile.Default(50.0), configfile.Above(0.))
	s.moveToPrevious = config.GetBool("move_to_previous", configfile.Default(false))
	s.printer.LoadObject(config, "homing")
	s.gcode = gcode.Of(s.printer)
	s.prevG28 = s.gcode.UnregisterCommand("G28")
	s.gcode.RegisterCommand("G28", s.cmdG28, false, "")

	if config.HasSection("homing_override") {
		configfile.Raise("homing_override and safe_z_homing cannot" +
			" be used simultaneously")
	}
	return s
}

func init() { host.RegisterModule("safe_z_home", load, nil) }

func homedAxes(th *toolhead.ToolHead, curtime float64) string {
	h, _ := th.Kinematics().(kinematics).Status(curtime)["homed_axes"].(string)
	return h
}

func (s *SafeZHoming) cmdG28(gcmd *gcode.Command) {
	th := toolhead.Of(s.printer)
	none := math.NaN()

	// Perform Z Hop if necessary
	if s.zHop != 0.0 {
		// Check if Z axis is homed and its last known position
		curtime := s.printer.Reactor().Monotonic()
		pos := th.Position()

		if !strings.Contains(homedAxes(th, curtime), "z") {
			// Always perform the z_hop if the Z axis is not homed
			pos[2] = 0
			th.SetPosition(pos, "z")
			th.ManualMove([]float64{none, none, s.zHop}, s.zHopSpeed)
			th.Kinematics().(kinematics).ClearHomingState("z")
		} else if pos[2] < s.zHop {
			// If the Z axis is homed, and below z_hop, lift it to z_hop
			th.ManualMove([]float64{none, none, s.zHop}, s.zHopSpeed)
		}
	}

	// Determine which axes we need to home
	_, needX := gcmd.Get("X", gcode.Default(nil))
	_, needY := gcmd.Get("Y", gcode.Default(nil))
	_, needZ := gcmd.Get("Z", gcode.Default(nil))
	if !needX && !needY && !needZ {
		needX, needY, needZ = true, true, true
	}

	// Home XY axes if necessary
	newParams := map[string]string{}
	if needX {
		newParams["X"] = "0"
	}
	if needY {
		newParams["Y"] = "0"
	}
	if len(newParams) > 0 {
		g28Gcmd := s.gcode.CreateCommand("G28", "G28", newParams)
		s.prevG28(g28Gcmd)
	}

	// Home Z axis if necessary
	if needZ {
		// Throw an error if X or Y are not homed
		curtime := s.printer.Reactor().Monotonic()
		homed := homedAxes(th, curtime)
		if !strings.Contains(homed, "x") || !strings.Contains(homed, "y") {
			panic(gcmd.Error("Must home X and Y axes first"))
		}
		// Move to safe XY homing position
		prevpos := th.Position()
		th.ManualMove([]float64{s.homeXPos, s.homeYPos}, s.speed)
		// Home Z
		g28Gcmd := s.gcode.CreateCommand("G28", "G28", map[string]string{"Z": "0"})
		s.prevG28(g28Gcmd)
		// Perform Z Hop again for pressure-based probes
		if s.zHop != 0 {
			pos := th.Position()
			if pos[2] < s.zHop {
				th.ManualMove([]float64{none, none, s.zHop}, s.zHopSpeed)
			}
		}
		// Move XY back to previous positions
		if s.moveToPrevious {
			th.ManualMove(prevpos[:2], s.speed)
		}
	}
}
