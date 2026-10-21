// Package serialqueue is the serial port link to the printer's board: it queues the commands, sends
// them at their clock times in priority order, packs them into message blocks, and sends the blocks
// again when the board does not acknowledge them.
//
// Ported to Go from klippy/chelper/serialqueue.c and the clock functions of klippy/chelper/msgblock.c
// of Klipper (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
// The functions have the names and the order of the C file. The background thread of the C file is
// a goroutine with an event loop here, and its pipe is a channel. CAN bus support is not ported.
//
//	Copyright (C) 2016-2025  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package serialqueue

import (
	"fmt"
	"io"
	"math"
	monotonic_ "printdeck/internal/klipper/monotonic"
	"sync"
	"time"

	"printdeck/internal/klipper/msgproto"
)

const (
	// MaxClock is a clock that never comes.
	MaxClock = uint64(0x7fffffffffffffff)
	// BackgroundPriorityClock is the request clock of a command with no deadline. It goes out when
	// the link is idle.
	BackgroundPriorityClock = uint64(0x7fffffff00000000)

	minRTO               = 0.025
	maxRTO               = 5.000
	maxPendingBlocks     = 12
	minReqtimeDelta      = 0.100
	minBackgroundDelta   = 0.005
	debugQueueSent       = 100
	debugQueueReceive    = 100
	prNow                = 0.
	prNever              = 9999999999999999.
	maxSendBuf           = msgproto.MessageMax * maxPendingBlocks
	receiveSeqDebugFile  = ^uint64(0) // (uint64_t)-1 of the C file
	backgroundOffsetTime = minReqtimeDelta + minBackgroundDelta
)

// Kind is the kind of the port.
type Kind int

const (
	// UART is a serial port or a USB serial device.
	UART Kind = iota
	// DebugFile is a file that receives the bytes and never answers (klippy's batch mode).
	DebugFile
)

// ClockEstimate converts between host time (seconds) and the clock of the board.
type ClockEstimate struct {
	ConvClock uint64
	ConvTime  float64
	EstFreq   float64
}

// ClockToTime converts a board clock to host time.
func (ce ClockEstimate) ClockToTime(clock uint64) float64 {
	return ce.ConvTime + float64(int64(clock-ce.ConvClock))/ce.EstFreq
}

// ClockFromTime converts host time to the nearest board clock.
func (ce ClockEstimate) ClockFromTime(t float64) uint64 {
	return uint64(int64((t-ce.ConvTime)*ce.EstFreq+.5)) + ce.ConvClock
}

// ClockFromClock32 extends a 32-bit board clock to 64 bits, with the last known clock.
func ClockFromClock32(lastClock uint64, clock32 uint32) uint64 {
	diff := int32(clock32 - uint32(lastClock))
	return uint64(int64(lastClock) + int64(diff))
}

// queueMessage is a message in a queue: a command to send, a block that was sent, or a received
// message.
type queueMessage struct {
	msg                   []byte
	minClock, reqClock    uint64
	notifyID              uint64
	sentTime, receiveTime float64
}

// Message is a message for SendBatch.
type Message struct {
	Data               []byte
	MinClock, ReqClock uint64
	NotifyID           uint64
}

// Received is a message from the board, or the notification of a sent command (Data is empty).
type Received struct {
	Data                  []byte
	SentTime, ReceiveTime float64
	NotifyID              uint64
}

// CommandQueue keeps the order of the commands of one object of the board. Commands of one queue
// go out in the order of their sends.
type CommandQueue struct {
	ready, upcoming     []*queueMessage
	inReady, inUpcoming bool
}

// FastReader gets the messages that start with Prefix on the reading goroutine, with no wait. The
// endstops and the probe use it.
type FastReader struct {
	Prefix []byte
	Func   func(eventtime float64, msg []byte)
}

