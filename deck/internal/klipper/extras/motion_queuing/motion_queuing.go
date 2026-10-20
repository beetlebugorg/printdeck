// Package motion_queuing flushes the moves of the toolhead to the step generation, and sends the
// steps of the steppers to the boards at the right times.
//
// Ported to Go from klippy/extras/motion_queuing.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2025  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package motion_queuing

import (
	"log"
	"math"
	"runtime/debug"
	"slices"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/motion"
	"printdeck/internal/klipper/reactor"
	"printdeck/internal/klipper/serialqueue"
)

const (
	bgflushLowTime    = 0.200
	bgflushHighTime   = 0.400
	bgflushSGLowTime  = 0.450
	bgflushSGHighTime = 0.700
	bgflushExtraTime  = 0.250

	moveHistoryExpire     = 30.
	minKinTime            = 0.100
	stepcompressFlushTime = 0.050
	sdsCheckTime          = 0.001 // step+dir+step filter in stepcompress.c

	dripSegmentTime = 0.050
	dripTime        = 0.100
)

// FlushCallback is called at each flush with the flush time and the step generation time.
type FlushCallback func(flushTime, stepGenTime float64)

type flushCallback struct {
	id int
	cb FlushCallback
}

type mcuSync struct {
	mcu *mcu.MCU
	ss  *motion.StepperSync
}

// PrinterMotionQueuing holds the move queues and the step syncs of the boards.
type PrinterMotionQueuing struct {
	printer          *host.Printer
	reactor          *reactor.Reactor
	trapqs           []*motion.TrapQ
	steppersyncmgr   *motion.StepperSyncMgr
	syncemitters     []*motion.SyncEmitter
	syncemitterNames map[*motion.SyncEmitter]string
	steppersyncs     []mcuSync
	clearHistoryTime float64
	flushCallbacks   []flushCallback
	nextCallbackID   int
	kinFlushDelay    float64
	allMCUs          []*mcu.MCU
	mcu              *mcu.MCU
	canPause         bool
	flushTimer       *reactor.Timer
	doKickFlushTimer bool
	lastFlushTime    float64
	lastStepGenTime  float64
	needFlushTime    float64
	needStepGenTime  float64
	dripStartTimes   []float64
}

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	mq := &PrinterMotionQueuing{printer: printer, reactor: printer.Reactor(),
		steppersyncmgr: &motion.StepperSyncMgr{}, syncemitterNames: map[*motion.SyncEmitter]string{},
		kinFlushDelay: sdsCheckTime, canPause: true, doKickFlushTimer: true}
	for _, o := range printer.LookupObjects("mcu") {
		mq.allMCUs = append(mq.allMCUs, o.Obj.(*mcu.MCU))
	}
	mq.mcu = mq.allMCUs[0]
	if mq.mcu.IsFileoutput() {
		mq.canPause = false
	}
	// Flush tracking
	flushHandler := mq.flushHandler
	if !mq.canPause {
		flushHandler = mq.flushHandlerDebug
	}
	mq.flushTimer = mq.reactor.RegisterTimer(flushHandler, reactor.Never)
	printer.RegisterEventHandler("klippy:shutdown", func(...any) { mq.canPause = false })
	return mq
}

func init() { host.RegisterModule("motion_queuing", load, nil) }

// Load returns the motion_queuing object, and loads it the first time.
func Load(config *configfile.Wrapper) *PrinterMotionQueuing {
	return host.Of(config).LoadObject(config, "motion_queuing").(*PrinterMotionQueuing)
}

// C trapq tracking

// AllocateTrapQ makes a move queue that the flushes finalize.
func (mq *PrinterMotionQueuing) AllocateTrapQ() *motion.TrapQ {
	tq := motion.NewTrapQ()
	mq.trapqs = append(mq.trapqs, tq)
	return tq
}

// WipeTrapQ moves the remaining moves of a queue to its history.
func (mq *PrinterMotionQueuing) WipeTrapQ(tq *motion.TrapQ) {
	tq.FinalizeMoves(reactor.Never, 0.)
}

// C steppersync tracking

func (mq *PrinterMotionQueuing) lookupSteppersync(m *mcu.MCU) *motion.StepperSync {
	for _, s := range mq.steppersyncs {
		if s.mcu == m {
			return s.ss
		}
	}
	ss := mq.steppersyncmgr.AllocStepperSync()
	mq.steppersyncs = append(mq.steppersyncs, mcuSync{m, ss})
	return ss
}

