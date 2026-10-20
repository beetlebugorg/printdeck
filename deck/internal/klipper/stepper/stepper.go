// Package stepper is the host side of a stepper motor: its board config, its step solver and step
// queue, and the rails of the kinematics (the steppers and endstops of an axis).
//
// Ported to Go from klippy/stepper.py of Klipper (https://github.com/Klipper3d/klipper), commit
// 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2016-2025  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package stepper

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/motion_queuing"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/motion"
	"printdeck/internal/klipper/pins"
)

// Error is an internal error of a stepper (stepper.error).
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

// Steppers

const (
	minBothEdgeDuration          = 0.000000500
	minOptimizedBothEdgeDuration = 0.000000150
	maxStepcompressError         = 0.000025
)

// MCUStepper is a stepper on a board (MCU_stepper).
type MCUStepper struct {
	name                 string
	rotationDist         float64
	stepsPerRotation     float64
	stepPulseDuration    float64
	hasStepPulseDuration bool
	unitsInRadians       bool
	stepDist             float64
	mcu                  *mcu.MCU
	oid                  int
	stepPin              string
	invertStep           int
	dirPin               string
	invertDir            bool
	origInvertDir        bool
	stepBothEdge         bool
	reqStepBothEdge      bool
	mcuPositionOffset    float64
	resetCmdTag          uint32
	getPositionCmd       *mcu.CommandQueryWrapper
	activeCallbacks      []func(printTime float64)
	activeCallbackID     int
	syncemitter          *motion.SyncEmitter
	stepqueue            *motion.StepCompress
	stepperKinematics    *motion.StepperKinematics
	trapq                *motion.TrapQ
	printer              *host.Printer
}

// NewMCUStepper makes a stepper from its pins. stepPulseDuration < 0 is no value (None).
func NewMCUStepper(config *configfile.Wrapper, stepPin, dirPin *pins.Params, rotationDist,
	stepsPerRotation, stepPulseDuration float64, unitsInRadians bool) *MCUStepper {
	m, ok := stepPin.Chip.(*mcu.MCU)
	if !ok {
		configfile.Raise("Stepper step pin must be on a board")
	}
	s := &MCUStepper{name: config.Name(), rotationDist: rotationDist, stepsPerRotation: stepsPerRotation,
		stepPulseDuration: stepPulseDuration, hasStepPulseDuration: stepPulseDuration >= 0,
		unitsInRadians: unitsInRadians, stepDist: rotationDist / stepsPerRotation, mcu: m}
	s.oid = m.CreateOID()
	m.RegisterConfigCallback(s.buildConfig)
	s.stepPin = stepPin.Pin
	s.invertStep = stepPin.Invert
	s.printer = m.Printer()
	if dirPin.Chip != stepPin.Chip {
		configfile.Raise("Stepper dir pin must be on same mcu as step pin")
	}
	s.dirPin = dirPin.Pin
	s.invertDir = dirPin.Invert != 0
	s.origInvertDir = s.invertDir
	mq := motion_queuing.Load(config)
	s.syncemitter = mq.AllocateSyncEmitter(m, s.name, true)
	s.stepqueue = s.syncemitter.StepCompress()
	s.stepqueue.SetInvertSdir(s.invertDir)
	s.printer.RegisterEventHandler("klippy:connect", func(...any) { s.queryMCUPosition() })
	return s
}

func (s *MCUStepper) MCU() *mcu.MCU { return s.mcu }

// Name is the name of the config section, for example "stepper_x" (mcu.Stepper).
func (s *MCUStepper) Name() string { return s.name }

// ShortName is the name without "stepper_", for example "x".
func (s *MCUStepper) ShortName() string {
	if strings.HasPrefix(s.name, "stepper") && len(s.name) > 8 {
		// Skip an extra symbol after 'stepper'
		return s.name[8:]
	}
	return s.name
}

// UnitsInRadians reports that the distances are in radians.
func (s *MCUStepper) UnitsInRadians() bool { return s.unitsInRadians }

// PulseDuration returns the step pulse duration (-1 for none yet) and the step on both edges.
func (s *MCUStepper) PulseDuration() (float64, bool) {
	if !s.hasStepPulseDuration {
		return -1, s.stepBothEdge
	}
	return s.stepPulseDuration, s.stepBothEdge
}

// SetupDefaultPulseDuration sets the pulse duration when the config has none.
func (s *MCUStepper) SetupDefaultPulseDuration(pulseDuration float64, stepBothEdge bool) {
	if !s.hasStepPulseDuration {
		s.stepPulseDuration, s.hasStepPulseDuration = pulseDuration, true
	}
	s.reqStepBothEdge = stepBothEdge
}

