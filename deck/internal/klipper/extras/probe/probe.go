// Package probe is the Z probe: probing moves, multi-sample probing, the probe commands (PROBE,
// QUERY_PROBE, PROBE_CALIBRATE, PROBE_ACCURACY), Z homing with probe:z_virtual_endstop, and the
// helper that probes a list of points for the bed modules.
//
// Ported to Go from klippy/extras/probe.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2017-2026  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package probe

import (
	"fmt"
	"log"
	"math"
	"slices"
	"sort"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/gcode_macro"
	"printdeck/internal/klipper/extras/homing"
	"printdeck/internal/klipper/extras/manual_probe"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mathutil"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/pins"
	"printdeck/internal/klipper/reactor"
	"printdeck/internal/klipper/stepper"
	"printdeck/internal/klipper/toolhead"
)

const hintTimeout = `
If the probe did not move far enough to trigger, then
consider reducing the Z axis minimum position so the probe
can travel further (the Z minimum position can be negative).
`

type result = manual_probe.ProbeResult

// CalcProbeZAverage returns the mean ("average") or the median ("median") of probe results.
func CalcProbeZAverage(positions []*result, method string) *result {
	if method != "median" {
		// Use mean average
		inv := 1. / float64(len(positions))
		avg := func(get func(p *result) float64) float64 {
			vals := make([]float64, len(positions))
			for i, p := range positions {
				vals[i] = get(p)
			}
			return mathutil.Sum(vals...) * inv
		}
		r := result{
			BedX_: avg(func(p *result) float64 { return p.BedX_ }),
			BedY_: avg(func(p *result) float64 { return p.BedY_ }),
			BedZ_: avg(func(p *result) float64 { return p.BedZ_ }),
			TestX: avg(func(p *result) float64 { return p.TestX }),
			TestY: avg(func(p *result) float64 { return p.TestY }),
			TestZ: avg(func(p *result) float64 { return p.TestZ }),
		}
		return &r
	}
	// Use median
	zSorted := append([]*result(nil), positions...)
	sort.SliceStable(zSorted, func(i, j int) bool { return zSorted[i].BedZ_ < zSorted[j].BedZ_ })
	middle := len(positions) / 2
	if len(positions)&1 == 1 {
		// odd number of samples
		return zSorted[middle]
	}
	// even number of samples
	return CalcProbeZAverage(zSorted[middle-1:middle+1], "average")
}

// Params are the probing parameters of a command.
type Params struct {
	ProbeSpeed, LiftSpeed   float64
	Samples                 int
	SampleRetractDist       float64
	SamplesTolerance        float64
	SamplesToleranceRetries int
	SamplesResult           string
}

// Probe is a probe, as the commands and the probe point helper use it.
type Probe interface {
	ProbeParams(gcmd *gcode.Command) Params
	Offsets(gcmd *gcode.Command) [3]float64
	StartProbeSession(gcmd *gcode.Command) homing.ProbeSession
}

// Probe device implementation helpers

// CommandHelper has the probe commands.
type CommandHelper struct {
	printer           *host.Printer
	probe             Probe
	queryEndstop      func(printTime float64) int
	name              string
	lastState         bool
	lastProbePosition gcode.Coord
	lastZResult       float64
	calibratePos      *result
	calibrateOffsets  [3]float64
}

// NewCommandHelper registers the probe commands. queryEndstop can be nil.
func NewCommandHelper(config *configfile.Wrapper, probe Probe, queryEndstop func(float64) int,
	canSetZOffset bool) *CommandHelper {
	printer := host.Of(config)
	h := &CommandHelper{printer: printer, probe: probe, queryEndstop: queryEndstop, name: config.Name()}
	g := gcode.Of(printer)
	g.RegisterCommand("QUERY_PROBE", h.cmdQueryProbe, false, "Return the status of the z-probe")
	g.RegisterCommand("PROBE", h.cmdProbe, false, "Probe Z-height at current XY position")
	if canSetZOffset {
		g.RegisterCommand("PROBE_CALIBRATE", h.cmdProbeCalibrate, false, "Calibrate the probe's z_offset")
	}
	g.RegisterCommand("PROBE_ACCURACY", h.cmdProbeAccuracy, false, "Probe Z-height accuracy at current XY position")
	if canSetZOffset {
		g.RegisterCommand("Z_OFFSET_APPLY_PROBE", h.cmdZOffsetApplyProbe, false, "Adjust the probe's z_offset")
	}
	return h
}