// AllocateSyncEmitter makes the sync emitter of a stepper (or another user of the move queue) on
// a board.
func (mq *PrinterMotionQueuing) AllocateSyncEmitter(m *mcu.MCU, name string, allocStepcompress bool) *motion.SyncEmitter {
	f := strings.Fields(name)
	sname := f[len(f)-1]
	ss := mq.lookupSteppersync(m)
	se := ss.AllocSyncEmitter(sname, allocStepcompress)
	mq.syncemitterNames[se] = name
	mq.syncemitters = append(mq.syncemitters, se)
	return se
}

// SetupMCUMovequeue sets the move queue of a board, after its config (mcu.MoveQueueSetup).
func (mq *PrinterMotionQueuing) SetupMCUMovequeue(m *mcu.MCU, sq *serialqueue.SerialQueue, moveCount int) {
	// Setup steppersync object for the mcu's main movequeue
	ss := mq.lookupSteppersync(m)
	ss.SetupMoveQueue(sq, moveCount)
	mcuFreq := float64(m.SecondsToClock(1.))
	ss.SetTime(0., mcuFreq)
}

// Stats calibrates the board clocks and the history expiration.
func (mq *PrinterMotionQueuing) Stats(eventtime float64) (bool, string) {
	// Globally calibrate mcu clocks (and step generation clocks)
	syncTime := mq.lastStepGenTime
	for _, s := range mq.steppersyncs {
		offset, freq := s.mcu.CalibrateClock(syncTime, eventtime)
		s.ss.SetTime(offset, freq)
	}
	// Calculate history expiration
	estPrintTime := mq.mcu.EstimatedPrintTime(eventtime)
	mq.clearHistoryTime = math.Max(0., estPrintTime-moveHistoryExpire)
	return false, ""
}

// Flush notification callbacks

// RegisterFlushCallback adds a flush callback, and returns its id for the removal. A callback
// that can add moves to a trapq goes first.
func (mq *PrinterMotionQueuing) RegisterFlushCallback(callback FlushCallback, canAddTrapq bool) int {
	mq.nextCallbackID++
	fc := flushCallback{mq.nextCallbackID, callback}
	fcbs := slices.Clone(mq.flushCallbacks)
	if canAddTrapq {
		mq.flushCallbacks = append([]flushCallback{fc}, fcbs...)
	} else {
		mq.flushCallbacks = append(fcbs, fc)
	}
	return fc.id
}

// UnregisterFlushCallback removes a flush callback.
func (mq *PrinterMotionQueuing) UnregisterFlushCallback(id int) {
	mq.flushCallbacks = slices.DeleteFunc(slices.Clone(mq.flushCallbacks),
		func(fc flushCallback) bool { return fc.id == id })
}

// Kinematic step generation scan window time tracking

// KinFlushDelay is the time that the step generation reads before and after a move.
func (mq *PrinterMotionQueuing) KinFlushDelay() float64 { return mq.kinFlushDelay }

// CheckStepGenerationScanWindows updates the kin flush delay from the steppers.
func (mq *PrinterMotionQueuing) CheckStepGenerationScanWindows() {
	kinFlushDelay := sdsCheckTime
	for _, se := range mq.syncemitters {
		sk := se.StepperKinematics()
		if sk == nil || sk.TQ == nil {
			continue
		}
		kinFlushDelay = math.Max(kinFlushDelay, math.Max(sk.GenStepsPreActive, sk.GenStepsPostActive))
	}
	mq.kinFlushDelay = kinFlushDelay
}

// Flush tracking

