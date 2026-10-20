// Package output_pin has the queue of the pin requests of the G-code commands, which the fans and
// the output pins use.
//
// Ported to Go from klippy/extras/output_pin.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f. Only GCodeRequestQueue is ported now.
//
//	Copyright (C) 2017-2025  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package output_pin

import (
	"math"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/motion_queuing"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/toolhead"
)

// The actions that a request callback returns.
const (
	ActionNone       = ""
	ActionDiscard    = "discard"
	ActionReschedule = "reschedule"
	ActionRepeat     = "repeat"
)

// RequestCallback applies a value at a print time. It returns an action and the minimum time of
// the next request, or ActionNone.
type RequestCallback func(printTime, value float64) (action string, nextMinTime float64)

type request struct{ printTime, value float64 }

// GCodeRequestQueue sends the pin changes of G-code commands at the print time of the command.
type GCodeRequestQueue struct {
	printer          *host.Printer
	mcu              *mcu.MCU
	callback         RequestCallback
	rqueue           []request
	nextMinFlushTime float64
	toolhead         *toolhead.ToolHead
	motionQueuing    *motion_queuing.PrinterMotionQueuing
}

func NewGCodeRequestQueue(config *configfile.Wrapper, m *mcu.MCU, callback RequestCallback) *GCodeRequestQueue {
	printer := host.Of(config)
	q := &GCodeRequestQueue{printer: printer, mcu: m, callback: callback}
	q.motionQueuing = motion_queuing.Load(config)
	q.motionQueuing.RegisterFlushCallback(q.flushNotification, false)
	printer.RegisterEventHandler("klippy:connect", func(...any) { q.toolhead = toolhead.Of(printer) })
	return q
}

func (q *GCodeRequestQueue) flushNotification(mustFlushTime, maxStepGenTime float64) {
	minSchedTime := q.mcu.MinScheduleTime()
	for len(q.rqueue) > 0 {
		nextTime := math.Max(q.rqueue[0].printTime, q.nextMinFlushTime)
		if nextTime > mustFlushTime {
			return
		}
		// Skip requests that have been overridden with a following request
		pos := 0
		for pos+1 < len(q.rqueue) && q.rqueue[pos+1].printTime <= nextTime {
			pos++
		}
		reqVal := q.rqueue[pos].value
		// Invoke callback for the request
		action, nextMinTime := q.callback(nextTime, reqVal)
		if action != ActionNone {
			// Handle special cases
			q.nextMinFlushTime = math.Max(q.nextMinFlushTime, nextMinTime)
			switch action {
			case ActionDiscard:
				q.rqueue = q.rqueue[pos+1:]
				continue
			case ActionReschedule:
				q.rqueue = q.rqueue[pos:]
				continue
			case ActionRepeat:
				pos--
			}
		}
		q.rqueue = q.rqueue[pos+1:]
		q.nextMinFlushTime = math.Max(q.nextMinFlushTime, nextTime+minSchedTime)
		// Ensure following queue items are flushed
		q.motionQueuing.NoteMCUMovequeueActivity(q.nextMinFlushTime, false)
	}
}

func (q *GCodeRequestQueue) queueRequest(printTime, value float64) {
	q.rqueue = append(q.rqueue, request{printTime, value})
	q.motionQueuing.NoteMCUMovequeueActivity(printTime, false)
}

// QueueGcodeRequest sets a value at the end of the queued moves.
func (q *GCodeRequestQueue) QueueGcodeRequest(value float64) {
	q.toolhead.RegisterLookaheadCallback(func(pt float64) { q.queueRequest(pt, value) })
}

// SendAsyncRequest sets a value now, or at a print time (NaN for now).
func (q *GCodeRequestQueue) SendAsyncRequest(value, printTime float64) {
	minSchedTime := q.mcu.MinScheduleTime()
	if math.IsNaN(printTime) {
		systime := q.printer.Reactor().Monotonic()
		printTime = q.mcu.EstimatedPrintTime(systime + minSchedTime)
	}
	for {
		nextTime := math.Max(printTime, q.nextMinFlushTime)
		// Invoke callback for the request
		action, nextMinTime := q.callback(nextTime, value)
		if action != ActionNone {
			// Handle special cases
			q.nextMinFlushTime = math.Max(q.nextMinFlushTime, nextMinTime)
			if action == ActionDiscard {
				break
			}
			if action == ActionReschedule {
				continue
			}
		}
		q.nextMinFlushTime = math.Max(q.nextMinFlushTime, nextTime+minSchedTime)
		if action != ActionRepeat {
			break
		}
	}
}