func (h *CommandHelper) move(coord []float64, speed float64) {
	toolhead.Of(h.printer).ManualMove(coord, speed)
}

// Status returns the probe status.
func (h *CommandHelper) Status(eventtime float64) map[string]any {
	return map[string]any{"name": h.name, "last_query": h.lastState, "last_probe_position": h.lastProbePosition,
		"last_z_result": h.lastZResult}
}

func (h *CommandHelper) cmdQueryProbe(gcmd *gcode.Command) {
	if h.queryEndstop == nil {
		panic(gcmd.Error("Probe does not support QUERY_PROBE"))
	}
	printTime := toolhead.Of(h.printer).LastMoveTime()
	res := h.queryEndstop(printTime)
	h.lastState = res != 0
	state := "open"
	if res != 0 {
		state = "TRIGGERED"
	}
	gcmd.RespondInfo("probe: "+state, true)
}

func (h *CommandHelper) cmdProbe(gcmd *gcode.Command) {
	pos := RunSingleProbe(h.probe, gcmd)
	gcmd.RespondInfo(fmt.Sprintf("Result: at %.3f,%.3f estimate contact at z=%.6f", pos.BedX_, pos.BedY_, pos.BedZ_), true)
	h.lastProbePosition = gcode.Coord{pos.BedX_, pos.BedY_, pos.BedZ_}
	offsets := h.probe.Offsets(gcmd)
	h.lastZResult = pos.BedZ_ + offsets[2] // Deprecated
}

func (h *CommandHelper) saveZOffset(z float64) {
	gcode.Of(h.printer).RespondInfo(fmt.Sprintf("%s: z_offset: %.3f\n"+
		"The SAVE_CONFIG command will update the printer config file\n"+
		"with the above and restart the printer.", h.name, z), true)
	pconfig := h.printer.LookupObject("configfile").(*configfile.PrinterConfig)
	pconfig.Set(h.name, "z_offset", fmt.Sprintf("%.3f", z))
}

func (h *CommandHelper) probeCalibrateFinalize(r *result) {
	if r == nil {
		return
	}
	h.saveZOffset(h.calibrateOffsets[2] - r.BedZ_ + h.calibratePos.BedZ_)
}

func (h *CommandHelper) cmdProbeCalibrate(gcmd *gcode.Command) {
	manual_probe.VerifyNoManualProbe(h.printer)
	params := h.probe.ProbeParams(gcmd)
	// Perform initial probe
	ppos := RunSingleProbe(h.probe, gcmd)
	// Move away from the bed
	curpos := toolhead.Of(h.printer).Position()
	curpos[2] += 5.
	h.move(curpos, params.LiftSpeed)
	// Move the nozzle over the probe point
	curpos[0] = ppos.BedX_
	curpos[1] = ppos.BedY_
	h.move(curpos, params.ProbeSpeed)
	// Start manual probe
	h.calibratePos, h.calibrateOffsets = ppos, h.probe.Offsets(gcmd)
	manual_probe.NewManualProbeHelper(h.printer, gcmd, h.probeCalibrateFinalize)
}

func (h *CommandHelper) cmdProbeAccuracy(gcmd *gcode.Command) {
	params := h.probe.ProbeParams(gcmd)
	sampleCount := gcmd.GetInt("SAMPLES", gcode.Default(10), gcode.Min(1))
	th := toolhead.Of(h.printer)
	startPos := th.Position()
	gcmd.RespondInfo(fmt.Sprintf("PROBE_ACCURACY at X:%.3f Y:%.3f Z:%.3f (samples=%d retract=%.3f speed=%.1f lift_speed=%.1f)\n",
		startPos[0], startPos[1], startPos[2], sampleCount, params.SampleRetractDist, params.ProbeSpeed, params.LiftSpeed), true)
	// Create dummy gcmd with SAMPLES=1
	foParams := map[string]string{}
	for k, v := range gcmd.Params() {
		foParams[k] = v
	}
	foParams["SAMPLES"] = "1"
	foGcmd := gcode.Of(h.printer).CreateCommand("", "", foParams)
	// Probe bed sample_count times
	session := h.probe.StartProbeSession(foGcmd)
	for probeNum := 0; probeNum < sampleCount; probeNum++ {
		// Probe position
		session.RunProbe(foGcmd)
		// Retract
		liftZ := th.Position()[2] + params.SampleRetractDist
		h.move([]float64{startPos[0], startPos[1], liftZ}, params.LiftSpeed)
	}
	positions := toResults(session.PullProbedResults())
	session.EndProbeSession()
	// Calculate maximum, minimum and average values
	maxV, minV := math.Inf(-1), math.Inf(1)
	for _, p := range positions {
		maxV, minV = math.Max(maxV, p.BedZ_), math.Min(minV, p.BedZ_)
	}
	avg := CalcProbeZAverage(positions, "average").BedZ_
	median := CalcProbeZAverage(positions, "median").BedZ_
	// calculate the standard deviation
	devSum := 0.
	for _, p := range positions {
		devSum += math.Pow(p.BedZ_-avg, 2.)
	}
	sigma := math.Pow(devSum/float64(len(positions)), 0.5)
	gcmd.RespondInfo(fmt.Sprintf("probe accuracy results: maximum %.6f, minimum %.6f, range %.6f, "+
		"average %.6f, median %.6f, standard deviation %.6f", maxV, minV, maxV-minV, avg, median, sigma), true)
}

