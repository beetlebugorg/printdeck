// Package delta is the kinematics of a linear delta printer: three towers with arms to the head.
//
// Ported to Go from klippy/kinematics/delta.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2016-2021  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package delta

import (
	"fmt"
	"log"
	"math"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/mathutil"
	"printdeck/internal/klipper/motion"
	"printdeck/internal/klipper/stepper"
	"printdeck/internal/klipper/toolhead"
)

// Slow moves once the ratio of tower to XY movement exceeds SlowRatio
const SlowRatio = 3.

func radians(a float64) float64 { return a * (math.Pi / 180.) }

// DeltaKinematics is the delta kinematics.
type DeltaKinematics struct {
	rails                   []*stepper.Rail
	maxVelocity, maxAccel   float64
	maxZVelocity, maxZAccel float64
	radius                  float64
	armLengths              [3]float64
	arm2                    [3]float64
	absEndstops             [3]float64
	angles                  [3]float64
	towers                  [3][2]float64
	needHome                bool
	limitXY2                float64
	homePosition            [3]float64
	maxZ, minZ, limitZ      float64
	minArmLength, minArm2   float64
	slowXY2, verySlowXY2    float64
	maxXY2                  float64
	axesMin, axesMax        gcode.Coord
}

type homingAxesSetter interface{ SetAxes(axes []int) }

func load(th *toolhead.ToolHead, config *configfile.Wrapper) toolhead.Kinematics {
	k := &DeltaKinematics{}
	// Setup tower rails
	var stepperConfigs []*configfile.Wrapper
	for _, a := range "abc" {
		stepperConfigs = append(stepperConfigs, config.GetSection("stepper_"+string(a)))
	}
	railA := stepper.LookupMultiRail(stepperConfigs[0], false, nil, false)
	aEndstop := railA.HomingInfo().PositionEndstop
	railB := stepper.LookupMultiRail(stepperConfigs[1], false, &aEndstop, false)
	railC := stepper.LookupMultiRail(stepperConfigs[2], false, &aEndstop, false)
	k.rails = []*stepper.Rail{railA, railB, railC}
	// Setup max velocity
	k.maxVelocity, k.maxAccel = th.MaxVelocity()
	k.maxZVelocity = config.GetFloat("max_z_velocity", configfile.Default(k.maxVelocity),
		configfile.Above(0.), configfile.Max(k.maxVelocity))
	k.maxZAccel = config.GetFloat("max_z_accel", configfile.Default(k.maxAccel), configfile.Above(0.),
		configfile.Max(k.maxAccel))
	// Read radius and arm lengths
	radius := config.GetFloat("delta_radius", configfile.Above(0.))
	k.radius = radius
	printRadius := config.GetFloat("print_radius", configfile.Default(radius), configfile.Above(0.))
	armLengthA := stepperConfigs[0].GetFloat("arm_length", configfile.Above(radius))
	for i, sconfig := range stepperConfigs {
		k.armLengths[i] = sconfig.GetFloat("arm_length", configfile.Default(armLengthA), configfile.Above(radius))
	}
	for i, arm := range k.armLengths {
		k.arm2[i] = arm * arm
	}
	for i, r := range k.rails {
		k.absEndstops[i] = r.HomingInfo().PositionEndstop + mathutil.Sqrt(k.arm2[i]-radius*radius)
	}
	// Determine tower locations in cartesian space
	for i, angle := range []float64{210., 330., 90.} {
		k.angles[i] = stepperConfigs[i].GetFloat("angle", configfile.Default(angle))
	}
	for i, angle := range k.angles {
		k.towers[i] = [2]float64{math.Cos(radians(angle)) * radius, math.Sin(radians(angle)) * radius}
	}
	for i, r := range k.rails {
		a, t := k.arm2[i], k.towers[i]
		r.SetupItersolve(func() *motion.StepperKinematics { return motion.NewDeltaStepper(a, t[0], t[1]) })
	}
	for _, s := range k.steppers() {
		s.SetTrapQ(th.TrapQ())
	}
	// Setup boundary checks
	k.needHome = true
	k.limitXY2 = -1.
	k.homePosition = k.actuatorToCartesian(k.absEndstops)
	k.maxZ = math.Inf(1)
	for _, r := range k.rails {
		k.maxZ = math.Min(k.maxZ, r.HomingInfo().PositionEndstop)
	}
	k.minZ = config.GetFloat("minimum_z_position", configfile.Default(0.), configfile.Max(k.maxZ))
	k.limitZ = math.Inf(1)
	for i, ep := range k.absEndstops {
		k.limitZ = math.Min(k.limitZ, ep-k.armLengths[i])
	}
	minArmLength := math.Min(k.armLengths[0], math.Min(k.armLengths[1], k.armLengths[2]))
	k.minArmLength = minArmLength
	k.minArm2 = minArmLength * minArmLength
	log.Printf("Delta max build height %.2fmm (radius tapered above %.2fmm)", k.maxZ, k.limitZ)
	// Find the point where an XY move could result in excessive
	// tower movement
	halfMinStepDist := math.Inf(1)
	for _, r := range k.rails {
		halfMinStepDist = math.Min(halfMinStepDist, r.Steppers()[0].StepDist())
	}
	halfMinStepDist *= .5
	ratioToXY := func(ratio float64) float64 {
		return ratio*mathutil.Sqrt(minArmLength*minArmLength/(ratio*ratio+1.)-
			halfMinStepDist*halfMinStepDist) + halfMinStepDist - radius
	}
	sq := func(v float64) float64 { return v * v }
	k.slowXY2 = sq(ratioToXY(SlowRatio))
	k.verySlowXY2 = sq(ratioToXY(2. * SlowRatio))
	k.maxXY2 = sq(math.Min(printRadius, math.Min(minArmLength-radius, ratioToXY(4.*SlowRatio))))
	maxXY := mathutil.Sqrt(k.maxXY2)
	log.Printf("Delta max build radius %.2fmm (moves slowed past %.2fmm and %.2fmm)",
		maxXY, mathutil.Sqrt(k.slowXY2), mathutil.Sqrt(k.verySlowXY2))
	k.axesMin = gcode.Coord{-maxXY, -maxXY, k.minZ}
	k.axesMax = gcode.Coord{maxXY, maxXY, k.maxZ}
	k.SetPosition([]float64{0., 0., 0.}, "")
	return k
}

