// Package manual_probe finds a Z height with the paper test: MANUAL_PROBE, Z_ENDSTOP_CALIBRATE,
// and the TESTZ, ACCEPT and ABORT commands. It has the result type of the probes.
//
// Ported to Go from klippy/extras/manual_probe.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2019-2026  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package manual_probe

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/pyfmt"
	"printdeck/internal/klipper/reactor"
	"printdeck/internal/klipper/stepper"
	"printdeck/internal/klipper/toolhead"
)

// ProbeResult is a probe result. If the toolhead goes to [BedX, BedY] and descends, the nozzle
// touches the bed at a toolhead Z of BedZ. The probe test was at [TestX, TestY, TestZ]. All
// coordinates are in the frame of the config file.
type ProbeResult struct {
	BedX_, BedY_, BedZ_ float64
	TestX, TestY, TestZ float64
}

// String is Python's repr() of the namedtuple, for the log.
func (r *ProbeResult) String() string {
	f := pyfmt.Float
	return fmt.Sprintf("probe_result(bed_x=%s, bed_y=%s, bed_z=%s, test_x=%s, test_y=%s, test_z=%s)",
		f(r.BedX_), f(r.BedY_), f(r.BedZ_), f(r.TestX), f(r.TestY), f(r.TestZ))
}

