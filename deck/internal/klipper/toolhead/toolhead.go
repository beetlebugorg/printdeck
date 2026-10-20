// Package toolhead plans the moves of the toolhead: the look-ahead of the junction speeds, the
// print time of each move, and the move queue of the kinematics and the extruder.
//
// Ported to Go from klippy/toolhead.py of Klipper (https://github.com/Klipper3d/klipper), commit
// 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f. The DummyExtruder class of
// klippy/kinematics/extruder.py is here too, as the toolhead makes it.
//
// Common suffixes: _d is distance (in mm), _v is velocity (in mm/second), _v2 is velocity squared
// (mm^2/s^2), _t is time (in seconds), _r is ratio (scalar between 0.0 and 1.0).
//
//	Copyright (C) 2016-2025  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package toolhead

import (
	"fmt"
	"log"
	"math"
	"runtime/debug"
	"sync"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/motion_queuing"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/motion"
	"printdeck/internal/klipper/pins"
	"printdeck/internal/klipper/reactor"
)

// ExtraAxis is an axis after X, Y and Z: the extruder, or another axis of a module.
type ExtraAxis interface {
	Name() string
	CheckMove(move *Move, eIndex int)
	CalcJunction(prevMove, move *Move, eIndex int) float64
	ProcessMove(printTime float64, move *Move, eIndex int)
}

// Kinematics is the kinematics of the toolhead.
type Kinematics interface {
	Steppers() []any
	CalcPosition(stepperPositions map[string]float64) []float64
	SetPosition(newpos []float64, homingAxes string)
	ClearHomingState(clearAxes string)
	CheckMove(move *Move)
	Status(eventtime float64) map[string]any
}

// KinematicsFunc makes the kinematics of the [printer] section.
type KinematicsFunc func(toolhead *ToolHead, config *configfile.Wrapper) Kinematics

var (
	kinematicsMu sync.Mutex
	kinematics   = map[string]KinematicsFunc{}
)

// RegisterKinematics adds a kinematics type (a module of klippy/kinematics). Call it from an init
// function.
func RegisterKinematics(name string, load KinematicsFunc) {
	kinematicsMu.Lock()
	defer kinematicsMu.Unlock()
	kinematics[name] = load
}

// Move is a move request (the Move class).
type Move struct {
	toolhead          *ToolHead
	StartPos, EndPos  []float64
	Accel             float64
	JunctionDeviation float64
	TimingCallbacks   []func(printTime float64)
	IsKinematicMove   bool
	AxesD             []float64
	MoveD             float64
	AxesR             []float64
	MinMoveT          float64
	// Junction speeds are tracked in velocity squared. The delta_v2 is the maximum amount of this
	// squared-velocity that can change in this move.
	MaxStartV2     float64
	MaxCruiseV2    float64
	DeltaV2        float64
	NextJunctionV2 float64
	// Setup for minimum_cruise_ratio checks
	MaxMCRStartV2 float64
	MCRDeltaV2    float64
	// Set by SetJunction
	StartV, CruiseV, EndV   float64
	AccelT, CruiseT, DecelT float64
}

// NewMove makes a move from startPos to endPos at a speed.
func NewMove(th *ToolHead, startPos, endPos []float64, speed float64) *Move {
	m := &Move{toolhead: th, StartPos: append([]float64(nil), startPos...),
		EndPos: append([]float64(nil), endPos...), Accel: th.maxAccel, JunctionDeviation: th.junctionDeviation,
		IsKinematicMove: true}
	velocity := math.Min(speed, th.maxVelocity)
	m.AxesD = make([]float64, len(startPos))
	for i := range startPos {
		m.AxesD[i] = endPos[i] - startPos[i]
	}
	axesD := m.AxesD
	m.MoveD = math.Sqrt(axesD[0]*axesD[0] + axesD[1]*axesD[1] + axesD[2]*axesD[2])
	var invMoveD float64
	if m.MoveD < .000000001 {
		// Extrude only move
		copy(m.EndPos[:3], startPos[:3])
		axesD[0], axesD[1], axesD[2] = 0., 0., 0.
		m.MoveD = 0.
		for _, ad := range axesD[3:] {
			m.MoveD = math.Max(m.MoveD, math.Abs(ad))
		}
		invMoveD = 0.
		if m.MoveD != 0 {
			invMoveD = 1. / m.MoveD
		}
		m.Accel = 99999999.9
		velocity = speed
		m.IsKinematicMove = false
	} else {
		invMoveD = 1. / m.MoveD
	}
	m.AxesR = make([]float64, len(axesD))
	for i, d := range axesD {
		m.AxesR[i] = d * invMoveD
	}
	m.MinMoveT = m.MoveD / velocity
	m.MaxStartV2 = 0.
	m.MaxCruiseV2 = velocity * velocity
	m.DeltaV2 = 2.0 * m.MoveD * m.Accel
	m.NextJunctionV2 = 999999999.9
	m.MaxMCRStartV2 = 0.
	m.MCRDeltaV2 = 2.0 * m.MoveD * th.mcrPseudoAccel
	return m
}