func init() { toolhead.RegisterKinematics("delta", load) }

func (k *DeltaKinematics) steppers() []*stepper.MCUStepper {
	var out []*stepper.MCUStepper
	for _, r := range k.rails {
		out = append(out, r.Steppers()...)
	}
	return out
}

// Steppers lists the steppers of the towers.
func (k *DeltaKinematics) Steppers() []any {
	var out []any
	for _, s := range k.steppers() {
		out = append(out, s)
	}
	return out
}

// Rails returns the rails of the towers.
func (k *DeltaKinematics) Rails() []*stepper.Rail { return k.rails }

func (k *DeltaKinematics) actuatorToCartesian(spos [3]float64) [3]float64 {
	var sphereCoords [3][3]float64
	for i, t := range k.towers {
		sphereCoords[i] = [3]float64{t[0], t[1], spos[i]}
	}
	return mathutil.Trilateration(sphereCoords, k.arm2)
}

// CalcPosition returns the XYZ position of the stepper positions.
func (k *DeltaKinematics) CalcPosition(stepperPositions map[string]float64) []float64 {
	var spos [3]float64
	for i, r := range k.rails {
		spos[i] = stepperPositions[r.Name()]
	}
	p := k.actuatorToCartesian(spos)
	return p[:]
}

// SetPosition sets the position of the rails.
func (k *DeltaKinematics) SetPosition(newpos []float64, homingAxes string) {
	for _, r := range k.rails {
		r.SetPosition(newpos)
	}
	k.limitXY2 = -1.
	if homingAxes == "xyz" {
		k.needHome = false
	}
}

// ClearHomingState marks all axes as not homed: one axis alone is not implemented.
func (k *DeltaKinematics) ClearHomingState(clearAxes string) {
	if clearAxes != "" {
		k.limitXY2 = -1
		k.needHome = true
	}
}

