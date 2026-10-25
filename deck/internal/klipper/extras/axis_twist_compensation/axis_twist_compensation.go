// Package axis_twist_compensation corrects the probe results for a twist of the X or Y axis.
//
// Ported to Go from klippy/extras/axis_twist_compensation.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2022  Jeremy Tan <jeremytkw98@gmail.com>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package axis_twist_compensation

import (
	"fmt"
	"math"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/manual_probe"
	"printdeck/internal/klipper/extras/probe"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mathutil"
	"printdeck/internal/klipper/pyfmt"
	"printdeck/internal/klipper/toolhead"
)

const (
	DefaultSampleCount     = 3
	DefaultSpeed           = 50.
	DefaultHorizontalMoveZ = 5.
)

var none = math.NaN()

// optFloat reads an optional number. NaN is None.
func optFloat(config *configfile.Wrapper, option string) float64 {
	if v, ok := config.GetFloatOptional(option, configfile.Default(nil)); ok {
		return v
	}
	return none
}

// AxisTwistCompensation is the [axis_twist_compensation] section. All its positions are bed
// coordinates.
type AxisTwistCompensation struct {
	printer            *host.Printer
	gcode              *gcode.Dispatch
	horizontalMoveZ    float64
	speed              float64
	calibrateStartX    float64
	calibrateEndX      float64
	calibrateY         float64
	zCompensations     []float64
	compensationStartX float64
	compensationEndX   float64
	calibrateStartY    float64
	calibrateEndY      float64
	calibrateX         float64
	compensationStartY float64
	compensationEndY   float64
	zyCompensations    []float64
	calibrater         *Calibrater
}

func load(config *configfile.Wrapper) any {
	// get printer
	a := &AxisTwistCompensation{printer: host.Of(config)}
	a.gcode = gcode.Of(a.printer)

	// get values from [axis_twist_compensation] section in printer .cfg
	a.horizontalMoveZ = config.GetFloat("horizontal_move_z", configfile.Default(DefaultHorizontalMoveZ))
	a.speed = config.GetFloat("speed", configfile.Default(DefaultSpeed))
	a.calibrateStartX = optFloat(config, "calibrate_start_x")
	a.calibrateEndX = optFloat(config, "calibrate_end_x")
	a.calibrateY = optFloat(config, "calibrate_y")
	a.zCompensations = config.GetFloatList("z_compensations", ",", 0, configfile.Default([]float64{}))
	a.compensationStartX = optFloat(config, "compensation_start_x")
	a.compensationEndX = optFloat(config, "compensation_end_x")

	a.calibrateStartY = optFloat(config, "calibrate_start_y")
	a.calibrateEndY = optFloat(config, "calibrate_end_y")
	a.calibrateX = optFloat(config, "calibrate_x")
	a.compensationStartY = optFloat(config, "compensation_start_y")
	a.compensationEndY = optFloat(config, "compensation_end_y")
	a.zyCompensations = config.GetFloatList("zy_compensations", ",", 0, configfile.Default([]float64{}))

	// setup calibrater
	a.calibrater = newCalibrater(a, config)
	// register events
	a.printer.RegisterEventHandler("probe:update_results", func(args ...any) {
		a.updateZCompensationValue(args[0].([]*manual_probe.ProbeResult))
	})
	return a
}

func init() { host.RegisterModule("axis_twist_compensation", load, nil) }

func (a *AxisTwistCompensation) updateZCompensationValue(poslist []*manual_probe.ProbeResult) {
	for i, pos := range poslist {
		zo := 0.
		if len(a.zCompensations) > 0 {
			zo += a.interpolatedZCompensation(pos.BedX_, a.zCompensations,
				a.compensationStartX, a.compensationEndX)
		}
		if len(a.zyCompensations) > 0 {
			zo += a.interpolatedZCompensation(pos.BedY_, a.zyCompensations,
				a.compensationStartY, a.compensationEndY)
		}
		poslist[i] = &manual_probe.ProbeResult{BedX_: pos.BedX_, BedY_: pos.BedY_, BedZ_: pos.BedZ_ + zo,
			TestX: pos.TestX, TestY: pos.TestY, TestZ: pos.TestZ}
	}
}