// LimitSpeed limits the speed and the acceleration of the move.
func (m *Move) LimitSpeed(speed, accel float64) {
	speed2 := speed * speed
	if speed2 < m.MaxCruiseV2 {
		m.MaxCruiseV2 = speed2
		m.MinMoveT = m.MoveD / speed
	}
	m.Accel = math.Min(m.Accel, accel)
	m.DeltaV2 = 2.0 * m.MoveD * m.Accel
	m.MCRDeltaV2 = math.Min(m.MCRDeltaV2, m.DeltaV2)
}

// LimitNextJunctionSpeed limits the speed at the end of the move.
func (m *Move) LimitNextJunctionSpeed(speed float64) {
	m.NextJunctionV2 = math.Min(m.NextJunctionV2, speed*speed)
}

// MoveError returns the command error of a move out of range. An empty msg is "Move out of
// range".
func (m *Move) MoveError(msg string) *gcode.CommandError {
	if msg == "" {
		msg = "Move out of range"
	}
	ep := m.EndPos
	return gcode.Errorf("%s: %.3f %.3f %.3f [%.3f]", msg, ep[0], ep[1], ep[2], ep[3])
}

// calcJunction finds the maximum start speed of the move after prevMove.
func (m *Move) calcJunction(prevMove *Move) {
	if !m.IsKinematicMove || !prevMove.IsKinematicMove {
		return
	}
	// Allow extra axes to calculate maximum junction
	maxStartV2 := math.Min(math.Min(m.MaxCruiseV2, prevMove.MaxCruiseV2),
		math.Min(prevMove.NextJunctionV2, prevMove.MaxStartV2+prevMove.DeltaV2))
	for eIndex, ea := range m.toolhead.extraAxes {
		maxStartV2 = math.Min(maxStartV2, ea.CalcJunction(prevMove, m, eIndex+3))
	}
	// Find max velocity using "approximated centripetal velocity"
	axesR := m.AxesR
	prevAxesR := prevMove.AxesR
	junctionCosTheta := -(axesR[0]*prevAxesR[0] + axesR[1]*prevAxesR[1] + axesR[2]*prevAxesR[2])
	sinThetaD2 := math.Sqrt(math.Max(0.5*(1.0-junctionCosTheta), 0.))
	cosThetaD2 := math.Sqrt(math.Max(0.5*(1.0+junctionCosTheta), 0.))
	oneMinusSinThetaD2 := 1. - sinThetaD2
	if oneMinusSinThetaD2 > 0. && cosThetaD2 > 0. {
		rJD := sinThetaD2 / oneMinusSinThetaD2
		moveJDV2 := rJD * m.JunctionDeviation * m.Accel
		pmoveJDV2 := rJD * prevMove.JunctionDeviation * prevMove.Accel
		// Approximated circle must contact moves no further than mid-move
		//   centripetal_v2 = .5 * self.move_d * self.accel * tan_theta_d2
		quarterTanThetaD2 := .25 * sinThetaD2 / cosThetaD2
		moveCentripetalV2 := m.DeltaV2 * quarterTanThetaD2
		pmoveCentripetalV2 := prevMove.DeltaV2 * quarterTanThetaD2
		maxStartV2 = math.Min(math.Min(math.Min(maxStartV2, moveJDV2), math.Min(pmoveJDV2, moveCentripetalV2)),
			pmoveCentripetalV2)
	}
	// Apply limits
	m.MaxStartV2 = maxStartV2
	m.MaxMCRStartV2 = math.Min(maxStartV2, prevMove.MaxMCRStartV2+prevMove.MCRDeltaV2)
}