// ReprList is Python's repr() of a list of probe results, for the log.
func ReprList(positions []*ProbeResult) string {
	parts := make([]string, len(positions))
	for i, p := range positions {
		parts[i] = p.String()
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func (r *ProbeResult) BedX() float64 { return r.BedX_ }
func (r *ProbeResult) BedY() float64 { return r.BedY_ }
func (r *ProbeResult) BedZ() float64 { return r.BedZ_ }

// CreateProbeResult makes a probe result from a test position and the probe offsets.
func CreateProbeResult(testPos []float64, offsets [3]float64) *ProbeResult {
	return &ProbeResult{testPos[0] + offsets[0], testPos[1] + offsets[1], testPos[2] - offsets[2],
		testPos[0], testPos[1], testPos[2]}
}

// LookupZEndstopConfig returns the config section of the Z stepper, or nil.
func LookupZEndstopConfig(config *configfile.Wrapper) *configfile.Wrapper {
	if config.HasSection("stepper_z") {
		return config.GetSection("stepper_z")
	}
	for _, c := range config.PrefixSections("carriage ") {
		f := strings.Fields(c.Name())
		carriageName := strings.TrimSpace(f[len(f)-1])
		if c.Get("axis", configfile.Default(carriageName), configfile.NoteValid(false)) == "z" {
			return c
		}
	}
	return nil
}

// ManualMethod is a probe method of another module, for MANUAL_METHOD.
type ManualMethod func(gcmd *gcode.Command) *ProbeResult

// ManualProbe has the manual probe commands.
type ManualProbe struct {
	printer                      *host.Printer
	gcode                        *gcode.Dispatch
	manualMethods                map[string]ManualMethod
	zPositionEndstop             float64
	hasZEndstop                  bool
	zEndstopConfigName           string
	aEndstop, bEndstop, cEndstop *float64
	status                       map[string]any
}

type statuser interface {
	Status(eventtime float64) map[string]any
}

func optFloat(c *configfile.Wrapper, option string) *float64 {
	if v, ok := c.GetFloatOptional(option, configfile.Default(nil), configfile.NoteValid(false)); ok {
		return &v
	}
	return nil
}

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	mp := &ManualProbe{printer: printer, gcode: gcode.Of(printer), manualMethods: map[string]ManualMethod{}}
	printer.LoadObject(config, "gcode_move")
	mp.gcode.RegisterCommand("MANUAL_PROBE", mp.cmdManualProbe, false, "Start manual probe helper script")
	// Endstop value for cartesian printers with separate Z axis
	if zconfig := LookupZEndstopConfig(config); zconfig != nil {
		if v := optFloat(zconfig, "position_endstop"); v != nil {
			mp.zPositionEndstop, mp.hasZEndstop = *v, true
		}
		mp.zEndstopConfigName = zconfig.Name()
	}
	// Endstop values for linear delta printers with vertical A,B,C towers
	mp.aEndstop = optFloat(config.GetSection("stepper_a"), "position_endstop")
	mp.bEndstop = optFloat(config.GetSection("stepper_b"), "position_endstop")
	mp.cEndstop = optFloat(config.GetSection("stepper_c"), "position_endstop")
	// Conditionally register appropriate commands depending on printer
	if mp.hasZEndstop {
		mp.gcode.RegisterCommand("Z_ENDSTOP_CALIBRATE", mp.cmdZEndstopCalibrate, false, "Calibrate a Z endstop")
		mp.gcode.RegisterCommand("Z_OFFSET_APPLY_ENDSTOP", mp.cmdZOffsetApplyEndstop, false,
			"Adjust the z endstop_position")
	}
	// Linear delta printers with A,B,C towers
	if config.GetSection("printer").Get("kinematics") == "delta" {
		mp.gcode.RegisterCommand("Z_OFFSET_APPLY_ENDSTOP", mp.cmdZOffsetApplyDeltaEndstops, false,
			"Adjust the z endstop_position")
	}
	mp.resetStatus()
	return mp
}

func init() { host.RegisterModule("manual_probe", load, nil) }

// RegisterManualMethod adds a probe method for MANUAL_METHOD. A nil callback removes it.
func (mp *ManualProbe) RegisterManualMethod(key string, callback ManualMethod) {
	if callback != nil {
		mp.manualMethods[key] = callback
	} else {
		delete(mp.manualMethods, key)
	}
}

// ManualMethod returns a probe method.
func (mp *ManualProbe) ManualMethod(key string) ManualMethod {
	m, ok := mp.manualMethods[key]
	if !ok {
		panic(gcode.Errorf("Not supported manual method"))
	}
	return m
}

func (mp *ManualProbe) manualProbeFinalize(r *ProbeResult) {
	if r != nil {
		mp.gcode.RespondInfo(fmt.Sprintf("Z position is %.3f", r.BedZ_), true)
	}
}

func (mp *ManualProbe) resetStatus() {
	mp.status = map[string]any{"is_active": false, "z_position": nil, "z_position_lower": nil,
		"z_position_upper": nil}
}

// Status returns the state of the manual probe.
func (mp *ManualProbe) Status(eventtime float64) map[string]any { return mp.status }

func (mp *ManualProbe) cmdManualProbe(gcmd *gcode.Command) {
	NewManualProbeHelper(mp.printer, gcmd, mp.manualProbeFinalize)
}

func (mp *ManualProbe) saveEndstop(z float64) {
	mp.gcode.RespondInfo(fmt.Sprintf("%s: position_endstop: %.3f\n"+
		"The SAVE_CONFIG command will update the printer config file\n"+
		"with the above and restart the printer.", mp.zEndstopConfigName, z), true)
	pconfig := mp.printer.LookupObject("configfile").(*configfile.PrinterConfig)
	pconfig.Set(mp.zEndstopConfigName, "position_endstop", fmt.Sprintf("%.3f", z))
}

func (mp *ManualProbe) zEndstopFinalize(r *ProbeResult) {
	if r == nil {
		return
	}
	mp.saveEndstop(mp.zPositionEndstop - r.BedZ_)
}

func (mp *ManualProbe) cmdZEndstopCalibrate(gcmd *gcode.Command) {
	NewManualProbeHelper(mp.printer, gcmd, mp.zEndstopFinalize)
}

func (mp *ManualProbe) cmdZOffsetApplyEndstop(gcmd *gcode.Command) {
	gm := mp.printer.LookupObject("gcode_move").(statuser)
	offset := gm.Status(0)["homing_origin"].(gcode.Coord).Z()
	if offset == 0 {
		mp.gcode.RespondInfo("Nothing to do: Z Offset is 0", true)
		return
	}
	mp.saveEndstop(mp.zPositionEndstop - offset)
}

func (mp *ManualProbe) cmdZOffsetApplyDeltaEndstops(gcmd *gcode.Command) {
	gm := mp.printer.LookupObject("gcode_move").(statuser)
	offset := gm.Status(0)["homing_origin"].(gcode.Coord).Z()
	pconfig := mp.printer.LookupObject("configfile").(*configfile.PrinterConfig)
	if offset == 0 {
		mp.gcode.RespondInfo("Nothing to do: Z Offset is 0", true)
		return
	}
	if mp.aEndstop == nil || mp.bEndstop == nil || mp.cEndstop == nil {
		// Klipper reads position_endstop of each tower with no default, so a tower that uses the
		// endstop of stepper_a is an internal error there too.
		panic(fmt.Errorf("TypeError: unsupported operand type(s) for -: 'NoneType' and 'float'"))
	}
	newA := *mp.aEndstop - offset
	newB := *mp.bEndstop - offset
	newC := *mp.cEndstop - offset
	mp.gcode.RespondInfo(fmt.Sprintf("stepper_a: position_endstop: %.3f\n"+
		"stepper_b: position_endstop: %.3f\n"+
		"stepper_c: position_endstop: %.3f\n"+
		"The SAVE_CONFIG command will update the printer config file\n"+
		"with the above and restart the printer.", newA, newB, newC), true)
	pconfig.Set("stepper_a", "position_endstop", fmt.Sprintf("%.3f", newA))
	pconfig.Set("stepper_b", "position_endstop", fmt.Sprintf("%.3f", newB))
	pconfig.Set("stepper_c", "position_endstop", fmt.Sprintf("%.3f", newC))
}

// VerifyNoManualProbe is a command error when a manual probe is running.
func VerifyNoManualProbe(printer *host.Printer) {
	if gcode.Of(printer).Registered("ACCEPT") {
		panic(gcode.Errorf("Already in a manual Z probe. Use ABORT to abort it."))
	}
}

// autoProbeHelper runs a probe method in place of the manual probe.
type autoProbeHelper struct {
	gcode         *gcode.Dispatch
	probeCallback ManualMethod
	gcmd          *gcode.Command
	pos           *ProbeResult
	finalize      func(*ProbeResult)
}

func newAutoProbeHelper(printer *host.Printer, cb ManualMethod, gcmd *gcode.Command, finalize func(*ProbeResult)) *autoProbeHelper {
	a := &autoProbeHelper{gcode: gcode.Of(printer), probeCallback: cb, gcmd: gcmd, finalize: finalize}
	a.gcode.RegisterCommand("NEXT", func(*gcode.Command) {
		a.gcode.UnregisterCommand("NEXT")
		a.pos = a.probeCallback(a.gcmd)
	}, false, "")
	a.gcode.RegisterCommand("ACCEPT", func(*gcode.Command) {
		a.gcode.UnregisterCommand("ACCEPT")
		a.finalize(a.pos)
	}, false, "")
	return a
}

func (a *autoProbeHelper) callback(eventtime float64) any {
	// Necessary error output happened inside run_script
	runIgnore := func(script string) {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(*gcode.CommandError); !ok {
					panic(r)
				}
			}
		}()
		a.gcode.RunScript(script)
	}
	runIgnore("NEXT")
	runIgnore("ACCEPT")
	return nil
}

