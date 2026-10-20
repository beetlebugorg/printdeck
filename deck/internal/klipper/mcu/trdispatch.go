package mcu

// Trigger sync "trsync" message dispatch.
//
// Ported to Go from klippy/chelper/trdispatch.c of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2021  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
//
// The trsync_state messages of the boards come in on the reading goroutines. When one board
// reports a trigger, the dispatcher sends trsync_trigger to all boards at once. While the boards
// report that they can trigger, it extends their timeouts.

import (
	"sync"

	"printdeck/internal/klipper/msgproto"
	"printdeck/internal/klipper/serialqueue"
)

const prNever = 9999999999999999.

type trdispatch struct {
	tdmList []*trdispatchMCU

	mu                   sync.Mutex // protects variables below
	isActive, canTrigger bool
	dispatchReason       uint32
}

type trdispatchMCU struct {
	fr                                         serialqueue.FastReader
	td                                         *trdispatch
	sq                                         *serialqueue.SerialQueue
	cq                                         *serialqueue.CommandQueue
	trsyncOID, setTimeoutMsgtag, triggerMsgtag uint32

	// Remaining fields protected by trdispatch lock
	lastStatusClock, expireClock uint64
	expireTicks, minExtendTicks  uint64
	ce                           serialqueue.ClockEstimate
}

// Send: trsync_trigger oid=%c reason=%c
func (tdm *trdispatchMCU) sendTrsyncTrigger() {
	msg := msgproto.EncodeInts(tdm.triggerMsgtag, tdm.trsyncOID, tdm.td.dispatchReason)
	tdm.sq.Send(tdm.cq, msg, 0, 0, 0)
}

// Send: trsync_set_timeout oid=%c clock=%u
func (tdm *trdispatchMCU) sendTrsyncSetTimeout() {
	msg := msgproto.EncodeInts(tdm.setTimeoutMsgtag, tdm.trsyncOID, uint32(tdm.expireClock))
	tdm.sq.Send(tdm.cq, msg, 0, tdm.expireClock, 0)
}

// handleTrsyncState handles a trsync_state message (callback from serialqueue fastreader).
func (tdm *trdispatchMCU) handleTrsyncState(eventtime float64, data []byte) {
	// Parse: trsync_state oid=%c can_trigger=%c trigger_reason=%c clock=%u
	fields, err := msgproto.DecodeInts(data, 5)
	if err != nil || fields[1] != tdm.trsyncOID {
		return
	}
	canTrigger, clock := fields[2], fields[4]

	// Process message
	td := tdm.td
	td.mu.Lock()
	defer td.mu.Unlock()
	if !td.canTrigger {
		return
	}

	if canTrigger == 0 {
		// mcu reports trigger or timeout - propagate to all mcus
		td.canTrigger = false
		for _, m := range td.tdmList {
			m.sendTrsyncTrigger()
		}
		return
	}

	// mcu is still working okay - update last_status_clock
	tdm.ce = tdm.sq.ClockEst()
	estMsgClock := tdm.ce.ClockFromTime(eventtime)
	tdm.lastStatusClock = serialqueue.ClockFromClock32(estMsgClock, clock)

	// Determine minimum acknowledged time among all mcus
	minTime, nextMinTime := prNever, prNever
	var minTdm *trdispatchMCU
	for _, m := range td.tdmList {
		statusTime := m.ce.ClockToTime(m.lastStatusClock)
		if statusTime < nextMinTime {
			nextMinTime = statusTime
			if statusTime < minTime {
				nextMinTime = minTime
				minTime = statusTime
				minTdm = m
			}
		}
	}
	if nextMinTime == prNever {
		nextMinTime = minTime
	}

	// Send trsync_set_timeout messages to other mcus (if needed)
	for _, m := range td.tdmList {
		statusTime := minTime
		if m == minTdm {
			statusTime = nextMinTime
		}
		expire := m.ce.ClockFromTime(statusTime) + m.expireTicks
		// The C code compares an int64_t with a uint64_t, so the comparison is unsigned.
		if expire-m.expireClock >= m.minExtendTicks {
			m.expireClock = expire
			m.sendTrsyncSetTimeout()
		}
	}
}

// start begins the synchronization.
func (td *trdispatch) start(dispatchReason uint32) {
	td.mu.Lock()
	if td.isActive || len(td.tdmList) == 0 {
		td.mu.Unlock()
		return
	}
	td.dispatchReason = dispatchReason
	td.isActive, td.canTrigger = true, true
	td.mu.Unlock()

	// Register handle_trsync_state message parser for each mcu
	for _, tdm := range td.tdmList {
		if tdm.sq != nil {
			tdm.sq.AddFastReader(&tdm.fr)
		}
	}
}

// stop cleans up after a test completes.
func (td *trdispatch) stop() {
	td.mu.Lock()
	if !td.isActive {
		td.mu.Unlock()
		return
	}
	td.isActive = false
	td.mu.Unlock()

	// Unregister handle_trsync_state message parsers
	for _, tdm := range td.tdmList {
		if tdm.sq != nil {
			tdm.sq.RemoveFastReader(&tdm.fr)
		}
	}
}

func newTrdispatch() *trdispatch { return &trdispatch{} }

// newTrdispatchMCU makes the dispatch object of one board.
func (td *trdispatch) newTrdispatchMCU(sq *serialqueue.SerialQueue, cq *serialqueue.CommandQueue,
	trsyncOID, setTimeoutMsgtag, triggerMsgtag, stateMsgtag uint32) *trdispatchMCU {
	tdm := &trdispatchMCU{sq: sq, cq: cq, trsyncOID: trsyncOID, setTimeoutMsgtag: setTimeoutMsgtag,
		triggerMsgtag: triggerMsgtag}
	// Setup fastreader to match trsync_state messages
	tdm.fr.Prefix = msgproto.EncodeInts(stateMsgtag, trsyncOID)
	tdm.fr.Func = tdm.handleTrsyncState
	tdm.td = td
	td.tdmList = append(td.tdmList, tdm)
	return tdm
}

// setup prepares a board for a trigger test.
func (tdm *trdispatchMCU) setup(lastStatusClock, expireClock, expireTicks, minExtendTicks uint64) {
	td := tdm.td
	td.mu.Lock()
	defer td.mu.Unlock()
	tdm.lastStatusClock = lastStatusClock
	tdm.expireClock = expireClock
	tdm.expireTicks = expireTicks
	tdm.minExtendTicks = minExtendTicks
	if tdm.sq != nil {
		tdm.ce = tdm.sq.ClockEst()
	}
}