// SerialQueue is the link to one board.
type SerialQueue struct {
	port io.ReadWriter
	kind Kind
	name string

	// Now returns the host time in seconds. Tests replace it.
	Now func() float64

	input    chan []byte
	kick     chan struct{}
	readDone chan struct{} // closed when the reading goroutine ends
	exit     chan struct{}
	exited   chan struct{}
	exiting  sync.Once

	// The receiver: messages for Pull.
	recvMu       sync.Mutex
	recvCond     *sync.Cond
	recvQueue    []*queueMessage
	oldReceive   []*queueMessage
	recvExited   bool
	needSync     bool
	inputBuf     []byte
	timerCmd     float64
	timerRetrans float64

	// transmit requests, from the senders (the transmit_requests struct of the C file).
	treqMu          sync.Mutex
	upcomingQueues  []*CommandQueue
	upcomingBytes   int
	needKickClock   uint64
	minReleaseClock uint64

	mu sync.Mutex // protects the fields below
	// Baud and clock tracking
	receiveWindow       int
	bittimeAdjust       float64
	idleTime            float64
	ce                  ClockEstimate
	lastReceiveSentTime float64
	// Retransmit support
	sendSeq, receiveSeq                                   uint64
	ignoreNakSeq, lastAckSeq, retransmitSeq, rttSampleSeq uint64
	sentQueue                                             []*queueMessage
	srtt, rttvar, rto                                     float64
	// Pending transmission message queues
	readyQueues                            []*CommandQueue
	readyBytes, needAckBytes, lastAckBytes int
	notifyQueue                            []*queueMessage
	// Fast readers
	fastMu      sync.Mutex
	fastReaders []*FastReader
	// Debugging
	oldSent []*queueMessage
	// Stats
	bytesWrite, bytesRead, bytesRetransmit, bytesInvalid uint32
}

// monotonic is get_monotonic of chelper: CLOCK_MONOTONIC_RAW.
func monotonic() float64 { return monotonic_.Now() }

// New starts the link on port (serialqueue_alloc). A DebugFile port is written only.
func New(port io.ReadWriter, kind Kind, name string) *SerialQueue {
	sq := &SerialQueue{port: port, kind: kind, name: name, Now: monotonic,
		input: make(chan []byte, 16), kick: make(chan struct{}, 1), exit: make(chan struct{}),
		readDone: make(chan struct{}),
		exited:   make(chan struct{}), timerCmd: prNever, timerRetrans: prNever,
		needKickClock: MaxClock, minReleaseClock: MaxClock, sendSeq: 1}
	sq.recvCond = sync.NewCond(&sq.recvMu)
	if kind == DebugFile {
		sq.receiveSeq = receiveSeqDebugFile
		sq.rto = prNever
		close(sq.readDone)
	} else {
		sq.receiveSeq = 1
		sq.rto = minRTO
		go sq.read()
	}
	go sq.background()
	return sq
}

// calculateBittime is the shortest time to send a number of bytes.
func (sq *SerialQueue) calculateBittime(bytes int) float64 {
	return sq.bittimeAdjust * float64(bytes)
}

// updateReceiveSeq updates the state when the receive sequence grows. sq.mu must be locked.
func (sq *SerialQueue) updateReceiveSeq(eventtime float64, rseq uint64) {
	// Remove from sent queue
	sentSeq := sq.receiveSeq
	for {
		if len(sq.sentQueue) == 0 {
			// Got an ack for a message not sent; must be connection init
			sq.sendSeq = rseq
			sq.lastReceiveSentTime = 0
			break
		}
		sent := sq.sentQueue[0]
		sq.sentQueue = sq.sentQueue[1:]
		sq.needAckBytes -= len(sent.msg)
		sq.oldSent = debugQueueAdd(sq.oldSent, sent, debugQueueSent)
		sentSeq++
		if rseq == sentSeq {
			// Found sent message corresponding with the received sequence
			sq.lastReceiveSentTime = sent.receiveTime
			sq.lastAckBytes = len(sent.msg)
			break
		}
	}
	sq.receiveSeq = rseq
	sq.timerCmd = prNow

	// Update retransmit info
	if sq.rttSampleSeq != 0 && rseq > sq.rttSampleSeq && sq.lastReceiveSentTime != 0 {
		// RFC6298 rtt calculations
		delta := eventtime - sq.lastReceiveSentTime
		if sq.srtt == 0 {
			sq.rttvar = delta / 2.0
			sq.srtt = delta * 10.0 // use a higher start default
		} else {
			sq.rttvar = (3.0*sq.rttvar + math.Abs(sq.srtt-delta)) / 4.0
			sq.srtt = (7.0*sq.srtt + delta) / 8.0
		}
		rttvar4 := max(sq.rttvar*4.0, 0.001)
		sq.rto = min(max(sq.srtt+rttvar4, minRTO), maxRTO)
		sq.rttSampleSeq = 0
	}
	if len(sq.sentQueue) == 0 {
		sq.timerRetrans = prNever
	} else {
		sq.timerRetrans = eventtime + sq.rto + sq.calculateBittime(len(sq.sentQueue[0].msg))
	}
}

