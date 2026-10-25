// Package delta_calibrate is the delta calibration: DELTA_CALIBRATE and DELTA_ANALYZE.
//
// Ported to Go from klippy/extras/delta_calibrate.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
// background_coordinate_descent of klippy/mathutil.py runs the descent on a goroutine.
//
//	Copyright (C) 2017-2019  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package delta_calibrate

import (
	"fmt"
	"log"
	"math"
	"strings"
	"sync/atomic"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/manual_probe"
	"printdeck/internal/klipper/extras/probe"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/jinja"
	"printdeck/internal/klipper/kinematics/delta"
	"printdeck/internal/klipper/mathutil"
	"printdeck/internal/klipper/pyfmt"
	"printdeck/internal/klipper/stepper"
	"printdeck/internal/klipper/toolhead"
)

// A "stable position" is a 3-tuple containing the number of steps
// taken since hitting the endstop on each delta tower.  Delta
// calibration uses this coordinate system because it allows a position
// to be described independent of the software parameters.

// stablePos is a stable position, and if it is a tuple or a list in the Python code (for the
// text of the log).
type stablePos struct {
	pos   [3]float64
	tuple bool
}

func (s stablePos) repr() string {
	items := []any{s.pos[0], s.pos[1], s.pos[2]}
	if s.tuple {
		return jinja.Repr(jinja.Tuple(items))
	}
	return jinja.Repr(items)
}

// Load a stable position from a config entry
func loadConfigStable(config *configfile.Wrapper, option string) stablePos {
	l := config.GetFloatList(option, ",", 3)
	return stablePos{[3]float64{l[0], l[1], l[2]}, true}
}

// ######################################################################
// Delta calibration object
// ######################################################################

// The angles and distances of the calibration object found in
// docs/prints/calibrate_size.stl
var MeasureAngles = []float64{210., 270., 330., 30., 90., 150.}

const (
	MeasureOuterRadius = 65
	MeasureRidgeRadius = 5. - .5

	// How much to prefer a distance measurement over a height measurement
	MeasureWeight = 0.5
)

type heightPos struct {
	height float64
	pos    stablePos
}

type distance struct {
	dist       float64
	pos1, pos2 stablePos
}

func radians(a float64) float64 { return a * (math.Pi / 180.) }

// Convert distance measurements made on the calibration object to
// 3-tuples of (actual_distance, stable_position1, stable_position2)
func measurementsToDistances(mp map[string][]float64, dp *delta.DeltaCalibration) []distance {
	// Extract params
	scale := mp["SCALE"][0]
	cpw := mp["CENTER_PILLAR_WIDTHS"]
	centerWidths := []float64{cpw[0], cpw[2], cpw[1], cpw[0], cpw[2], cpw[1]}
	var centerDists, outerDists []float64
	for i, od := range mp["CENTER_DISTS"] {
		centerDists = append(centerDists, od-centerWidths[i])
	}
	for i, od := range mp["OUTER_DISTS"] {
		outerDists = append(outerDists, od-mp["OUTER_PILLAR_WIDTHS"][i])
	}
	// Convert angles in degrees to an XY multiplier
	var xyAngles [][2]float64
	for _, a := range MeasureAngles {
		r := radians(a)
		xyAngles = append(xyAngles, [2]float64{math.Cos(r), math.Sin(r)})
	}
	// Calculate stable positions for center measurements
	innerRidge := MeasureRidgeRadius * scale
	outerRidge := (MeasureOuterRadius + MeasureRidgeRadius) * scale
	csp := func(c [3]float64) stablePos { return stablePos{dp.CalcStablePosition(c), false} }
	var out []distance
	for i, a := range xyAngles {
		ip := [3]float64{a[0] * innerRidge, a[1] * innerRidge, 0.}
		op := [3]float64{a[0] * outerRidge, a[1] * outerRidge, 0.}
		out = append(out, distance{centerDists[i], csp(ip), csp(op)})
	}
	// Calculate positions of outer measurements
	outerCenter := MeasureOuterRadius * scale
	shifted := append(append([][2]float64(nil), xyAngles[2:]...), xyAngles[:2]...)
	for i, a := range xyAngles {
		sp := [2]float64{a[0] * outerCenter, a[1] * outerCenter}
		s := shifted[i]
		fp := [3]float64{s[0]*innerRidge + sp[0], s[1]*innerRidge + sp[1], 0.}
		sp2 := [3]float64{s[0]*outerRidge + sp[0], s[1]*outerRidge + sp[1], 0.}
		out = append(out, distance{outerDists[i], csp(fp), csp(sp2)})
	}
	return out
}

