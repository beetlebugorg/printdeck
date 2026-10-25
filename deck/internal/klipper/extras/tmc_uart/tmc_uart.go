// Package tmc_uart is the UART link of the TMC drivers: a bit-banged single wire UART, shared
// by drivers with their address or an analog mux.
//
// Ported to Go from klippy/extras/tmc_uart.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018-2021  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package tmc_uart

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/tmc"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/pins"
	"printdeck/internal/klipper/reactor"
	"printdeck/internal/klipper/serialqueue"
)

// ######################################################################
// TMC uart analog mux support
// ######################################################################

// instanceID is the select pin values of a driver on an analog mux ("" for no mux).
type instanceID string

func muxID(values []bool) instanceID {
	var b strings.Builder
	for _, v := range values {
		if v {
			b.WriteByte('1')
		} else {
			b.WriteByte('0')
		}
	}
	return instanceID(b.String())
}

// MCUAnalogMux is the analog mux of the select pins of a shared uart.
type MCUAnalogMux struct {
	mcu          *mcu.MCU
	cmdQueue     *serialqueue.CommandQueue
	oids         []int
	pins         []string
	pinValues    []int
	updatePinCmd *mcu.CommandWrapper
}

func newMCUAnalogMux(m *mcu.MCU, cmdQueue *serialqueue.CommandQueue, selectPinsDesc []string) *MCUAnalogMux {
	a := &MCUAnalogMux{mcu: m, cmdQueue: cmdQueue}
	ppins := m.Printer().LookupObject("pins").(*pins.Pins)
	var params []*pins.Params
	for _, spd := range selectPinsDesc {
		params = append(params, ppins.LookupPin(spd, true, false, ""))
	}
	for range params {
		a.oids = append(a.oids, m.CreateOID())
	}
	for _, pp := range params {
		a.pins = append(a.pins, pp.Pin)
		a.pinValues = append(a.pinValues, -1)
	}
	for i, oid := range a.oids {
		m.AddConfigCmd(fmt.Sprintf("config_digital_out oid=%d pin=%s"+
			" value=0 default_value=0 max_duration=0", oid, a.pins[i]), false, false)
	}
	m.RegisterConfigCallback(a.buildConfig)
	return a
}

func (a *MCUAnalogMux) buildConfig() {
	a.updatePinCmd = a.mcu.LookupCommand("update_digital_out oid=%c value=%c", a.cmdQueue)
}

func (a *MCUAnalogMux) instanceID(selectPinsDesc []string) instanceID {
	ppins := a.mcu.Printer().LookupObject("pins").(*pins.Pins)
	var params []*pins.Params
	for _, spd := range selectPinsDesc {
		params = append(params, ppins.ParsePin(spd, true, false))
	}
	for _, pp := range params {
		if pp.Chip != pins.Chip(a.mcu) {
			configfile.Raise("TMC mux pins must be on the same mcu")
		}
	}
	var ps []string
	for _, pp := range params {
		ps = append(ps, pp.Pin)
	}
	if !slices.Equal(ps, a.pins) {
		configfile.Raise("All TMC mux instances must use identical pins")
	}
	var values []bool
	for _, pp := range params {
		values = append(values, pp.Invert == 0)
	}
	return muxID(values)
}

func (a *MCUAnalogMux) activate(id instanceID) {
	for i, oid := range a.oids {
		v := 0
		if id[i] == '1' {
			v = 1
		}
		if a.pinValues[i] != v {
			a.updatePinCmd.Send([]any{oid, v}, 0, 0)
		}
	}
	for i := range a.pinValues {
		a.pinValues[i] = 0
		if id[i] == '1' {
			a.pinValues[i] = 1
		}
	}
}

// ######################################################################
// TMC uart communication
// ######################################################################

// PrinterTMCUartMutexes share the mutexes, so only one active tmc_uart command on a single mcu at
// a time. This helps limit cpu usage on slower micro-controllers.
type PrinterTMCUartMutexes struct {
	mcuToMutex map[*mcu.MCU]*reactor.Mutex
}

