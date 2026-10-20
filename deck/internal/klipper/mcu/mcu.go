// Package mcu is the host side of a Klipper board: its connection, its configuration (the oids
// and config commands of the printer objects), its statistics, its restart, and the objects of
// its pins (endstop, digital_out, pwm, adc).
//
// Ported to Go from klippy/mcu.py of Klipper (https://github.com/Klipper3d/klipper), commit
// 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f. The trigger dispatch of the homing moves
// (klippy/chelper/trdispatch.c) is in trdispatch.go. The CAN bus link and the rpi_usb restart
// are not ported.
//
//	Copyright (C) 2016-2026  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package mcu

import (
	"errors"
	"fmt"
	"hash/crc32"
	"log"
	"math"
	"os"
	"sort"
	"strings"

	"printdeck/internal/klipper/clocksync"
	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/msgproto"
	"printdeck/internal/klipper/pins"
	"printdeck/internal/klipper/serialhdl"
	"printdeck/internal/klipper/serialqueue"
)

// Error is an error of a board (mcu.error).
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

// ErrorKind tells the printer core that this is an MCU error.
func (e *Error) ErrorKind() string { return "mcu" }

func raise(format string, args ...any) { panic(&Error{fmt.Sprintf(format, args...)}) }

func pinError(format string, args ...any) { panic(&pins.Error{Msg: fmt.Sprintf(format, args...)}) }

const (
	// MinScheduleTime is the minimum time the host needs to queue events on the board.
	MinScheduleTime = 0.100
	// MaxScheduleTicks is the maximum number of clock cycles a board can schedule into the future.
	MaxScheduleTicks = (1 << 31) - 1
	// MaxNominalDuration is the maximum time all boards can schedule into the future.
	MaxNominalDuration = 3.0
)

// Command transmit helper classes

type responder interface {
	getResponse(cmds [][]byte, cq *serialqueue.CommandQueue, minclock, reqclock uint64, retry bool) (serialhdl.Params, error)
}

// dummyResponse gives a fixed response to the queries in batch mode.
type dummyResponse struct{ response serialhdl.Params }

func newDummyResponse(serial *serialhdl.SerialReader, name string, oid int64) responder {
	params := map[string]msgproto.Value{}
	if oid != serialhdl.NoOID {
		params["oid"] = oid
	}
	resp, err := serial.MsgParser().CreateDummyResponse(name, params)
	if err != nil {
		panic(err)
	}
	r := serialhdl.Params{}
	for k, v := range resp {
		r[k] = v
	}
	r["#sent_time"] = 0.
	r["#receive_time"] = 0.
	return &dummyResponse{r}
}

func (d *dummyResponse) getResponse([][]byte, *serialqueue.CommandQueue, uint64, uint64, bool) (serialhdl.Params, error) {
	out := serialhdl.Params{}
	for k, v := range d.response {
		out[k] = v
	}
	return out, nil
}

type retryResponder struct{ rc *serialhdl.RetryCommand }

func (r retryResponder) getResponse(cmds [][]byte, cq *serialqueue.CommandQueue, minclock, reqclock uint64, retry bool) (serialhdl.Params, error) {
	return r.rc.GetResponse(cmds, cq, minclock, reqclock, retry)
}

// retryAsyncCommand sends a query until a response comes that was sent after the query.
type retryAsyncCommand struct {
	serial       *serialhdl.SerialReader
	name         string
	oid          int64
	completion   asyncCompletion
	minQueryTime float64
	needResponse bool
}

type asyncCompletion interface {
	Wait(waketime float64, timeoutResult any) any
}

const (
	asyncTimeoutTime = 5.0
	asyncRetryTime   = 0.500
)

func newRetryAsyncCommand(serial *serialhdl.SerialReader, name string, oid int64) responder {
	r := serial.Reactor()
	c := r.NewCompletion()
	ra := &retryAsyncCommand{serial: serial, name: name, oid: oid, completion: c,
		minQueryTime: r.Monotonic(), needResponse: true}
	serial.RegisterResponse(func(params serialhdl.Params) {
		if ra.needResponse && params.Float("#sent_time") >= ra.minQueryTime {
			ra.needResponse = false
			r.AsyncComplete(c, params)
		}
	}, name, oid)
	return ra
}

func (ra *retryAsyncCommand) getResponse(cmds [][]byte, cq *serialqueue.CommandQueue, minclock, reqclock uint64, retry bool) (serialhdl.Params, error) {
	cmd := cmds[0]
	r := ra.serial.Reactor()
	if _, err := ra.serial.RawSendWaitAck(cmd, minclock, reqclock, cq); err != nil {
		return nil, err
	}
	ra.minQueryTime = 0.
	queryTime := r.Monotonic()
	timeoutTime := queryTime
	if retry {
		timeoutTime += asyncTimeoutTime
	}
	for {
		if params, ok := ra.completion.Wait(queryTime+asyncRetryTime, nil).(serialhdl.Params); ok {
			ra.serial.RegisterResponse(nil, ra.name, ra.oid)
			return params, nil
		}
		queryTime = r.Monotonic()
		if queryTime > timeoutTime {
			ra.serial.RegisterResponse(nil, ra.name, ra.oid)
			return nil, fmt.Errorf("Timeout on wait for '%s' response", ra.name)
		}
		ra.serial.RawSend(cmd, minclock, minclock, cq)
	}
}

