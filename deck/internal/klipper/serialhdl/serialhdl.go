// Package serialhdl manages the link to one board: it connects, reads the data dictionary of the
// firmware (identify), sends commands, and passes the responses to their handlers.
//
// Ported to Go from klippy/serialhdl.py of Klipper (https://github.com/Klipper3d/klipper), commit
// 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f. CAN bus support is not ported.
//
//	Copyright (C) 2016-2021  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package serialhdl

import (
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"sync/atomic"

	"printdeck/internal/klipper/msgproto"
	"printdeck/internal/klipper/reactor"
	"printdeck/internal/klipper/serialport"
	"printdeck/internal/klipper/simmcu"
	"printdeck/internal/klipper/serialqueue"
)

// Error is an error of the link.
type Error struct{ msg string }

func (e *Error) Error() string { return e.msg }

// Params are the parameters of a response, by name. "#name" is the name of the response, and
// "#sent_time" and "#receive_time" are host times.
type Params map[string]any

// Int returns an integer parameter.
func (p Params) Int(name string) int64 {
	v, _ := p[name].(int64)
	return v
}

// Bytes returns a buffer parameter.
func (p Params) Bytes(name string) []byte {
	v, _ := p[name].([]byte)
	return v
}

// Float returns a host time parameter, such as "#sent_time".
func (p Params) Float(name string) float64 {
	v, _ := p[name].(float64)
	return v
}

// Handler handles a response from the board. Handlers run on the reading goroutine.
type Handler func(params Params)

type handlerKey struct {
	name string
	oid  int64 // noOID for a response with no oid
}

const noOID = -1

// Port is the port of a link: a serial port, or a debug file in batch mode.
type Port interface {
	io.ReadWriter
	Close() error
}

// SerialReader is the link to one board.
type SerialReader struct {
	reactor    *reactor.Reactor
	warnPrefix string
	mcuName    string
	sqName     string

	port      Port
	msgparser atomic.Pointer[msgproto.Parser] // replaced after identify, read by bgThread
	sq        *serialqueue.SerialQueue

	defaultCmdQueue *serialqueue.CommandQueue
	bgDone          chan struct{}

	mu             sync.Mutex // protects handlers and defaultHandler
	handlers       map[handlerKey]Handler
	defaultHandler Handler // for the responses with no handler; nil logs them

	notifyMu             sync.Mutex
	lastNotifyID         uint64
	pendingNotifications map[uint64]*reactor.Completion
}

// New makes a link for the board mcuName ("" for the main board).
func New(r *reactor.Reactor, mcuName string) *SerialReader {
	s := &SerialReader{reactor: r, mcuName: mcuName,
		handlers: map[handlerKey]Handler{}, pendingNotifications: map[uint64]*reactor.Completion{}}
	s.msgparser.Store(msgproto.NewParser())
	if mcuName != "" {
		s.warnPrefix = fmt.Sprintf("mcu '%s': ", mcuName)
	}
	s.sqName = trim15("serialq " + mcuName)
	s.msgparser.Load().WarnPrefix = s.warnPrefix
	s.defaultCmdQueue = &serialqueue.CommandQueue{}
	s.RegisterResponse(s.handleUnknownInit, "#unknown", noOID)
	s.RegisterResponse(s.handleOutput, "#output", noOID)
	return s
}

func trim15(s string) string {
	if len(s) > 15 {
		return s[:15]
	}
	return s
}

func (s *SerialReader) errorf(format string, args ...any) error {
	return &Error{s.warnPrefix + fmt.Sprintf(format, args...)}
}