// debugQueueAdd keeps the last count messages of a debug queue.
func debugQueueAdd(q []*queueMessage, qm *queueMessage, count int) []*queueMessage {
	q = append(q, qm)
	if len(q) > count {
		q = q[len(q)-count:]
	}
	return q
}

// handleMessage processes a well formed message from the board.
func (sq *SerialQueue) handleMessage(eventtime float64, buf []byte) {
	l := len(buf)
	sq.mu.Lock()

	// Calculate receive sequence number
	rseqDelta := uint64((buf[msgproto.MessagePosSeq] - byte(sq.receiveSeq)) & msgproto.MessageSeqMask)
	rseq := sq.receiveSeq + rseqDelta
	if rseq != sq.receiveSeq {
		// New sequence number
		if rseq > sq.sendSeq && sq.receiveSeq != 1 {
			// An ack for a message not sent?  Out of order message?
			sq.bytesInvalid += uint32(l)
			sq.mu.Unlock()
			return
		}
		sq.updateReceiveSeq(eventtime, rseq)
	}
	sq.bytesRead += uint32(l)

	// Check for pending messages on notify_queue
	var received []*queueMessage
	for len(sq.notifyQueue) > 0 {
		qm := sq.notifyQueue[0]
		wakeSeq := rseq - 1
		if l > msgproto.MessageMin {
			wakeSeq--
		}
		notifyMsgSentSeq := qm.reqClock
		if notifyMsgSentSeq > wakeSeq {
			break
		}
		sq.notifyQueue = sq.notifyQueue[1:]
		qm.msg = nil
		qm.sentTime = sq.lastReceiveSentTime
		qm.receiveTime = eventtime
		received = append(received, qm)
	}

	// Process message
	if l == msgproto.MessageMin {
		// Ack/nak message
		if sq.lastAckSeq < rseq {
			sq.lastAckSeq = rseq
		} else if rseq > sq.ignoreNakSeq && len(sq.sentQueue) > 0 {
			// Duplicate Ack is a Nak - do fast retransmit
			sq.timerRetrans = prNow
		}
	} else {
		// Data message - add to receive queue
		qm := &queueMessage{msg: append([]byte(nil), buf...)}
		if rseq > sq.retransmitSeq {
			qm.sentTime = sq.lastReceiveSentTime
		}
		qm.receiveTime = sq.Now() - sq.calculateBittime(l) // must be time post read()
		received = append(received, qm)
	}

	if len(received) > 0 {
		sq.receiveAppendWake(received)
	}

	// Check fast readers
	sq.fastMu.Lock()
	var fr *FastReader
	for _, f := range sq.fastReaders {
		if l >= len(f.Prefix)+msgproto.MessageMin &&
			string(buf[msgproto.MessageHeaderSize:msgproto.MessageHeaderSize+len(f.Prefix)]) == string(f.Prefix) {
			fr = f
			break
		}
	}
	sq.mu.Unlock()
	if fr != nil {
		fr.Func(eventtime, buf)
	}
	sq.fastMu.Unlock()
}

// receiveAppendWake adds messages for Pull and wakes a waiting Pull.
func (sq *SerialQueue) receiveAppendWake(msgs []*queueMessage) {
	sq.recvMu.Lock()
	sq.recvQueue = append(sq.recvQueue, msgs...)
	sq.recvMu.Unlock()
	sq.recvCond.Broadcast()
}