func (mq *PrinterMotionQueuing) advanceFlushTime(wantFlushTime, wantStepGenTime float64) {
	flushTime := math.Max(wantFlushTime, math.Max(mq.lastFlushTime, wantStepGenTime-stepcompressFlushTime))
	stepGenTime := math.Max(wantStepGenTime, math.Max(mq.lastStepGenTime, flushTime))
	// Invoke flush callbacks (if any)
	mq.reactor.AssertNoPause(func() {
		for _, fc := range mq.flushCallbacks {
			fc.cb(flushTime, stepGenTime)
		}
	})
	// Determine maximum history to keep
	trapqFreeTime := stepGenTime - mq.kinFlushDelay
	clearHistoryTime := mq.clearHistoryTime
	if !mq.canPause {
		clearHistoryTime = math.Max(0., trapqFreeTime-moveHistoryExpire)
	}
	// Generate stepper movement and transmit
	if failed := mq.steppersyncmgr.GenSteps(flushTime, stepGenTime, clearHistoryTime); failed != nil {
		msg := "Internal error in stepcompress"
		details := map[string]any{"queue_name": mq.syncemitterNames[failed], "flush_time": flushTime,
			"step_gen_time": stepGenTime, "last_flush_time": mq.lastFlushTime,
			"last_step_gen_time": mq.lastStepGenTime, "clear_history_time": clearHistoryTime}
		mq.printer.InvokeShutdown(msg, details)
		panic(&mcu.Error{Msg: msg})
	}
	mq.lastFlushTime = flushTime
	mq.lastStepGenTime = stepGenTime
	// Move processed trapq entries to history list, and expire old history
	for _, tq := range mq.trapqs {
		tq.FinalizeMoves(trapqFreeTime, clearHistoryTime)
	}
}

func (mq *PrinterMotionQueuing) awaitFlushTime(wantFlushTime float64) {
	for {
		if mq.lastFlushTime >= wantFlushTime || !mq.canPause {
			return
		}
		systime := mq.reactor.Monotonic()
		estPrintTime := mq.mcu.EstimatedPrintTime(systime)
		wait := wantFlushTime - bgflushHighTime - estPrintTime
		if wait <= 0. {
			return
		}
		mq.reactor.Pause(systime + math.Min(1., wait))
	}
}

// FlushAllSteps generates and sends all the steps of the moves in the queues.
func (mq *PrinterMotionQueuing) FlushAllSteps() {
	flushTime := mq.needStepGenTime
	mq.awaitFlushTime(flushTime)
	mq.advanceFlushTime(flushTime, 0.)
}

// CalcStepGenRestart returns the first print time for new moves after the steps stopped.
func (mq *PrinterMotionQueuing) CalcStepGenRestart(estPrintTime float64) float64 {
	kinTime := math.Max(estPrintTime+minKinTime, mq.lastStepGenTime)
	return kinTime + mq.kinFlushDelay
}

func (mq *PrinterMotionQueuing) recoverShutdown(name string, result *float64) {
	if r := recover(); r != nil {
		log.Printf("Exception in %s: %v\n%s", name, r, debug.Stack())
		mq.printer.InvokeShutdown("Exception in "+name, nil)
		*result = reactor.Never
	}
}

func (mq *PrinterMotionQueuing) flushHandler(eventtime float64) (result float64) {
	defer mq.recoverShutdown("flush_handler", &result)
	estPrintTime := mq.mcu.EstimatedPrintTime(eventtime)
	aggrSGTime := mq.needStepGenTime - 2.*mq.kinFlushDelay
	if mq.lastStepGenTime < aggrSGTime {
		// Actively stepping - want more aggressive flushing
		wantSGTime := estPrintTime + bgflushSGHighTime
		batchTime := bgflushSGHighTime - bgflushSGLowTime
		nextBatchTime := mq.lastStepGenTime + batchTime
		if nextBatchTime > estPrintTime {
			// Improve run-to-run reproducibility by batching from last
			if nextBatchTime > wantSGTime+0.005 {
				// Delay flushing until next wakeup
				nextBatchTime = mq.lastStepGenTime
			}
			wantSGTime = nextBatchTime
		}
		wantSGTime = math.Min(wantSGTime, aggrSGTime)
		// Flush motion queues (if needed)
		if wantSGTime > mq.lastStepGenTime {
			mq.advanceFlushTime(0., wantSGTime)
		}
	} else {
		// Not stepping (or only step remnants) - use relaxed flushing
		wantFlushTime := estPrintTime + bgflushHighTime
		maxFlushTime := mq.needFlushTime + bgflushExtraTime
		wantFlushTime = math.Min(wantFlushTime, maxFlushTime)
		// Flush motion queues (if needed)
		if wantFlushTime > mq.lastFlushTime {
			mq.advanceFlushTime(wantFlushTime, 0.)
		}
	}
	// Reschedule timer
	aggrSGTime = mq.needStepGenTime - 2.*mq.kinFlushDelay
	var waketime float64
	if mq.lastStepGenTime < aggrSGTime {
		waketime = mq.lastStepGenTime - bgflushSGLowTime
	} else {
		mq.doKickFlushTimer = true
		maxFlushTime := mq.needFlushTime + bgflushExtraTime
		if mq.lastFlushTime >= maxFlushTime {
			return reactor.Never
		}
		waketime = mq.lastFlushTime - bgflushLowTime
	}
	return eventtime + waketime - estPrintTime
}

