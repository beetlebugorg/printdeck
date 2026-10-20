package motion

// Stepper pulse schedule compression (stepcompress.c).
//
//	Copyright (C) 2016-2025  Kevin O'Connor <kevin@koconnor.net>
//
// The goal of this code is to take a series of scheduled stepper pulse times and compress them
// into a handful of commands that can be efficiently transmitted and executed on a
// microcontroller (mcu). The mcu accepts step pulse commands that take interval, count, and add
// parameters such that 'count' pulses occur, with each step event calculating the next step event
// time using:
//
//	next_wake_time = last_wake_time + interval; interval += add

import (
	"log"
	"math"

	"printdeck/internal/klipper/serialqueue"
)

// ErrorRet is the result of a failed step compression.
const ErrorRet = -989898989

const (
	checkLines     = true
	queueStartSize = 1024
	// The maximum add delta between two valid quadratic sequences of the form
	// "add*count*(count-1)/2 + interval*count" is "(6 + 4*sqrt(2)) * maxerror / (count*count)".
	// The "6 + 4*sqrt(2)" is 11.65685, but using 11 works well in practice.
	quadraticDev = 11
	// Maximium clock delta between messages in the queue
	clockDiffMax  = 3 << 28
	sdsFilterTime = .000750
)

// historySteps is a queue_step command that was sent.
type historySteps struct {
	firstClock, lastClock uint64
	startPosition         int64
	stepCount, interval   int
	add                   int
}

// HistorySteps is a queue_step command of the history (struct pull_history_steps).
type HistorySteps struct {
	FirstClock, LastClock uint64
	StartPosition         int64
	StepCount, Interval   int
	Add                   int
}

// StepCompress makes the queue_step commands of one stepper (struct stepcompress).
type StepCompress struct {
	// Buffer management: the step clocks are in queue[queuePos:queueNext]. Only the lower 32 bits
	// of a step clock are stored.
	queue               []uint32
	queuePos, queueNext int
	// Internal tracking
	maxError                                  uint32
	mcuTimeOffset, mcuFreq, lastStepPrintTime float64
	// Message generation
	lastStepClock                         uint64
	msgQueue                              *[]serialqueue.Message
	oid                                   uint32
	queueStepMsgtag, setNextStepDirMsgtag int32
	sdir, invertSdir                      int
	// Step+dir+step filter
	nextStepClock uint64
	nextStepDir   int
	// History tracking, the oldest first
	lastPosition int64
	history      []*historySteps
}

// NewStepCompress makes a step compression that adds its commands to msgQueue.
func NewStepCompress(msgQueue *[]serialqueue.Message) *StepCompress {
	return &StepCompress{sdir: -1, msgQueue: msgQueue}
}

// Step compression

type points struct{ minp, maxp int32 }

func idivUp(n, d int32) int32 {
	if n >= 0 {
		return (n + d - 1) / d
	}
	return n / d
}

func idivDown(n, d int32) int32 {
	if n >= 0 {
		return n / d
	}
	return (n - d + 1) / d
}

// minmaxPoint returns the minimum and maximum acceptable times of a requested step time.
func (sc *StepCompress) minmaxPoint(pos int) points {
	lsc := uint32(sc.lastStepClock)
	point := sc.queue[pos] - lsc
	var prevpoint uint32
	if pos > sc.queuePos {
		prevpoint = sc.queue[pos-1] - lsc
	}
	maxError := (point - prevpoint) / 2
	if maxError > sc.maxError {
		maxError = sc.maxError
	}
	return points{int32(point - maxError), int32(point)}
}

type stepMove struct {
	interval uint32
	count    uint16
	add      int16
}

