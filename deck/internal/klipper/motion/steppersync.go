package motion

// Stepper step transmit synchronization (steppersync.c).
//
//	Copyright (C) 2016-2025  Kevin O'Connor <kevin@koconnor.net>
//
// The steppersync object is used to synchronize the output of mcu step commands. The mcu can only
// queue a limited number of step commands - this code tracks when items on the mcu step queue
// become free so that new commands can be transmitted. It also ensures the mcu step queue is
// ordered between steppers so that no stepper starves the other steppers of space in the mcu step
// queue.
//
// Klipper generates the steps of each stepper on its own thread. The Go code generates them in
// sequence: the steps of each stepper and the message order are the same.

import (
	"log"
	"math"

	"printdeck/internal/klipper/serialqueue"
)

// SyncEmitter makes the messages of one stepper, or of another object of the board's move queue
// (struct syncemitter).
type SyncEmitter struct {
	msgQueue []serialqueue.Message
	sc       *StepCompress
	sk       *StepperKinematics
	name     string
}

// StepCompress returns the step compression, or nil.
func (se *SyncEmitter) StepCompress() *StepCompress { return se.sc }

// Name is the name of the stepper.
func (se *SyncEmitter) Name() string { return se.name }

// SetStepperKinematics sets the step solver.
func (se *SyncEmitter) SetStepperKinematics(sk *StepperKinematics) { se.sk = sk }

// StepperKinematics returns the step solver, or nil.
func (se *SyncEmitter) StepperKinematics() *StepperKinematics { return se.sk }

// QueueMsg queues a command that uses a place in the board's move queue.
func (se *SyncEmitter) QueueMsg(reqClock uint64, data ...uint32) {
	se.msgQueue = append(se.msgQueue, serialqueue.Message{Data: encodeMessage(data...),
		MinClock: reqClock, ReqClock: reqClock})
}

// generateSteps generates the steps and flushes them (se_generate_steps).
func (se *SyncEmitter) generateSteps(genStepsTime float64, flushClock, clearHistoryClock uint64) int32 {
	if se.sc == nil || se.sk == nil {
		return 0
	}
	// Generate steps
	if ret := se.sk.GenerateSteps(se.sc, genStepsTime); ret != 0 {
		return ret
	}
	// Flush steps
	if ret := se.sc.Flush(flushClock); ret != 0 {
		return int32(ret)
	}
	// Clear history
	se.sc.HistoryExpire(clearHistoryClock)
	return 0
}

func newSyncEmitter(name string, allocStepcompress bool) *SyncEmitter {
	if len(name) > 15 {
		name = name[:15]
	}
	se := &SyncEmitter{name: name}
	if allocStepcompress {
		se.sc = NewStepCompress(&se.msgQueue)
	}
	return se
}

// StepperSync sorts the move queue messages of one board (struct steppersync).
type StepperSync struct {
	sq         *serialqueue.SerialQueue
	cq         *serialqueue.CommandQueue
	seList     []*SyncEmitter
	ce         serialqueue.ClockEstimate
	moveClocks []uint64
}

// AllocSyncEmitter adds a sync emitter, with a step compression when allocStepcompress is set.
func (ss *StepperSync) AllocSyncEmitter(name string, allocStepcompress bool) *SyncEmitter {
	se := newSyncEmitter(name, allocStepcompress)
	ss.seList = append(ss.seList, se)
	return se
}

// SetupMoveQueue sets the serial queue and the size of the board's move queue.
func (ss *StepperSync) SetupMoveQueue(sq *serialqueue.SerialQueue, moveNum int) {
	ss.sq = sq
	ss.cq = sq.AllocCommandQueue()
	ss.moveClocks = make([]uint64, moveNum)
}

// SetTime sets the conversion of print times to board clocks.
func (ss *StepperSync) SetTime(timeOffset, mcuFreq float64) {
	ss.ce = serialqueue.ClockEstimate{ConvClock: 0, ConvTime: timeOffset, EstFreq: mcuFreq}
	for _, se := range ss.seList {
		if se.sc != nil {
			se.sc.SetTime(timeOffset, mcuFreq)
		}
	}
}

// heapReplace tracks with a binary heap when the next move of the board's queue is free.
func (ss *StepperSync) heapReplace(reqClock uint64) {
	mc := ss.moveClocks
	nmc, pos := len(mc), 0
	for {
		child1, child2 := 2*pos+1, 2*pos+2
		child2Clock, child1Clock := uint64(math.MaxUint64), uint64(math.MaxUint64)
		if child2 < nmc {
			child2Clock = mc[child2]
		}
		if child1 < nmc {
			child1Clock = mc[child1]
		}
		if reqClock <= child1Clock && reqClock <= child2Clock {
			mc[pos] = reqClock
			break
		}
		if child1Clock < child2Clock {
			mc[pos] = child1Clock
			pos = child1
		} else {
			mc[pos] = child2Clock
			pos = child2
		}
	}
}

// flush sends the messages before moveClock, in the order of their req_clock.
func (ss *StepperSync) flush(moveClock uint64) {
	var msgs []serialqueue.Message
	for {
		// Find message with lowest reqclock
		reqClock := serialqueue.MaxClock
		var from *SyncEmitter
		for _, se := range ss.seList {
			if len(se.msgQueue) > 0 && se.msgQueue[0].ReqClock < reqClock {
				from = se
				reqClock = se.msgQueue[0].ReqClock
			}
		}
		if from == nil || (from.msgQueue[0].MinClock != 0 && reqClock > moveClock) {
			break
		}
		qm := from.msgQueue[0]
		from.msgQueue = from.msgQueue[1:]
		nextAvail := ss.moveClocks[0]
		if qm.MinClock != 0 {
			// The min_clock field is overloaded to indicate that the command uses the 'move
			// queue' and to store the time that move queue item becomes available.
			ss.heapReplace(qm.MinClock)
		}
		// Reset the min_clock to its normal meaning (minimum transmit time)
		qm.MinClock = nextAvail
		msgs = append(msgs, qm)
	}
	// Transmit commands
	if len(msgs) > 0 {
		ss.sq.SendBatch(ss.cq, msgs)
	}
}

// StepperSyncMgr holds the stepper syncs of the boards (struct steppersyncmgr).
type StepperSyncMgr struct {
	ssList []*StepperSync
}

// AllocStepperSync adds the stepper sync of a board.
func (m *StepperSyncMgr) AllocStepperSync() *StepperSync {
	ss := &StepperSync{}
	m.ssList = append(m.ssList, ss)
	return ss
}

// GenSteps generates the steps to genStepsTime, sends the messages to flushTime, and clears the
// history to clearHistoryTime. It returns the sync emitter of a failed step generation, or nil.
func (m *StepperSyncMgr) GenSteps(flushTime, genStepsTime, clearHistoryTime float64) *SyncEmitter {
	// Prepare trapqs for step generation
	for _, ss := range m.ssList {
		for _, se := range ss.seList {
			if se.sc == nil || se.sk == nil {
				continue
			}
			if se.sk.TQ != nil {
				se.sk.TQ.CheckSentinels()
			}
		}
	}
	var failed *SyncEmitter
	for _, ss := range m.ssList {
		flushClock := ss.ce.ClockFromTime(flushTime)
		clearClock := ss.ce.ClockFromTime(clearHistoryTime)
		for _, se := range ss.seList {
			if ret := se.generateSteps(genStepsTime, flushClock, clearClock); ret != 0 {
				log.Printf("Error in syncemitter '%s' step generation", se.name)
				failed = se
			}
		}
		if failed != nil {
			continue
		}
		ss.flush(flushClock)
	}
	return failed
}