type statuser interface {
	Status(eventtime float64) map[string]any
}

func (h *CommandHelper) cmdZOffsetApplyProbe(gcmd *gcode.Command) {
	gm := h.printer.LookupObject("gcode_move").(statuser)
	offset := gm.Status(0)["homing_origin"].(gcode.Coord).Z()
	if offset == 0 {
		gcmd.RespondInfo("Nothing to do: Z Offset is 0", true)
		return
	}
	h.saveZOffset(h.probe.Offsets(gcmd)[2] - offset)
}

func toResults(rs []homing.ProbeResult) []*result {
	out := make([]*result, len(rs))
	for i, r := range rs {
		out[i] = r.(*result)
	}
	return out
}

func fromResults(rs []*result) []homing.ProbeResult {
	out := make([]homing.ProbeResult, len(rs))
	for i, r := range rs {
		out[i] = r
	}
	return out
}

// LookupMinimumZ returns the minimum Z position of the printer.
func LookupMinimumZ(config *configfile.Wrapper) float64 {
	if zconfig := manual_probe.LookupZEndstopConfig(config); zconfig != nil {
		return zconfig.GetFloat("position_min", configfile.Default(0.), configfile.NoteValid(false))
	}
	return config.GetSection("printer").GetFloat("minimum_z_position", configfile.Default(0.), configfile.NoteValid(false))
}

// LookupZSteppers calls addStepper with each Z stepper, at the board identify.
func LookupZSteppers(config *configfile.Wrapper, addStepper func(s mcu.Stepper)) {
	printer := host.Of(config)
	printer.RegisterEventHandler("klippy:mcu_identify", func(...any) {
		for _, s := range toolhead.Of(printer).Kin().Steppers() {
			ms := s.(*stepper.MCUStepper)
			if ms.IsActiveAxis("z") {
				addStepper(ms)
			}
		}
	})
}

// HomingViaProbeHelper is the probe:z_virtual_endstop pin, for Z homing with the probe.
type HomingViaProbeHelper struct {
	printer         *host.Printer
	positionEndstop float64
	queryEndstopCB  func(printTime float64) int
}

func NewHomingViaProbeHelper(config *configfile.Wrapper, positionEndstop float64, queryEndstop func(float64) int) *HomingViaProbeHelper {
	h := &HomingViaProbeHelper{printer: host.Of(config), positionEndstop: positionEndstop, queryEndstopCB: queryEndstop}
	// Register z_virtual_endstop pin
	h.printer.LookupObject("pins").(*pins.Pins).RegisterChip("probe", h)
	return h
}

func (h *HomingViaProbeHelper) raiseError() {
	panic(gcode.Errorf("Internal error. Can't home via HomingViaProbeHelper."))
}

// MCU_endstop interface
func (h *HomingViaProbeHelper) AddStepper(s mcu.Stepper) {}
func (h *HomingViaProbeHelper) Steppers() []mcu.Stepper  { return nil }
func (h *HomingViaProbeHelper) HomeStart(printTime, sampleTime float64, sampleCount int, restTime float64, triggered bool) *reactor.Completion {
	h.raiseError()
	return nil
}
func (h *HomingViaProbeHelper) HomeWait(homeEndTime float64) float64 { h.raiseError(); return 0 }
func (h *HomingViaProbeHelper) QueryEndstop(printTime float64) int {
	if h.queryEndstopCB == nil {
		return 0
	}
	return h.queryEndstopCB(printTime)
}
func (h *HomingViaProbeHelper) PositionEndstop() float64 { return h.positionEndstop }