// CommandQueryWrapper sends a query command and returns its response.
type CommandQueryWrapper struct {
	serial     *serialhdl.SerialReader
	cmd        *msgproto.MessageFormat
	response   string
	oid        int64
	xmitHelper func(serial *serialhdl.SerialReader, name string, oid int64) responder
	cmdQueue   *serialqueue.CommandQueue
}

func lookup(serial *serialhdl.SerialReader, msgformat string) *msgproto.MessageFormat {
	cmd, err := serial.MsgParser().LookupCommand(msgformat)
	if err != nil {
		panic(err)
	}
	return cmd
}

// Send sends the query with its parameters, and returns the response. An error of the link is a
// command error.
func (c *CommandQueryWrapper) Send(data []any, minclock, reqclock int64, retry bool) serialhdl.Params {
	return c.doSend([][]byte{encode(c.cmd, data)}, minclock, reqclock, retry)
}

// SendWithPreface sends a command before the query, in the same block.
func (c *CommandQueryWrapper) SendWithPreface(preface *CommandWrapper, prefaceData, data []any, minclock, reqclock int64, retry bool) serialhdl.Params {
	cmds := [][]byte{encode(preface.cmd, prefaceData), encode(c.cmd, data)}
	return c.doSend(cmds, minclock, reqclock, retry)
}

func (c *CommandQueryWrapper) doSend(cmds [][]byte, minclock, reqclock int64, retry bool) serialhdl.Params {
	xh := c.xmitHelper(c.serial, c.response, c.oid)
	reqclock = max(minclock, reqclock)
	params, err := xh.getResponse(cmds, c.cmdQueue, uint64(minclock), uint64(reqclock), retry)
	if err != nil {
		panic(gcode.Errorf("%s", err.Error()))
	}
	return params
}

func encode(cmd *msgproto.MessageFormat, data []any) []byte {
	args := make([]msgproto.Value, len(data))
	for i, d := range data {
		args[i] = d
	}
	b, err := cmd.Encode(args...)
	if err != nil {
		panic(err)
	}
	return b
}

// CommandWrapper sends a command.
type CommandWrapper struct {
	serial     *serialhdl.SerialReader
	cmd        *msgproto.MessageFormat
	cmdQueue   *serialqueue.CommandQueue
	msgtag     uint32
	fileoutput bool
}

// Send queues the command with its parameters.
func (c *CommandWrapper) Send(data []any, minclock, reqclock int64) {
	c.serial.RawSend(encode(c.cmd, data), uint64(minclock), uint64(reqclock), c.cmdQueue)
}

// SendWaitAck queues the command and waits until the board acknowledges it. In batch mode, it
// does not wait.
func (c *CommandWrapper) SendWaitAck(data []any, minclock, reqclock int64) {
	if c.fileoutput {
		c.Send(data, minclock, reqclock)
		return
	}
	if _, err := c.serial.RawSendWaitAck(encode(c.cmd, data), uint64(minclock), uint64(reqclock), c.cmdQueue); err != nil {
		panic(&Error{err.Error()})
	}
}

// CommandTag is the tag of the command, for the C helpers.
func (c *CommandWrapper) CommandTag() uint32 { return c.msgtag }

// AsyncResponseWrapper is a handler of a response that the board sends at any time.
type AsyncResponseWrapper struct {
	serial    *serialhdl.SerialReader
	callback  serialhdl.Handler
	msgformat string
	name      string
	oid       int64
}

func (a *AsyncResponseWrapper) register() {
	lookup(a.serial, a.msgformat)
	a.serial.RegisterResponse(a.callback, a.name, a.oid)
}

// Unregister removes the handler.
func (a *AsyncResponseWrapper) Unregister() { a.serial.RegisterResponse(nil, a.name, a.oid) }

// Main MCU class (and its helper classes)

// MCU is one board.
type MCU struct {
	printer   *host.Printer
	clocksync *clocksync.ClockSync
	name      string
	serial    *serialhdl.SerialReader
	conn      *connectHelper
	cfg       *configHelper
	stats     *statsHelper
}

// New makes the board of a config section ("[mcu]" or "[mcu name]").
func New(config *configfile.Wrapper, cs *clocksync.ClockSync) *MCU {
	m := &MCU{printer: host.Of(config), clocksync: cs, name: config.Name()}
	m.name = strings.TrimPrefix(m.name, "mcu ")
	// Low-level connection and helpers
	m.conn = newConnectHelper(config, m, cs)
	m.serial = m.conn.serial
	m.cfg = newConfigHelper(m, m.conn)
	m.stats = newStatsHelper(m, m.conn)
	m.printer.LoadObject(config, "error_mcu")
	return m
}

func init() {
	host.ConfigObjects = append(host.ConfigObjects, func(config *configfile.Wrapper) {
		printer := host.Of(config)
		mainsync := clocksync.New(printer.Reactor())
		printer.AddObject("mcu", New(config.GetSection("mcu"), mainsync))
		if len(config.PrefixSections("mcu ")) > 0 {
			configfile.Raise("A second board ([mcu name]) is not supported")
		}
	})
}