func lookupTMCUartMutex(m *mcu.MCU) *reactor.Mutex {
	printer := m.Printer()
	pmutexes, ok := printer.TryLookupObject("tmc_uart").(*PrinterTMCUartMutexes)
	if !ok {
		pmutexes = &PrinterTMCUartMutexes{mcuToMutex: map[*mcu.MCU]*reactor.Mutex{}}
		printer.AddObject("tmc_uart", pmutexes)
	}
	mutex, ok := pmutexes.mcuToMutex[m]
	if !ok {
		mutex = printer.Reactor().NewMutex(false)
		pmutexes.mcuToMutex[m] = mutex
	}
	return mutex
}

const (
	TMCBaudRate    = 40000
	TMCBaudRateAVR = 9000
)

type instanceKey struct {
	id   instanceID
	addr int
}

// MCUTMCUartBitbang sends the messages of a TMC uart.
type MCUTMCUartBitbang struct {
	mcu            *mcu.MCU
	mutex          *reactor.Mutex
	pullup         int
	rxPin, txPin   string
	oid            int
	cmdQueue       *serialqueue.CommandQueue
	analogMux      *MCUAnalogMux
	instances      map[instanceKey]bool
	tmcuartSendCmd *mcu.CommandQueryWrapper
}

func newMCUTMCUartBitbang(rxPinParams, txPinParams *pins.Params, selectPinsDesc []string) *MCUTMCUartBitbang {
	m := rxPinParams.Chip.(*mcu.MCU)
	u := &MCUTMCUartBitbang{mcu: m, mutex: lookupTMCUartMutex(m), pullup: rxPinParams.Pullup,
		rxPin: rxPinParams.Pin, txPin: txPinParams.Pin, instances: map[instanceKey]bool{}}
	u.oid = m.CreateOID()
	u.cmdQueue = m.AllocCommandQueue()
	if selectPinsDesc != nil {
		u.analogMux = newMCUAnalogMux(m, u.cmdQueue, selectPinsDesc)
	}
	m.RegisterConfigCallback(u.buildConfig)
	return u
}

func (u *MCUTMCUartBitbang) buildConfig() {
	baud := TMCBaudRate
	mcuType, _ := u.mcu.Constants()["MCU"].(string)
	if strings.HasPrefix(mcuType, "atmega") || strings.HasPrefix(mcuType, "at90usb") {
		baud = TMCBaudRateAVR
	}
	bitTicks := u.mcu.SecondsToClock(1. / float64(baud))
	u.mcu.AddConfigCmd(fmt.Sprintf("config_tmcuart oid=%d rx_pin=%s pull_up=%d tx_pin=%s bit_time=%d",
		u.oid, u.rxPin, u.pullup, u.txPin, bitTicks), false, false)
	u.tmcuartSendCmd = u.mcu.LookupQueryCommand("tmcuart_send oid=%c write=%*s read=%c",
		"tmcuart_response oid=%c read=%*s", int64(u.oid), u.cmdQueue, true)
}

func (u *MCUTMCUartBitbang) registerInstance(rxPinParams, txPinParams *pins.Params,
	selectPinsDesc []string, addr int) instanceID {
	if rxPinParams.Pin != u.rxPin || txPinParams.Pin != u.txPin ||
		(selectPinsDesc == nil) != (u.analogMux == nil) {
		configfile.Raise("Shared TMC uarts must use the same pins")
	}
	var id instanceID
	if u.analogMux != nil {
		id = u.analogMux.instanceID(selectPinsDesc)
	}
	key := instanceKey{id, addr}
	if u.instances[key] {
		configfile.Raise("Shared TMC uarts need unique address or select_pins polarity")
	}
	u.instances[key] = true
	return id
}

// calcCRC8 generates a CRC8-ATM value for the bytes.
func calcCRC8(data []byte) byte {
	crc := 0
	for _, b0 := range data {
		b := int(b0)
		for i := 0; i < 8; i++ {
			if (crc>>7)^(b&0x01) != 0 {
				crc = (crc << 1) ^ 0x07
			} else {
				crc = crc << 1
			}
			crc &= 0xff
			b >>= 1
		}
	}
	return byte(crc)
}