// inputEvent handles bytes from the port.
func (sq *SerialQueue) inputEvent(eventtime float64, data []byte) {
	sq.inputBuf = append(sq.inputBuf, data...)
	for {
		l := msgblockCheck(&sq.needSync, sq.inputBuf)
		if l == 0 {
			// Need more data
			return
		}
		if l > 0 {
			// Received a valid message
			sq.handleMessage(eventtime, sq.inputBuf[:l])
		} else {
			// Skip bad data at beginning of input
			l = -l
			sq.mu.Lock()
			sq.bytesInvalid += uint32(l)
			sq.mu.Unlock()
		}
		sq.inputBuf = append(sq.inputBuf[:0], sq.inputBuf[l:]...)
	}
}

// msgblockCheck checks that buf starts with a valid message block (msgblock_check of msgblock.c).
// It returns the length of the block, 0 for more data, or minus the number of bytes to skip.
func msgblockCheck(needSync *bool, buf []byte) int {
	if len(buf) < msgproto.MessageMin {
		return 0
	}
	if !*needSync {
		msglen := int(buf[msgproto.MessagePosLen])
		if msglen >= msgproto.MessageMin && msglen <= msgproto.MessageMax &&
			buf[msgproto.MessagePosSeq]&^msgproto.MessageSeqMask == msgproto.MessageDest {
			if len(buf) < msglen {
				return 0
			}
			crc := msgproto.CRC16CCITT(buf[:msglen-msgproto.MessageTrailerSize])
			if buf[msglen-msgproto.MessageTrailerSync] == msgproto.MessageSync &&
				crc[0] == buf[msglen-msgproto.MessageTrailerCRC] && crc[1] == buf[msglen-msgproto.MessageTrailerCRC+1] {
				return msglen
			}
		}
	}
	// Discard bytes until next SYNC found
	for i, b := range buf {
		if b == msgproto.MessageSync {
			*needSync = false
			return -(i + 1)
		}
	}
	*needSync = true
	return -len(buf)
}

// doWrite writes bytes to the port.
func (sq *SerialQueue) doWrite(buf []byte) {
	sq.port.Write(buf)
}

// flusher is a port that can drop its unsent output (tcflush TCOFLUSH).
type flusher interface{ FlushOutput() error }

// retransmitEvent sends all the unacknowledged blocks again. It returns the next wake time.
func (sq *SerialQueue) retransmitEvent(eventtime float64) float64 {
	if f, ok := sq.port.(flusher); ok && sq.kind == UART {
		f.FlushOutput()
	}

	sq.mu.Lock()
	defer sq.mu.Unlock()

	// Retransmit all pending messages
	buf := []byte{msgproto.MessageSync}
	firstBuflen := 0
	for _, qm := range sq.sentQueue {
		buf = append(buf, qm.msg...)
		if firstBuflen == 0 {
			firstBuflen = len(qm.msg) + 1
		}
	}
	sq.doWrite(buf)
	sq.bytesRetransmit += uint32(len(buf))

	// Update rto
	if sq.timerRetrans == prNow {
		// Retransmit due to nak
		sq.ignoreNakSeq = sq.receiveSeq
		if sq.receiveSeq < sq.retransmitSeq {
			// Second nak for this retransmit - don't allow third
			sq.ignoreNakSeq = sq.retransmitSeq
		}
	} else {
		// Retransmit due to timeout
		sq.rto = min(sq.rto*2.0, maxRTO)
		sq.ignoreNakSeq = sq.sendSeq
	}
	sq.retransmitSeq = sq.sendSeq
	sq.rttSampleSeq = 0
	sq.idleTime = eventtime + sq.calculateBittime(len(buf))
	return eventtime + sq.rto + sq.calculateBittime(firstBuflen)
}