// setJunction sets the speeds and the times of the acceleration, cruise and deceleration.
func (m *Move) setJunction(startV2, cruiseV2, endV2 float64) {
	// Determine accel, cruise, and decel portions of the move distance
	halfInvAccel := .5 / m.Accel
	accelD := (cruiseV2 - startV2) * halfInvAccel
	decelD := (cruiseV2 - endV2) * halfInvAccel
	cruiseD := m.MoveD - accelD - decelD
	// Determine move velocities
	m.StartV = math.Sqrt(startV2)
	m.CruiseV = math.Sqrt(cruiseV2)
	m.EndV = math.Sqrt(endV2)
	// Determine time spent in each portion of move (time is the distance divided by average
	// velocity)
	m.AccelT = accelD / ((m.StartV + m.CruiseV) * 0.5)
	m.CruiseT = cruiseD / m.CruiseV
	m.DecelT = decelD / ((m.EndV + m.CruiseV) * 0.5)
}

const lookaheadFlushTime = 0.150

// lookAheadQueue is the queue of moves of the look-ahead (LookAheadQueue).
type lookAheadQueue struct {
	queue         []*Move
	junctionFlush float64
}

func (q *lookAheadQueue) reset() {
	q.queue = nil
	q.junctionFlush = lookaheadFlushTime
}

func (q *lookAheadQueue) setFlushTime(t float64) { q.junctionFlush = t }
func (q *lookAheadQueue) isEmpty() bool          { return len(q.queue) == 0 }

func (q *lookAheadQueue) last() *Move {
	if len(q.queue) == 0 {
		return nil
	}
	return q.queue[len(q.queue)-1]
}

type junctionInfo struct {
	move              *Move
	startV2, cruiseV2 float64
	hasCruiseV2       bool
	nextStartV2       float64
}

// flush sets the speeds of the moves, and returns the moves that are ready. With lazy, it keeps
// the moves that a next move can still change.
func (q *lookAheadQueue) flush(lazy bool) []*Move {
	q.junctionFlush = lookaheadFlushTime
	updateFlushCount := lazy
	queue := q.queue
	flushCount := len(queue)
	// Traverse queue from last to first move and determine maximum junction speed assuming the
	// robot comes to a complete stop after the last move.
	info := make([]junctionInfo, flushCount)
	nextStartV2, nextMCRStartV2, peakCruiseV2 := 0., 0., 0.
	pendingCV2Assign := 0
	for i := flushCount - 1; i >= 0; i-- {
		move := queue[i]
		reachableStartV2 := nextStartV2 + move.DeltaV2
		startV2 := math.Min(move.MaxStartV2, reachableStartV2)
		var cruiseV2 float64
		hasCruiseV2 := false
		pendingCV2Assign++
		reachMCRStartV2 := nextMCRStartV2 + move.MCRDeltaV2
		mcrStartV2 := math.Min(move.MaxMCRStartV2, reachMCRStartV2)
		if mcrStartV2 < reachMCRStartV2 {
			// It's possible for this move to accelerate
			if mcrStartV2+move.MCRDeltaV2 > nextMCRStartV2 || pendingCV2Assign > 1 {
				// This move can both accel and decel, or this is a full accel move followed by a
				// full decel move
				if updateFlushCount && peakCruiseV2 != 0 {
					flushCount = i + pendingCV2Assign
					updateFlushCount = false
				}
				peakCruiseV2 = (mcrStartV2 + reachMCRStartV2) * .5
			}
			cruiseV2 = math.Min(math.Min((startV2+reachableStartV2)*.5, move.MaxCruiseV2), peakCruiseV2)
			hasCruiseV2 = true
			pendingCV2Assign = 0
		}
		info[i] = junctionInfo{move, startV2, cruiseV2, hasCruiseV2, nextStartV2}
		nextStartV2 = startV2
		nextMCRStartV2 = mcrStartV2
	}
	if updateFlushCount || flushCount == 0 {
		return nil
	}
	// Traverse queue in forward direction to propagate cruise_v2
	prevCruiseV2 := 0.
	for i := 0; i < flushCount; i++ {
		ji := info[i]
		cruiseV2 := ji.cruiseV2
		if !ji.hasCruiseV2 {
			// This move can't accelerate - propagate cruise_v2 from previous
			cruiseV2 = math.Min(prevCruiseV2, ji.startV2)
		}
		ji.move.setJunction(math.Min(ji.startV2, cruiseV2), cruiseV2, math.Min(ji.nextStartV2, cruiseV2))
		prevCruiseV2 = cruiseV2
	}
	// Remove processed moves from the queue
	res := append([]*Move(nil), queue[:flushCount]...)
	q.queue = append([]*Move(nil), queue[flushCount:]...)
	return res
}