// Lookup returns the board of a name ("mcu", or the name of "[mcu name]").
func Lookup(printer *host.Printer, name string) *MCU {
	if name == "mcu" {
		return printer.LookupObject(name).(*MCU)
	}
	return printer.LookupObject("mcu " + name).(*MCU)
}

func (m *MCU) Name() string           { return m.name }
func (m *MCU) Printer() *host.Printer { return m.printer }

// IsFileoutput reports klippy's batch mode, which writes the commands to a file.
func (m *MCU) IsFileoutput() bool {
	_, ok := m.printer.StartArgs().Get("debugoutput")
	return ok
}

// MCU Configuration wrappers

// SetupPin makes the object of a pin (pins.Chip).
func (m *MCU) SetupPin(pinType string, params *pins.Params) any {
	return m.cfg.setupPin(pinType, params)
}

// CreateOID allocates an oid.
func (m *MCU) CreateOID() int { return m.cfg.createOID() }

// RegisterConfigCallback adds a function that adds config commands at the config finalize.
func (m *MCU) RegisterConfigCallback(cb func()) { m.cfg.registerConfigCallback(cb) }

// AddConfigCmd adds a config command. An init command goes after the config, and a restart
// command goes in place of the config when the board has the config already.
func (m *MCU) AddConfigCmd(cmd string, isInit, onRestart bool) {
	m.cfg.addConfigCmd(cmd, isInit, onRestart)
}

// RequestMoveQueueSlot reserves a move queue slot of the board.
func (m *MCU) RequestMoveQueueSlot() { m.cfg.reservedMoveSlots++ }

// GetQuerySlot returns the clock of the first query of an oid.
func (m *MCU) GetQuerySlot(oid int) int64 { return m.cfg.getQuerySlot(oid) }

// SecondsToClock converts a time to board clocks.
func (m *MCU) SecondsToClock(time float64) int64 { return int64(time * m.cfg.mcuFreq) }

// RegisterPostInitCallback adds a function that runs after the config is sent.
func (m *MCU) RegisterPostInitCallback(cb func()) { m.cfg.registerPostInitCallback(cb) }

// IsConfigFinalized reports that the config commands are made.
func (m *MCU) IsConfigFinalized() bool { return m.cfg.finalized }

// Command Handler helpers

func (m *MCU) MinScheduleTime() float64    { return MinScheduleTime }
func (m *MCU) MaxNominalDuration() float64 { return MaxNominalDuration }

// LookupCommand returns the command of a format, for example "get_clock". cq nil is the default
// queue.
func (m *MCU) LookupCommand(msgformat string, cq *serialqueue.CommandQueue) *CommandWrapper {
	cmd := lookup(m.serial, msgformat)
	if cq == nil {
		cq = m.serial.DefaultCommandQueue()
	}
	id, _ := m.serial.MsgParser().LookupMsgID(msgformat)
	return &CommandWrapper{serial: m.serial, cmd: cmd, cmdQueue: cq, msgtag: uint32(id), fileoutput: m.IsFileoutput()}
}

// LookupQueryCommand returns a query command and the format of its response. oid is the oid of
// the response, or serialhdl.NoOID.
func (m *MCU) LookupQueryCommand(msgformat, respformat string, oid int64, cq *serialqueue.CommandQueue, isAsync bool) *CommandQueryWrapper {
	cmd := lookup(m.serial, msgformat)
	lookup(m.serial, respformat)
	c := &CommandQueryWrapper{serial: m.serial, cmd: cmd, response: strings.Fields(respformat)[0], oid: oid,
		xmitHelper: func(s *serialhdl.SerialReader, name string, oid int64) responder {
			return retryResponder{serialhdl.NewRetryCommand(s, name, oid)}
		}}
	if m.IsFileoutput() {
		c.xmitHelper = newDummyResponse
	} else if isAsync {
		c.xmitHelper = newRetryAsyncCommand
	}
	if cq == nil {
		cq = m.serial.DefaultCommandQueue()
	}
	c.cmdQueue = cq
	return c
}

// TryLookupCommand returns the command of a format, or nil when the board does not have it.
func (m *MCU) TryLookupCommand(msgformat string) *CommandWrapper {
	if _, err := m.serial.MsgParser().LookupCommand(msgformat); err != nil {
		return nil
	}
	return m.LookupCommand(msgformat, nil)
}

// AllocCommandQueue makes a command queue.
func (m *MCU) AllocCommandQueue() *serialqueue.CommandQueue { return m.serial.AllocCommandQueue() }

// RegisterSerialResponse adds the handler of a response that the board sends at any time. Before
// the config is finalized, the handler starts after the config.
func (m *MCU) RegisterSerialResponse(cb serialhdl.Handler, msgformat string, oid int64) *AsyncResponseWrapper {
	a := &AsyncResponseWrapper{serial: m.serial, callback: cb, msgformat: msgformat,
		name: strings.Fields(msgformat)[0], oid: oid}
	if m.cfg.finalized {
		a.register()
	} else {
		m.serial.RegisterResponse(func(serialhdl.Params) {}, a.name, oid)
		m.cfg.registerPostInitCallback(a.register)
	}
	return a
}