// buildAndSendCommand packs the ready commands into one message block, highest priority (lowest
// request clock) first, and appends the block to buf. sq.mu must be locked.
func (sq *SerialQueue) buildAndSendCommand(buf []byte, pending int, eventtime float64) []byte {
	block := make([]byte, msgproto.MessageHeaderSize, msgproto.MessageMax)
	for sq.readyBytes > 0 {
		// Find highest priority message (message with lowest req_clock)
		minClock := MaxClock
		ci := -1
		for i, q := range sq.readyQueues {
			if m := q.ready[0]; m.reqClock < minClock {
				minClock, ci = m.reqClock, i
			}
		}
		if ci < 0 {
			break
		}
		cq := sq.readyQueues[ci]
		qm := cq.ready[0]
		// Append message to outgoing command
		if len(block)+len(qm.msg) > msgproto.MessageMax-msgproto.MessageTrailerSize {
			break
		}
		cq.ready = cq.ready[1:]
		if len(cq.ready) == 0 {
			cq.inReady = false
			sq.readyQueues = append(sq.readyQueues[:ci], sq.readyQueues[ci+1:]...)
		}
		block = append(block, qm.msg...)
		sq.readyBytes -= len(qm.msg)
		if qm.notifyID != 0 {
			// Message requires notification - add to notify list
			qm.reqClock = sq.sendSeq
			sq.notifyQueue = append(sq.notifyQueue, qm)
		}
	}

	// Fill header / trailer
	l := len(block) + msgproto.MessageTrailerSize
	block[msgproto.MessagePosLen] = byte(l)
	block[msgproto.MessagePosSeq] = byte(msgproto.MessageDest | (sq.sendSeq & msgproto.MessageSeqMask))
	crc := msgproto.CRC16CCITT(block)
	block = append(block, crc[0], crc[1], msgproto.MessageSync)

	// Store message block
	idletime := max(eventtime, sq.idleTime) + sq.calculateBittime(pending+l)
	out := &queueMessage{msg: block, sentTime: eventtime, receiveTime: idletime}
	if len(sq.sentQueue) == 0 {
		sq.timerRetrans = idletime + sq.rto
	}
	if sq.rttSampleSeq == 0 {
		sq.rttSampleSeq = sq.sendSeq
	}
	sq.sendSeq++
	sq.needAckBytes += l
	sq.sentQueue = append(sq.sentQueue, out)
	return append(buf, block...)
}

// checkUpcomingQueues moves the commands whose min_clock came from the upcoming queues to the
// ready queues. It returns the lowest min_clock that still waits.
func (sq *SerialQueue) checkUpcomingQueues(ackClock uint64) uint64 {
	sq.treqMu.Lock()
	defer sq.treqMu.Unlock()
	sq.needKickClock = 0
	if ackClock < sq.minReleaseClock {
		return sq.minReleaseClock
	}

	minStalledClock := MaxClock
	keep := sq.upcomingQueues[:0]
	for _, cq := range sq.upcomingQueues {
		notInReadyQueues := len(cq.ready) == 0
		// Move messages from the upcoming queue to the ready queue
		n := 0
		for _, qm := range cq.upcoming {
			if ackClock < qm.minClock {
				minStalledClock = min(minStalledClock, qm.minClock)
				break
			}
			cq.ready = append(cq.ready, qm)
			sq.upcomingBytes -= len(qm.msg)
			sq.readyBytes += len(qm.msg)
			n++
		}
		cq.upcoming = cq.upcoming[n:]
		// Remove cq from the list if it is now empty
		if len(cq.upcoming) == 0 {
			cq.inUpcoming = false
		} else {
			keep = append(keep, cq)
		}
		// Add to ready queues
		if notInReadyQueues && len(cq.ready) > 0 {
			cq.inReady = true
			sq.readyQueues = append(sq.readyQueues, cq)
		}
	}
	sq.upcomingQueues = keep
	sq.minReleaseClock = minStalledClock
	return minStalledClock
}

// updateNeedKickClock sets the clock at which a new command must wake the loop. It returns false
// when a command with an earlier clock came in the meantime.
func (sq *SerialQueue) updateNeedKickClock(wantclock uint64) bool {
	sq.treqMu.Lock()
	defer sq.treqMu.Unlock()
	if wantclock > sq.minReleaseClock {
		return false
	}
	sq.needKickClock = wantclock
	return true
}