// SetupItersolve sets a new step solver.
func (s *MCUStepper) SetupItersolve(sk *motion.StepperKinematics) { s.SetStepperKinematics(sk) }

func constantInt(consts map[string]any, name string) int {
	v, ok := consts[name]
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(fmt.Sprint(v))
	if err != nil {
		panic(fmt.Errorf("invalid literal for int(): %v", v))
	}
	return n
}

type mcuCodeDeprecator interface {
	DeprecateMCUCode(mcuName, feature, mcuVersion, msg string)
}

func (s *MCUStepper) buildConfig() {
	if !s.hasStepPulseDuration {
		s.stepPulseDuration, s.hasStepPulseDuration = .000002, true
	}
	invertStep := s.invertStep
	// Check if can enable "step on both edges"
	constants := s.mcu.Constants()
	ssbe := constantInt(constants, "STEPPER_STEP_BOTH_EDGE")
	sbe := constantInt(constants, "STEPPER_BOTH_EDGE")
	sou := constantInt(constants, "STEPPER_OPTIMIZED_UNSTEP")
	wantBothEdges := s.reqStepBothEdge
	switch {
	case s.stepPulseDuration > minBothEdgeDuration:
		// If user has requested a very large step pulse duration then disable step on both edges
		// (rise and fall times may not be symmetric)
		wantBothEdges = false
	case sbe != 0 && s.stepPulseDuration > minOptimizedBothEdgeDuration:
		// Older MCU and user has requested large pulse duration
		wantBothEdges = false
	case sbe == 0 && ssbe == 0:
		// Older MCU that doesn't support step on both edges
		wantBothEdges = false
	case sou != 0:
		// MCU has optimized step/unstep - better to use that
		wantBothEdges = false
	}
	if ssbe == 0 {
		pconfig := s.printer.LookupObject("configfile").(mcuCodeDeprecator)
		version := fmt.Sprint(s.mcu.Status(0)["mcu_version"])
		pconfig.DeprecateMCUCode(s.mcu.Name(), "STEPPER_STEP_BOTH_EDGE", version, "")
	}
	if wantBothEdges {
		s.stepBothEdge = true
		invertStep = -1
		if sbe != 0 {
			// Older MCU requires setting step_pulse_ticks=0 to enable
			s.stepPulseDuration = 0.
		}
	}
	// Configure stepper object
	stepPulseTicks := s.mcu.SecondsToClock(s.stepPulseDuration)
	s.mcu.AddConfigCmd(fmt.Sprintf("config_stepper oid=%d step_pin=%s dir_pin=%s invert_step=%d step_pulse_ticks=%d",
		s.oid, s.stepPin, s.dirPin, invertStep, uint32(stepPulseTicks)), false, false)
	s.mcu.AddConfigCmd(fmt.Sprintf("reset_step_clock oid=%d clock=0", s.oid), false, true)
	stepCmdTag := s.mcu.LookupCommand("queue_step oid=%c interval=%u count=%hu add=%hi", nil).CommandTag()
	dirCmdTag := s.mcu.LookupCommand("set_next_step_dir oid=%c dir=%c", nil).CommandTag()
	s.resetCmdTag = s.mcu.LookupCommand("reset_step_clock oid=%c clock=%u", nil).CommandTag()
	s.getPositionCmd = s.mcu.LookupQueryCommand("stepper_get_position oid=%c",
		"stepper_position oid=%c pos=%i", int64(s.oid), nil, false)
	maxErrorTicks := s.mcu.SecondsToClock(maxStepcompressError)
	s.stepqueue.Fill(uint32(s.oid), uint32(maxErrorTicks), int32(stepCmdTag), int32(dirCmdTag))
}

// OID is the oid of the stepper (mcu.Stepper).
func (s *MCUStepper) OID() int { return s.oid }

// StepDist is the distance of one step.
func (s *MCUStepper) StepDist() float64 { return s.stepDist }

// RotationDistance returns the rotation distance and the steps of a rotation.
func (s *MCUStepper) RotationDistance() (float64, float64) { return s.rotationDist, s.stepsPerRotation }

// SetRotationDistance sets the rotation distance, and keeps the board position.
func (s *MCUStepper) SetRotationDistance(rotationDist float64) {
	mcuPos := s.MCUPosition()
	s.rotationDist = rotationDist
	s.stepDist = rotationDist / s.stepsPerRotation
	s.SetTrapQ(s.trapq)
	s.setMCUPosition(mcuPos)
}

// DirInverted returns the direction inversion, and the inversion of the config.
func (s *MCUStepper) DirInverted() (bool, bool) { return s.invertDir, s.origInvertDir }

// SetDirInverted sets the direction inversion.
func (s *MCUStepper) SetDirInverted(invertDir bool) {
	if invertDir == s.invertDir {
		return
	}
	s.invertDir = invertDir
	s.stepqueue.SetInvertSdir(invertDir)
	s.printer.SendEvent("stepper:set_dir_inverted", s)
}