// bgThread passes the messages from the board to their handlers, and the notifications of sent
// commands to their waiting code.
func (s *SerialReader) bgThread(sq *serialqueue.SerialQueue, done chan struct{}) {
	defer close(done)
	for {
		resp, ok := sq.Pull()
		if !ok {
			return
		}
		if resp.NotifyID != 0 {
			params := Params{"#sent_time": resp.SentTime, "#receive_time": resp.ReceiveTime}
			s.notifyMu.Lock()
			completion := s.pendingNotifications[resp.NotifyID]
			delete(s.pendingNotifications, resp.NotifyID)
			s.notifyMu.Unlock()
			if completion != nil {
				s.reactor.AsyncComplete(completion, params)
			}
			continue
		}
		raw, err := s.msgparser.Load().Parse(resp.Data)
		if err != nil {
			log.Printf("%sException in serial callback: %v", s.warnPrefix, err)
			continue
		}
		params := Params{}
		for k, v := range raw {
			params[k] = v
		}
		params["#sent_time"] = resp.SentTime
		params["#receive_time"] = resp.ReceiveTime
		key := handlerKey{name: params["#name"].(string), oid: noOID}
		if oid, ok := params["oid"].(int64); ok {
			key.oid = oid
		}
		s.mu.Lock()
		hdl := s.handlers[key]
		if hdl == nil {
			hdl = s.defaultHandler
		}
		if hdl == nil {
			hdl = s.handleDefault
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("%sException in serial callback: %v", s.warnPrefix, r)
				}
			}()
			hdl(params)
		}()
		s.mu.Unlock()
	}
}

// getIdentifyData reads the data dictionary from the board, 40 bytes at a time. It runs as a
// reactor callback, and returns nil on an error.
func (s *SerialReader) getIdentifyData(eventtime float64) any {
	var identifyData []byte
	for {
		msg := fmt.Sprintf("identify offset=%d count=%d", len(identifyData), 40)
		params, err := s.SendWithResponse(msg, "identify_response")
		if err != nil {
			log.Printf("%sWait for identify_response: %v", s.warnPrefix, err)
			return nil
		}
		if params.Int("offset") == int64(len(identifyData)) {
			msgdata := params.Bytes("data")
			if len(msgdata) == 0 {
				// Done
				return identifyData
			}
			identifyData = append(identifyData, msgdata...)
		}
	}
}

// startSession starts the queue on an open port and reads the data dictionary. It runs in reactor
// code, and returns false when the board does not answer within 5 seconds.
func (s *SerialReader) startSession(port Port) bool {
	s.port = port
	s.sq = serialqueue.New(port, serialqueue.UART, s.sqName)
	s.bgDone = make(chan struct{})
	go s.bgThread(s.sq, s.bgDone)
	// Obtain and load the data dictionary from the firmware
	completion := s.reactor.RegisterCallback(s.getIdentifyData, reactor.Now)
	identifyData, _ := completion.Wait(s.reactor.Monotonic()+5., nil).([]byte)
	if identifyData == nil {
		log.Printf("%sTimeout on connect", s.warnPrefix)
		s.Disconnect()
		return false
	}
	msgparser := msgproto.NewParser()
	msgparser.WarnPrefix = s.warnPrefix
	if err := msgparser.ProcessIdentify(identifyData, true); err != nil {
		log.Printf("%s%v", s.warnPrefix, err)
		s.Disconnect()
		return false
	}
	s.msgparser.Store(msgparser)
	s.RegisterResponse(s.handleUnknown, "#unknown", noOID)
	// Setup baud adjust
	if wireFreq, err := msgparser.ConstantFloat("SERIAL_BAUD"); err == nil {
		s.sq.SetWireFrequency(wireFreq)
	}
	if receiveWindow, err := msgparser.ConstantInt("RECEIVE_WINDOW"); err == nil {
		s.sq.SetReceiveWindow(int(receiveWindow))
	}
	return true
}

// ConnectPipe connects to a pseudo-terminal or a pipe, such as a simulator. "sim" is the simulated
// board of the deck (simmcu), in this process. It runs in reactor code, and tries for 90 seconds.
func (s *SerialReader) ConnectPipe(filename string) error {
	log.Printf("%sStarting connect", s.warnPrefix)
	start := s.reactor.Monotonic()
	for {
		if s.reactor.Monotonic() > start+90. {
			return s.errorf("Unable to connect")
		}
		var p Port
		var err error
		if filename == simmcu.Serial {
			p, err = simmcu.Open()
		} else {
			p, err = serialport.Open(filename, 250000, false)
		}
		if err != nil {
			log.Printf("%sUnable to open port: %v", s.warnPrefix, err)
			s.reactor.Pause(s.reactor.Monotonic() + 5.)
			continue
		}
		if s.startSession(p) {
			return nil
		}
	}
}