// checkSendCommand returns prNow when a block can go out now, or the time to wake again.
// sq.mu must be locked.
func (sq *SerialQueue) checkSendCommand(pending int, eventtime float64) float64 {
	// Check for upcoming messages now ready
	idletime := max(eventtime, sq.idleTime) + sq.calculateBittime(pending+msgproto.MessageMin)
	ackClock := sq.ce.ClockFromTime(idletime)
	minStalledClock := sq.checkUpcomingQueues(ackClock)

	// Check if valid to send messages
	if sq.sendSeq-sq.receiveSeq >= maxPendingBlocks && sq.receiveSeq != receiveSeqDebugFile {
		// Need an ack before more messages can be sent
		return eventtime + 0.250
	}
	if sq.sendSeq > sq.receiveSeq && sq.receiveWindow != 0 {
		needAckBytes := sq.needAckBytes + msgproto.MessageMax
		if sq.lastAckSeq < sq.receiveSeq {
			needAckBytes += sq.lastAckBytes
		}
		if needAckBytes > sq.receiveWindow {
			// Wait for ack from past messages before sending next message
			return eventtime + 0.250
		}
	}

	// Check if a block is fully ready to send
	if sq.readyBytes >= msgproto.MessagePayloadMax {
		return prNow
	}
	if sq.ce.EstFreq == 0 {
		// Clock unknown during initial startup - recheck on each add
		if sq.readyBytes > 0 {
			return prNow
		}
		if !sq.updateNeedKickClock(1) {
			return eventtime
		}
		return prNever
	}

	// Check if it is still needed to send messages from the ready_queues
	minReadyClock := MaxClock
	for _, cq := range sq.readyQueues {
		reqClock := cq.ready[0].reqClock
		bgtime := sq.idleTime
		if pending > 0 {
			bgtime = idletime
		}
		if reqClock == BackgroundPriorityClock {
			reqClock = sq.ce.ClockFromTime(bgtime + backgroundOffsetTime)
		}
		minReadyClock = min(minReadyClock, reqClock)
	}
	reqclockDelta := uint64(minReqtimeDelta * sq.ce.EstFreq)
	if minReadyClock <= ackClock+reqclockDelta {
		return prNow
	}

	// Determine next wakeup time
	if pending > 0 {
		// Caller wont sleep anyway - just return
		return eventtime
	}
	wantclock := min(minReadyClock-reqclockDelta, minStalledClock)
	if !sq.updateNeedKickClock(wantclock) {
		// Raced with add of new command - avoid sleeping
		return eventtime
	}
	return idletime + float64(wantclock-ackClock)/sq.ce.EstFreq
}

// commandEvent sends the blocks that are ready. It returns the next wake time.
func (sq *SerialQueue) commandEvent(eventtime float64) float64 {
	sq.mu.Lock()
	defer sq.mu.Unlock()
	var buf []byte
	var waketime float64
	for {
		waketime = sq.checkSendCommand(len(buf), eventtime)
		if waketime != prNow {
			break
		}
		buf = sq.buildAndSendCommand(buf, len(buf), eventtime)
		if len(buf)+msgproto.MessageMax > maxSendBuf {
			break
		}
	}
	if len(buf) > 0 {
		// Write message blocks
		sq.doWrite(buf)
		sq.bytesWrite += uint32(len(buf))
		sq.idleTime = max(eventtime, sq.idleTime) + sq.calculateBittime(len(buf))
		waketime = prNow
	}
	return waketime
}

// read reads the port and passes the bytes to the loop, until the port fails.
func (sq *SerialQueue) read() {
	defer close(sq.readDone)
	buf := make([]byte, 4096)
	for {
		n, err := sq.port.Read(buf)
		if n > 0 {
			select {
			case sq.input <- append([]byte(nil), buf[:n]...):
			case <-sq.exit:
				return
			}
		}
		if err != nil {
			sq.Exit()
			return
		}
	}
}