// CheckValidResponse reports that the board has a response format.
func (m *MCU) CheckValidResponse(msgformat string) bool {
	_, err := m.serial.MsgParser().LookupCommand(msgformat)
	return err == nil
}

// MsgParser wrappers

func (m *MCU) Enumerations() map[string]map[string]int64 { return m.serial.MsgParser().Enumerations() }
func (m *MCU) Constants() map[string]any                 { return m.serial.MsgParser().Constants() }

// ConstantFloat returns a firmware constant. A missing constant is a protocol error.
func (m *MCU) ConstantFloat(name string) float64 {
	f, err := m.serial.MsgParser().ConstantFloat(name)
	if err != nil {
		panic(err)
	}
	return f
}

// ClockSync wrappers

func (m *MCU) PrintTimeToClock(printTime float64) int64 {
	return m.clocksync.PrintTimeToClock(printTime)
}
func (m *MCU) ClockToPrintTime(clock float64) float64 { return m.clocksync.ClockToPrintTime(clock) }
func (m *MCU) Clock32ToClock64(clock32 uint32) int64 {
	return int64(m.clocksync.Clock32ToClock64(clock32))
}

// EstimatedPrintTime returns the print time at a host time. It is 0 in batch mode.
func (m *MCU) EstimatedPrintTime(eventtime float64) float64 {
	if m.IsFileoutput() {
		return 0.
	}
	return m.clocksync.EstimatedPrintTime(eventtime)
}

// CalibrateClock returns the print time offset and the frequency, and checks the link timeout.
func (m *MCU) CalibrateClock(printTime, eventtime float64) (float64, float64) {
	offset, freq := m.clocksync.CalibrateClock(printTime, eventtime)
	m.conn.checkTimeout(eventtime)
	return offset, freq
}

// Serial returns the link of the board.
func (m *MCU) Serial() *serialhdl.SerialReader { return m.serial }

// Statistics wrappers

func (m *MCU) Status(eventtime float64) map[string]any { return m.stats.status() }
func (m *MCU) Stats(eventtime float64) (bool, string)  { return m.stats.stats(eventtime) }

// Low-level mcu connection management helper

type connectHelper struct {
	mcu           *MCU
	clocksync     *clocksync.ClockSync
	printer       *host.Printer
	name          string
	serial        *serialhdl.SerialReader
	serialport    string
	baud          int
	emergencyStop *CommandWrapper
	isShutdown    bool
	isTimeout     bool
	shutdownMsg   string
	restart       *restartHelper
}

func newConnectHelper(config *configfile.Wrapper, m *MCU, cs *clocksync.ClockSync) *connectHelper {
	printer := host.Of(config)
	c := &connectHelper{mcu: m, clocksync: cs, printer: printer, name: m.name,
		serial: serialhdl.New(printer.Reactor(), m.name)}
	if _, ok := config.GetOptional("canbus_uuid", configfile.Default(nil)); ok {
		configfile.Raise("CAN bus boards are not supported")
	}
	c.serialport = config.Get("serial")
	if !strings.HasPrefix(c.serialport, "/dev/rpmsg_") && !strings.HasPrefix(c.serialport, "/tmp/klipper_host_") {
		c.baud = config.GetInt("baud", configfile.Default(250000), configfile.Min(2400))
	}
	printer.RegisterEventHandler("klippy:mcu_identify", func(...any) { c.mcuIdentify() })
	c.restart = newRestartHelper(config, c)
	printer.RegisterEventHandler("klippy:shutdown", func(...any) { c.shutdown(false) })
	printer.RegisterEventHandler("klippy:analyze_shutdown", func(args ...any) { c.analyzeShutdown() })
	return c
}

func (c *connectHelper) handleShutdown(params serialhdl.Params) {
	if c.isShutdown {
		return
	}
	c.isShutdown = true
	msg := fmt.Sprint(params["static_string_id"])
	c.shutdownMsg = msg
	var shutdownClock any
	if clk, ok := params["clock"]; ok {
		v, _ := clk.(int64)
		shutdownClock = c.mcu.Clock32ToClock64(uint32(v))
	}
	c.printer.InvokeAsyncShutdown("MCU shutdown", map[string]any{"reason": msg, "mcu": c.name,
		"event_type": params["#name"], "shutdown_clock": shutdownClock})
}

func (c *connectHelper) handleStarting(params serialhdl.Params) {
	if !c.isShutdown {
		c.printer.InvokeAsyncShutdown(fmt.Sprintf("MCU '%s' spontaneous restart", c.name), nil)
	}
}