// compressBisectAdd finds a step_move that covers a series of step times.
func (sc *StepCompress) compressBisectAdd() stepMove {
	qlast := sc.queueNext
	if qlast > sc.queuePos+65535 {
		qlast = sc.queuePos + 65535
	}
	point := sc.minmaxPoint(sc.queuePos)
	outerMininterval, outerMaxinterval := point.minp, point.maxp
	var add, minadd, maxadd int32 = 0, -0x8000, 0x7fff
	var bestinterval, bestcount, bestadd, bestreach int32 = 0, 1, 1, math.MinInt32
	var zerointerval, zerocount int32
	for {
		// Find longest valid sequence with the given 'add'
		var nextpoint points
		nextmininterval := outerMininterval
		nextmaxinterval := outerMaxinterval
		interval := nextmaxinterval
		var nextcount int32 = 1
		for {
			nextcount++
			if sc.queuePos+int(nextcount)-1 >= qlast {
				count := nextcount - 1
				return stepMove{uint32(interval), uint16(count), int16(add)}
			}
			nextpoint = sc.minmaxPoint(sc.queuePos + int(nextcount) - 1)
			nextaddfactor := nextcount * (nextcount - 1) / 2
			c := add * nextaddfactor
			if nextmininterval*nextcount < nextpoint.minp-c {
				nextmininterval = idivUp(nextpoint.minp-c, nextcount)
			}
			if nextmaxinterval*nextcount > nextpoint.maxp-c {
				nextmaxinterval = idivDown(nextpoint.maxp-c, nextcount)
			}
			if nextmininterval > nextmaxinterval {
				break
			}
			interval = nextmaxinterval
		}
		// Check if this is the best sequence found so far
		count := nextcount - 1
		addfactor := count * (count - 1) / 2
		reach := add*addfactor + interval*count
		if reach > bestreach || (reach == bestreach && interval > bestinterval) {
			bestinterval, bestcount, bestadd, bestreach = interval, count, add, reach
			if add == 0 {
				zerointerval, zerocount = interval, count
			}
			if count > 0x200 {
				// No 'add' will improve sequence; avoid integer overflow
				break
			}
		}
		// Check if a greater or lesser add could extend the sequence
		nextaddfactor := nextcount * (nextcount - 1) / 2
		nextreach := add*nextaddfactor + interval*nextcount
		if nextreach < nextpoint.minp {
			minadd = add + 1
			outerMaxinterval = nextmaxinterval
		} else {
			maxadd = add - 1
			outerMininterval = nextmininterval
		}
		// The maximum valid deviation between two quadratic sequences can be calculated and used
		// to further limit the add range.
		if count > 1 {
			errdelta := int32(sc.maxError*quadraticDev) / (count * count)
			if minadd < add-errdelta {
				minadd = add - errdelta
			}
			if maxadd > add+errdelta {
				maxadd = add + errdelta
			}
		}
		// See if next point would further limit the add range
		c := outerMaxinterval * nextcount
		if minadd*nextaddfactor < nextpoint.minp-c {
			minadd = idivUp(nextpoint.minp-c, nextaddfactor)
		}
		c = outerMininterval * nextcount
		if maxadd*nextaddfactor > nextpoint.maxp-c {
			maxadd = idivDown(nextpoint.maxp-c, nextaddfactor)
		}
		// Bisect valid add range and try again with new 'add'
		if minadd > maxadd {
			break
		}
		add = maxadd - (maxadd-minadd)/4
	}
	if zerocount+zerocount/16 >= bestcount {
		// Prefer add=0 if it's similar to the best found sequence
		return stepMove{uint32(zerointerval), uint16(zerocount), 0}
	}
	return stepMove{uint32(bestinterval), uint16(bestcount), int16(bestadd)}
}

// Step compress checking

// checkLine verifies that a step_move matches the step times.
func (sc *StepCompress) checkLine(move stepMove) int {
	if !checkLines {
		return 0
	}
	if move.count == 0 || (move.interval == 0 && move.add == 0 && move.count > 1) || move.interval >= 0x80000000 {
		log.Printf("stepcompress o=%d i=%d c=%d a=%d: Invalid sequence", sc.oid, int32(move.interval), move.count, move.add)
		return ErrorRet
	}
	interval, p := move.interval, uint32(0)
	for i := uint16(0); i < move.count; i++ {
		point := sc.minmaxPoint(sc.queuePos + int(i))
		p += interval
		if int32(p) < point.minp || int32(p) > point.maxp {
			log.Printf("stepcompress o=%d i=%d c=%d a=%d: Point %d: %d not in %d:%d", sc.oid,
				int32(move.interval), move.count, move.add, i+1, int32(p), point.minp, point.maxp)
			return ErrorRet
		}
		if interval >= 0x80000000 {
			log.Printf("stepcompress o=%d i=%d c=%d a=%d: Point %d: interval overflow %d", sc.oid,
				int32(move.interval), move.count, move.add, i+1, int32(interval))
			return ErrorRet
		}
		interval += uint32(int32(move.add))
	}
	return 0
}

