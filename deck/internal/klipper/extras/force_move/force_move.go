// Package force_move moves a stepper by itself, for diagnostics: STEPPER_BUZZ, FORCE_MOVE and
// SET_KINEMATIC_POSITION.
//
// Ported to Go from klippy/extras/force_move.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018-2025  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package force_move

import (
	"log"
	"math"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/motion_queuing"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/motion"
	"printdeck/internal/klipper/stepper"
)

const (
	buzzDistance        = 1.
	buzzVelocity        = buzzDistance / .250
	buzzRadiansDistance = math.Pi / 180.
	buzzRadiansVelocity = buzzRadiansDistance / .250
)

// CalcMoveTime returns the direction, the accel time, the cruise time and the cruise speed of a
// move of a distance.
func CalcMoveTime(dist, speed, accel float64) (axisR, accelT, cruiseT, cruiseV float64) {
	axisR = 1.
	if dist < 0. {
		axisR = -1.
		dist = -dist
	}
	if accel == 0 || dist == 0 {
		return axisR, 0., dist / speed, speed
	}
	maxCruiseV2 := dist * accel
	if maxCruiseV2 < speed*speed {
		speed = math.Sqrt(maxCruiseV2)
	}
	accelT = speed / accel
	accelDecelD := accelT * speed
	cruiseT = (dist - accelDecelD) / speed
	return axisR, accelT, cruiseT, speed
}

// toolhead is the part of the toolhead that the forced moves use.
type toolhead interface {
	FlushStepGeneration()
	LastMoveTime() float64
	Dwell(delay float64)
	Position() []float64
	SetPosition(newpos []float64, homingAxes string)
	Kinematics() any
}

type homingStateClearer interface{ ClearHomingState(axes string) }

type motorsEnabler interface {
	SetMotorsEnable(stepperNames []string, enable bool) bool
}

// ForceMove holds the steppers for the forced moves.
type ForceMove struct {
	printer           *host.Printer
	steppers          map[string]*stepper.MCUStepper
	motionQueuing     *motion_queuing.PrinterMotionQueuing
	trapq             *motion.TrapQ
	stepperKinematics *motion.StepperKinematics
	enableForceMove   bool
}

func load(config *configfile.Wrapper) any {
	fm := &ForceMove{printer: host.Of(config), steppers: map[string]*stepper.MCUStepper{}}
	// Setup iterative solver
	fm.motionQueuing = motion_queuing.Load(config)
	fm.trapq = fm.motionQueuing.AllocateTrapQ()
	fm.stepperKinematics = motion.NewCartesianStepper('x')
	// Register commands
	fm.enableForceMove = config.GetBool("enable_force_move", configfile.Default(false))
	if fm.enableForceMove {
		gcode.Of(fm.printer).RegisterCommand("SET_KINEMATIC_POSITION", fm.cmdSetKinematicPosition, false,
			"Force a low-level kinematic position")
	}
	return fm
}

func init() { host.RegisterModule("force_move", load, nil) }

// RegisterStepper adds a stepper and its commands.
func (fm *ForceMove) RegisterStepper(config *configfile.Wrapper, s *stepper.MCUStepper) {
	name := s.Name()
	fm.steppers[name] = s
	// Reuse mux helper args checks
	g := gcode.Of(fm.printer)
	g.RegisterMuxCommand("STEPPER_BUZZ", "STEPPER", &name, fm.cmdStepperBuzz,
		"Oscillate a given stepper to help id it")
	if fm.enableForceMove {
		g.RegisterMuxCommand("FORCE_MOVE", "STEPPER", &name, fm.cmdForceMove,
			"Manually move a stepper; invalidates kinematics")
	}
}

// LookupStepper returns a stepper by name.
func (fm *ForceMove) LookupStepper(name string) *stepper.MCUStepper {
	s, ok := fm.steppers[name]
	if !ok {
		configfile.Raise("Unknown stepper %s", name)
	}
	return s
}

func (fm *ForceMove) forceEnable(s *stepper.MCUStepper) bool {
	se := fm.printer.LookupObject("stepper_enable").(motorsEnabler)
	return se.SetMotorsEnable([]string{s.Name()}, true)
}

func (fm *ForceMove) restoreEnable(s *stepper.MCUStepper, didEnable bool) {
	if !didEnable {
		return
	}
	se := fm.printer.LookupObject("stepper_enable").(motorsEnabler)
	se.SetMotorsEnable([]string{s.Name()}, false)
}