// addMove adds a move, and reports that the queue reached its flush time.
func (q *lookAheadQueue) addMove(move *Move) bool {
	q.queue = append(q.queue, move)
	if len(q.queue) == 1 {
		return false
	}
	move.calcJunction(q.queue[len(q.queue)-2])
	q.junctionFlush -= move.MinMoveT
	// Check if enough moves have been queued to reach the target flush time.
	return q.junctionFlush <= 0.
}

const (
	bufferTimeHigh  = 1.0
	bufferTimeStart = 0.250
	primingCmdTime  = 0.100
)

// ToolHead tracks the events and their timing on the printer toolhead.
type ToolHead struct {
	printer              *host.Printer
	reactor              *reactor.Reactor
	mcu                  *mcu.MCU
	lookahead            lookAheadQueue
	commandedPos         []float64
	maxVelocity          float64
	maxAccel             float64
	minCruiseRatio       float64
	squareCornerVelocity float64
	junctionDeviation    float64
	mcrPseudoAccel       float64
	// Input stall detection
	checkStallTime float64
	printStall     int
	// Input pause tracking
	canPause       bool
	needCheckPause float64
	// Print time tracking
	printTime           float64
	specialQueuingState string
	primingTimer        *reactor.Timer
	// Setup for generating moves
	motionQueuing   *motion_queuing.PrinterMotionQueuing
	trapq           *motion.TrapQ
	extraAxes       []ExtraAxis
	extraAxesStatus map[string]int
	kin             Kinematics
}

func newToolHead(config *configfile.Wrapper) *ToolHead {
	printer := host.Of(config)
	th := &ToolHead{printer: printer, reactor: printer.Reactor(), mcu: printer.LookupObject("mcu").(*mcu.MCU),
		commandedPos: []float64{0., 0., 0., 0.}, canPause: true, needCheckPause: -1.,
		specialQueuingState: "NeedPrime"}
	th.lookahead.reset()
	th.lookahead.setFlushTime(bufferTimeHigh)
	// Velocity and acceleration control
	th.maxVelocity = config.GetFloat("max_velocity", configfile.Above(0.))
	th.maxAccel = config.GetFloat("max_accel", configfile.Above(0.))
	th.minCruiseRatio = config.GetFloat("minimum_cruise_ratio", configfile.Default(0.5), configfile.Below(1.),
		configfile.Min(0.))
	th.squareCornerVelocity = config.GetFloat("square_corner_velocity", configfile.Default(5.), configfile.Min(0.))
	th.calcJunctionDeviation()
	if th.mcu.IsFileoutput() {
		th.canPause = false
	}
	// Setup for generating moves
	th.motionQueuing = motion_queuing.Load(config)
	th.motionQueuing.RegisterFlushCallback(th.handleStepFlush, true)
	th.trapq = th.motionQueuing.AllocateTrapQ()
	// Create kinematics class
	th.extraAxes = []ExtraAxis{&DummyExtruder{printer: printer}}
	th.buildExtraAxesStatus()
	kinName := config.Get("kinematics")
	kinematicsMu.Lock()
	load, ok := kinematics[kinName]
	kinematicsMu.Unlock()
	if !ok {
		log.Printf("Error loading kinematics '%s'", kinName)
		configfile.Raise("Error loading kinematics '%s'", kinName)
	}
	th.kin = loadKinematics(load, th, config, kinName)
	// Register handlers
	printer.RegisterEventHandler("klippy:shutdown", func(...any) { th.handleShutdown() })
	return th
}