// ConnectUART connects to a serial port. It runs in reactor code, and tries for 90 seconds.
func (s *SerialReader) ConnectUART(serialPort string, baud int, rts bool) error {
	log.Printf("%sStarting serial connect", s.warnPrefix)
	start := s.reactor.Monotonic()
	for {
		if s.reactor.Monotonic() > start+90. {
			return s.errorf("Unable to connect")
		}
		p, err := serialport.Open(serialPort, baud, rts)
		if err != nil {
			log.Printf("%sUnable to open serial port: %v", s.warnPrefix, err)
			s.reactor.Pause(s.reactor.Monotonic() + 5.)
			continue
		}
		Stk500v2Leave(p, s.reactor, baud)
		if s.startSession(p) {
			return nil
		}
	}
}

// TryUART opens a serial port and reads the data dictionary, one time. It runs in reactor code,
// and fails when the board does not answer within 5 seconds. Klipper has no such method: the deck
// uses it to find a board with Klipper firmware.
func (s *SerialReader) TryUART(serialPort string, baud int, rts bool) error {
	p, err := serialport.Open(serialPort, baud, rts)
	if err != nil {
		return s.errorf("Unable to open serial port: %v", err)
	}
	Stk500v2Leave(p, s.reactor, baud)
	if !s.startSession(p) {
		return s.errorf("No answer from the board")
	}
	return nil
}

// ConnectFile writes the commands to a file with no answers (klippy's batch mode), with the data
// dictionary of the board.
func (s *SerialReader) ConnectFile(debugoutput Port, dictionary []byte) error {
	s.port = debugoutput
	if err := s.msgparser.Load().ProcessIdentify(dictionary, false); err != nil {
		return err
	}
	s.sq = serialqueue.New(debugoutput, serialqueue.DebugFile, s.sqName)
	return nil
}

// SetClockEst sets the clock estimate of the queue.
func (s *SerialReader) SetClockEst(freq, convTime float64, convClock uint64) {
	s.sq.SetClockEst(freq, convTime, convClock)
}

// Disconnect stops the queue and closes the port. The code that waits for a sent command gets nil.
func (s *SerialReader) Disconnect() {
	var readerDone <-chan struct{}
	if s.sq != nil {
		s.sq.Exit()
		if s.bgDone != nil {
			<-s.bgDone
		}
		readerDone = s.sq.ReaderDone()
		s.sq, s.bgDone = nil, nil
	}
	if s.port != nil {
		s.port.Close()
		s.port = nil
	}
	// The file closes when its last read returns. Wait for it, so the port and its lock are free
	// when Disconnect returns.
	if readerDone != nil {
		<-readerDone
	}
	s.notifyMu.Lock()
	pending := s.pendingNotifications
	s.pendingNotifications = map[uint64]*reactor.Completion{}
	s.notifyMu.Unlock()
	for _, c := range pending {
		c.Complete(nil)
	}
}

// Stats returns the statistics of the link.
func (s *SerialReader) Stats(eventtime float64) string {
	if s.sq == nil {
		return ""
	}
	return s.sq.Stats()
}

func (s *SerialReader) Reactor() *reactor.Reactor                      { return s.reactor }
func (s *SerialReader) MsgParser() *msgproto.Parser                    { return s.msgparser.Load() }
func (s *SerialReader) SerialQueue() *serialqueue.SerialQueue          { return s.sq }
func (s *SerialReader) DefaultCommandQueue() *serialqueue.CommandQueue { return s.defaultCmdQueue }

// RegisterResponse sets the handler of a response, for one oid or for noOID. A nil callback
// removes the handler.
func (s *SerialReader) RegisterResponse(callback Handler, name string, oid int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if callback == nil {
		delete(s.handlers, handlerKey{name, oid})
	} else {
		s.handlers[handlerKey{name, oid}] = callback
	}
}

