// Package gcode_arcs adds the arc commands G2 and G3. The arcs become G1 moves. The XY, XZ and
// YZ planes are supported, with the other axis as the helical axis.
//
// Ported to Go from klippy/extras/gcode_arcs.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2019  Aleksej Vasiljkovic <achmed21@gmail.com>
//	planArc() originates from https://github.com/MarlinFirmware/Marlin
//	Copyright (C) 2011  Camiel Gubbels / Erik van der Zalm
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package gcode_arcs

import (
	"math"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/gcode_move"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mathutil"
	"printdeck/internal/klipper/pyfmt"
)

// Enum
const (
	arcPlaneXY = 0
	arcPlaneXZ = 1
	arcPlaneYZ = 2
)

// Enum
const (
	xAxis = 0
	yAxis = 1
	zAxis = 2
)

// gcodeMove is the gcode_move object.
type gcodeMove interface {
	Status(eventtime float64) map[string]any
	CmdG1(gcmd *gcode.Command)
}

// ArcSupport is the gcode_arcs object.
type ArcSupport struct {
	mmPerArcSegment float64
	gcodeMove       gcodeMove
	gcode           *gcode.Dispatch
	plane           int
}

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	a := &ArcSupport{}
	a.mmPerArcSegment = config.GetFloat("resolution", configfile.Default(1.), configfile.Above(0.0))
	printer.LoadObject(config, "gcode_move")
	a.gcodeMove = gcode_move.Of(printer)
	a.gcode = gcode.Of(printer)
	a.gcode.RegisterCommand("G2", a.cmdG2, false, "")
	a.gcode.RegisterCommand("G3", a.cmdG3, false, "")
	a.gcode.RegisterCommand("G17", a.cmdG17, false, "")
	a.gcode.RegisterCommand("G18", a.cmdG18, false, "")
	a.gcode.RegisterCommand("G19", a.cmdG19, false, "")
	// backwards compatibility, prior implementation only supported XY
	a.plane = arcPlaneXY
	return a
}

func init() { host.RegisterModule("gcode_arcs", load, nil) }

func (a *ArcSupport) cmdG2(gcmd *gcode.Command)  { a.cmdInner(gcmd, true) }
func (a *ArcSupport) cmdG3(gcmd *gcode.Command)  { a.cmdInner(gcmd, false) }
func (a *ArcSupport) cmdG17(gcmd *gcode.Command) { a.plane = arcPlaneXY }
func (a *ArcSupport) cmdG18(gcmd *gcode.Command) { a.plane = arcPlaneXZ }
func (a *ArcSupport) cmdG19(gcmd *gcode.Command) { a.plane = arcPlaneYZ }

func (a *ArcSupport) cmdInner(gcmd *gcode.Command, clockwise bool) {
	gcodestatus := a.gcodeMove.Status(0)
	if !gcodestatus["absolute_coordinates"].(bool) {
		panic(gcmd.Error("G2/G3 does not support relative move mode"))
	}
	currentPos := gcodestatus["gcode_position"].(gcode.Coord)
	absolutExtrude := gcodestatus["absolute_extrude"].(bool)
	// Parse parameters
	asTarget := []float64{gcmd.GetFloat("X", configfile.Default(currentPos[0])),
		gcmd.GetFloat("Y", configfile.Default(currentPos[1])),
		gcmd.GetFloat("Z", configfile.Default(currentPos[2]))}
	if _, ok := gcmd.GetFloatOptional("R"); ok {
		panic(gcmd.Error("G2/G3 does not support R moves"))
	}
	// determine the plane coordinates and the helical axis
	i := gcmd.GetFloat("I", configfile.Default(0.))
	j := gcmd.GetFloat("J", configfile.Default(0.))
	asPlanar := [2]float64{i, j}
	axes := [3]int{xAxis, yAxis, zAxis}
	if a.plane == arcPlaneXZ {
		k := gcmd.GetFloat("K", configfile.Default(0.))
		asPlanar = [2]float64{i, k}
		axes = [3]int{xAxis, zAxis, yAxis}
	} else if a.plane == arcPlaneYZ {
		k := gcmd.GetFloat("K", configfile.Default(0.))
		asPlanar = [2]float64{j, k}
		axes = [3]int{yAxis, zAxis, xAxis}
	}
	if !(asPlanar[0] != 0 || asPlanar[1] != 0) {
		panic(gcmd.Error("G2/G3 requires IJ, IK or JK parameters"))
	}
	// Build linear coordinates to move
	a.planArc(currentPos, asTarget, asPlanar, clockwise, gcmd, absolutExtrude, axes[0], axes[1], axes[2])
}