// SetupPin is the pin setup of the probe chip (pins.Chip).
func (h *HomingViaProbeHelper) SetupPin(pinType string, params *pins.Params) any {
	if pinType != "endstop" || params.Pin != "z_virtual_endstop" {
		panic(&pins.Error{Msg: "Probe virtual endstop only useful as endstop pin"})
	}
	if params.Invert != 0 || params.Pullup != 0 {
		panic(&pins.Error{Msg: "Can not pullup/invert probe virtual endstop"})
	}
	return h
}

// probeEndstop is the endstop of a probe pin.
type probeEndstop interface {
	homing.Endstop
	AddStepper(s mcu.Stepper)
	QueryEndstop(printTime float64) int
}

type offsetsHelper interface {
	Offsets(gcmd *gcode.Command) [3]float64
}

// DescendToEndstopHelper probes with a move down until the probe endstop triggers.
type DescendToEndstopHelper struct {
	printer             *host.Printer
	mcuProbe            probeEndstop
	probeOffsets        offsetsHelper
	paramHelper         *ParameterHelper
	alwaysCheckMovement bool
	zMinPosition        float64
	results             []*result
}

func NewDescendToEndstopHelper(config *configfile.Wrapper, mcuProbe probeEndstop, offsets offsetsHelper,
	paramHelper *ParameterHelper, alwaysCheckMovement bool) *DescendToEndstopHelper {
	d := &DescendToEndstopHelper{printer: host.Of(config), mcuProbe: mcuProbe, probeOffsets: offsets,
		paramHelper: paramHelper, alwaysCheckMovement: alwaysCheckMovement, zMinPosition: LookupMinimumZ(config)}
	LookupZSteppers(config, mcuProbe.AddStepper)
	return d
}

// DescendUntilTrigger moves down until the probe triggers, and keeps the result.
func (d *DescendToEndstopHelper) DescendUntilTrigger(gcmd *gcode.Command) {
	th := toolhead.Of(d.printer)
	pos := th.Position()
	pos[2] = d.zMinPosition
	speed := d.paramHelper.ProbeParams(gcmd).ProbeSpeed
	phoming := d.printer.LookupObject("homing").(*homing.PrinterHoming)
	checkMovement := d.alwaysCheckMovement || !phoming.CheckProbeFirstHome(gcmd)
	ppos := phoming.ProbingMove(d.mcuProbe, pos, speed, checkMovement)
	d.results = append(d.results, manual_probe.CreateProbeResult(ppos, d.probeOffsets.Offsets(nil)))
}

// PullTriggerPositions returns the results and clears them.
func (d *DescendToEndstopHelper) PullTriggerPositions() []*result {
	res := d.results
	d.results = nil
	return res
}

// ClearTriggerPositions clears the results.
func (d *DescendToEndstopHelper) ClearTriggerPositions() { d.PullTriggerPositions() }

// ParameterHelper reads the multi-sample parameters of the config and the commands.
type ParameterHelper struct {
	dummyGcmd         *gcode.Command
	speed, liftSpeed  float64
	sampleCount       int
	sampleRetractDist float64
	samplesResult     string
	samplesTolerance  float64
	samplesRetries    int
}

func NewParameterHelper(config *configfile.Wrapper) *ParameterHelper {
	p := &ParameterHelper{dummyGcmd: gcode.Of(host.Of(config)).CreateCommand("", "", map[string]string{})}
	// Configurable probing speeds
	p.speed = config.GetFloat("speed", configfile.Default(5.0), configfile.Above(0.))
	p.liftSpeed = config.GetFloat("lift_speed", configfile.Default(p.speed), configfile.Above(0.))
	// Multi-sample support (for improved accuracy)
	p.sampleCount = config.GetInt("samples", configfile.Default(1), configfile.Min(1))
	p.sampleRetractDist = config.GetFloat("sample_retract_dist", configfile.Default(2.), configfile.Above(0.))
	p.samplesResult = config.GetChoice("samples_result", map[string]any{"median": "median", "average": "average"},
		configfile.Default("average")).(string)
	p.samplesTolerance = config.GetFloat("samples_tolerance", configfile.Default(0.100), configfile.Min(0.))
	p.samplesRetries = config.GetInt("samples_tolerance_retries", configfile.Default(0), configfile.Min(0))
	return p
}