// loadKinematics makes the kinematics. A config or pin error goes up; another error is the
// config error of Klipper.
func loadKinematics(load KinematicsFunc, th *ToolHead, config *configfile.Wrapper, kinName string) (kin Kinematics) {
	defer func() {
		if r := recover(); r != nil {
			switch r.(type) {
			case *configfile.Error, *pins.Error:
				panic(r)
			}
			msg := fmt.Sprintf("Error loading kinematics '%s'", kinName)
			log.Printf("%s: %v\n%s", msg, r, debug.Stack())
			configfile.Raise("%s", msg)
		}
	}()
	return load(th, config)
}

// Print time tracking

func (th *ToolHead) advanceMoveTime(nextPrintTime float64) {
	th.printTime = math.Max(th.printTime, nextPrintTime)
}

func (th *ToolHead) calcPrintTime() {
	curtime := th.reactor.Monotonic()
	estPrintTime := th.mcu.EstimatedPrintTime(curtime)
	kinTime := th.motionQueuing.CalcStepGenRestart(estPrintTime)
	minPrintTime := math.Max(estPrintTime+bufferTimeStart, kinTime)
	if minPrintTime > th.printTime {
		th.printTime = minPrintTime
		th.printer.SendEvent("toolhead:sync_print_time", curtime, estPrintTime, th.printTime)
	}
}

func (th *ToolHead) processLookahead(lazy bool) {
	moves := th.lookahead.flush(lazy)
	if len(moves) == 0 {
		return
	}
	// Resync print_time if necessary
	if th.specialQueuingState != "" {
		// Transition from "NeedPrime"/"Priming" state to main state
		th.specialQueuingState = ""
		th.needCheckPause = -1.
		th.calcPrintTime()
	}
	// Queue moves into trapezoid motion queue (trapq)
	nextMoveTime := th.printTime
	th.reactor.AssertNoPause(func() {
		for _, move := range moves {
			if move.IsKinematicMove {
				th.trapq.Append(nextMoveTime, move.AccelT, move.CruiseT, move.DecelT, move.StartPos[0],
					move.StartPos[1], move.StartPos[2], move.AxesR[0], move.AxesR[1], move.AxesR[2],
					move.StartV, move.CruiseV, move.Accel)
			}
			for eIndex, ea := range th.extraAxes {
				if move.AxesD[eIndex+3] != 0 {
					ea.ProcessMove(nextMoveTime, move, eIndex+3)
				}
			}
			nextMoveTime = nextMoveTime + move.AccelT + move.CruiseT + move.DecelT
			for _, cb := range move.TimingCallbacks {
				cb(nextMoveTime)
			}
		}
	})
	// Generate steps for moves
	th.advanceMoveTime(nextMoveTime)
	th.motionQueuing.NoteMCUMovequeueActivity(nextMoveTime, true)
}

func (th *ToolHead) flushLookahead(isRunout bool) {
	// Transit from "NeedPrime"/"Priming"/main state to "NeedPrime"
	prevPrintTime := th.printTime
	th.processLookahead(false)
	th.specialQueuingState = "NeedPrime"
	th.needCheckPause = -1.
	th.lookahead.setFlushTime(bufferTimeHigh)
	th.checkStallTime = 0.
	if isRunout && prevPrintTime != th.printTime {
		th.checkStallTime = th.printTime
	}
}

func (th *ToolHead) handleStepFlush(flushTime, stepGenTime float64) {
	if th.specialQueuingState != "" {
		return
	}
	// In "main" state - flush lookahead if buffer runs low
	kinFlushDelay := th.motionQueuing.KinFlushDelay()
	if stepGenTime >= th.printTime-kinFlushDelay-0.001 {
		th.flushLookahead(true)
	}
}

// FlushStepGeneration sends all the moves and their steps.
func (th *ToolHead) FlushStepGeneration() {
	th.flushLookahead(false)
	th.motionQueuing.FlushAllSteps()
}

// LastMoveTime returns the print time of the end of the queued moves.
func (th *ToolHead) LastMoveTime() float64 {
	if th.specialQueuingState != "" {
		th.flushLookahead(false)
		th.calcPrintTime()
	} else {
		th.processLookahead(false)
	}
	return th.printTime
}