const (
	zBobMinimum = 0.500
	bisectMax   = 0.200
)

// ManualProbeHelper is a manual probe: the TESTZ, ACCEPT, NEXT and ABORT commands.
type ManualProbeHelper struct {
	printer           *host.Printer
	finalizeCallback  func(*ProbeResult)
	gcode             *gcode.Dispatch
	toolhead          *toolhead.ToolHead
	manualProbe       *ManualProbe
	speed             float64
	pastPositions     []float64
	lastToolheadPos   []float64
	lastKinematicsPos []float64
	startPosition     []float64
}

// NewManualProbeHelper starts a manual probe. finalize gets the result, or nil after an abort.
func NewManualProbeHelper(printer *host.Printer, gcmd *gcode.Command, finalize func(*ProbeResult)) *ManualProbeHelper {
	h := &ManualProbeHelper{printer: printer, finalizeCallback: finalize, gcode: gcode.Of(printer),
		toolhead: toolhead.Of(printer), manualProbe: printer.LookupObject("manual_probe").(*ManualProbe)}
	h.speed = gcmd.GetFloat("SPEED", gcode.Default(5.))
	// Register commands
	VerifyNoManualProbe(printer)
	method := gcmd.GetString("MANUAL_METHOD", gcode.Default("manual"))
	if method != "manual" {
		cb := h.manualProbe.ManualMethod(method)
		aph := newAutoProbeHelper(printer, cb, gcmd, finalize)
		printer.Reactor().RegisterCallback(aph.callback, reactor.Now)
		return h
	}
	h.gcode.RegisterCommand("ACCEPT", h.cmdAccept, false, "Accept the current Z position")
	h.gcode.RegisterCommand("NEXT", h.cmdAccept, false, "")
	h.gcode.RegisterCommand("ABORT", func(*gcode.Command) { h.finalize(false) }, false, "Abort manual Z probing tool")
	h.gcode.RegisterCommand("TESTZ", h.cmdTestz, false, "Move to new Z height")
	h.gcode.RespondInfo("Starting manual Z probe. Use TESTZ to adjust position.\n"+
		"Finish with ACCEPT or ABORT command.", true)
	h.startPosition = h.toolhead.Position()
	h.reportZStatus(false, 0)
	return h
}