func (c *connectHelper) logInfo() string {
	p := c.serial.MsgParser()
	version, buildVersions := p.Version()
	consts := p.Constants()
	names := make([]string, 0, len(consts))
	for k := range consts {
		names = append(names, k)
	}
	sort.Strings(names)
	var kv []string
	for _, k := range names {
		v, _ := p.Constant(k)
		kv = append(kv, k+"="+v)
	}
	return strings.Join([]string{
		fmt.Sprintf("Loaded MCU '%s' %d commands (%s / %s)", c.name, len(p.Messages()), version, buildVersions),
		fmt.Sprintf("MCU '%s' config: %s", c.name, strings.Join(kv, " ")),
		fmt.Sprintf("MCU '%s' kconfig: %v", c.name, p.Kconfig()),
	}, "\n")
}

func (c *connectHelper) attachFile() {
	// In a debugging mode.  Open debug output file and read data dictionary
	args := c.printer.StartArgs()
	outName, _ := args.Get("debugoutput")
	dictName, _ := args.Get("dictionary")
	if c.name != "mcu" {
		outName += "-" + c.name
		dictName, _ = args.Get("dictionary_" + c.name)
	}
	outfile, err := os.Create(outName)
	if err != nil {
		panic(err)
	}
	dictData, err := os.ReadFile(dictName)
	if err != nil {
		panic(err)
	}
	if err := c.serial.ConnectFile(outfile, dictData); err != nil {
		panic(err)
	}
	if err := c.clocksync.ConnectFile(c.serial, false); err != nil {
		panic(err)
	}
}

func (c *connectHelper) attach() {
	c.restart.checkRestartOnAttach()
	var err error
	if c.baud != 0 {
		err = c.serial.ConnectUART(c.serialport, c.baud, c.restart.lookupAttachUartRTS())
	} else {
		err = c.serial.ConnectPipe(c.serialport)
	}
	if err == nil {
		err = c.clocksync.Connect(c.serial)
	}
	if err != nil {
		var me *msgproto.Error
		if errors.As(err, &me) {
			panic(err)
		}
		raise("%s", err.Error())
	}
}

func (c *connectHelper) mcuIdentify() {
	if c.mcu.IsFileoutput() {
		c.attachFile()
	} else {
		c.attach()
	}
	log.Print(c.logInfo())
	// Setup shutdown handling
	c.emergencyStop = c.mcu.LookupCommand("emergency_stop", nil)
	c.serial.RegisterResponse(c.handleShutdown, "shutdown", serialhdl.NoOID)
	c.serial.RegisterResponse(c.handleShutdown, "is_shutdown", serialhdl.NoOID)
	c.serial.RegisterResponse(c.handleStarting, "starting", serialhdl.NoOID)
}

func (c *connectHelper) analyzeShutdown() {
	if c.mcu.IsFileoutput() {
		return
	}
	log.Printf("MCU '%s' shutdown: %s\n%s\n%s", c.name, c.shutdownMsg, c.clocksync.DumpDebug(), c.serial.DumpDebug())
}

func (c *connectHelper) shutdown(force bool) {
	if c.emergencyStop == nil || (c.isShutdown && !force) {
		return
	}
	c.emergencyStop.Send(nil, 0, 0)
}

func (c *connectHelper) forceLocalShutdown() {
	c.isShutdown = true
	c.shutdown(true)
}

func (c *connectHelper) checkTimeout(eventtime float64) {
	if c.clocksync.IsActive() || c.mcu.IsFileoutput() || c.isTimeout {
		return
	}
	c.isTimeout = true
	log.Printf("Timeout with MCU '%s' (eventtime=%f)", c.name, eventtime)
	c.printer.InvokeShutdown(fmt.Sprintf("Lost communication with MCU '%s'", c.name), nil)
}

// Support for restarting a micro-controller

type restartHelper struct {
	printer        *host.Printer
	conn           *connectHelper
	mcu            *MCU
	name           string
	restartMethod  string
	resetCmd       *CommandWrapper
	configResetCmd *CommandWrapper
	isMCUBridge    bool
}

func newRestartHelper(config *configfile.Wrapper, conn *connectHelper) *restartHelper {
	r := &restartHelper{printer: conn.printer, conn: conn, mcu: conn.mcu, name: conn.name, restartMethod: "command"}
	if conn.baud != 0 {
		methods := map[string]any{"None": "", "arduino": "arduino", "cheetah": "cheetah", "command": "command", "rpi_usb": "rpi_usb"}
		r.restartMethod = config.GetChoice("restart_method", methods, configfile.Default(nil)).(string)
		if r.restartMethod == "rpi_usb" {
			configfile.Raise("restart_method rpi_usb is not supported")
		}
	}
	r.printer.RegisterEventHandler("klippy:firmware_restart", func(...any) { r.firmwareRestart(false) })
	r.printer.RegisterEventHandler("klippy:disconnect", func(...any) { r.disconnect() })
	r.printer.RegisterEventHandler("klippy:mcu_identify", func(...any) { r.mcuIdentify() })
	return r
}

// Connection phase

func (r *restartHelper) checkRestart(reason string) {
	if s, _ := r.printer.StartArgs().Get("start_reason"); s == "firmware_restart" {
		return
	}
	log.Printf("Attempting automated MCU '%s' restart: %s", r.name, reason)
	r.printer.RequestExit("firmware_restart")
	rc := r.printer.Reactor()
	rc.Pause(rc.Monotonic() + 2.000)
	raise("Attempt MCU '%s' restart failed", r.name)
}