// SetDefaultHandler sets the handler of the responses that have no handler. nil logs them, as
// klippy does.
func (s *SerialReader) SetDefaultHandler(h Handler) {
	s.mu.Lock()
	s.defaultHandler = h
	s.mu.Unlock()
}

// NoOID is the oid of RegisterResponse for a response with no oid.
const NoOID = noOID

// RawSend queues an encoded command.
func (s *SerialReader) RawSend(cmd []byte, minclock, reqclock uint64, cq *serialqueue.CommandQueue) {
	s.sq.Send(cq, cmd, minclock, reqclock, 0)
}

// RawSendWaitAck queues an encoded command and waits until the board acknowledges it. It runs in
// reactor code.
func (s *SerialReader) RawSendWaitAck(cmd []byte, minclock, reqclock uint64, cq *serialqueue.CommandQueue) (Params, error) {
	s.notifyMu.Lock()
	s.lastNotifyID++
	nid := s.lastNotifyID
	completion := s.reactor.NewCompletion()
	s.pendingNotifications[nid] = completion
	s.notifyMu.Unlock()
	s.sq.Send(cq, cmd, minclock, reqclock, nid)
	params, _ := completion.Wait(reactor.Never, nil).(Params)
	if params == nil {
		return nil, s.errorf("Serial connection closed")
	}
	return params, nil
}

// Send queues a command text, for example "get_uptime".
func (s *SerialReader) Send(msg string, minclock, reqclock uint64) error {
	cmd, err := s.msgparser.Load().CreateCommand(msg)
	if err != nil {
		return err
	}
	s.RawSend(cmd, minclock, reqclock, s.defaultCmdQueue)
	return nil
}

// SendWithResponse sends a command text and waits for a response with the name response. It runs
// in reactor code.
func (s *SerialReader) SendWithResponse(msg, response string) (Params, error) {
	cmd, err := s.msgparser.Load().CreateCommand(msg)
	if err != nil {
		return nil, err
	}
	src := NewRetryCommand(s, response, noOID)
	return src.GetResponse([][]byte{cmd}, s.defaultCmdQueue, 0, 0, true)
}

// AllocCommandQueue makes a command queue.
func (s *SerialReader) AllocCommandQueue() *serialqueue.CommandQueue {
	return &serialqueue.CommandQueue{}
}

// DumpDebug returns the last sent and received messages, for the log after an error.
func (s *SerialReader) DumpDebug() string {
	if s.sq == nil {
		return ""
	}
	out := []string{fmt.Sprintf("Dumping serial stats: %s", s.Stats(s.reactor.Monotonic()))}
	sent := s.sq.ExtractOld(true)
	recv := s.sq.ExtractOld(false)
	out = append(out, fmt.Sprintf("Dumping send queue %d messages", len(sent)))
	for i, m := range sent {
		cmds, _ := s.msgparser.Load().Dump(m.Data)
		out = append(out, fmt.Sprintf("Sent %02d %f %f %02d: %s", i, m.ReceiveTime, m.SentTime, len(m.Data), strings.Join(cmds, ", ")))
	}
	out = append(out, fmt.Sprintf("Dumping receive queue %d messages", len(recv)))
	for i, m := range recv {
		cmds, _ := s.msgparser.Load().Dump(m.Data)
		out = append(out, fmt.Sprintf("Receive: %02d %f %f %02d: %s", i, m.ReceiveTime, m.SentTime, len(m.Data), strings.Join(cmds, ", ")))
	}
	return strings.Join(out, "\n")
}

// Default message handlers

// handleUnknownInit handles a message before the dictionary is loaded, for example a sensor
// report of an earlier session. klippy logs these at debug level.
func (s *SerialReader) handleUnknownInit(params Params) {
	if Debug {
		log.Printf("%sUnknown message %d (len %d) while identifying", s.warnPrefix, params.Int("#msgid"), len(params.Bytes("#msg")))
	}
}

// Debug turns on the debug messages of the link.
var Debug = false