// addSerialBits adds the serial start and stop bits to a message.
func addSerialBits(data []byte) []byte {
	// The message is up to 8 bytes, 80 bits: two words hold it.
	var out [2]uint64
	pos := 0
	for _, d := range data {
		b := (uint64(d) << 1) | 0x200
		for i := 0; i < 10; i++ {
			if b&(1<<i) != 0 {
				bit := pos + i
				out[bit/64] |= 1 << (bit % 64)
			}
		}
		pos += 10
	}
	res := make([]byte, (pos+7)/8)
	for i := range res {
		bit := i * 8
		v := out[bit/64] >> (bit % 64)
		if bit%64 > 56 {
			v |= out[bit/64+1] << (64 - bit%64)
		}
		res[i] = byte(v)
	}
	return res
}

func encodeRead(sync, addr, reg byte) []byte {
	// Generate a uart read register message
	msg := []byte{sync, addr, reg}
	msg = append(msg, calcCRC8(msg))
	return addSerialBits(msg)
}

func encodeWrite(sync, addr, reg byte, val int64) []byte {
	// Generate a uart write register message
	msg := []byte{sync, addr, reg, byte((val >> 24) & 0xff), byte((val >> 16) & 0xff),
		byte((val >> 8) & 0xff), byte(val & 0xff)}
	msg = append(msg, calcCRC8(msg))
	return addSerialBits(msg)
}

// decodeRead extracts the register value of a uart read response, or false.
func decodeRead(reg byte, data []byte) (int64, bool) {
	if len(data) != 10 {
		return 0, false
	}
	// The 80 bits of the response
	bitAt := func(n int) uint64 { return uint64(data[n/8]>>(n%8)) & 1 }
	field := func(shift int) int64 {
		var v int64
		for i := 0; i < 8; i++ {
			v |= int64(bitAt(shift+i)) << i
		}
		return v
	}
	// Extract register value
	val := field(31)<<24 | field(41)<<16 | field(51)<<8 | field(61)
	// Verify start/stop bits and crc
	encodedData := encodeWrite(0x05, 0xff, reg, val)
	if !slices.Equal(data, encodedData) {
		return 0, false
	}
	return val, true
}

func (u *MCUTMCUartBitbang) regRead(id instanceID, addr int, reg int) (int64, bool, float64) {
	if u.analogMux != nil {
		u.analogMux.activate(id)
	}
	msg := encodeRead(0xf5, byte(addr), byte(reg))
	params := u.tmcuartSendCmd.Send([]any{u.oid, msg, 10}, 0, 0, true)
	v, ok := decodeRead(byte(reg), params.Bytes("read"))
	return v, ok, params.Float("#receive_time")
}

func (u *MCUTMCUartBitbang) regWrite(id instanceID, addr int, reg int, val int64, printTime float64) {
	var minclock int64
	if !math.IsNaN(printTime) {
		minclock = u.mcu.PrintTimeToClock(printTime)
	}
	if u.analogMux != nil {
		u.analogMux.activate(id)
	}
	msg := encodeWrite(0xf5, byte(addr), byte(reg|0x80), val)
	u.tmcuartSendCmd.Send([]any{u.oid, msg, 0}, minclock, 0, true)
}