// ProbeParams returns the parameters of a command (nil for the config values).
func (p *ParameterHelper) ProbeParams(gcmd *gcode.Command) Params {
	if gcmd == nil {
		gcmd = p.dummyGcmd
	}
	return Params{
		ProbeSpeed:              gcmd.GetFloat("PROBE_SPEED", gcode.Default(p.speed), gcode.Above(0.)),
		LiftSpeed:               gcmd.GetFloat("LIFT_SPEED", gcode.Default(p.liftSpeed), gcode.Above(0.)),
		Samples:                 gcmd.GetInt("SAMPLES", gcode.Default(p.sampleCount), gcode.Min(1)),
		SampleRetractDist:       gcmd.GetFloat("SAMPLE_RETRACT_DIST", gcode.Default(p.sampleRetractDist), gcode.Above(0.)),
		SamplesTolerance:        gcmd.GetFloat("SAMPLES_TOLERANCE", gcode.Default(p.samplesTolerance), gcode.Min(0.)),
		SamplesToleranceRetries: gcmd.GetInt("SAMPLES_TOLERANCE_RETRIES", gcode.Default(p.samplesRetries), gcode.Min(0)),
		SamplesResult:           gcmd.GetString("SAMPLES_RESULT", gcode.Default(p.samplesResult)),
	}
}

// HWSession is the probing session of the probe hardware.
type HWSession interface {
	RunProbe(gcmd *gcode.Command)
	PullProbedResults() []*result
	EndProbeSession()
}

// SampleAveragingHelper probes several samples at each point (homing.ProbeSession).
type SampleAveragingHelper struct {
	printer        *host.Printer
	paramHelper    *ParameterHelper
	startSessionCB func(gcmd *gcode.Command) HWSession
	hwProbeSession HWSession
	results        []*result
}

func NewSampleAveragingHelper(config *configfile.Wrapper, paramHelper *ParameterHelper,
	startSession func(gcmd *gcode.Command) HWSession) *SampleAveragingHelper {
	s := &SampleAveragingHelper{printer: host.Of(config), paramHelper: paramHelper, startSessionCB: startSession}
	s.printer.RegisterEventHandler("gcode:command_error", func(...any) {
		if s.hwProbeSession != nil {
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("Multi-probe end: %v", r)
					}
				}()
				s.EndProbeSession()
			}()
		}
	})
	return s
}

func (s *SampleAveragingHelper) probeStateError() {
	panic(gcode.Errorf("Internal probe error - start/end probe session mismatch"))
}

// StartProbeSession starts a session.
func (s *SampleAveragingHelper) StartProbeSession(gcmd *gcode.Command) homing.ProbeSession {
	if s.hwProbeSession != nil {
		s.probeStateError()
	}
	s.hwProbeSession = s.startSessionCB(gcmd)
	s.results = nil
	return s
}

// EndProbeSession ends the session.
func (s *SampleAveragingHelper) EndProbeSession() {
	hw := s.hwProbeSession
	if hw == nil {
		s.probeStateError()
	}
	s.results = nil
	s.hwProbeSession = nil
	hw.EndProbeSession()
}

func (s *SampleAveragingHelper) probe(gcmd *gcode.Command) *result {
	th := toolhead.Of(s.printer)
	curtime := s.printer.Reactor().Monotonic()
	if homed, _ := th.Status(curtime)["homed_axes"].(string); !strings.Contains(homed, "z") {
		panic(gcode.Errorf("Must home before probe"))
	}
	var epos *result
	func() {
		defer func() {
			if r := recover(); r != nil {
				if e, ok := r.(*gcode.CommandError); ok {
					reason := e.Msg
					if strings.Contains(reason, "Timeout during endstop homing") {
						reason += hintTimeout
					}
					panic(gcode.Errorf("%s", reason))
				}
				panic(r)
			}
		}()
		s.hwProbeSession.RunProbe(gcmd)
		epos = s.hwProbeSession.PullProbedResults()[0]
	}()
	// Allow axis_twist_compensation to update results
	results := []*result{epos}
	s.printer.SendEvent("probe:update_results", results)
	epos = results[0]
	// Report results
	if gcmd.Command() != "G28" {
		gcode.Of(s.printer).RespondInfo(fmt.Sprintf("probe: at %.3f,%.3f bed will contact at z=%.6f",
			epos.BedX_, epos.BedY_, epos.BedZ_), true)
	}
	return epos
}