func (a *AxisTwistCompensation) interpolatedZCompensation(coord float64, zCompensations []float64,
	compStart, compEnd float64) float64 {
	sampleCount := len(zCompensations)
	spacing := (compEnd - compStart) / float64(sampleCount-1)
	interpolateT := (coord - compStart) / spacing
	interpolateI := int(math.Floor(interpolateT))
	interpolateI = max(0, min(sampleCount-2, interpolateI))
	interpolateT -= float64(interpolateI)
	v0, v1 := zCompensations[interpolateI], zCompensations[interpolateI+1]
	return (1.-interpolateT)*v0 + interpolateT*v1
}

// clearCompensations clears the compensations of an axis ("X", "Y", or "" for all).
func (a *AxisTwistCompensation) clearCompensations(axis string) {
	switch axis {
	case "":
		a.zCompensations = nil
		a.zyCompensations = nil
	case "X":
		a.zCompensations = nil
	case "Y":
		a.zyCompensations = nil
	}
}

// Calibrater runs AXIS_TWIST_COMPENSATION_CALIBRATE. All its positions are bed coordinates.
type Calibrater struct {
	compensation      *AxisTwistCompensation
	printer           *host.Printer
	gcode             *gcode.Dispatch
	probe             probe.Probe
	liftSpeed         float64
	speed             float64
	horizontalMoveZ   float64
	xStartPoint       [2]float64
	xEndPoint         [2]float64
	yStartPoint       [2]float64
	yEndPoint         [2]float64
	results           []float64
	currentPointIndex int
	gcmd              *gcode.Command
	configname        string
	currentAxis       string
	currentMeasuredZ  float64
}

func newCalibrater(compensation *AxisTwistCompensation, config *configfile.Wrapper) *Calibrater {
	// setup self attributes
	c := &Calibrater{compensation: compensation, printer: compensation.printer}
	c.gcode = gcode.Of(c.printer)
	c.printer.RegisterEventHandler("klippy:connect", func(...any) { c.handleConnect() })
	c.speed = compensation.speed
	c.horizontalMoveZ = compensation.horizontalMoveZ
	c.xStartPoint = [2]float64{compensation.calibrateStartX, compensation.calibrateY}
	c.xEndPoint = [2]float64{compensation.calibrateEndX, compensation.calibrateY}
	c.yStartPoint = [2]float64{compensation.calibrateX, compensation.calibrateStartY}
	c.yEndPoint = [2]float64{compensation.calibrateX, compensation.calibrateEndY}
	c.configname = config.Name()

	// register gcode handlers
	c.gcode.RegisterCommand("AXIS_TWIST_COMPENSATION_CALIBRATE", c.cmdAxisTwistCompensationCalibrate,
		false, "\n    Performs the x twist calibration wizard\n"+
			"    Measure z probe offset at n points along the x axis,\n"+
			"    and calculate x twist compensation\n    ")
	return c
}

func (c *Calibrater) handleConnect() {
	p, _ := c.printer.TryLookupObject("probe").(probe.Probe)
	if p == nil {
		configfile.Raise("AXIS_TWIST_COMPENSATION requires [probe] to be defined")
	}
	c.probe = p
	c.liftSpeed = c.probe.ProbeParams(nil).LiftSpeed
}

// truthy is Python's bool() of an optional number.
func truthy(v float64) bool { return !math.IsNaN(v) && v != 0 }