func (th *ToolHead) primingHandler(eventtime float64) float64 {
	th.reactor.UnregisterTimer(th.primingTimer)
	th.primingTimer = nil
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Exception in priming_handler: %v\n%s", r, debug.Stack())
				th.printer.InvokeShutdown("Exception in priming_handler", nil)
			}
		}()
		if th.specialQueuingState == "Priming" {
			th.flushLookahead(true)
		}
	}()
	return reactor.Never
}

func (th *ToolHead) checkPrimingState(eventtime float64) {
	if th.lookahead.isEmpty() {
		// In "NeedPrime" state and can remain there
		return
	}
	estPrintTime := th.mcu.EstimatedPrintTime(eventtime)
	if th.checkStallTime != 0 {
		// Was in "NeedPrime" state and got there from idle input
		if estPrintTime < th.checkStallTime {
			th.printStall++
		}
		th.checkStallTime = 0.
	}
	// Transition from "NeedPrime"/"Priming" state to "Priming" state
	th.specialQueuingState = "Priming"
	th.needCheckPause = -1.
	if th.primingTimer == nil {
		th.primingTimer = th.reactor.RegisterTimer(th.primingHandler, reactor.Never)
	}
	willPauseTime := th.printTime - estPrintTime - bufferTimeHigh
	wtime := eventtime + math.Max(0., willPauseTime) + primingCmdTime
	th.reactor.UpdateTimer(th.primingTimer, wtime)
}

func (th *ToolHead) checkPause() {
	eventtime := th.reactor.Monotonic()
	if th.specialQueuingState != "" {
		// In "NeedPrime"/"Priming" state - update priming expiration timer
		th.checkPrimingState(eventtime)
	}
	// Check if there are lots of queued moves and pause if so
	didPause := false
	for {
		estPrintTime := th.mcu.EstimatedPrintTime(eventtime)
		pauseTime := th.printTime - estPrintTime - bufferTimeHigh
		if pauseTime <= 0. {
			break
		}
		if !th.canPause {
			th.needCheckPause = reactor.Never
			return
		}
		pauseTime = math.Max(.005, math.Min(1., pauseTime))
		eventtime = th.reactor.Pause(eventtime + pauseTime)
		didPause = true
	}
	if th.specialQueuingState == "" {
		// In main state - defer pause checking
		th.needCheckPause = th.printTime
		if !didPause {
			// May be falling behind - yield to avoid starving other tasks
			th.reactor.Pause(reactor.Now)
		}
	}
}

// Movement commands

// Position returns the commanded position: X, Y, Z, E and the extra axes.
func (th *ToolHead) Position() []float64 { return append([]float64(nil), th.commandedPos...) }

// SetPosition sets the position of the toolhead and the kinematics. homingAxes are the axes that
// are homed now ("xyz" or a part).
func (th *ToolHead) SetPosition(newpos []float64, homingAxes string) {
	th.FlushStepGeneration()
	th.trapq.SetPosition(th.printTime, newpos[0], newpos[1], newpos[2])
	copy(th.commandedPos[:3], newpos[:3])
	th.kin.SetPosition(newpos, homingAxes)
	th.printer.SendEvent("toolhead:set_position")
}

// LimitNextJunctionSpeed limits the speed at the end of the last move.
func (th *ToolHead) LimitNextJunctionSpeed(speed float64) {
	if last := th.lookahead.last(); last != nil {
		last.LimitNextJunctionSpeed(speed)
	}
}

// Move queues a move to a new position at a speed.
func (th *ToolHead) Move(newpos []float64, speed float64) {
	move := NewMove(th, th.commandedPos, newpos, speed)
	if move.MoveD == 0 {
		return
	}
	if move.IsKinematicMove {
		th.kin.CheckMove(move)
	}
	for eIndex, ea := range th.extraAxes {
		if move.AxesD[eIndex+3] != 0 {
			ea.CheckMove(move, eIndex+3)
		}
	}
	copy(th.commandedPos, move.EndPos)
	if th.lookahead.addMove(move) {
		th.processLookahead(true)
	}
	if th.printTime > th.needCheckPause {
		th.checkPause()
	}
}

// ManualMove moves the axes of coord that are not NaN, at a speed.
func (th *ToolHead) ManualMove(coord []float64, speed float64) {
	curpos := append([]float64(nil), th.commandedPos...)
	for i, c := range coord {
		if !math.IsNaN(c) {
			curpos[i] = c
		}
	}
	th.Move(curpos, speed)
	th.printer.SendEvent("toolhead:manual_move")
}

