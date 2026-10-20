// Package cartesian is the kinematics of a cartesian printer: one rail for each of X, Y and Z.
//
// Ported to Go from klippy/kinematics/cartesian.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f. The
// dual carriage of idex_modes.py is not ported.
//
//	Copyright (C) 2016-2021  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package cartesian

import (
	"math"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/motion"
	"printdeck/internal/klipper/stepper"
	"printdeck/internal/klipper/toolhead"
)

// HomingState is the homing of a G28 command, as the kinematics use it. A NaN position in
// HomeRails is an axis that does not move (None in Klipper).
type HomingState interface {
	Axes() []int
	HomeRails(rails []*stepper.Rail, forcepos, homepos []float64)
}

// CartKinematics is the cartesian kinematics.
type CartKinematics struct {
	rails                   []*stepper.Rail
	axesMin, axesMax        gcode.Coord
	maxZVelocity, maxZAccel float64
	limits                  [3][2]float64
}

func load(th *toolhead.ToolHead, config *configfile.Wrapper) toolhead.Kinematics {
	k := &CartKinematics{}
	// Setup axis rails
	for _, n := range "xyz" {
		k.rails = append(k.rails, stepper.LookupMultiRail(config.GetSection("stepper_"+string(n)), true, nil, false))
	}
	for i, axis := range []byte("xyz") {
		a := axis
		k.rails[i].SetupItersolve(func() *motion.StepperKinematics { return motion.NewCartesianStepper(a) })
	}
	for i, r := range k.rails {
		k.axesMin[i], k.axesMax[i] = r.Range()
	}
	if config.HasSection("dual_carriage") {
		configfile.Raise("A dual carriage ([dual_carriage]) is not supported")
	}
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
	return k
}

func init() { toolhead.RegisterKinematics("cartesian", load) }

func (k *CartKinematics) steppers() []*stepper.MCUStepper {
	var out []*stepper.MCUStepper
	for _, r := range k.rails {
		out = append(out, r.Steppers()...)
	}
	return out
}

// Steppers lists the steppers of the rails.
func (k *CartKinematics) Steppers() []any {
	var out []any
	for _, s := range k.steppers() {
		out = append(out, s)
	}
	return out
}

// Rails returns the rails of X, Y and Z.
func (k *CartKinematics) Rails() []*stepper.Rail { return k.rails }

// CalcPosition returns the XYZ position of the stepper positions.
func (k *CartKinematics) CalcPosition(stepperPositions map[string]float64) []float64 {
	out := make([]float64, len(k.rails))
	for i, r := range k.rails {
		out[i] = stepperPositions[r.Name()]
	}
	return out
}

// UpdateLimits sets the range of an axis that is homed.
func (k *CartKinematics) UpdateLimits(i int, rng [2]float64) {
	// Only update limits if this axis was already homed, otherwise leave in un-homed state.
	if k.limits[i][0] <= k.limits[i][1] {
		k.limits[i] = rng
	}
}

// SetPosition sets the position of the rails, and the ranges of the homed axes.
func (k *CartKinematics) SetPosition(newpos []float64, homingAxes string) {
	for _, r := range k.rails {
		r.SetPosition(newpos)
	}
	for _, axisName := range homingAxes {
		axis := strings.IndexRune("xyz", axisName)
		lo, hi := k.rails[axis].Range()
		k.limits[axis] = [2]float64{lo, hi}
	}
}

// ClearHomingState marks axes as not homed.
func (k *CartKinematics) ClearHomingState(clearAxes string) {
	for axis, axisName := range "xyz" {
		if strings.ContainsRune(clearAxes, axisName) {
			k.limits[axis] = [2]float64{1.0, -1.0}
		}
	}
}

func (k *CartKinematics) homeAxis(hs HomingState, axis int, rail *stepper.Rail) {
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

// Home homes each axis of the homing state, in order.
func (k *CartKinematics) Home(hs HomingState) {
	// Each axis is homed independently and in order
	for _, axis := range hs.Axes() {
		k.homeAxis(hs, axis, k.rails[axis])
	}
}

func (k *CartKinematics) checkEndstops(move *toolhead.Move) {
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
func (k *CartKinematics) CheckMove(move *toolhead.Move) {
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
func (k *CartKinematics) Status(eventtime float64) map[string]any {
	var axes strings.Builder
	for i, a := range "xyz" {
		if k.limits[i][0] <= k.limits[i][1] {
			axes.WriteRune(a)
		}
	}
	return map[string]any{"homed_axes": axes.String(), "axis_minimum": k.axesMin, "axis_maximum": k.axesMax}
}