func (r *restartHelper) checkRestartOnCRCMismatch() { r.checkRestart("CRC mismatch") }
func (r *restartHelper) checkRestartOnSendConfig()  {}
func (r *restartHelper) checkRestartOnAttach()      {}

// lookupAttachUartRTS: Cheetah boards require RTS to be deasserted, else a reset will trigger the
// built-in bootloader.
func (r *restartHelper) lookupAttachUartRTS() bool { return r.restartMethod != "cheetah" }

func (r *restartHelper) mcuIdentify() {
	r.resetCmd = r.mcu.TryLookupCommand("reset")
	r.configResetCmd = r.mcu.TryLookupCommand("config_reset")
	extOnly := r.resetCmd == nil && r.configResetCmd == nil
	p := r.conn.serial.MsgParser()
	_, mbaudErr := p.Constant("SERIAL_BAUD")
	if r.restartMethod == "" && mbaudErr != nil && !extOnly {
		r.restartMethod = "command"
	}
	if v, err := p.ConstantInt("CANBUS_BRIDGE"); err == nil && v != 0 {
		r.isMCUBridge = true
		r.printer.RegisterEventHandler("klippy:firmware_restart", func(...any) { r.firmwareRestart(true) })
	}
}

func (r *restartHelper) disconnect() { r.conn.serial.Disconnect() }

func (r *restartHelper) restartArduino() {
	log.Printf("Attempting MCU '%s' reset", r.name)
	r.disconnect()
	if err := serialhdl.ArduinoReset(r.conn.serialport, r.printer.Reactor()); err != nil {
		log.Printf("MCU '%s' reset: %v", r.name, err)
	}
}

func (r *restartHelper) restartViaCommand() {
	if (r.resetCmd == nil && r.configResetCmd == nil) || !r.conn.clocksync.IsActive() {
		log.Printf("Unable to issue reset command on MCU '%s'", r.name)
		return
	}
	rc := r.printer.Reactor()
	if r.resetCmd == nil {
		// Attempt reset via config_reset command
		log.Printf("Attempting MCU '%s' config_reset command", r.name)
		r.conn.forceLocalShutdown()
		rc.Pause(rc.Monotonic() + 0.015)
		r.configResetCmd.Send(nil, 0, 0)
	} else {
		// Attempt reset via reset command
		log.Printf("Attempting MCU '%s' reset command", r.name)
		r.resetCmd.Send(nil, 0, 0)
	}
	rc.Pause(rc.Monotonic() + 0.015)
	r.disconnect()
}

func (r *restartHelper) firmwareRestart(force bool) {
	if r.isMCUBridge && !force {
		return
	}
	switch r.restartMethod {
	case "command":
		r.restartViaCommand()
	case "cheetah":
		// The Cheetah reset is the Arduino reset with RTS deasserted at the open.
		r.restartArduino()
	default:
		r.restartArduino()
	}
}

// Handle statistics reporting

type statsHelper struct {
	printer        *host.Printer
	mcu            *MCU
	conn           *connectHelper
	mcuFreq        float64
	statusInfo     map[string]any
	statsSumsqBase float64
	tickAvg        float64
	tickStddev     float64
	tickAwake      float64
}

func newStatsHelper(m *MCU, conn *connectHelper) *statsHelper {
	s := &statsHelper{printer: m.printer, mcu: m, conn: conn, statusInfo: map[string]any{}}
	m.printer.RegisterEventHandler("klippy:ready", func(...any) { s.ready() })
	m.printer.RegisterEventHandler("klippy:mcu_identify", func(...any) { s.mcuIdentify() })
	return s
}

func (s *statsHelper) handleMCUStats(params serialhdl.Params) {
	count := float64(params.Int("count"))
	tickSum := float64(params.Int("sum"))
	c := 1.0 / (count * s.mcuFreq)
	s.tickAvg = tickSum * c
	tickSumsq := float64(params.Int("sumsq")) * s.statsSumsqBase
	diff := count*tickSumsq - tickSum*tickSum
	s.tickStddev = c * math.Sqrt(max(0., diff))
	s.tickAwake = tickSum / s.mcuFreq
}

func (s *statsHelper) mcuIdentify() {
	s.mcuFreq = s.mcu.ConstantFloat("CLOCK_FREQ")
	s.statsSumsqBase = s.mcu.ConstantFloat("STATS_SUMSQ_BASE")
	p := s.conn.serial.MsgParser()
	version, buildVersions := p.Version()
	s.statusInfo["mcu_version"] = version
	s.statusInfo["mcu_build_versions"] = buildVersions
	s.statusInfo["mcu_constants"] = p.Constants()
	s.statusInfo["mcu_kconfig"] = p.Kconfig()
	s.conn.serial.RegisterResponse(s.handleMCUStats, "stats", serialhdl.NoOID)
}

// Warner is the part of the config object that the frequency check needs.
type warner interface{ RuntimeWarning(msg string) }