// background is the event loop (pollreactor_run of the C file): it handles the input, the kicks of
// the senders, and the two timers.
func (sq *SerialQueue) background() {
	defer func() {
		// Wake any waiting receivers
		sq.recvMu.Lock()
		sq.recvExited = true
		sq.recvMu.Unlock()
		sq.recvCond.Broadcast()
		close(sq.exited)
	}()
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		// Run the timers that are due.
		for {
			now := sq.Now()
			sq.mu.Lock()
			cmd, ret := sq.timerCmd, sq.timerRetrans
			sq.mu.Unlock()
			if ret <= now {
				w := sq.retransmitEvent(now)
				sq.mu.Lock()
				sq.timerRetrans = w
				sq.mu.Unlock()
				continue
			}
			if cmd <= now {
				w := sq.commandEvent(now)
				sq.mu.Lock()
				sq.timerCmd = w
				sq.mu.Unlock()
				continue
			}
			break
		}
		sq.mu.Lock()
		next := min(sq.timerCmd, sq.timerRetrans)
		sq.mu.Unlock()
		wait := time.Hour
		if next < prNever {
			wait = time.Duration(max(next-sq.Now(), 0) * float64(time.Second))
		}
		timer.Reset(wait)
		select {
		case <-sq.exit:
			return
		case data := <-sq.input:
			sq.inputEvent(sq.Now(), data)
		case <-sq.kick:
			// kick_event: send the commands now
			sq.mu.Lock()
			sq.timerCmd = prNow
			sq.mu.Unlock()
		case <-timer.C:
		}
	}
}

// Exit stops the loop. Pull returns false after the queue is empty.
func (sq *SerialQueue) Exit() {
	sq.exiting.Do(func() { close(sq.exit) })
	<-sq.exited
}

// Done is closed when the loop has stopped.
func (sq *SerialQueue) Done() <-chan struct{} { return sq.exited }

// ReaderDone is closed when the reading goroutine has ended. After Exit, it ends when the port
// closes or fails.
func (sq *SerialQueue) ReaderDone() <-chan struct{} { return sq.readDone }

// AllocCommandQueue makes a command queue.
func (sq *SerialQueue) AllocCommandQueue() *CommandQueue { return &CommandQueue{} }

// AddFastReader adds a reader for the messages with a prefix.
func (sq *SerialQueue) AddFastReader(fr *FastReader) {
	sq.fastMu.Lock()
	sq.fastReaders = append(sq.fastReaders, fr)
	sq.fastMu.Unlock()
}

// RemoveFastReader removes a reader. A call of the reader that runs now finishes first.
func (sq *SerialQueue) RemoveFastReader(fr *FastReader) {
	sq.fastMu.Lock()
	for i, f := range sq.fastReaders {
		if f == fr {
			sq.fastReaders = append(sq.fastReaders[:i], sq.fastReaders[i+1:]...)
			break
		}
	}
	sq.fastMu.Unlock()
}

// SendBatch adds commands to a command queue (serialqueue_send_batch).
func (sq *SerialQueue) SendBatch(cq *CommandQueue, msgs []Message) {
	// Make sure min_clock is set in list and calculate total bytes
	qms := make([]*queueMessage, 0, len(msgs))
	l := 0
	for _, m := range msgs {
		qm := &queueMessage{msg: append([]byte(nil), m.Data...), minClock: m.MinClock, reqClock: m.ReqClock, notifyID: m.NotifyID}
		if qm.minClock+(3<<29) < qm.reqClock && qm.reqClock != BackgroundPriorityClock {
			// Avoid mcu clock comparison 31-bit overflow issues
			qm.minClock = qm.reqClock - (3 << 29)
		}
		l += len(qm.msg)
		qms = append(qms, qm)
	}
	if l == 0 {
		return
	}
	minClock := qms[0].minClock

	// Add list to cq->upcoming_queue
	mustwake := false
	sq.treqMu.Lock()
	if len(cq.upcoming) == 0 {
		cq.inUpcoming = true
		sq.upcomingQueues = append(sq.upcomingQueues, cq)
		sq.minReleaseClock = min(sq.minReleaseClock, minClock)
		if minClock < sq.needKickClock {
			sq.needKickClock = 0
			mustwake = true
		}
	}
	cq.upcoming = append(cq.upcoming, qms...)
	sq.upcomingBytes += l
	sq.treqMu.Unlock()

	// Wake the background loop if necessary
	if mustwake {
		select {
		case sq.kick <- struct{}{}:
		default:
		}
	}
}