// Step compress interface

// Fill sets the oid, the max error in clocks, and the message tags of the commands.
func (sc *StepCompress) Fill(oid, maxError uint32, queueStepMsgtag, setNextStepDirMsgtag int32) {
	sc.oid = oid
	sc.maxError = maxError
	sc.queueStepMsgtag = queueStepMsgtag
	sc.setNextStepDirMsgtag = setNextStepDirMsgtag
}

// SetInvertSdir sets the inverted direction flag.
func (sc *StepCompress) SetInvertSdir(invertSdir bool) {
	inv := 0
	if invertSdir {
		inv = 1
	}
	if inv != sc.invertSdir {
		sc.invertSdir = inv
		if sc.sdir >= 0 {
			sc.sdir ^= 1
		}
	}
}

// HistoryExpire removes the history to endClock.
func (sc *StepCompress) HistoryExpire(endClock uint64) {
	for len(sc.history) > 0 {
		hs := sc.history[0]
		if hs.lastClock > endClock {
			break
		}
		sc.history[0] = nil
		sc.history = sc.history[1:]
	}
}

// OID is the oid of the stepper.
func (sc *StepCompress) OID() uint32 { return sc.oid }

// StepDir is the direction of the next step.
func (sc *StepCompress) StepDir() int { return sc.nextStepDir }

// calcLastStepPrintTime sets the print time of last_step_clock.
func (sc *StepCompress) calcLastStepPrintTime() {
	lsc := float64(sc.lastStepClock)
	sc.lastStepPrintTime = sc.mcuTimeOffset + (lsc-.5)/sc.mcuFreq
}

// SetTime sets the conversion of print times to board clocks.
func (sc *StepCompress) SetTime(timeOffset, mcuFreq float64) {
	sc.mcuTimeOffset = timeOffset
	sc.mcuFreq = mcuFreq
	sc.calcLastStepPrintTime()
}

// encodeInt encodes an integer as a "variable length quantity", as msgblock.c does.
func encodeInt(out []byte, v uint32) []byte {
	sv := int32(v)
	switch {
	case sv < 3<<5 && sv >= -(1<<5):
	case sv < 3<<12 && sv >= -(1<<12):
		out = append(out, byte((v>>7)&0x7f|0x80))
	case sv < 3<<19 && sv >= -(1<<19):
		out = append(out, byte((v>>14)&0x7f|0x80), byte((v>>7)&0x7f|0x80))
	case sv < 3<<26 && sv >= -(1<<26):
		out = append(out, byte((v>>21)&0x7f|0x80), byte((v>>14)&0x7f|0x80), byte((v>>7)&0x7f|0x80))
	default:
		out = append(out, byte(v>>28|0x80), byte((v>>21)&0x7f|0x80), byte((v>>14)&0x7f|0x80),
			byte((v>>7)&0x7f|0x80))
	}
	return append(out, byte(v&0x7f))
}

func encodeMessage(data ...uint32) []byte {
	var out []byte
	for _, d := range data {
		out = encodeInt(out, d)
	}
	return out
}

// addMove queues a queue_step command for a step_move.
func (sc *StepCompress) addMove(firstClock uint64, move stepMove) {
	addfactor := int32(move.count) * (int32(move.count) - 1) / 2
	ticks := uint32(int32(move.add)*addfactor) + move.interval*(uint32(move.count)-1)
	lastClock := firstClock + uint64(ticks)
	// Create and queue a queue_step command
	qm := serialqueue.Message{Data: encodeMessage(uint32(sc.queueStepMsgtag), sc.oid, move.interval,
		uint32(move.count), uint32(int32(move.add)))}
	qm.MinClock, qm.ReqClock = sc.lastStepClock, sc.lastStepClock
	if move.count == 1 && firstClock >= sc.lastStepClock+clockDiffMax {
		qm.ReqClock = firstClock
	}
	*sc.msgQueue = append(*sc.msgQueue, qm)
	sc.lastStepClock = lastClock
	// Create and store move in history tracking
	hs := &historySteps{firstClock: firstClock, lastClock: lastClock, startPosition: sc.lastPosition,
		interval: int(int32(move.interval)), add: int(move.add)}
	hs.stepCount = -int(move.count)
	if sc.sdir != 0 {
		hs.stepCount = int(move.count)
	}
	sc.lastPosition += int64(hs.stepCount)
	sc.history = append(sc.history, hs)
}