func (s *statsHelper) ready() {
	if s.mcu.IsFileoutput() {
		return
	}
	// Check that reported mcu frequency is in range
	systime := s.printer.Reactor().Monotonic()
	calcFreq := float64(s.conn.clocksync.GetClock(systime+1) - s.conn.clocksync.GetClock(systime))
	freqDiff := math.Abs(s.mcuFreq - calcFreq)
	mcuFreqMHz := int(s.mcuFreq/1000000. + 0.5)
	calcFreqMHz := int(calcFreq/1000000. + 0.5)
	if freqDiff > s.mcuFreq*0.01 && mcuFreqMHz != calcFreqMHz {
		pconfig := s.printer.LookupObject("configfile").(warner)
		pconfig.RuntimeWarning(fmt.Sprintf("MCU '%s' configured for %dMhz but running at %dMhz!",
			s.mcu.name, mcuFreqMHz, calcFreqMHz))
	}
}

func (s *statsHelper) status() map[string]any {
	out := map[string]any{}
	for k, v := range s.statusInfo {
		out[k] = v
	}
	return out
}

func (s *statsHelper) stats(eventtime float64) (bool, string) {
	load := fmt.Sprintf("mcu_awake=%.03f mcu_task_avg=%.06f mcu_task_stddev=%.06f", s.tickAwake, s.tickAvg, s.tickStddev)
	stats := strings.Join([]string{load, s.conn.serial.Stats(eventtime), s.conn.clocksync.Stats(eventtime)}, " ")
	last := map[string]any{}
	for _, part := range strings.Fields(stats) {
		k, v, _ := strings.Cut(part, "=")
		if strings.Contains(v, ".") {
			var f float64
			fmt.Sscan(v, &f)
			last[k] = f
		} else {
			var n int64
			fmt.Sscan(v, &n)
			last[k] = n
		}
	}
	s.statusInfo["last_stats"] = last
	return false, s.mcu.name + ": " + stats
}

// Handle process of configuring an mcu

type configHelper struct {
	printer           *host.Printer
	conn              *connectHelper
	mcu               *MCU
	name              string
	finalized         bool
	oidCount          int
	configCallbacks   []func()
	postInitCallbacks []func()
	configCmds        []string
	restartCmds       []string
	initCmds          []string
	configCRC         uint32
	mcuFreq           float64
	reservedMoveSlots int
}

func newConfigHelper(m *MCU, conn *connectHelper) *configHelper {
	c := &configHelper{printer: m.printer, conn: conn, mcu: m, name: m.name}
	m.printer.LookupObject("pins").(*pins.Pins).RegisterChip(c.name, m)
	m.printer.RegisterEventHandler("klippy:mcu_identify", func(...any) { c.mcuIdentify() })
	m.printer.RegisterEventHandler("klippy:connect", func(...any) { c.connect() })
	return c
}

func (c *configHelper) finalizeConfig() {
	// Build config commands
	for _, cb := range c.configCallbacks {
		cb()
	}
	c.finalized = true
	c.configCmds = append([]string{fmt.Sprintf("allocate_oids count=%d", c.oidCount)}, c.configCmds...)
	// Resolve pin names
	resolver := c.printer.LookupObject("pins").(*pins.Pins).PinResolver(c.name)
	for _, cmdlist := range [][]string{c.configCmds, c.restartCmds, c.initCmds} {
		for i, cmd := range cmdlist {
			cmdlist[i] = resolver.UpdateCommand(cmd)
		}
	}
	// Calculate config CRC
	c.configCRC = crc32.ChecksumIEEE([]byte(strings.Join(c.configCmds, "\n")))
	c.configCmds = append(c.configCmds, fmt.Sprintf("finalize_config crc=%d", c.configCRC))
}

// ConfigCommands returns the config commands, after the finalize (for the tests).
func (m *MCU) ConfigCommands() (config, restart, init []string) {
	return m.cfg.configCmds, m.cfg.restartCmds, m.cfg.initCmds
}

func (c *configHelper) sendCfgInitCommands(cmds []string) {
	for _, cmd := range cmds {
		if err := c.conn.serial.Send(cmd, 0, 0); err != nil {
			var ee *msgproto.EnumerationError
			if errors.As(err, &ee) && ee.Enum == "pin" {
				// Raise pin name errors as a config error (not a protocol error)
				configfile.Raise("Pin '%s' is not a valid pin name on mcu '%s'", ee.Value, c.name)
			}
			panic(err)
		}
	}
}

func (c *configHelper) sendGetConfig() serialhdl.Params {
	cmd := c.mcu.LookupQueryCommand("get_config", "config is_config=%c crc=%u is_shutdown=%c move_count=%hu",
		serialhdl.NoOID, nil, false)
	if c.mcu.IsFileoutput() {
		return serialhdl.Params{"is_config": int64(0), "move_count": int64(500), "crc": int64(0)}
	}
	params := c.sendQuery(cmd)
	if c.conn.isShutdown {
		raise("MCU '%s' error during config: %s", c.name, c.conn.shutdownMsg)
	}
	if params.Int("is_shutdown") != 0 {
		raise("Can not update MCU '%s' config as it is shutdown", c.name)
	}
	return params
}