// Dwell waits a time after the queued moves.
func (th *ToolHead) Dwell(delay float64) {
	th.flushLookahead(false)
	nextPrintTime := th.LastMoveTime() + math.Max(0., delay)
	th.advanceMoveTime(nextPrintTime)
	th.checkPause()
}

// WaitMoves waits until the queued moves are done.
func (th *ToolHead) WaitMoves() {
	th.flushLookahead(false)
	eventtime := th.reactor.Monotonic()
	for th.specialQueuingState == "" || th.printTime >= th.mcu.EstimatedPrintTime(eventtime) {
		if !th.canPause {
			break
		}
		eventtime = th.reactor.Pause(eventtime + 0.100)
	}
}

func (th *ToolHead) buildExtraAxesStatus() {
	th.extraAxesStatus = map[string]int{}
	for eIndex, ea := range th.extraAxes {
		if n := ea.Name(); n != "" {
			th.extraAxesStatus[n] = eIndex + 3
		}
	}
}

// SetExtruder sets the active extruder and its position.
func (th *ToolHead) SetExtruder(extruder ExtraAxis, extrudePos float64) {
	th.extraAxes[0] = extruder
	th.commandedPos[3] = extrudePos
	th.buildExtraAxesStatus()
}

// Extruder returns the active extruder.
func (th *ToolHead) Extruder() ExtraAxis { return th.extraAxes[0] }

// AddExtraAxis adds an axis after the extruder.
func (th *ToolHead) AddExtraAxis(ea ExtraAxis, axisPos float64) {
	th.flushLookahead(false)
	th.extraAxes = append(th.extraAxes, ea)
	th.commandedPos = append(th.commandedPos, axisPos)
	th.buildExtraAxesStatus()
	th.printer.SendEvent("toolhead:update_extra_axes")
}

// RemoveExtraAxis removes an extra axis.
func (th *ToolHead) RemoveExtraAxis(ea ExtraAxis) {
	th.flushLookahead(false)
	for i, o := range th.extraAxes {
		if o == ea {
			th.commandedPos = append(th.commandedPos[:i+3], th.commandedPos[i+4:]...)
			th.extraAxes = append(th.extraAxes[:i], th.extraAxes[i+1:]...)
			th.buildExtraAxesStatus()
			th.printer.SendEvent("toolhead:update_extra_axes")
			return
		}
	}
}

// ExtraAxes returns nil for X, Y and Z, and then the extra axes.
func (th *ToolHead) ExtraAxes() []any {
	out := []any{nil, nil, nil}
	for _, ea := range th.extraAxes {
		out = append(out, ea)
	}
	return out
}

// Homing "drip move" handling

func (th *ToolHead) dripLoadTrapq(submitMove *Move) (float64, float64) {
	// Queue move into trapezoid motion queue (trapq)
	if submitMove.MoveD != 0 {
		copy(th.commandedPos, submitMove.EndPos)
		th.lookahead.addMove(submitMove)
	}
	moves := th.lookahead.flush(false)
	th.calcPrintTime()
	startTime, endTime := th.printTime, th.printTime
	for _, move := range moves {
		th.trapq.Append(endTime, move.AccelT, move.CruiseT, move.DecelT, move.StartPos[0], move.StartPos[1],
			move.StartPos[2], move.AxesR[0], move.AxesR[1], move.AxesR[2], move.StartV, move.CruiseV, move.Accel)
		endTime = endTime + move.AccelT + move.CruiseT + move.DecelT
	}
	th.lookahead.reset()
	return startTime, endTime
}

// DripMove moves in small parts until the completion (a homing move).
func (th *ToolHead) DripMove(newpos []float64, speed float64, dripCompletion motion_queuing.DripCompletion) {
	// Create and verify move is valid
	pos := append(append([]float64(nil), newpos[:3]...), th.commandedPos[3:]...)
	move := NewMove(th, th.commandedPos, pos, speed)
	if move.MoveD != 0 {
		th.kin.CheckMove(move)
	}
	// Make sure stepper movement doesn't start before nominal start time
	kinFlushDelay := th.motionQueuing.KinFlushDelay()
	th.Dwell(kinFlushDelay)
	// Transmit move in "drip" mode
	th.processLookahead(false)
	startTime, endTime := th.dripLoadTrapq(move)
	th.motionQueuing.DripUpdateTime(startTime, endTime, dripCompletion)
	// Move finished; cleanup any remnants on trapq
	th.motionQueuing.WipeTrapQ(th.trapq)
}