// ######################################################################
// Delta Calibrate class
// ######################################################################

type calibrator interface {
	Calibration() *delta.DeltaCalibration
}

// DeltaCalibrate is the [delta_calibrate] section.
type DeltaCalibrate struct {
	printer            *host.Printer
	probeHelper        *probe.PointsHelper
	lastProbePositions []heightPos
	manualHeights      []heightPos
	deltaAnalyzeEntry  map[string][]float64
	lastDistances      []distance
	gcode              *gcode.Dispatch
}

func load(config *configfile.Wrapper) any {
	d := &DeltaCalibrate{printer: host.Of(config)}
	d.printer.RegisterEventHandler("klippy:connect", func(...any) { d.handleConnect() })
	// Calculate default probing points
	radius := config.GetFloat("radius", configfile.Above(0.))
	points := [][]float64{{0., 0.}}
	scatter := []float64{.95, .90, .85, .70, .75, .80}
	for i := 0; i < 6; i++ {
		r := radians(90. + 60.*float64(i))
		dist := radius * scatter[i]
		points = append(points, []float64{math.Cos(r) * dist, math.Sin(r) * dist})
	}
	d.probeHelper = probe.NewPointsHelper(config, func(results []*manual_probe.ProbeResult) string {
		d.probeFinalize(results)
		return ""
	}, points)
	d.probeHelper.MinimumPoints(3)
	// Restore probe stable positions
	for i := 0; i < 999; i++ {
		height, ok := config.GetFloatOptional(fmt.Sprintf("height%d", i), configfile.Default(nil))
		if !ok {
			break
		}
		heightPos := loadConfigStable(config, fmt.Sprintf("height%d_pos", i))
		d.lastProbePositions = append(d.lastProbePositions, heightPos2(height, heightPos))
	}
	// Restore manually entered heights
	for i := 0; i < 999; i++ {
		height, ok := config.GetFloatOptional(fmt.Sprintf("manual_height%d", i), configfile.Default(nil))
		if !ok {
			break
		}
		heightPos := loadConfigStable(config, fmt.Sprintf("manual_height%d_pos", i))
		d.manualHeights = append(d.manualHeights, heightPos2(height, heightPos))
	}
	// Restore distance measurements
	d.deltaAnalyzeEntry = map[string][]float64{"SCALE": {1.}}
	for i := 0; i < 999; i++ {
		dist, ok := config.GetFloatOptional(fmt.Sprintf("distance%d", i), configfile.Default(nil))
		if !ok {
			break
		}
		pos1 := loadConfigStable(config, fmt.Sprintf("distance%d_pos1", i))
		pos2 := loadConfigStable(config, fmt.Sprintf("distance%d_pos2", i))
		d.lastDistances = append(d.lastDistances, distance{dist, pos1, pos2})
	}
	// Register gcode commands
	d.gcode = gcode.Of(d.printer)
	d.gcode.RegisterCommand("DELTA_CALIBRATE", d.cmdDeltaCalibrate, false, "Delta calibration script")
	d.gcode.RegisterCommand("DELTA_ANALYZE", d.cmdDeltaAnalyze, false, "Extended delta calibration tool")
	return d
}