// sendQuery sends a query, with an error of the link as an MCU error.
func (c *configHelper) sendQuery(cmd *CommandQueryWrapper) (params serialhdl.Params) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(*gcode.CommandError); ok {
				raise("%s", e.Msg)
			}
			panic(r)
		}
	}()
	return cmd.Send(nil, 0, 0, true)
}

// MoveQueueSetup is the part of the motion_queuing object that the board needs.
type MoveQueueSetup interface {
	SetupMCUMovequeue(m *MCU, sq *serialqueue.SerialQueue, moveCount int)
}

func (c *configHelper) connect() {
	// Finalize the config and check if a restart is needed
	configParams := c.sendGetConfig()
	var cmds []string
	if configParams.Int("is_config") == 0 {
		// Not configured - sending full config will be required
		c.conn.restart.checkRestartOnSendConfig()
		c.finalizeConfig()
		cmds = append(append([]string{}, c.configCmds...), c.initCmds...)
		log.Printf("Sending MCU '%s' printer configuration...", c.name)
	} else {
		// Already configured - may need to only send init commands
		if s, _ := c.printer.StartArgs().Get("start_reason"); s == "firmware_restart" {
			raise("Failed automated reset of MCU '%s'", c.name)
		}
		c.finalizeConfig()
		if int64(c.configCRC) != configParams.Int("crc") {
			c.conn.restart.checkRestartOnCRCMismatch()
			raise("MCU '%s' CRC does not match config", c.name)
		}
		cmds = append(append([]string{}, c.restartCmds...), c.initCmds...)
	}
	// Send config and init messages
	c.sendCfgInitCommands(cmds)
	configParams = c.sendGetConfig()
	if configParams.Int("is_config") == 0 && !c.mcu.IsFileoutput() {
		raise("Unable to configure MCU '%s'", c.name)
	}
	// Run post_init callbacks
	for _, cb := range c.postInitCallbacks {
		cb()
	}
	// Setup steppersync with the move_count returned by get_config
	moveCount := int(configParams.Int("move_count"))
	if moveCount < c.reservedMoveSlots {
		raise("Too few moves available on MCU '%s'", c.name)
	}
	mq := c.printer.LookupObject("motion_queuing").(MoveQueueSetup)
	mq.SetupMCUMovequeue(c.mcu, c.conn.serial.SerialQueue(), moveCount-c.reservedMoveSlots)
	// Log config information
	log.Printf("Configured MCU '%s' (%d moves)", c.name, moveCount)
}

func (c *configHelper) mcuIdentify() {
	c.mcuFreq = c.mcu.ConstantFloat("CLOCK_FREQ")
	resolver := c.printer.LookupObject("pins").(*pins.Pins).PinResolver(c.name)
	p := c.conn.serial.MsgParser()
	consts := p.Constants()
	names := make([]string, 0, len(consts))
	for k := range consts {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, cname := range names {
		if strings.HasPrefix(cname, "RESERVE_PINS_") {
			value, _ := p.Constant(cname)
			for _, pin := range strings.Split(value, ",") {
				resolver.ReservePin(pin, cname[13:])
			}
		}
	}
	if MaxNominalDuration*c.mcuFreq > MaxScheduleTicks {
		maxPossible := MaxScheduleTicks * 1 / c.mcuFreq
		raise("Too high clock speed for MCU '%s' to be able to resolve a maximum nominal duration"+
			" of %ds. Max possible duration: %ds", c.name, int(MaxNominalDuration), int(maxPossible))
	}
}

func (c *configHelper) verifyNotFinalized() {
	if c.finalized {
		raise("Internal error! MCU already configured")
	}
}

// Config creation helpers

func (c *configHelper) setupPin(pinType string, params *pins.Params) any {
	c.verifyNotFinalized()
	switch pinType {
	case "endstop":
		return newEndstop(c.mcu, params)
	case "digital_out":
		return newDigitalOut(c.mcu, params)
	case "pwm":
		return newPWM(c.mcu, params)
	case "adc":
		return newADC(c.mcu, params)
	}
	pinError("pin type %s not supported on mcu", pinType)
	return nil
}

func (c *configHelper) createOID() int {
	c.verifyNotFinalized()
	c.oidCount++
	return c.oidCount - 1
}

func (c *configHelper) registerConfigCallback(cb func()) {
	c.verifyNotFinalized()
	c.configCallbacks = append(c.configCallbacks, cb)
}

func (c *configHelper) addConfigCmd(cmd string, isInit, onRestart bool) {
	c.verifyNotFinalized()
	switch {
	case isInit:
		c.initCmds = append(c.initCmds, cmd)
	case onRestart:
		c.restartCmds = append(c.restartCmds, cmd)
	default:
		c.configCmds = append(c.configCmds, cmd)
	}
}

func (c *configHelper) registerPostInitCallback(cb func()) {
	c.verifyNotFinalized()
	c.postInitCallbacks = append(c.postInitCallbacks, cb)
}

func (c *configHelper) getQuerySlot(oid int) int64 {
	slot := c.mcu.SecondsToClock(float64(oid) * .01)
	t := int64(c.mcu.EstimatedPrintTime(c.printer.Reactor().Monotonic()) + 1.5)
	return c.mcu.PrintTimeToClock(float64(t)) + slot
}