func (s *SerialReader) handleUnknown(params Params) {
	log.Printf("%sUnknown message type %d: %s", s.warnPrefix, params.Int("#msgid"), msgproto.PyBytesRepr(params.Bytes("#msg")))
}

func (s *SerialReader) handleOutput(params Params) {
	log.Printf("%s%s: %v", s.warnPrefix, params["#name"], params["#msg"])
}

func (s *SerialReader) handleDefault(params Params) {
	log.Printf("%sgot %v", s.warnPrefix, map[string]any(params))
}

// RetryCommand sends a query command and returns the received response (SerialRetryCommand).
type RetryCommand struct {
	serial     *SerialReader
	name       string
	oid        int64
	mu         sync.Mutex
	lastParams Params
}

// NewRetryCommand registers the handler of the response name for oid (or NoOID).
func NewRetryCommand(serial *SerialReader, name string, oid int64) *RetryCommand {
	rc := &RetryCommand{serial: serial, name: name, oid: oid}
	serial.RegisterResponse(rc.handleCallback, name, oid)
	return rc
}

func (rc *RetryCommand) handleCallback(params Params) {
	rc.mu.Lock()
	rc.lastParams = params
	rc.mu.Unlock()
}

// GetResponse sends the commands, the last one with a wait for its ack, until the response comes:
// 5 tries with a doubling delay, from 10 ms. It runs in reactor code.
func (rc *RetryCommand) GetResponse(cmds [][]byte, cq *serialqueue.CommandQueue, minclock, reqclock uint64, retry bool) (Params, error) {
	retries := 5
	retryDelay := .010
	if !retry {
		retries = 0
	}
	for {
		for _, cmd := range cmds[:len(cmds)-1] {
			rc.serial.RawSend(cmd, minclock, reqclock, cq)
		}
		if _, err := rc.serial.RawSendWaitAck(cmds[len(cmds)-1], minclock, reqclock, cq); err != nil {
			rc.serial.RegisterResponse(nil, rc.name, rc.oid)
			return nil, err
		}
		rc.mu.Lock()
		params := rc.lastParams
		rc.mu.Unlock()
		if params != nil {
			rc.serial.RegisterResponse(nil, rc.name, rc.oid)
			return params, nil
		}
		if retries <= 0 {
			rc.serial.RegisterResponse(nil, rc.name, rc.oid)
			return nil, &Error{fmt.Sprintf("Unable to obtain '%s' response", rc.name)}
		}
		r := rc.serial.reactor
		r.Pause(r.Monotonic() + retryDelay)
		retries--
		retryDelay *= 2.
	}
}

// Stk500v2Leave sends the "leave programmer" sequence of an AVR stk500v2 bootloader, so the board
// runs its firmware.
func Stk500v2Leave(p *serialport.Port, r *reactor.Reactor, baud int) {
	p.ClearHUPCL()
	// Request a dummy speed first as this seems to help reset the port
	p.SetBaud(2400)
	p.ReadFor(0)
	// Send stk500v2 leave programmer sequence
	p.SetBaud(115200)
	r.Pause(r.Monotonic() + 0.100)
	p.ReadFor(0)
	p.Write([]byte{0x1b, 0x01, 0x00, 0x01, 0x0e, 0x11, 0x04})
	r.Pause(r.Monotonic() + 0.050)
	res, _ := p.ReadFor(0)
	log.Printf("Got %s from stk500v2", msgproto.PyBytesRepr(res))
	p.SetBaud(baud)
}

// ArduinoReset resets an Arduino style board (such as the RAMBo) with its DTR line.
func ArduinoReset(serialPort string, r *reactor.Reactor) error {
	// First try opening the port at a different baud
	p, err := serialport.Open(serialPort, 2400, true)
	if err != nil {
		return err
	}
	defer p.Close()
	p.ReadFor(0)
	r.Pause(r.Monotonic() + 0.100)
	// Then toggle DTR
	if err := p.SetDTR(true); err != nil {
		return err
	}
	r.Pause(r.Monotonic() + 0.100)
	if err := p.SetDTR(false); err != nil {
		return err
	}
	r.Pause(r.Monotonic() + 0.100)
	return nil
}