func heightPos2(h float64, p stablePos) heightPos { return heightPos{h, p} }

func init() { host.RegisterModule("delta_calibrate", load, nil) }

func (d *DeltaCalibrate) kin() calibrator {
	kin, _ := toolhead.Of(d.printer).Kinematics().(calibrator)
	return kin
}

func (d *DeltaCalibrate) handleConnect() {
	if d.kin() == nil {
		configfile.Raise("Delta calibrate is only for delta printers")
	}
}

func fmtStable(p stablePos) string {
	return fmt.Sprintf("%.3f,%.3f,%.3f", p.pos[0], p.pos[1], p.pos[2])
}

func (d *DeltaCalibrate) saveState(probePositions []heightPos, distances []distance,
	deltaParams *delta.DeltaCalibration) {
	// Save main delta parameters
	pconfig := d.printer.LookupObject("configfile").(*configfile.PrinterConfig)
	msg := deltaParams.SaveState(pconfig)
	d.gcode.RespondInfo(msg, true)
	// Save probe stable positions
	section := "delta_calibrate"
	pconfig.RemoveSection(section)
	for i, hp := range probePositions {
		pconfig.Set(section, fmt.Sprintf("height%d", i), pyfmt.Float(hp.height))
		pconfig.Set(section, fmt.Sprintf("height%d_pos", i), fmtStable(hp.pos))
	}
	// Save manually entered heights
	for i, hp := range d.manualHeights {
		pconfig.Set(section, fmt.Sprintf("manual_height%d", i), pyfmt.Float(hp.height))
		pconfig.Set(section, fmt.Sprintf("manual_height%d_pos", i), fmtStable(hp.pos))
	}
	// Save distance measurements
	for i, dm := range distances {
		pconfig.Set(section, fmt.Sprintf("distance%d", i), pyfmt.Float(dm.dist))
		pconfig.Set(section, fmt.Sprintf("distance%d_pos1", i), fmtStable(dm.pos1))
		pconfig.Set(section, fmt.Sprintf("distance%d_pos2", i), fmtStable(dm.pos2))
	}
}

func (d *DeltaCalibrate) probeFinalize(positions []*manual_probe.ProbeResult) {
	// Convert positions into (z_offset, stable_position) pairs
	csp := d.kin().Calibration().CalcStablePosition
	var probePositions []heightPos
	for _, p := range positions {
		probePositions = append(probePositions, heightPos{p.TestZ - p.BedZ(),
			stablePos{csp([3]float64{p.TestX, p.TestY, p.TestZ}), false}})
	}
	// Perform analysis
	d.calculateParams(probePositions, d.lastDistances)
}