func (h *ManualProbeHelper) kinematicsPos() []float64 {
	toolheadPos := h.toolhead.Position()
	if h.lastToolheadPos != nil && slices.Equal(toolheadPos, h.lastToolheadPos) {
		return h.lastKinematicsPos
	}
	h.toolhead.FlushStepGeneration()
	kin := h.toolhead.Kin()
	kinSpos := map[string]float64{}
	for _, s := range kin.Steppers() {
		ms := s.(*stepper.MCUStepper)
		kinSpos[ms.Name()] = ms.CommandedPosition()
	}
	kinPos := kin.CalcPosition(kinSpos)
	h.lastToolheadPos = toolheadPos
	h.lastKinematicsPos = kinPos
	return kinPos
}

func (h *ManualProbeHelper) moveZ(zPos float64) {
	curpos := h.toolhead.Position()
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(*gcode.CommandError); ok {
				h.finalize(false)
			}
			panic(r)
		}
	}()
	zBobPos := zPos + zBobMinimum
	nan := math.NaN()
	if curpos[2] < zBobPos {
		h.toolhead.ManualMove([]float64{nan, nan, zBobPos}, h.speed)
	}
	h.toolhead.ManualMove([]float64{nan, nan, zPos}, h.speed)
}

func (h *ManualProbeHelper) reportZStatus(warnNoChange bool, prevPos float64) {
	// Get position
	zPos := h.kinematicsPos()[2]
	if warnNoChange && zPos == prevPos {
		h.gcode.RespondInfo("WARNING: No change in position (reached stepper resolution)", true)
	}
	// Find recent positions that were tested
	pp := h.pastPositions
	next := sort.SearchFloat64s(pp, zPos)
	prev := next - 1
	if next < len(pp) && pp[next] == zPos {
		next++
	}
	var prevVal, nextVal any
	prevStr, nextStr := "??????", "??????"
	if prev >= 0 {
		prevVal = pp[prev]
		prevStr = fmt.Sprintf("%.3f", pp[prev])
	}
	if next < len(pp) {
		nextVal = pp[next]
		nextStr = fmt.Sprintf("%.3f", pp[next])
	}
	h.manualProbe.status = map[string]any{"is_active": true, "z_position": zPos, "z_position_lower": prevVal,
		"z_position_upper": nextVal}
	// Find recent positions
	h.gcode.RespondInfo(fmt.Sprintf("Z position: %s --> %.3f <-- %s", prevStr, zPos, nextStr), true)
}

func (h *ManualProbeHelper) cmdAccept(gcmd *gcode.Command) {
	pos := h.toolhead.Position()
	start := h.startPosition
	if pos[0] != start[0] || pos[1] != start[1] || pos[2] >= start[2] {
		gcmd.RespondInfo("Manual probe failed! Use TESTZ commands to position the\n"+
			"nozzle prior to running ACCEPT.", true)
		h.finalize(false)
		return
	}
	h.finalize(true)
}

func (h *ManualProbeHelper) cmdTestz(gcmd *gcode.Command) {
	// Store current position for later reference
	zPos := h.kinematicsPos()[2]
	pp := h.pastPositions
	insertPos := sort.SearchFloat64s(pp, zPos)
	if insertPos >= len(pp) || pp[insertPos] != zPos {
		pp = slices.Insert(pp, insertPos, zPos)
		h.pastPositions = pp
	}
	// Determine next position to move to
	req := gcmd.GetString("Z")
	var nextZPos float64
	switch req {
	case "+", "++":
		checkZ := 9999999999999.9
		if insertPos < len(pp)-1 {
			checkZ = pp[insertPos+1]
		}
		if req == "+" {
			checkZ = (checkZ + zPos) / 2.
		}
		nextZPos = math.Min(checkZ, zPos+bisectMax)
	case "-", "--":
		checkZ := -9999999999999.9
		if insertPos > 0 {
			checkZ = pp[insertPos-1]
		}
		if req == "-" {
			checkZ = (checkZ + zPos) / 2.
		}
		nextZPos = math.Max(checkZ, zPos-bisectMax)
	default:
		nextZPos = zPos + gcmd.GetFloat("Z")
	}
	// Move to given position and report it
	h.moveZ(nextZPos)
	h.reportZStatus(nextZPos != zPos, zPos)
}

func (h *ManualProbeHelper) finalize(success bool) {
	h.manualProbe.resetStatus()
	for _, c := range []string{"ACCEPT", "NEXT", "ABORT", "TESTZ"} {
		h.gcode.UnregisterCommand(c)
	}
	var r *ProbeResult
	if success {
		r = CreateProbeResult(h.kinematicsPos(), [3]float64{})
	}
	h.finalizeCallback(r)
}