// queueFlush converts the queued steps to board commands, to moveClock.
func (sc *StepCompress) queueFlush(moveClock uint64) int {
	if sc.queuePos >= sc.queueNext {
		return 0
	}
	for sc.lastStepClock < moveClock {
		move := sc.compressBisectAdd()
		if ret := sc.checkLine(move); ret != 0 {
			return ret
		}
		sc.addMove(sc.lastStepClock+uint64(move.interval), move)
		if sc.queuePos+int(move.count) >= sc.queueNext {
			sc.queuePos, sc.queueNext = 0, 0
			break
		}
		sc.queuePos += int(move.count)
	}
	sc.calcLastStepPrintTime()
	return 0
}

// flushFar makes a queue_step for a step far after the last step.
func (sc *StepCompress) flushFar(absStepClock uint64) int {
	sc.addMove(absStepClock, stepMove{uint32(absStepClock - sc.lastStepClock), 1, 0})
	sc.calcLastStepPrintTime()
	return 0
}

// setNextStepDir sends the set_next_step_dir command.
func (sc *StepCompress) setNextStepDir(sdir int) int {
	if sc.sdir == sdir {
		return 0
	}
	if ret := sc.queueFlush(math.MaxUint64); ret != 0 {
		return ret
	}
	sc.sdir = sdir
	qm := serialqueue.Message{Data: encodeMessage(uint32(sc.setNextStepDirMsgtag), sc.oid,
		uint32(sdir^sc.invertSdir)), ReqClock: sc.lastStepClock}
	*sc.msgQueue = append(*sc.msgQueue, qm)
	return 0
}

func (sc *StepCompress) store(clock uint64) {
	sc.queue[sc.queueNext] = uint32(clock)
	sc.queueNext++
}

// queueAppendFar is the slow path of queueAppend for a step far in the future.
func (sc *StepCompress) queueAppendFar() int {
	stepClock := sc.nextStepClock
	sc.nextStepClock = 0
	if ret := sc.queueFlush(stepClock - clockDiffMax + 1); ret != 0 {
		return ret
	}
	if stepClock >= sc.lastStepClock+clockDiffMax {
		return sc.flushFar(stepClock)
	}
	sc.store(stepClock)
	return 0
}

// queueAppendExtend is the slow path of queueAppend that makes room in the queue.
func (sc *StepCompress) queueAppendExtend() int {
	if sc.queueNext-sc.queuePos > 65535+2000 {
		// No point in keeping more than 64K steps in memory
		flush := sc.queue[sc.queueNext-65535] - uint32(sc.lastStepClock)
		if ret := sc.queueFlush(sc.lastStepClock + uint64(flush)); ret != 0 {
			return ret
		}
	}
	if sc.queueNext >= len(sc.queue) {
		// Make room in the queue
		inUse := sc.queueNext - sc.queuePos
		if sc.queuePos > 0 {
			// Shuffle the internal queue to avoid having to allocate more ram
			copy(sc.queue, sc.queue[sc.queuePos:sc.queueNext])
		} else {
			// Expand the internal queue of step times
			alloc := len(sc.queue)
			if alloc == 0 {
				alloc = queueStartSize
			}
			for inUse >= alloc {
				alloc *= 2
			}
			q := make([]uint32, alloc)
			copy(q, sc.queue[:sc.queueNext])
			sc.queue = q
		}
		sc.queuePos = 0
		sc.queueNext = inUse
	}
	sc.store(sc.nextStepClock)
	sc.nextStepClock = 0
	return 0
}

// queueAppend adds the pending step to the queue.
func (sc *StepCompress) queueAppend() int {
	if sc.nextStepDir != sc.sdir {
		if ret := sc.setNextStepDir(sc.nextStepDir); ret != 0 {
			return ret
		}
	}
	if sc.nextStepClock >= sc.lastStepClock+clockDiffMax {
		return sc.queueAppendFar()
	}
	if sc.queueNext >= len(sc.queue) {
		return sc.queueAppendExtend()
	}
	sc.store(sc.nextStepClock)
	sc.nextStepClock = 0
	return 0
}