func reprHeights(hs []heightPos) string {
	var parts []string
	for _, h := range hs {
		parts = append(parts, "("+pyfmt.Float(h.height)+", "+h.pos.repr()+")")
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func reprDistances(ds []distance) string {
	var parts []string
	for _, dm := range ds {
		parts = append(parts, "("+pyfmt.Float(dm.dist)+", "+dm.pos1.repr()+", "+dm.pos2.repr()+")")
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// paramKeys is the order of the keys of the parameters of coordinate_descent_params.
var paramKeys = []string{"radius", "angle_a", "arm_a", "endstop_a", "stepdist_a",
	"angle_b", "arm_b", "endstop_b", "stepdist_b", "angle_c", "arm_c", "endstop_c", "stepdist_c"}

func reprParams(params map[string]float64) string {
	var parts []string
	for _, k := range paramKeys {
		parts = append(parts, fmt.Sprintf("'%s': %s", k, pyfmt.Float(params[k])))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func dist3(a, b [3]float64) float64 {
	dx, dy, dz := a[0]-b[0], a[1]-b[1], a[2]-b[2]
	return mathutil.Sqrt(dx*dx + dy*dy + dz*dz)
}

func (d *DeltaCalibrate) calculateParams(probePositions []heightPos, distances []distance) {
	heightPositions := append(append([]heightPos(nil), d.manualHeights...), probePositions...)
	// Setup for coordinate descent analysis
	odp := d.kin().Calibration()
	origDeltaParams := odp
	adjParams, params := odp.CoordinateDescentParams(len(distances) > 0)
	log.Printf("Calculating delta_calibrate with:\n%s\n%s\nInitial delta_calibrate parameters: %s",
		reprHeights(heightPositions), reprDistances(distances), reprParams(params))
	zWeight := 1.
	if len(distances) > 0 {
		zWeight = float64(len(distances)) / (MeasureWeight * float64(len(probePositions)))
	}
	// Perform coordinate descent
	deltaErrorfunc := func(params map[string]float64) (totalError float64) {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(*mathutil.ValueError); !ok {
					panic(r)
				}
				totalError = 9999999999999.9
			}
		}()
		// Build new delta_params for params under test
		deltaParams := origDeltaParams.NewCalibration(params)
		getpos := deltaParams.PositionFromStable
		// Calculate z height errors
		totalError = 0.
		for _, hp := range heightPositions {
			p := getpos(hp.pos.pos)
			totalError += (p[2] - hp.height) * (p[2] - hp.height)
		}
		totalError *= zWeight
		// Calculate distance errors
		for _, dm := range distances {
			dd := dist3(getpos(dm.pos1.pos), getpos(dm.pos2.pos))
			totalError += (dd - dm.dist) * (dd - dm.dist)
		}
		return totalError
	}
	newParams := d.backgroundCoordinateDescent(adjParams, params, deltaErrorfunc)
	// Log and report results
	log.Printf("Calculated delta_calibrate parameters: %s", reprParams(newParams))
	newDeltaParams := origDeltaParams.NewCalibration(newParams)
	for _, hp := range heightPositions {
		log.Printf("height orig: %.6f new: %.6f goal: %.6f",
			origDeltaParams.PositionFromStable(hp.pos.pos)[2],
			newDeltaParams.PositionFromStable(hp.pos.pos)[2], hp.height)
	}
	for _, dm := range distances {
		origDist := dist3(origDeltaParams.PositionFromStable(dm.pos1.pos),
			origDeltaParams.PositionFromStable(dm.pos2.pos))
		newDist := dist3(newDeltaParams.PositionFromStable(dm.pos1.pos),
			newDeltaParams.PositionFromStable(dm.pos2.pos))
		log.Printf("distance orig: %.6f new: %.6f goal: %.6f", origDist, newDist, dm.dist)
	}
	// Store results for SAVE_CONFIG
	d.saveState(probePositions, distances, newDeltaParams)
	d.gcode.RespondInfo("The SAVE_CONFIG command will update the printer config file\n"+
		"with these parameters and restart the printer.", true)
}

// backgroundCoordinateDescent runs the coordinate descent on a goroutine, so that it does not
// block the reactor (background_coordinate_descent of mathutil.py). Klipper runs it in a child
// process, which logs only when klippy has no log file.
func (d *DeltaCalibrate) backgroundCoordinateDescent(adjParams []string, params map[string]float64,
	errorFunc func(map[string]float64) float64) map[string]float64 {
	var logf func(format string, args ...any)
	if _, ok := d.printer.StartArgs().Get("log_file"); !ok {
		logf = log.Printf
	}
	var done atomic.Bool
	var res map[string]float64
	var failure any
	go func() {
		defer done.Store(true)
		defer func() {
			if r := recover(); r != nil {
				failure = r
			}
		}()
		res = mathutil.CoordinateDescent(adjParams, params, errorFunc, logf)
	}()
	// Wait for the calculation to finish
	rc := d.printer.Reactor()
	eventtime := rc.Monotonic()
	lastReportTime := eventtime
	for !done.Load() {
		if eventtime > lastReportTime+5. {
			lastReportTime = eventtime
			d.gcode.RespondInfo("Working on calibration...", false)
		}
		eventtime = rc.Pause(eventtime + .1)
	}
	if failure != nil {
		panic(fmt.Errorf("Error in coordinate descent: %v", failure))
	}
	return res
}

func (d *DeltaCalibrate) cmdDeltaCalibrate(gcmd *gcode.Command) {
	d.probeHelper.StartProbe(gcmd)
}

func (d *DeltaCalibrate) addManualHeight(height float64) {
	// Determine current location of toolhead
	th := toolhead.Of(d.printer)
	th.FlushStepGeneration()
	kin := th.Kinematics().(toolhead.Kinematics)
	kinSpos := map[string]float64{}
	for _, s := range kin.Steppers() {
		ms := s.(*stepper.MCUStepper)
		kinSpos[ms.Name()] = ms.CommandedPosition()
	}
	kinPos := kin.CalcPosition(kinSpos)
	// Convert location to a stable position
	deltaParams := d.kin().Calibration()
	stable := deltaParams.CalcStablePosition([3]float64{kinPos[0], kinPos[1], kinPos[2]})
	// Add to list of manual heights
	d.manualHeights = append(d.manualHeights, heightPos{height, stablePos{stable, true}})
	d.gcode.RespondInfo(fmt.Sprintf("Adding manual height: %.3f,%.3f,%.3f is actually z=%.3f",
		kinPos[0], kinPos[1], kinPos[2], height), true)
}

func (d *DeltaCalibrate) doExtendedCalibration() {
	// Extract distance positions
	var distances []distance
	if len(d.deltaAnalyzeEntry) <= 1 {
		distances = d.lastDistances
	} else if len(d.deltaAnalyzeEntry) < 5 {
		panic(gcode.Errorf("Not all measurements provided"))
	} else {
		deltaParams := d.kin().Calibration()
		distances = measurementsToDistances(d.deltaAnalyzeEntry, deltaParams)
	}
	if len(d.lastProbePositions) == 0 {
		panic(gcode.Errorf("Must run basic calibration with DELTA_CALIBRATE first"))
	}
	// Perform analysis
	d.calculateParams(d.lastProbePositions, distances)
}

func (d *DeltaCalibrate) cmdDeltaAnalyze(gcmd *gcode.Command) {
	// Check for manual height entry
	if mheight, ok := gcmd.GetFloatOptional("MANUAL_HEIGHT"); ok {
		d.addManualHeight(mheight)
		return
	}
	// Parse distance measurements
	args := []struct {
		name  string
		count int
	}{{"CENTER_DISTS", 6}, {"CENTER_PILLAR_WIDTHS", 3}, {"OUTER_DISTS", 6},
		{"OUTER_PILLAR_WIDTHS", 6}, {"SCALE", 1}}
	for _, a := range args {
		data, ok := gcmd.Get(a.name, gcode.Default(nil))
		if !ok {
			continue
		}
		var parts []float64
		for _, p := range strings.Split(data, ",") {
			f, err := pyfmt.ParseFloat(strings.TrimSpace(p))
			if err != nil {
				panic(gcmd.Error("Unable to parse parameter '%s'", a.name))
			}
			parts = append(parts, f)
		}
		if len(parts) != a.count {
			panic(gcmd.Error("Parameter '%s' must have %d values", a.name, a.count))
		}
		d.deltaAnalyzeEntry[a.name] = parts
		items := make([]any, len(parts))
		for i, p := range parts {
			items[i] = p
		}
		log.Printf("DELTA_ANALYZE %s = %s", a.name, jinja.Repr(items))
	}
	// Perform analysis if requested
	if action, ok := gcmd.Get("CALIBRATE", gcode.Default(nil)); ok {
		if action != "extended" {
			panic(gcmd.Error("Unknown calibrate action"))
		}
		d.doExtendedCalibration()
	}
}