// RunProbe probes the samples at the current position, and keeps their average.
func (s *SampleAveragingHelper) RunProbe(gcmd *gcode.Command) {
	if s.hwProbeSession == nil {
		s.probeStateError()
	}
	params := s.paramHelper.ProbeParams(gcmd)
	th := toolhead.Of(s.printer)
	probexy := th.Position()[:2]
	retries := 0
	var positions []*result
	for len(positions) < params.Samples {
		// Probe position
		positions = append(positions, s.probe(gcmd))
		// Check samples tolerance
		maxZ, minZ := math.Inf(-1), math.Inf(1)
		for _, p := range positions {
			maxZ, minZ = math.Max(maxZ, p.BedZ_), math.Min(minZ, p.BedZ_)
		}
		if maxZ-minZ > params.SamplesTolerance {
			if retries >= params.SamplesToleranceRetries {
				panic(gcmd.Error("Probe samples exceed samples_tolerance"))
			}
			gcmd.RespondInfo("Probe samples exceed tolerance. Retrying...", true)
			retries++
			positions = nil
		}
		// Retract
		if len(positions) < params.Samples {
			curZ := th.Position()[2]
			th.ManualMove([]float64{probexy[0], probexy[1], curZ + params.SampleRetractDist}, params.LiftSpeed)
		}
	}
	// Calculate result
	s.results = append(s.results, CalcProbeZAverage(positions, params.SamplesResult))
}

// PullProbedResults returns the results and clears them.
func (s *SampleAveragingHelper) PullProbedResults() []homing.ProbeResult {
	res := s.results
	s.results = nil
	return fromResults(res)
}

// OffsetsHelper reads the probe offsets of the config.
type OffsetsHelper struct{ x, y, z float64 }

func NewOffsetsHelper(config *configfile.Wrapper) *OffsetsHelper {
	return &OffsetsHelper{config.GetFloat("x_offset", configfile.Default(0.)),
		config.GetFloat("y_offset", configfile.Default(0.)), config.GetFloat("z_offset")}
}

// Offsets returns the X, Y and Z offsets.
func (o *OffsetsHelper) Offsets(gcmd *gcode.Command) [3]float64 { return [3]float64{o.x, o.y, o.z} }

// Tools for utilizing the probe

// PointsFinalize gets the results of all points. It returns "retry" to probe the points again.
type PointsFinalize func(results []*result) string

// PointsHelper probes a list of points and reports the results (ProbePointsHelper).
type PointsHelper struct {
	printer                *host.Printer
	finalizeCallback       PointsFinalize
	probePoints            [][]float64
	name                   string
	gcode                  *gcode.Dispatch
	defaultHorizontalMoveZ float64
	horizontalMoveZ        float64
	speed                  float64
	useOffsets             bool
	liftSpeed              float64
	probeOffsets           [3]float64
	manualResults          []*result
}

// NewPointsHelper reads the points of the config (or uses defaultPoints, when not nil and the
// config has no points).
func NewPointsHelper(config *configfile.Wrapper, finalize PointsFinalize, defaultPoints [][]float64) *PointsHelper {
	printer := host.Of(config)
	p := &PointsHelper{printer: printer, finalizeCallback: finalize, probePoints: defaultPoints, name: config.Name(),
		gcode: gcode.Of(printer)}
	// Read config settings
	if _, ok := config.GetOptional("points", configfile.Default(nil)); defaultPoints == nil || ok {
		p.probePoints = config.GetFloatLists("points", []string{",", "\n"}, 2)
	}
	p.defaultHorizontalMoveZ = config.GetFloat("horizontal_move_z", configfile.Default(5.))
	p.speed = config.GetFloat("speed", configfile.Default(50.), configfile.Above(0.))
	p.liftSpeed = p.speed
	return p
}

// MinimumPoints is a config error when there are less than n points.
func (p *PointsHelper) MinimumPoints(n int) {
	if len(p.probePoints) < n {
		configfile.Raise("Need at least %d probe points for %s", n, p.name)
	}
}

// UpdateProbePoints sets the points.
func (p *PointsHelper) UpdateProbePoints(points [][]float64, minPoints int) {
	p.probePoints = points
	p.MinimumPoints(minPoints)
}

func (p *PointsHelper) UseXYOffsets(useOffsets bool) { p.useOffsets = useOffsets }
func (p *PointsHelper) LiftSpeed() float64           { return p.liftSpeed }