// ManualMove moves a stepper by a distance, at a speed and an acceleration (0 for none).
func (fm *ForceMove) ManualMove(s *stepper.MCUStepper, dist, speed, accel float64) {
	th := fm.printer.LookupObject("toolhead").(toolhead)
	th.FlushStepGeneration()
	prevSK := s.SetStepperKinematics(fm.stepperKinematics)
	prevTrapq := s.SetTrapQ(fm.trapq)
	s.SetPosition([]float64{0., 0., 0.})
	axisR, accelT, cruiseT, cruiseV := CalcMoveTime(dist, speed, accel)
	printTime := th.LastMoveTime()
	fm.trapq.Append(printTime, accelT, cruiseT, accelT, 0., 0., 0., axisR, 0., 0., 0., cruiseV, accel)
	printTime = printTime + accelT + cruiseT + accelT
	fm.motionQueuing.NoteMCUMovequeueActivity(printTime, true)
	th.Dwell(accelT + cruiseT + accelT)
	th.FlushStepGeneration()
	s.SetTrapQ(prevTrapq)
	s.SetStepperKinematics(prevSK)
	fm.motionQueuing.WipeTrapQ(fm.trapq)
}

func (fm *ForceMove) cmdStepperBuzz(gcmd *gcode.Command) {
	s := fm.LookupStepper(gcmd.GetString("STEPPER"))
	log.Printf("Stepper buzz %s", s.Name())
	didEnable := fm.forceEnable(s)
	th := fm.printer.LookupObject("toolhead").(toolhead)
	dist, speed := buzzDistance, buzzVelocity
	if s.UnitsInRadians() {
		dist, speed = buzzRadiansDistance, buzzRadiansVelocity
	}
	for i := 0; i < 10; i++ {
		fm.ManualMove(s, dist, speed, 0.)
		th.Dwell(.050)
		fm.ManualMove(s, -dist, speed, 0.)
		th.Dwell(.450)
	}
	fm.restoreEnable(s, didEnable)
}

func (fm *ForceMove) cmdForceMove(gcmd *gcode.Command) {
	s := fm.LookupStepper(gcmd.GetString("STEPPER"))
	distance := gcmd.GetFloat("DISTANCE")
	speed := gcmd.GetFloat("VELOCITY", gcode.Above(0.))
	accel := gcmd.GetFloat("ACCEL", gcode.Default(0.), gcode.Min(0.))
	log.Printf("FORCE_MOVE %s distance=%.3f velocity=%.3f accel=%.3f", s.Name(), distance, speed, accel)
	fm.forceEnable(s)
	fm.ManualMove(s, distance, speed, accel)
}

func axesIn(s string) string {
	var out strings.Builder
	for _, a := range "xyz" {
		if strings.ContainsRune(s, a) {
			out.WriteRune(a)
		}
	}
	return out.String()
}

func (fm *ForceMove) cmdSetKinematicPosition(gcmd *gcode.Command) {
	th := fm.printer.LookupObject("toolhead").(toolhead)
	th.LastMoveTime()
	curpos := th.Position()
	x := gcmd.GetFloat("X", gcode.Default(curpos[0]))
	y := gcmd.GetFloat("Y", gcode.Default(curpos[1]))
	z := gcmd.GetFloat("Z", gcode.Default(curpos[2]))
	setHomedAxes := axesIn(strings.ToLower(gcmd.GetString("SET_HOMED", gcode.Default("xyz"))))
	var clearHomed string
	if _, ok := gcmd.Get("CLEAR_HOMED", gcode.Default(nil)); !ok {
		// "CLEAR" is an alias for "CLEAR_HOMED"; should deprecate
		clearHomed = strings.ToLower(gcmd.GetString("CLEAR", gcode.Default("")))
	} else {
		clearHomed = strings.ToLower(gcmd.GetString("CLEAR_HOMED", gcode.Default("")))
	}
	clearHomedAxes := axesIn(clearHomed)
	log.Printf("SET_KINEMATIC_POSITION pos=%.3f,%.3f,%.3f set_homed=%s clear_homed=%s",
		x, y, z, setHomedAxes, clearHomedAxes)
	th.SetPosition([]float64{x, y, z}, setHomedAxes)
	th.Kinematics().(homingStateClearer).ClearHomingState(clearHomedAxes)
}