// Send schedules one command at a clock time and priority (serialqueue_send). min_clock is the
// earliest clock to send it, and req_clock is the clock that it must reach the board by. A notifyID
// other than 0 gives a Received message with that ID when the board acknowledges the block.
func (sq *SerialQueue) Send(cq *CommandQueue, msg []byte, minClock, reqClock, notifyID uint64) {
	sq.SendBatch(cq, []Message{{Data: msg, MinClock: minClock, ReqClock: reqClock, NotifyID: notifyID}})
}

// Pull returns the next message from the board, and waits for one. It returns false after Exit.
func (sq *SerialQueue) Pull() (Received, bool) {
	sq.recvMu.Lock()
	defer sq.recvMu.Unlock()
	for len(sq.recvQueue) == 0 {
		if sq.recvExited {
			return Received{}, false
		}
		sq.recvCond.Wait()
	}
	qm := sq.recvQueue[0]
	sq.recvQueue = sq.recvQueue[1:]
	if len(qm.msg) > 0 {
		sq.oldReceive = debugQueueAdd(sq.oldReceive, qm, debugQueueReceive)
	}
	return Received{Data: qm.msg, SentTime: qm.sentTime, ReceiveTime: qm.receiveTime, NotifyID: qm.notifyID}, true
}

// SetWireFrequency sets the baud rate of the port. An 8N1 serial line has 10 bits per byte.
func (sq *SerialQueue) SetWireFrequency(frequency float64) {
	sq.mu.Lock()
	sq.bittimeAdjust = 10. / frequency
	sq.mu.Unlock()
}

// SetReceiveWindow sets the receive buffer size of the board.
func (sq *SerialQueue) SetReceiveWindow(receiveWindow int) {
	sq.mu.Lock()
	sq.receiveWindow = receiveWindow
	sq.mu.Unlock()
}

// SetClockEst sets the estimated clock of the board.
func (sq *SerialQueue) SetClockEst(estFreq, convTime float64, convClock uint64) {
	sq.mu.Lock()
	sq.ce = ClockEstimate{ConvClock: convClock, ConvTime: convTime, EstFreq: estFreq}
	sq.mu.Unlock()
}

// ClockEst returns the latest clock estimate.
func (sq *SerialQueue) ClockEst() ClockEstimate {
	sq.mu.Lock()
	defer sq.mu.Unlock()
	return sq.ce
}

// Stats returns the statistics of the link, in the form of the C file.
func (sq *SerialQueue) Stats() string {
	sq.mu.Lock()
	sq.treqMu.Lock()
	defer sq.treqMu.Unlock()
	defer sq.mu.Unlock()
	return fmt.Sprintf("bytes_write=%d bytes_read=%d bytes_retransmit=%d bytes_invalid=%d"+
		" send_seq=%d receive_seq=%d retransmit_seq=%d srtt=%.3f rttvar=%.3f rto=%.3f"+
		" ready_bytes=%d upcoming_bytes=%d",
		sq.bytesWrite, sq.bytesRead, sq.bytesRetransmit, sq.bytesInvalid,
		int32(sq.sendSeq), int32(sq.receiveSeq), int32(sq.retransmitSeq),
		sq.srtt, sq.rttvar, sq.rto, sq.readyBytes, sq.upcomingBytes)
}

// ExtractOld returns the last sent blocks (sent is true) or the last received messages, for the
// log after an error.
func (sq *SerialQueue) ExtractOld(sent bool) []Received {
	var q []*queueMessage
	if sent {
		sq.mu.Lock()
		q, sq.oldSent = sq.oldSent, nil
		sq.mu.Unlock()
	} else {
		sq.recvMu.Lock()
		q, sq.oldReceive = sq.oldReceive, nil
		sq.recvMu.Unlock()
	}
	out := make([]Received, 0, len(q))
	for _, qm := range q {
		out = append(out, Received{Data: qm.msg, SentTime: qm.sentTime, ReceiveTime: qm.receiveTime})
	}
	return out
}