func (p *PointsHelper) move(coord []float64, speed float64) {
	toolhead.Of(p.printer).ManualMove(coord, speed)
}

func (p *PointsHelper) raiseTool(isFirst bool) {
	speed := p.liftSpeed
	if isFirst {
		// Use full speed to first probe position
		speed = p.speed
	}
	nan := math.NaN()
	p.move([]float64{nan, nan, p.horizontalMoveZ}, speed)
}

func (p *PointsHelper) invokeCallback(results []*result) bool {
	// Flush lookahead queue
	toolhead.Of(p.printer).LastMoveTime()
	// Invoke callback
	return p.finalizeCallback(results) != "retry"
}

func (p *PointsHelper) moveNext(probeNum int) {
	// Move to next XY probe point
	nextpos := slices.Clone(p.probePoints[probeNum])
	if p.useOffsets {
		nextpos[0] -= p.probeOffsets[0]
		nextpos[1] -= p.probeOffsets[1]
	}
	p.move(nextpos, p.speed)
}

// StartProbe probes the points, with the probe or manually.
func (p *PointsHelper) StartProbe(gcmd *gcode.Command) {
	manual_probe.VerifyNoManualProbe(p.printer)
	// Lookup objects
	probe, _ := p.printer.TryLookupObject("probe").(Probe)
	method := strings.ToLower(gcmd.GetString("METHOD", gcode.Default("automatic")))
	p.horizontalMoveZ = gcmd.GetFloat("HORIZONTAL_MOVE_Z", gcode.Default(p.defaultHorizontalMoveZ))
	if probe == nil || method == "manual" {
		// Manual probe
		p.liftSpeed = p.speed
		p.probeOffsets = [3]float64{}
		p.manualResults = nil
		p.manualProbeStart()
		return
	}
	// Perform automatic probing
	p.liftSpeed = probe.ProbeParams(gcmd).LiftSpeed
	p.probeOffsets = probe.Offsets(gcmd)
	if p.horizontalMoveZ < p.probeOffsets[2] {
		panic(gcmd.Error("horizontal_move_z can't be less than probe's z_offset"))
	}
	session := probe.StartProbeSession(gcmd)
	probeNum := 0
	for {
		p.raiseTool(probeNum == 0)
		if probeNum >= len(p.probePoints) {
			if p.invokeCallback(toResults(session.PullProbedResults())) {
				break
			}
			// Caller wants a "retry" - restart probing
			probeNum = 0
		}
		p.moveNext(probeNum)
		session.RunProbe(gcmd)
		probeNum++
	}
	session.EndProbeSession()
}

func (p *PointsHelper) manualProbeStart() {
	p.raiseTool(len(p.manualResults) == 0)
	if len(p.manualResults) >= len(p.probePoints) {
		if p.invokeCallback(p.manualResults) {
			return
		}
		// Caller wants a "retry" - clear results and restart probing
		p.manualResults = nil
	}
	p.moveNext(len(p.manualResults))
	gcmd := p.gcode.CreateCommand("", "", map[string]string{})
	manual_probe.NewManualProbeHelper(p.printer, gcmd, p.manualProbeFinalize)
}

func (p *PointsHelper) manualProbeFinalize(r *result) {
	if r == nil {
		return
	}
	p.manualResults = append(p.manualResults, r)
	p.manualProbeStart()
}

// RunSingleProbe probes once and returns the result.
func RunSingleProbe(probe Probe, gcmd *gcode.Command) *result {
	session := probe.StartProbeSession(gcmd)
	session.RunProbe(gcmd)
	pos := session.PullProbedResults()[0].(*result)
	session.EndProbeSession()
	return pos
}

// Handle [probe] config

// EndstopWrapper is the endstop of the probe pin, with the activate and deactivate scripts.
type EndstopWrapper struct {
	printer          *host.Printer
	stowOnEachSample bool
	activateGcode    *gcode_macro.TemplateWrapper
	deactivateGcode  *gcode_macro.TemplateWrapper
	mcuEndstop       *mcu.Endstop
	homingHelper     *DescendToEndstopHelper
	multi            string
}

