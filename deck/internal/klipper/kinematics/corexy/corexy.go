// Package corexy is the kinematics of a corexy printer: two belts move X and Y together.
//
// Ported to Go from klippy/kinematics/corexy.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2017-2021  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package corexy

import (
	"math"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/motion"
	"printdeck/internal/klipper/stepper"
	"printdeck/internal/klipper/toolhead"
)

// CoreXYKinematics is the corexy kinematics.
type CoreXYKinematics struct {
	rails                   []*stepper.Rail
	maxZVelocity, maxZAccel float64
	limits                  [3][2]float64
	axesMin, axesMax        gcode.Coord
}

func load(th *toolhead.ToolHead, config *configfile.Wrapper) toolhead.Kinematics {
	k := &CoreXYKinematics{}
	// Setup axis rails
	for _, n := range "xyz" {
		k.rails = append(k.rails, stepper.LookupMultiRail(config.GetSection("stepper_"+string(n)), true, nil, false))
	}
	for _, s := range k.rails[1].Steppers() {
		k.rails[0].Endstops()[0].Endstop.AddStepper(s)
	}
	for _, s := range k.rails[0].Steppers() {
		k.rails[1].Endstops()[0].Endstop.AddStepper(s)
	}
	k.rails[0].SetupItersolve(func() *motion.StepperKinematics { return motion.NewCoreXYStepper('+') })
	k.rails[1].SetupItersolve(func() *motion.StepperKinematics { return motion.NewCoreXYStepper('-') })
	k.rails[2].SetupItersolve(func() *motion.StepperKinematics { return motion.NewCartesianStepper('z') })
	for _, s := range k.steppers() {
		s.SetTrapQ(th.TrapQ())
	}
	// Setup boundary checks
	maxVelocity, maxAccel := th.MaxVelocity()
	k.maxZVelocity = config.GetFloat("max_z_velocity", configfile.Default(maxVelocity), configfile.Above(0.),
		configfile.Max(maxVelocity))
	k.maxZAccel = config.GetFloat("max_z_accel", configfile.Default(maxAccel), configfile.Above(0.),
		configfile.Max(maxAccel))
	for i := range k.limits {
		k.limits[i] = [2]float64{1.0, -1.0}
	}
	for i, r := range k.rails {
		k.axesMin[i], k.axesMax[i] = r.Range()
	}
	return k
}

func init() { toolhead.RegisterKinematics("corexy", load) }

func (k *CoreXYKinematics) steppers() []*stepper.MCUStepper {
	var out []*stepper.MCUStepper
	for _, r := range k.rails {
		out = append(out, r.Steppers()...)
	}
	return out
}

// Steppers lists the steppers of the rails.
func (k *CoreXYKinematics) Steppers() []any {
	var out []any
	for _, s := range k.steppers() {
		out = append(out, s)
	}
	return out
}

// Rails returns the rails of X, Y and Z.
func (k *CoreXYKinematics) Rails() []*stepper.Rail { return k.rails }

// CalcPosition returns the XYZ position of the stepper positions.
func (k *CoreXYKinematics) CalcPosition(stepperPositions map[string]float64) []float64 {
	pos := make([]float64, len(k.rails))
	for i, r := range k.rails {
		pos[i] = stepperPositions[r.Name()]
	}
	return []float64{0.5 * (pos[0] + pos[1]), 0.5 * (pos[0] - pos[1]), pos[2]}
}

// SetPosition sets the position of the rails, and the ranges of the homed axes.
func (k *CoreXYKinematics) SetPosition(newpos []float64, homingAxes string) {
	for i, r := range k.rails {
		r.SetPosition(newpos)
		if strings.ContainsRune(homingAxes, rune("xyz"[i])) {
			lo, hi := r.Range()
			k.limits[i] = [2]float64{lo, hi}
		}
	}
}

// ClearHomingState marks axes as not homed.
func (k *CoreXYKinematics) ClearHomingState(clearAxes string) {
	for axis, axisName := range "xyz" {
		if strings.ContainsRune(clearAxes, axisName) {
			k.limits[axis] = [2]float64{1.0, -1.0}
		}
	}
}

// Home homes each axis of the homing state, in order.
func (k *CoreXYKinematics) Home(hs stepper.HomingState) {
	// Each axis is homed independently and in order
	for _, axis := range hs.Axes() {
		rail := k.rails[axis]
		// Determine movement
		positionMin, positionMax := rail.Range()
		hi := rail.HomingInfo()
		homepos := []float64{math.NaN(), math.NaN(), math.NaN(), math.NaN()}
		homepos[axis] = hi.PositionEndstop
		forcepos := append([]float64(nil), homepos...)
		if hi.PositiveDir {
			forcepos[axis] -= 1.5 * (hi.PositionEndstop - positionMin)
		} else {
			forcepos[axis] += 1.5 * (positionMax - hi.PositionEndstop)
		}
		// Perform homing
		hs.HomeRails([]*stepper.Rail{rail}, forcepos, homepos)
	}
}

func (k *CoreXYKinematics) checkEndstops(move *toolhead.Move) {
	endPos := move.EndPos
	for i := 0; i < 3; i++ {
		if move.AxesD[i] != 0 && (endPos[i] < k.limits[i][0] || endPos[i] > k.limits[i][1]) {
			if k.limits[i][0] > k.limits[i][1] {
				panic(move.MoveError("Must home axis first"))
			}
			panic(move.MoveError(""))
		}
	}
}

// CheckMove checks the move against the axis ranges, and limits the speed of a Z move.
func (k *CoreXYKinematics) CheckMove(move *toolhead.Move) {
	limits := k.limits
	xpos, ypos := move.EndPos[0], move.EndPos[1]
	if xpos < limits[0][0] || xpos > limits[0][1] || ypos < limits[1][0] || ypos > limits[1][1] {
		k.checkEndstops(move)
	}
	if move.AxesD[2] == 0 {
		// Normal XY move - use defaults
		return
	}
	// Move with Z - update velocity and accel for slower Z axis
	k.checkEndstops(move)
	zRatio := move.MoveD / math.Abs(move.AxesD[2])
	move.LimitSpeed(k.maxZVelocity*zRatio, k.maxZAccel*zRatio)
}

// Status returns the homed axes and the axis ranges.
func (k *CoreXYKinematics) Status(eventtime float64) map[string]any {
	var axes strings.Builder
	for i, a := range "xyz" {
		if k.limits[i][0] <= k.limits[i][1] {
			axes.WriteRune(a)
		}
	}
	return map[string]any{"homed_axes": axes.String(), "axis_minimum": k.axesMin, "axis_maximum": k.axesMax}
}