func (c *Calibrater) cmdAxisTwistCompensationCalibrate(gcmd *gcode.Command) {
	c.gcmd = gcmd
	offsets := c.probe.Offsets(gcmd)
	probeXOffset, probeYOffset := offsets[0], offsets[1]
	sampleCount := gcmd.GetInt("SAMPLE_COUNT", gcode.Default(DefaultSampleCount))
	axis := gcmd.GetString("AXIS", gcode.Default("X"))

	// check for valid sample_count
	if sampleCount < 2 {
		panic(gcmd.Error("SAMPLE_COUNT to probe must be at least 2"))
	}

	// calculate the points we want to measure
	var bedPoints [][2]float64
	var intervalDist float64
	switch axis {
	case "X":
		c.compensation.clearCompensations("X")
		if !(truthy(c.xStartPoint[0]) && truthy(c.xEndPoint[0]) && truthy(c.xStartPoint[1])) {
			panic(gcmd.Error("AXIS_TWIST_COMPENSATION for X axis requires\n" +
				"                    calibrate_start_x, calibrate_end_x and calibrate_y\n" +
				"                    to be defined\n                    "))
		}
		startPoint, endPoint := c.xStartPoint, c.xEndPoint
		xAxisRange := endPoint[0] - startPoint[0]
		intervalDist = xAxisRange / float64(sampleCount-1)
		for i := range sampleCount {
			x := startPoint[0] + float64(i)*intervalDist
			y := startPoint[1]
			bedPoints = append(bedPoints, [2]float64{x, y})
		}
	case "Y":
		c.compensation.clearCompensations("Y")
		if !(truthy(c.yStartPoint[0]) && truthy(c.yEndPoint[0]) && truthy(c.yStartPoint[1])) {
			panic(gcmd.Error("AXIS_TWIST_COMPENSATION for Y axis requires\n" +
				"                    calibrate_start_y, calibrate_end_y and calibrate_x\n" +
				"                    to be defined\n                    "))
		}
		startPoint, endPoint := c.yStartPoint, c.yEndPoint
		yAxisRange := endPoint[1] - startPoint[1]
		intervalDist = yAxisRange / float64(sampleCount-1)
		for i := range sampleCount {
			x := startPoint[0]
			y := startPoint[1] + float64(i)*intervalDist
			bedPoints = append(bedPoints, [2]float64{x, y})
		}
	default:
		panic(gcmd.Error("AXIS_TWIST_COMPENSATION_CALIBRATE: Invalid axis."))
	}

	testPoints := calculateTestPoints(bedPoints, probeXOffset, probeYOffset)

	// verify no other manual probe is in progress
	manual_probe.VerifyNoManualProbe(c.printer)

	// begin calibration
	c.currentPointIndex = 0
	c.results = nil
	c.currentAxis = axis
	c.calibration(testPoints, bedPoints, intervalDist)
}

// calculateTestPoints returns the points to move to, so that the probe is over the bed points.
func calculateTestPoints(bedPoints [][2]float64, probeXOffset, probeYOffset float64) [][2]float64 {
	var testPoints [][2]float64
	for _, point := range bedPoints {
		testPoints = append(testPoints, [2]float64{point[0] - probeXOffset, point[1] - probeYOffset})
	}
	return testPoints
}

// moveHelper moves to the coordinates. A NaN coordinate stays. overrideSpeed is NaN for none.
func (c *Calibrater) moveHelper(target []float64, overrideSpeed float64) {
	// pad target coordinates
	if len(target) == 2 {
		target = []float64{target[0], target[1], none}
	}
	th := c.printer.LookupObject("toolhead").(*toolhead.ToolHead)
	speed := c.liftSpeed
	if math.IsNaN(target[2]) {
		speed = c.speed
	}
	if !math.IsNaN(overrideSpeed) {
		speed = overrideSpeed
	}
	th.ManualMove(target, speed)
}