func NewEndstopWrapper(config *configfile.Wrapper, offsets *OffsetsHelper, paramHelper *ParameterHelper) *EndstopWrapper {
	printer := host.Of(config)
	w := &EndstopWrapper{printer: printer, multi: "OFF"}
	w.stowOnEachSample = config.GetBool("deactivate_on_each_sample", configfile.Default(true))
	gm := gcode_macro.Load(config)
	empty := ""
	w.activateGcode = gm.LoadTemplate(config, "activate_gcode", &empty)
	w.deactivateGcode = gm.LoadTemplate(config, "deactivate_gcode", &empty)
	// Create an "endstop" object to handle the probe pin
	ppins := printer.LookupObject("pins").(*pins.Pins)
	w.mcuEndstop = ppins.SetupPin("endstop", config.Get("pin")).(*mcu.Endstop)
	// Probing via homing to endstop
	w.homingHelper = NewDescendToEndstopHelper(config, w.mcuEndstop, offsets, paramHelper, false)
	return w
}

// QueryEndstop returns the state of the probe pin.
func (w *EndstopWrapper) QueryEndstop(printTime float64) int {
	return w.mcuEndstop.QueryEndstop(printTime)
}

func (w *EndstopWrapper) runScript(t *gcode_macro.TemplateWrapper, what string) {
	th := toolhead.Of(w.printer)
	startPos := th.Position()
	t.RunGcodeFromCommand(nil)
	if !slices.Equal(th.Position()[:3], startPos[:3]) {
		panic(gcode.Errorf("Toolhead moved during probe %s script", what))
	}
}

func (w *EndstopWrapper) StartProbeSession(gcmd *gcode.Command) HWSession {
	w.homingHelper.ClearTriggerPositions()
	if !w.stowOnEachSample {
		w.multi = "FIRST"
	}
	return w
}

func (w *EndstopWrapper) probePrepare() {
	if w.multi == "OFF" || w.multi == "FIRST" {
		w.runScript(w.activateGcode, "activate_gcode")
		if w.multi == "FIRST" {
			w.multi = "ON"
		}
	}
}

func (w *EndstopWrapper) probeFinish() {
	if w.multi == "OFF" {
		w.runScript(w.deactivateGcode, "deactivate_gcode")
	}
}

// RunProbe probes once.
func (w *EndstopWrapper) RunProbe(gcmd *gcode.Command) {
	w.probePrepare()
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(*gcode.CommandError); ok {
					w.probeFinish()
				}
				panic(r)
			}
		}()
		w.homingHelper.DescendUntilTrigger(gcmd)
	}()
	w.probeFinish()
}

func (w *EndstopWrapper) PullProbedResults() []*result { return w.homingHelper.PullTriggerPositions() }

func (w *EndstopWrapper) EndProbeSession() {
	w.homingHelper.ClearTriggerPositions()
	if !w.stowOnEachSample {
		w.runScript(w.deactivateGcode, "deactivate_gcode")
		w.multi = "OFF"
	}
}

// PrinterProbe is the probe of [probe] (homing.Prober and Probe).
type PrinterProbe struct {
	printer      *host.Printer
	probeOffsets *OffsetsHelper
	paramHelper  *ParameterHelper
	mcuProbe     *EndstopWrapper
	probeSession *SampleAveragingHelper
	cmdHelper    *CommandHelper
}

func load(config *configfile.Wrapper) any {
	p := &PrinterProbe{printer: host.Of(config)}
	p.probeOffsets = NewOffsetsHelper(config)
	p.paramHelper = NewParameterHelper(config)
	p.mcuProbe = NewEndstopWrapper(config, p.probeOffsets, p.paramHelper)
	p.probeSession = NewSampleAveragingHelper(config, p.paramHelper, p.mcuProbe.StartProbeSession)
	p.cmdHelper = NewCommandHelper(config, p, p.mcuProbe.QueryEndstop, true)
	NewHomingViaProbeHelper(config, p.probeOffsets.Offsets(nil)[2], p.mcuProbe.QueryEndstop)
	return p
}

func init() { host.RegisterModule("probe", load, nil) }

func (p *PrinterProbe) ProbeParams(gcmd *gcode.Command) Params {
	return p.paramHelper.ProbeParams(gcmd)
}
func (p *PrinterProbe) Offsets(gcmd *gcode.Command) [3]float64  { return p.probeOffsets.Offsets(gcmd) }
func (p *PrinterProbe) Status(eventtime float64) map[string]any { return p.cmdHelper.Status(eventtime) }
func (p *PrinterProbe) StartProbeSession(gcmd *gcode.Command) homing.ProbeSession {
	return p.probeSession.StartProbeSession(gcmd)
}