// flushHandlerDebug is the flushing of the batch mode.
func (mq *PrinterMotionQueuing) flushHandlerDebug(eventtime float64) (result float64) {
	defer mq.recoverShutdown("flush_handler_debug", &result)
	fauxTime := mq.needFlushTime - 1.5
	batchTime := bgflushSGHighTime - bgflushSGLowTime
	flushCount := 0
	for mq.lastStepGenTime < fauxTime {
		target := mq.lastStepGenTime + batchTime
		if flushCount > 100. && fauxTime > target {
			target += float64(int((fauxTime-target)/batchTime)) * batchTime
		}
		mq.advanceFlushTime(0., target)
		flushCount++
	}
	if flushCount != 0 {
		return reactor.Now
	}
	mq.advanceFlushTime(mq.needFlushTime+bgflushExtraTime, 0.)
	mq.doKickFlushTimer = true
	return reactor.Never
}

// NoteMCUMovequeueActivity notes moves in the board queues to a print time, and starts the
// flush timer.
func (mq *PrinterMotionQueuing) NoteMCUMovequeueActivity(mqTime float64, isStepGen bool) {
	if isStepGen {
		mqTime += mq.kinFlushDelay
		mq.needStepGenTime = math.Max(mq.needStepGenTime, mqTime)
	}
	mq.needFlushTime = math.Max(mq.needFlushTime, mqTime)
	if mq.doKickFlushTimer {
		mq.doKickFlushTimer = false
		mq.reactor.UpdateTimer(mq.flushTimer, reactor.Now)
	}
}

// "Drip" timing (for homing and probing moves)

// DripCompletion is the completion that ends a drip move (an endstop trigger).
type DripCompletion interface {
	Test() bool
	Wait(waketime float64, timeoutResult any) any
}

// DripUpdateTime sends the steps of a homing move in small parts, until the completion.
func (mq *PrinterMotionQueuing) DripUpdateTime(startTime, endTime float64, dripCompletion DripCompletion) {
	mq.dripStartTimes = append(mq.dripStartTimes, startTime)
	mq.awaitFlushTime(startTime)
	// Disable background flushing from timer
	mq.reactor.UpdateTimer(mq.flushTimer, reactor.Never)
	mq.doKickFlushTimer = false
	mq.advanceFlushTime(startTime-sdsCheckTime, startTime)
	// Flush in segments until drip_completion signal
	flushTime := startTime
	for flushTime < endTime {
		if dripCompletion.Test() {
			break
		}
		curtime := mq.reactor.Monotonic()
		estPrintTime := mq.mcu.EstimatedPrintTime(curtime)
		waitTime := flushTime - estPrintTime - dripTime
		if waitTime > 0. && mq.canPause {
			// Pause before sending more steps
			dripCompletion.Wait(curtime+waitTime, nil)
			continue
		}
		flushTime = math.Min(flushTime+dripSegmentTime, endTime)
		mq.NoteMCUMovequeueActivity(flushTime, true)
		mq.advanceFlushTime(flushTime-sdsCheckTime, flushTime)
	}
	// Restore background flushing
	mq.reactor.UpdateTimer(mq.flushTimer, reactor.Now)
	mq.advanceFlushTime(flushTime+mq.kinFlushDelay, 0.)
	if i := slices.Index(mq.dripStartTimes, startTime); i >= 0 {
		mq.dripStartTimes = slices.Delete(mq.dripStartTimes, i, i+1)
	}
}

// CheckDripTiming returns the first start time of the drip moves, and false for none.
func (mq *PrinterMotionQueuing) CheckDripTiming() (float64, bool) {
	if len(mq.dripStartTimes) == 0 {
		return 0, false
	}
	return slices.Min(mq.dripStartTimes), true
}