// lookupTMCUartBitbang returns the (possibly shared) tmc uart of a config section.
func lookupTMCUartBitbang(config *configfile.Wrapper, maxAddr int) (instanceID, int, *MCUTMCUartBitbang) {
	ppins := host.Of(config).LookupObject("pins").(*pins.Pins)
	rxPinParams := ppins.LookupPin(config.Get("uart_pin"), false, true, "tmc_uart_rx")
	txPinParams := rxPinParams
	if txPinDesc, ok := config.GetOptional("tx_pin", configfile.Default(nil)); ok {
		txPinParams = ppins.LookupPin(txPinDesc, false, false, "tmc_uart_tx")
	}
	if rxPinParams.Chip != txPinParams.Chip {
		panic(&pins.Error{Msg: "TMC uart rx and tx pins must be on the same mcu"})
	}
	selectPinsDesc := config.GetList("select_pins", ",", 0, configfile.Default(nil))
	addr := config.GetInt("uart_address", configfile.Default(0), configfile.Min(0),
		configfile.Max(float64(maxAddr)))
	mcuUart, _ := rxPinParams.Class.(*MCUTMCUartBitbang)
	if mcuUart == nil {
		mcuUart = newMCUTMCUartBitbang(rxPinParams, txPinParams, selectPinsDesc)
		rxPinParams.Class = mcuUart
	}
	id := mcuUart.registerInstance(rxPinParams, txPinParams, selectPinsDesc, addr)
	return id, addr, mcuUart
}

// MCUTMCUart is the link to a driver on a TMC uart.
type MCUTMCUart struct {
	printer      *host.Printer
	name         string
	nameToReg    map[string]int
	fields       *tmc.FieldHelper
	ifcnt        *int64
	instanceID   instanceID
	addr         int
	mcuUart      *MCUTMCUartBitbang
	mutex        *reactor.Mutex
	tmcFrequency float64
}

func NewMCUTMCUart(config *configfile.Wrapper, nameToReg map[string]int, fields *tmc.FieldHelper,
	maxAddr int, tmcFrequency float64) *MCUTMCUart {
	parts := strings.Fields(config.Name())
	u := &MCUTMCUart{printer: host.Of(config), name: parts[len(parts)-1], nameToReg: nameToReg,
		fields: fields, tmcFrequency: tmcFrequency}
	u.instanceID, u.addr, u.mcuUart = lookupTMCUartBitbang(config, maxAddr)
	u.mutex = u.mcuUart.mutex
	return u
}

func (u *MCUTMCUart) Fields() *tmc.FieldHelper  { return u.fields }
func (u *MCUTMCUart) NameToReg() map[string]int { return u.nameToReg }

func (u *MCUTMCUart) batchMode() bool {
	_, ok := u.printer.StartArgs().Get("debugoutput")
	return ok
}

func (u *MCUTMCUart) doGetRegister(regName string) map[string]any {
	reg := u.nameToReg[regName]
	if u.batchMode() {
		return map[string]any{"data": int64(0), "#receive_time": 0.}
	}
	for range 5 {
		v, ok, rt := u.mcuUart.regRead(u.instanceID, u.addr, reg)
		if ok {
			return map[string]any{"data": v, "#receive_time": rt}
		}
	}
	panic(gcode.Errorf("Unable to read tmc uart '%s' register %s", u.name, regName))
}

func (u *MCUTMCUart) RegisterRaw(regName string) map[string]any {
	u.mutex.Lock()
	defer u.mutex.Unlock()
	return u.doGetRegister(regName)
}

func (u *MCUTMCUart) Register(regName string) int64 {
	return u.RegisterRaw(regName)["data"].(int64)
}

func (u *MCUTMCUart) SetRegister(regName string, val int64, printTime float64) {
	reg := u.nameToReg[regName]
	if u.batchMode() {
		return
	}
	ok := func() bool {
		u.mutex.Lock()
		defer u.mutex.Unlock()
		for range 5 {
			if u.ifcnt == nil {
				v := u.doGetRegister("IFCNT")["data"].(int64)
				u.ifcnt = &v
			}
			ifcnt := *u.ifcnt
			u.mcuUart.regWrite(u.instanceID, u.addr, reg, val, printTime)
			v := u.doGetRegister("IFCNT")["data"].(int64)
			u.ifcnt = &v
			if v == (ifcnt+1)&0xff {
				return true
			}
		}
		return false
	}()
	if !ok {
		panic(gcode.Errorf("Unable to write tmc uart '%s' register %s", u.name, regName))
	}
}

func (u *MCUTMCUart) TMCFrequency() float64 { return u.tmcFrequency }
func (u *MCUTMCUart) MCU() *mcu.MCU         { return u.mcuUart.mcu }