// Home homes all axes together.
func (k *DeltaKinematics) Home(hs stepper.HomingState) {
	// All axes are homed simultaneously
	hs.(homingAxesSetter).SetAxes([]int{0, 1, 2})
	forcepos := []float64{k.homePosition[0], k.homePosition[1], k.homePosition[2]}
	maxArm2 := math.Max(k.arm2[0], math.Max(k.arm2[1], k.arm2[2]))
	forcepos[2] = -1.5 * mathutil.Sqrt(maxArm2-k.maxXY2)
	hs.HomeRails(k.rails, forcepos, []float64{k.homePosition[0], k.homePosition[1], k.homePosition[2]})
}

// CheckMove checks the move against the build volume, and limits the speed of the moves at its
// edge and of the Z moves.
func (k *DeltaKinematics) CheckMove(move *toolhead.Move) {
	endPos := move.EndPos
	endXY2 := endPos[0]*endPos[0] + endPos[1]*endPos[1]
	if endXY2 <= k.limitXY2 && move.AxesD[2] == 0 {
		// Normal XY move
		return
	}
	if k.needHome {
		panic(move.MoveError("Must home first"))
	}
	endZ := endPos[2]
	limitXY2 := k.maxXY2
	if endZ > k.limitZ {
		aboveZLimit := endZ - k.limitZ
		d := k.minArmLength - aboveZLimit
		allowedRadius := k.radius - mathutil.Sqrt(k.minArm2-d*d)
		limitXY2 = math.Min(limitXY2, allowedRadius*allowedRadius)
	}
	if endXY2 > limitXY2 || endZ > k.maxZ || endZ < k.minZ {
		// Move out of range - verify not a homing move
		if endPos[0] != k.homePosition[0] || endPos[1] != k.homePosition[1] ||
			endZ < k.minZ || endZ > k.homePosition[2] {
			panic(move.MoveError(""))
		}
		limitXY2 = -1.
	}
	if move.AxesD[2] != 0 {
		zRatio := move.MoveD / math.Abs(move.AxesD[2])
		move.LimitSpeed(k.maxZVelocity*zRatio, k.maxZAccel*zRatio)
		limitXY2 = -1.
	}
	// Limit the speed/accel of this move if is is at the extreme
	// end of the build envelope
	extremeXY2 := math.Max(endXY2, move.StartPos[0]*move.StartPos[0]+move.StartPos[1]*move.StartPos[1])
	if extremeXY2 > k.slowXY2 {
		r := 0.5
		if extremeXY2 > k.verySlowXY2 {
			r = 0.25
		}
		move.LimitSpeed(k.maxVelocity*r, k.maxAccel*r)
		limitXY2 = -1.
	}
	k.limitXY2 = math.Min(limitXY2, k.slowXY2)
}

// Status returns the homed axes, the axis ranges and the start of the cone.
func (k *DeltaKinematics) Status(eventtime float64) map[string]any {
	homed := "xyz"
	if k.needHome {
		homed = ""
	}
	return map[string]any{"homed_axes": homed, "axis_minimum": k.axesMin, "axis_maximum": k.axesMax,
		"cone_start_z": k.limitZ}
}

// Calibration returns the parameters of the towers for DELTA_CALIBRATE.
func (k *DeltaKinematics) Calibration() *DeltaCalibration {
	var endstops, stepdists [3]float64
	for i, r := range k.rails {
		endstops[i] = r.HomingInfo().PositionEndstop
		stepdists[i] = r.Steppers()[0].StepDist()
	}
	return NewDeltaCalibration(k.radius, k.angles, k.armLengths, endstops, stepdists)
}

// DeltaCalibration is the delta parameters for the DELTA_CALIBRATE tool.
type DeltaCalibration struct {
	Radius      float64
	Angles      [3]float64
	Arms        [3]float64
	Endstops    [3]float64
	Stepdists   [3]float64
	towers      [3][2]float64
	absEndstops [3]float64
}

func NewDeltaCalibration(radius float64, angles, arms, endstops, stepdists [3]float64) *DeltaCalibration {
	c := &DeltaCalibration{Radius: radius, Angles: angles, Arms: arms, Endstops: endstops, Stepdists: stepdists}
	// Calculate the XY cartesian coordinates of the delta towers
	for i, a := range angles {
		ra := radians(a)
		c.towers[i] = [2]float64{math.Cos(ra) * radius, math.Sin(ra) * radius}
	}
	// Calculate the absolute Z height of each tower endstop
	radius2 := radius * radius
	for i := range endstops {
		c.absEndstops[i] = endstops[i] + mathutil.Sqrt(arms[i]*arms[i]-radius2)
	}
	return c
}