// Misc commands

// Stats returns the statistics line of the toolhead.
func (th *ToolHead) Stats(eventtime float64) (bool, string) {
	estPrintTime := th.mcu.EstimatedPrintTime(eventtime)
	bufferTime := th.printTime - estPrintTime
	isActive := bufferTime > -60. || th.specialQueuingState == ""
	return isActive, fmt.Sprintf("print_time=%.3f buffer_time=%.3f print_stall=%d", th.printTime,
		math.Max(bufferTime, 0.), th.printStall)
}

// CheckBusy returns the print time, the estimated print time, and that the look-ahead is empty.
func (th *ToolHead) CheckBusy(eventtime float64) (float64, float64, bool) {
	return th.printTime, th.mcu.EstimatedPrintTime(eventtime), th.lookahead.isEmpty()
}

// Status returns the status of the toolhead.
func (th *ToolHead) Status(eventtime float64) map[string]any {
	res := map[string]any{}
	for k, v := range th.kin.Status(eventtime) {
		res[k] = v
	}
	var pos gcode.Coord
	copy(pos[:], th.commandedPos)
	res["print_time"] = th.printTime
	res["stalls"] = th.printStall
	res["estimated_print_time"] = th.mcu.EstimatedPrintTime(eventtime)
	res["extruder"] = th.extraAxes[0].Name()
	res["position"] = pos
	res["max_velocity"] = th.maxVelocity
	res["max_accel"] = th.maxAccel
	res["minimum_cruise_ratio"] = th.minCruiseRatio
	res["square_corner_velocity"] = th.squareCornerVelocity
	res["extra_axes"] = th.extraAxesStatus
	return res
}

func (th *ToolHead) handleShutdown() {
	th.canPause = false
	th.lookahead.reset()
}

// Kinematics returns the kinematics (as any, for the modules that use a local interface).
func (th *ToolHead) Kinematics() any { return th.kin }

// Kin returns the kinematics.
func (th *ToolHead) Kin() Kinematics { return th.kin }

// TrapQ returns the move queue of the toolhead.
func (th *ToolHead) TrapQ() *motion.TrapQ { return th.trapq }

// RegisterLookaheadCallback runs a callback at the print time of the end of the last queued move.
func (th *ToolHead) RegisterLookaheadCallback(callback func(printTime float64)) {
	last := th.lookahead.last()
	if last == nil {
		callback(th.LastMoveTime())
		return
	}
	last.TimingCallbacks = append(last.TimingCallbacks, callback)
}

// MaxVelocity returns the max velocity and acceleration.
func (th *ToolHead) MaxVelocity() (float64, float64) { return th.maxVelocity, th.maxAccel }

func (th *ToolHead) calcJunctionDeviation() {
	scv2 := th.squareCornerVelocity * th.squareCornerVelocity
	th.junctionDeviation = scv2 * (math.Sqrt(2.) - 1.) / th.maxAccel
	th.mcrPseudoAccel = th.maxAccel * (1. - th.minCruiseRatio)
}

// SetMaxVelocities sets the limits that are not NaN, and returns all four.
func (th *ToolHead) SetMaxVelocities(maxVelocity, maxAccel, squareCornerVelocity, minCruiseRatio float64) (float64, float64, float64, float64) {
	if !math.IsNaN(maxVelocity) {
		th.maxVelocity = maxVelocity
	}
	if !math.IsNaN(maxAccel) {
		th.maxAccel = maxAccel
	}
	if !math.IsNaN(squareCornerVelocity) {
		th.squareCornerVelocity = squareCornerVelocity
	}
	if !math.IsNaN(minCruiseRatio) {
		th.minCruiseRatio = minCruiseRatio
	}
	th.calcJunctionDeviation()
	return th.maxVelocity, th.maxAccel, th.squareCornerVelocity, th.minCruiseRatio
}