// CalcPositionFromCoord returns the stepper position at an XYZ position.
func (s *MCUStepper) CalcPositionFromCoord(coord []float64) float64 {
	return s.stepperKinematics.CalcPositionFromCoord(coord[0], coord[1], coord[2])
}

// SetPosition sets the stepper position from an XYZ position, and keeps the board position.
func (s *MCUStepper) SetPosition(coord []float64) {
	mcuPos := s.MCUPosition()
	s.stepperKinematics.SetPosition(coord[0], coord[1], coord[2])
	s.setMCUPosition(mcuPos)
}

// CommandedPosition is the position of the step solver.
func (s *MCUStepper) CommandedPosition() float64 { return s.stepperKinematics.CommandedPos }

// MCUPosition is the board position, in steps, of the commanded position.
func (s *MCUStepper) MCUPosition() int64 { return s.MCUPositionOf(s.CommandedPosition()) }

// MCUPositionOf is the board position, in steps, of a commanded position.
func (s *MCUStepper) MCUPositionOf(cmdPos float64) int64 {
	mcuPos := (cmdPos + s.mcuPositionOffset) / s.stepDist
	if mcuPos >= 0. {
		return int64(mcuPos + 0.5)
	}
	return int64(mcuPos - 0.5)
}

func (s *MCUStepper) setMCUPosition(mcuPos int64) {
	s.mcuPositionOffset = float64(mcuPos)*s.stepDist - s.CommandedPosition()
}

// PastMCUPosition is the board position at a past print time.
func (s *MCUStepper) PastMCUPosition(printTime float64) int64 {
	clock := s.mcu.PrintTimeToClock(printTime)
	return s.stepqueue.FindPastPosition(uint64(clock))
}

// MCUToCommandedPosition converts a board position to a commanded position.
func (s *MCUStepper) MCUToCommandedPosition(mcuPos int64) float64 {
	return float64(mcuPos)*s.stepDist - s.mcuPositionOffset
}

// DumpSteps returns up to count queue_step commands of the history.
func (s *MCUStepper) DumpSteps(count int, startClock, endClock uint64) []motion.HistorySteps {
	return s.stepqueue.ExtractOld(count, startClock, endClock)
}

// StepperKinematics returns the step solver.
func (s *MCUStepper) StepperKinematics() *motion.StepperKinematics { return s.stepperKinematics }

// SetStepperKinematics sets the step solver, keeps the board position, and returns the old solver.
func (s *MCUStepper) SetStepperKinematics(sk *motion.StepperKinematics) *motion.StepperKinematics {
	old := s.stepperKinematics
	var mcuPos int64
	if old != nil {
		mcuPos = s.MCUPosition()
	}
	s.stepperKinematics = sk
	s.syncemitter.SetStepperKinematics(sk)
	s.SetTrapQ(s.trapq)
	s.setMCUPosition(mcuPos)
	return old
}

// NoteHomingEnd resets the step queue after a homing move (mcu.Stepper).
func (s *MCUStepper) NoteHomingEnd() {
	if ret := s.stepqueue.Reset(0); ret != 0 {
		panic(&Error{"Internal error in stepcompress"})
	}
	s.syncemitter.QueueMsg(0, s.resetCmdTag, uint32(s.oid), 0)
	s.queryMCUPosition()
}

func (s *MCUStepper) queryMCUPosition() {
	if s.mcu.IsFileoutput() {
		return
	}
	params := s.getPositionCmd.Send([]any{s.oid}, 0, 0, true)
	lastPos := params.Int("pos")
	if s.invertDir {
		lastPos = -lastPos
	}
	printTime := s.mcu.EstimatedPrintTime(params.Float("#receive_time"))
	clock := s.mcu.PrintTimeToClock(printTime)
	if ret := s.stepqueue.SetLastPosition(uint64(clock), lastPos); ret != 0 {
		panic(&Error{"Internal error in stepcompress"})
	}
	s.setMCUPosition(lastPos)
	s.printer.SendEvent("stepper:sync_mcu_position", s)
}

// TrapQ is the move queue of the stepper, or nil.
func (s *MCUStepper) TrapQ() *motion.TrapQ { return s.trapq }

// SetTrapQ sets the move queue, and returns the old one.
func (s *MCUStepper) SetTrapQ(tq *motion.TrapQ) *motion.TrapQ {
	s.stepperKinematics.SetTrapQ(tq, s.stepDist)
	old := s.trapq
	s.trapq = tq
	return old
}