// Append adds a step at a time of a move.
func (sc *StepCompress) Append(sdir int, printTime, stepTime float64) int32 {
	// Calculate step clock
	offset := printTime - sc.lastStepPrintTime
	relSC := (stepTime + offset) * sc.mcuFreq
	stepClock := sc.lastStepClock + uint64(relSC)
	// Flush previous pending step (if any)
	if sc.nextStepClock != 0 {
		if sdir != sc.nextStepDir {
			diff := float64(int64(stepClock - sc.nextStepClock))
			if diff < sdsFilterTime*sc.mcuFreq {
				// Rollback last step to avoid rapid step+dir+step
				sc.nextStepClock = 0
				sc.nextStepDir = sdir
				return 0
			}
		}
		if ret := sc.queueAppend(); ret != 0 {
			return int32(ret)
		}
	}
	// Store this step as the next pending step
	sc.nextStepClock = stepClock
	sc.nextStepDir = sdir
	return 0
}

// Commit adds the pending step to the queue: it is not rolled back.
func (sc *StepCompress) Commit() {
	if sc.nextStepClock != 0 {
		sc.queueAppend()
	}
}

// CommitErr is Commit with the result of the C function.
func (sc *StepCompress) CommitErr() int {
	if sc.nextStepClock != 0 {
		return sc.queueAppend()
	}
	return 0
}

// Flush converts the steps to moveClock to board commands.
func (sc *StepCompress) Flush(moveClock uint64) int {
	if sc.nextStepClock != 0 && moveClock >= sc.nextStepClock {
		if ret := sc.queueAppend(); ret != 0 {
			return ret
		}
	}
	return sc.queueFlush(moveClock)
}

// Reset flushes the steps and sets the last step clock.
func (sc *StepCompress) Reset(lastStepClock uint64) int {
	if ret := sc.Flush(math.MaxUint64); ret != 0 {
		return ret
	}
	sc.lastStepClock = lastStepClock
	sc.sdir = -1
	sc.calcLastStepPrintTime()
	return 0
}

// SetLastPosition flushes the steps and sets the stepper position at a clock.
func (sc *StepCompress) SetLastPosition(clock uint64, lastPosition int64) int {
	if ret := sc.Flush(math.MaxUint64); ret != 0 {
		return ret
	}
	sc.lastPosition = lastPosition
	// Add a marker to the history list
	hs := &historySteps{firstClock: clock, lastClock: clock, startPosition: lastPosition}
	sc.history = append(sc.history, hs)
	return 0
}

// FindPastPosition returns the stepper position at a past clock.
func (sc *StepCompress) FindPastPosition(clock uint64) int64 {
	lastPosition := sc.lastPosition
	for i := len(sc.history) - 1; i >= 0; i-- {
		hs := sc.history[i]
		if clock < hs.firstClock {
			lastPosition = hs.startPosition
			continue
		}
		if clock >= hs.lastClock {
			return hs.startPosition + int64(hs.stepCount)
		}
		interval, add := int32(hs.interval), int32(hs.add)
		ticks := int32(clock-hs.firstClock) + interval
		var offset int32
		if add == 0 {
			offset = ticks / interval
		} else {
			// Solve for "count" using quadratic formula
			a, b, c := .5*float64(add), float64(interval)-.5*float64(add), -float64(ticks)
			offset = int32((math.Sqrt(b*b-4*a*c) - b) / (2. * a))
		}
		if hs.stepCount < 0 {
			return hs.startPosition - int64(offset)
		}
		return hs.startPosition + int64(offset)
	}
	return lastPosition
}

// ExtractOld returns up to max commands of the history between startClock and endClock, the
// newest first.
func (sc *StepCompress) ExtractOld(max int, startClock, endClock uint64) []HistorySteps {
	var res []HistorySteps
	for i := len(sc.history) - 1; i >= 0; i-- {
		hs := sc.history[i]
		if startClock >= hs.lastClock || len(res) >= max {
			break
		}
		if endClock <= hs.firstClock {
			continue
		}
		res = append(res, HistorySteps{hs.firstClock, hs.lastClock, hs.startPosition, hs.stepCount,
			hs.interval, hs.add})
	}
	return res
}