// planArc originates from marlin plan_arc() (https://github.com/MarlinFirmware/Marlin).
//
// The arc is approximated by generating many small linear segments. The length of each segment
// is configured in MM_PER_ARC_SEGMENT. Arcs smaller then this value, will be a Line only.
//
// alpha and beta axes are the current plane, helical axis is linear travel
func (a *ArcSupport) planArc(currentPos gcode.Coord, targetPos []float64, offset [2]float64, clockwise bool,
	gcmd *gcode.Command, absolutExtrude bool, alphaAxis, betaAxis, helicalAxis int) {
	// todo: sometimes produces full circles

	// Radius vector from center to current location
	rP := -offset[0]
	rQ := -offset[1]
	// Determine angular travel
	centerP := currentPos[alphaAxis] - rP
	centerQ := currentPos[betaAxis] - rQ
	rtAlpha := targetPos[alphaAxis] - centerP
	rtBeta := targetPos[betaAxis] - centerQ
	angularTravel := math.Atan2(rP*rtBeta-rQ*rtAlpha, rP*rtAlpha+rQ*rtBeta)
	if angularTravel < 0. {
		angularTravel += 2. * math.Pi
	}
	if clockwise {
		angularTravel -= 2. * math.Pi
	}
	if angularTravel == 0. && currentPos[alphaAxis] == targetPos[alphaAxis] &&
		currentPos[betaAxis] == targetPos[betaAxis] {
		// Make a circle if the angular rotation is 0 and the
		// target is current position
		angularTravel = 2. * math.Pi
	}
	// Determine number of segments
	linearTravel := targetPos[helicalAxis] - currentPos[helicalAxis]
	radius := mathutil.Hypot(rP, rQ)
	flatMM := radius * angularTravel
	var mmOfTravel float64
	if linearTravel != 0 {
		mmOfTravel = mathutil.Hypot(flatMM, linearTravel)
	} else {
		mmOfTravel = math.Abs(flatMM)
	}
	segments := math.Max(1., math.Floor(mmOfTravel/a.mmPerArcSegment))
	// Generate coordinates
	thetaPerSegment := angularTravel / segments
	linearPerSegment := linearTravel / segments
	asE, hasE := gcmd.GetFloatOptional("E")
	asF, hasF := gcmd.GetFloatOptional("F")
	ePerMove, eBase := 0., 0.
	if hasE {
		if absolutExtrude {
			eBase = currentPos[3]
		}
		ePerMove = (asE - eBase) / segments
	}
	for i := 1; i <= int(segments); i++ {
		distHelical := float64(i) * linearPerSegment
		cTheta := float64(i) * thetaPerSegment
		cosTi := math.Cos(cTheta)
		sinTi := math.Sin(cTheta)
		rP = -offset[0]*cosTi + offset[1]*sinTi
		rQ = -offset[0]*sinTi - offset[1]*cosTi
		c := make([]float64, 3)
		c[alphaAxis] = centerP + rP
		c[betaAxis] = centerQ + rQ
		c[helicalAxis] = currentPos[helicalAxis] + distHelical
		if float64(i) == segments {
			c = targetPos
		}
		// Convert coords into G1 commands
		g1Params := map[string]string{"X": pyfmt.Float(c[0]), "Y": pyfmt.Float(c[1]), "Z": pyfmt.Float(c[2])}
		if ePerMove != 0 {
			g1Params["E"] = pyfmt.Float(eBase + ePerMove)
			if absolutExtrude {
				eBase += ePerMove
			}
		}
		if hasF {
			g1Params["F"] = pyfmt.Float(asF)
		}
		g1Gcmd := a.gcode.CreateCommand("G1", "G1", g1Params)
		a.gcodeMove.CmdG1(g1Gcmd)
	}
}