func (c *Calibrater) calibration(testPoints, bedPoints [][2]float64, interval float64) {
	// begin the calibration process
	c.gcmd.RespondInfo(fmt.Sprintf("AXIS_TWIST_COMPENSATION_CALIBRATE: Probing point %d of %d",
		c.currentPointIndex+1, len(testPoints)), true)

	// horizontal_move_z (to prevent probe trigger or hitting bed)
	c.moveHelper([]float64{none, none, c.horizontalMoveZ}, none)

	// move to point to probe
	tp := testPoints[c.currentPointIndex]
	c.moveHelper([]float64{tp[0], tp[1], none}, none)

	// probe the point
	pos := probe.RunSingleProbe(c.probe, c.gcmd)
	c.currentMeasuredZ = pos.BedZ_

	// horizontal_move_z (to prevent probe trigger or hitting bed)
	c.moveHelper([]float64{none, none, c.horizontalMoveZ}, none)

	// move the nozzle over the position that was just probed
	bp := bedPoints[c.currentPointIndex]
	c.moveHelper(bp[:], none)

	// start the manual (nozzle) probe
	manual_probe.NewManualProbeHelper(c.printer, c.gcmd,
		c.manualProbeCallbackFactory(testPoints, bedPoints, interval))
}

// manualProbeCallbackFactory returns the callback of the manual probe.
func (c *Calibrater) manualProbeCallbackFactory(testPoints, bedPoints [][2]float64,
	interval float64) func(*manual_probe.ProbeResult) {
	isEnd := c.currentPointIndex == len(testPoints)-1
	return func(mpresult *manual_probe.ProbeResult) {
		if mpresult == nil {
			// probe was cancelled
			c.gcmd.RespondInfo("AXIS_TWIST_COMPENSATION_CALIBRATE: Probe cancelled, "+
				"calibration aborted", true)
			return
		}
		zOffset := c.currentMeasuredZ - mpresult.BedZ_
		c.results = append(c.results, zOffset)
		if isEnd {
			// end of calibration
			c.finalizeCalibration()
		} else {
			// move to next point
			c.currentPointIndex++
			c.calibration(testPoints, bedPoints, interval)
		}
	}
}

func (c *Calibrater) finalizeCalibration() {
	// calculate average of results
	avg := mathutil.Sum(c.results...) / float64(len(c.results))
	// subtract average from each result so that they are independent of z_offset
	for i, x := range c.results {
		c.results[i] = avg - x
	}
	// save the config
	pconfig := c.printer.LookupObject("configfile").(*configfile.PrinterConfig)
	strs := make([]string, len(c.results))
	reprs := make([]string, len(c.results))
	for i, x := range c.results {
		strs[i] = fmt.Sprintf("%.6f", x)
		reprs[i] = pyfmt.Float(x)
	}
	valuesAsStr := strings.Join(strs, ", ")

	switch c.currentAxis {
	case "X":
		pconfig.Set(c.configname, "z_compensations", valuesAsStr)
		pconfig.Set(c.configname, "compensation_start_x", pyfmt.Float(c.xStartPoint[0]))
		pconfig.Set(c.configname, "compensation_end_x", pyfmt.Float(c.xEndPoint[0]))
		c.compensation.zCompensations = c.results
		c.compensation.compensationStartX = c.xStartPoint[0]
		c.compensation.compensationEndX = c.xEndPoint[0]
	case "Y":
		pconfig.Set(c.configname, "zy_compensations", valuesAsStr)
		pconfig.Set(c.configname, "compensation_start_y", pyfmt.Float(c.yStartPoint[1]))
		pconfig.Set(c.configname, "compensation_end_y", pyfmt.Float(c.yEndPoint[1]))
		c.compensation.zyCompensations = c.results
		c.compensation.compensationStartY = c.yStartPoint[1]
		c.compensation.compensationEndY = c.yEndPoint[1]
	}

	c.gcode.RespondInfo("AXIS_TWIST_COMPENSATION state has been saved "+
		"for the current session.  The SAVE_CONFIG command will "+
		"update the printer config file and restart the printer.", true)
	// output result
	c.gcmd.RespondInfo(fmt.Sprintf("AXIS_TWIST_COMPENSATION_CALIBRATE: Calibration complete, "+
		"offsets: [%s], mean z_offset: %f", strings.Join(reprs, ", "), avg), true)
}