// CoordinateDescentParams returns the names of the parameters to adjust, and all parameters.
func (c *DeltaCalibration) CoordinateDescentParams(isExtended bool) ([]string, map[string]float64) {
	// Determine adjustment parameters (for use with coordinate_descent)
	adjParams := []string{"radius", "angle_a", "angle_b", "endstop_a", "endstop_b", "endstop_c"}
	if isExtended {
		adjParams = append(adjParams, "arm_a", "arm_b", "arm_c")
	}
	params := map[string]float64{"radius": c.Radius}
	for i, axis := range []string{"a", "b", "c"} {
		params["angle_"+axis] = c.Angles[i]
		params["arm_"+axis] = c.Arms[i]
		params["endstop_"+axis] = c.Endstops[i]
		params["stepdist_"+axis] = c.Stepdists[i]
	}
	return adjParams, params
}

// NewCalibration makes a calibration from coordinate_descent params.
func (c *DeltaCalibration) NewCalibration(params map[string]float64) *DeltaCalibration {
	var angles, arms, endstops, stepdists [3]float64
	for i, a := range []string{"a", "b", "c"} {
		angles[i] = params["angle_"+a]
		arms[i] = params["arm_"+a]
		endstops[i] = params["endstop_"+a]
		stepdists[i] = params["stepdist_"+a]
	}
	return NewDeltaCalibration(params["radius"], angles, arms, endstops, stepdists)
}

// PositionFromStable returns the cartesian coordinates of a stable position.
func (c *DeltaCalibration) PositionFromStable(stablePosition [3]float64) [3]float64 {
	var sphereCoords [3][3]float64
	var arm2 [3]float64
	for i := range c.towers {
		sphereCoords[i] = [3]float64{c.towers[i][0], c.towers[i][1],
			c.absEndstops[i] - stablePosition[i]*c.Stepdists[i]}
		arm2[i] = c.Arms[i] * c.Arms[i]
	}
	return mathutil.Trilateration(sphereCoords, arm2)
}

// CalcStablePosition returns the stable position of a cartesian coordinate.
func (c *DeltaCalibration) CalcStablePosition(coord [3]float64) [3]float64 {
	var out [3]float64
	for i, t := range c.towers {
		a := c.Arms[i]
		dx, dy := t[0]-coord[0], t[1]-coord[1]
		steppos := mathutil.Sqrt(a*a-dx*dx-dy*dy) + coord[2]
		out[i] = (c.absEndstops[i] - steppos) / c.Stepdists[i]
	}
	return out
}

type configSetter interface {
	Set(section, option, value string)
}

// SaveState stages the parameters for SAVE_CONFIG, and returns the text that reports them.
func (c *DeltaCalibration) SaveState(pconfig configSetter) string {
	pconfig.Set("printer", "delta_radius", fmt.Sprintf("%.6f", c.Radius))
	for i, axis := range []string{"a", "b", "c"} {
		pconfig.Set("stepper_"+axis, "angle", fmt.Sprintf("%.6f", c.Angles[i]))
		pconfig.Set("stepper_"+axis, "arm_length", fmt.Sprintf("%.6f", c.Arms[i]))
		pconfig.Set("stepper_"+axis, "position_endstop", fmt.Sprintf("%.6f", c.Endstops[i]))
	}
	return fmt.Sprintf("stepper_a: position_endstop: %.6f angle: %.6f arm_length: %.6f\n"+
		"stepper_b: position_endstop: %.6f angle: %.6f arm_length: %.6f\n"+
		"stepper_c: position_endstop: %.6f angle: %.6f arm_length: %.6f\n"+
		"delta_radius: %.6f",
		c.Endstops[0], c.Angles[0], c.Arms[0],
		c.Endstops[1], c.Angles[1], c.Arms[1],
		c.Endstops[2], c.Angles[2], c.Arms[2],
		c.Radius)
}