// AddActiveCallback adds a function that runs at the first print time when the stepper moves.
func (s *MCUStepper) AddActiveCallback(cb func(printTime float64)) {
	s.activeCallbacks = append(s.activeCallbacks, cb)
	if len(s.activeCallbacks) == 1 {
		mq := s.printer.LookupObject("motion_queuing").(*motion_queuing.PrinterMotionQueuing)
		s.activeCallbackID = mq.RegisterFlushCallback(s.checkActive, false)
	}
}

func (s *MCUStepper) checkActive(mustFlushTime, maxStepGenTime float64) {
	ret := s.stepperKinematics.CheckActive(maxStepGenTime)
	if ret == 0 {
		// Stepper motor still not active
		return
	}
	// Motor is active, disable future checking
	mq := s.printer.LookupObject("motion_queuing").(*motion_queuing.PrinterMotionQueuing)
	mq.UnregisterFlushCallback(s.activeCallbackID)
	cbs := s.activeCallbacks
	s.activeCallbacks = nil
	// Invoke callbacks
	for _, cb := range cbs {
		cb(ret)
	}
}

// IsActiveAxis reports that the stepper moves on an axis ("x", "y" or "z").
func (s *MCUStepper) IsActiveAxis(axis string) bool {
	return len(axis) == 1 && s.stepperKinematics.IsActiveAxis(axis[0])
}

// stepperRegistrar is a module that keeps a list of the steppers.
type stepperRegistrar interface {
	RegisterStepper(config *configfile.Wrapper, s *MCUStepper)
}

// NewPrinterStepper makes the stepper of a config section (PrinterStepper).
func NewPrinterStepper(config *configfile.Wrapper, unitsInRadians bool) *MCUStepper {
	printer := host.Of(config)
	// Stepper definition
	ppins := printer.LookupObject("pins").(*pins.Pins)
	stepPin := config.Get("step_pin")
	stepPinParams := ppins.LookupPin(stepPin, true, false, "")
	dirPin := config.Get("dir_pin")
	dirPinParams := ppins.LookupPin(dirPin, true, false, "")
	rotationDist, stepsPerRotation := ParseStepDistance(config, &unitsInRadians, true)
	stepPulseDuration, ok := config.GetFloatOptional("step_pulse_duration", configfile.Default(nil),
		configfile.Min(0.), configfile.Max(.001))
	if !ok {
		stepPulseDuration = -1
	}
	s := NewMCUStepper(config, stepPinParams, dirPinParams, rotationDist, stepsPerRotation,
		stepPulseDuration, unitsInRadians)
	// Register with helper modules
	for _, mname := range []string{"stepper_enable", "force_move", "motion_report"} {
		m := printer.LoadObject(config, mname).(stepperRegistrar)
		m.RegisterStepper(config, s)
	}
	return s
}

// ParseGearRatio returns the gear ratio of the gear_ratio option, or 1.
func ParseGearRatio(config *configfile.Wrapper, noteValid bool) float64 {
	gearRatio := config.GetFloatLists("gear_ratio", []string{":", ","}, 2, configfile.Default([]any{}),
		configfile.NoteValid(noteValid))
	result := 1.
	for _, g := range gearRatio {
		result *= g[0] / g[1]
	}
	return result
}

// ParseStepDistance returns the rotation distance and the steps of a rotation. A nil
// unitsInRadians is found from the options.
func ParseStepDistance(config *configfile.Wrapper, unitsInRadians *bool, noteValid bool) (float64, float64) {
	// Check rotation_distance and gear_ratio
	var inRadians bool
	if unitsInRadians == nil {
		// Caller doesn't know if units are in radians - infer it
		_, hasRD := config.GetOptional("rotation_distance", configfile.Default(nil), configfile.NoteValid(false))
		_, hasGR := config.GetOptional("gear_ratio", configfile.Default(nil), configfile.NoteValid(false))
		inRadians = !hasRD && hasGR
	} else {
		inRadians = *unitsInRadians
	}
	var rotationDist float64
	if inRadians {
		rotationDist = 2. * math.Pi
		config.Get("gear_ratio", configfile.NoteValid(noteValid))
	} else {
		rotationDist = config.GetFloat("rotation_distance", configfile.Above(0.), configfile.NoteValid(noteValid))
	}
	// Check microsteps and full_steps_per_rotation
	microsteps := config.GetInt("microsteps", configfile.Min(1), configfile.NoteValid(noteValid))
	fullSteps := config.GetInt("full_steps_per_rotation", configfile.Default(200), configfile.Min(1),
		configfile.NoteValid(noteValid))
	if fullSteps%4 != 0 {
		configfile.Raise("full_steps_per_rotation invalid in section '%s'", config.Name())
	}
	gearing := ParseGearRatio(config, noteValid)
	return rotationDist, float64(fullSteps*microsteps) * gearing
}
