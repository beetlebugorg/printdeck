// Package buttons reads the buttons of the boards: digital buttons, buttons on an analog pin, and
// rotary encoders, with an optional debounce.
//
// Ported to Go from klippy/extras/buttons.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018-2023  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package buttons

import (
	"fmt"
	"math"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/pins"
	"printdeck/internal/klipper/reactor"
	"printdeck/internal/klipper/serialhdl"
)

// Callback gets a button state change: the time and the new state.
type Callback func(eventtime float64, state int)

// Button state tracking

const (
	queryTime       = .002
	retransmitCount = 50
)

type buttonCallback struct {
	mask, shift int
	cb          Callback
}

type pinPull struct {
	pin    string
	pullup int
}

// MCUButtons are the buttons of one board (up to 8).
type MCUButtons struct {
	reactor    *reactor.Reactor
	mcu        *mcu.MCU
	pinList    []pinPull
	callbacks  []buttonCallback
	invert     int
	lastButton int
	oid        int
	ackCmd     *mcu.CommandWrapper
	ackCount   int
}

func newMCUButtons(printer *host.Printer, m *mcu.MCU) *MCUButtons {
	b := &MCUButtons{reactor: printer.Reactor(), mcu: m}
	m.RegisterConfigCallback(b.buildConfig)
	return b
}

func (b *MCUButtons) setupButtons(params []*pins.Params, cb Callback) {
	mask := 0
	shift := len(b.pinList)
	for _, p := range params {
		if p.Invert != 0 {
			b.invert |= 1 << len(b.pinList)
		}
		mask |= 1 << len(b.pinList)
		b.pinList = append(b.pinList, pinPull{p.Pin, p.Pullup})
	}
	b.callbacks = append(b.callbacks, buttonCallback{mask, shift, cb})
}

func (b *MCUButtons) buildConfig() {
	if len(b.pinList) == 0 {
		return
	}
	b.oid = b.mcu.CreateOID()
	b.mcu.AddConfigCmd(fmt.Sprintf("config_buttons oid=%d button_count=%d", b.oid, len(b.pinList)), false, false)
	for i, p := range b.pinList {
		b.mcu.AddConfigCmd(fmt.Sprintf("buttons_add oid=%d pos=%d pin=%s pull_up=%d", b.oid, i, p.pin, p.pullup), true, false)
	}
	cmdQueue := b.mcu.AllocCommandQueue()
	b.ackCmd = b.mcu.LookupCommand("buttons_ack oid=%c count=%c", cmdQueue)
	clock := b.mcu.GetQuerySlot(b.oid)
	restTicks := b.mcu.SecondsToClock(queryTime)
	b.mcu.AddConfigCmd(fmt.Sprintf("buttons_query oid=%d clock=%d rest_ticks=%d retransmit_count=%d invert=%d",
		b.oid, clock, restTicks, retransmitCount, b.invert), true, false)
	b.mcu.RegisterSerialResponse(b.handleButtonsState, "buttons_state oid=%c ack_count=%c state=%*s", int64(b.oid))
}

// handleButtonsState runs on the reading goroutine.
func (b *MCUButtons) handleButtonsState(params serialhdl.Params) {
	// Expand the message ack_count from 8-bit
	ackCount := b.ackCount
	ackDiff := (int(params.Int("ack_count")) - ackCount) & 0xff
	ackDiff -= (ackDiff & 0x80) << 1
	msgAckCount := ackCount + ackDiff
	// Determine new buttons
	buttons := params.Bytes("state")
	newCount := msgAckCount + len(buttons) - b.ackCount
	if newCount <= 0 {
		return
	}
	newButtons := buttons[len(buttons)-newCount:]
	// Send ack to MCU
	b.ackCmd.Send([]any{b.oid, newCount}, 0, 0)
	b.ackCount += newCount
	// Invoke callbacks with this event in main thread
	btime := params.Float("#receive_time")
	for _, button := range newButtons {
		bv := int(button) ^ b.invert
		changed := bv ^ b.lastButton
		b.lastButton = bv
		for _, c := range b.callbacks {
			if changed&c.mask != 0 {
				state := (bv & c.mask) >> c.shift
				cb := c.cb
				b.reactor.RegisterAsyncCallback(func(float64) any { cb(btime, state); return nil }, reactor.Now)
			}
		}
	}
}

// ADC button tracking

const (
	adcReportTime   = 0.010
	adcDebounceTime = 0.025
	adcSampleTime   = 0.001
	adcSampleCount  = 6
	adcBatchCount   = 5
)

type adcButton struct {
	minValue, maxValue float64
	cb                 Callback
}

// MCUADCButtons are the buttons of one analog pin, with a resistor each.
type MCUADCButtons struct {
	reactor          *reactor.Reactor
	buttons          []adcButton
	lastButton       int // -1 for none
	lastPressed      int // -1 for none
	lastDebounceTime float64
	pullup           float64
	pin              string
	minValue         float64
	maxValue         float64
	mcuADC           *mcu.ADC
}

type adcRegistrar interface {
	RegisterADC(name string, adc *mcu.ADC)
}

func newMCUADCButtons(printer *host.Printer, pin string, pullup float64) *MCUADCButtons {
	b := &MCUADCButtons{reactor: printer.Reactor(), lastButton: -1, lastPressed: -1, pullup: pullup, pin: pin,
		minValue: 999999999999.9}
	ppins := printer.LookupObject("pins").(*pins.Pins)
	b.mcuADC = ppins.SetupPin("adc", pin).(*mcu.ADC)
	b.mcuADC.SetupADCSample(adcReportTime, adcSampleTime, adcSampleCount, adcBatchCount, 0., 1., 0)
	b.mcuADC.SetupADCCallback(b.adcCallback)
	printer.LookupObject("query_adc").(adcRegistrar).RegisterADC("adc_button:"+strings.TrimSpace(pin), b.mcuADC)
	return b
}

func (b *MCUADCButtons) setupButton(minValue, maxValue float64, cb Callback) {
	b.minValue = math.Min(b.minValue, minValue)
	b.maxValue = math.Max(b.maxValue, maxValue)
	b.buttons = append(b.buttons, adcButton{minValue, maxValue, cb})
}

// adcCallback runs on the reading goroutine.
func (b *MCUADCButtons) adcCallback(samples []mcu.Sample) {
	for _, s := range samples {
		adc := math.Max(.00001, math.Min(.99999, s.Value))
		value := b.pullup * adc / (1.0 - adc)
		// Determine button pressed
		btn := -1
		if b.minValue <= value && value <= b.maxValue {
			for i, bt := range b.buttons {
				if bt.minValue < value && value < bt.maxValue {
					btn = i
					break
				}
			}
		}
		// If the button changed, due to noise or pressing:
		if btn != b.lastButton {
			// reset the debouncing timer
			b.lastDebounceTime = s.Time
		}
		// button debounce check & new button pressed
		if s.Time-b.lastDebounceTime >= adcDebounceTime && b.lastButton == btn && b.lastPressed != btn {
			// release last_pressed
			if b.lastPressed != -1 {
				b.callButton(b.lastPressed, 0)
				b.lastPressed = -1
			}
			if btn != -1 {
				b.callButton(btn, 1)
				b.lastPressed = btn
			}
		}
		b.lastButton = btn
	}
}

func (b *MCUADCButtons) callButton(button, state int) {
	cb := b.buttons[button].cb
	b.reactor.RegisterAsyncCallback(func(e float64) any { cb(e, state); return nil }, reactor.Now)
}

// Rotary Encoders
//
// Rotary encoder handler https://github.com/brianlow/Rotary
// Copyright 2011 Ben Buxton (bb@cactii.net).
// Licenced under the GNU GPL Version 3.

const (
	rStart   = 0x0
	rDirCW   = 0x10
	rDirCCW  = 0x20
	rDirMask = 0x30
)

// Use the full-step state table (emits a code at 00 only)
const (
	fCWFinal  = 0x1
	fCWBegin  = 0x2
	fCWNext   = 0x3
	fCCWBegin = 0x4
	fCCWFinal = 0x5
	fCCWNext  = 0x6
)

var fullStepStates = [][4]int{
	{rStart, fCWBegin, fCCWBegin, rStart},
	{fCWNext, rStart, fCWFinal, rStart | rDirCW},
	{fCWNext, fCWBegin, rStart, rStart},
	{fCWNext, fCWBegin, fCWFinal, rStart},
	{fCCWNext, rStart, fCCWBegin, rStart},
	{fCCWNext, fCCWFinal, rStart, rStart | rDirCCW},
	{fCCWNext, fCCWFinal, fCCWBegin, rStart},
}

// Use the half-step state table (emits a code at 00 and 11)
const (
	hCCWBegin  = 0x1
	hCWBegin   = 0x2
	hStartM    = 0x3
	hCWBeginM  = 0x4
	hCCWBeginM = 0x5
)

var halfStepStates = [][4]int{
	{hStartM, hCWBegin, hCCWBegin, rStart},
	{hStartM | rDirCCW, rStart, hCCWBegin, rStart},
	{hStartM | rDirCW, hCWBegin, rStart, rStart},
	{hStartM, hCCWBeginM, hCWBeginM, rStart},
	{hStartM, hStartM, hCWBeginM, rStart | rDirCW},
	{hStartM, hCCWBeginM, hStartM, rStart | rDirCCW},
}

type rotaryEncoder struct {
	states       [][4]int
	cwCallback   func(eventtime float64)
	ccwCallback  func(eventtime float64)
	encoderState int
}

func (r *rotaryEncoder) encoderCallback(eventtime float64, state int) {
	es := r.states[r.encoderState&0xf][state&0x3]
	r.encoderState = es
	switch es & rDirMask {
	case rDirCW:
		r.cwCallback(eventtime)
	case rDirCCW:
		r.ccwCallback(eventtime)
	}
}

// DebounceButton runs an action only when a button keeps its state for a delay.
type DebounceButton struct {
	reactor         *reactor.Reactor
	buttonAction    Callback
	debounceDelay   float64
	logicalState    int // -1 for none
	physicalState   int
	latestEventtime float64
}

func newDebounceButton(config *configfile.Wrapper, action Callback) *DebounceButton {
	return &DebounceButton{reactor: host.Of(config).Reactor(), buttonAction: action, logicalState: -1,
		debounceDelay: config.GetFloat("debounce_delay", configfile.Default(0.), configfile.Min(0.))}
}

func (d *DebounceButton) buttonHandler(eventtime float64, state int) {
	d.physicalState = state
	d.latestEventtime = eventtime
	// if there would be no state transition, ignore the event:
	if d.logicalState == d.physicalState {
		return
	}
	triggerTime := eventtime + d.debounceDelay
	d.reactor.RegisterCallback(d.debounceEvent, triggerTime)
}

func (d *DebounceButton) debounceEvent(eventtime float64) any {
	// if there would be no state transition, ignore the event:
	if d.logicalState == d.physicalState {
		return nil
	}
	// if there were more recent events, they supersede this one:
	if eventtime-d.debounceDelay < d.latestEventtime {
		return nil
	}
	// enact state transition and trigger action
	d.logicalState = d.physicalState
	d.buttonAction(d.latestEventtime, d.logicalState)
	return nil
}

// Button registration code

// PrinterButtons registers the buttons of the modules.
type PrinterButtons struct {
	printer    *host.Printer
	mcuButtons map[string]*MCUButtons
	adcButtons map[string]*MCUADCButtons
}

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	printer.LoadObject(config, "query_adc")
	return &PrinterButtons{printer: printer, mcuButtons: map[string]*MCUButtons{}, adcButtons: map[string]*MCUADCButtons{}}
}

func init() { host.RegisterModule("buttons", load, nil) }

// Load returns the buttons object, and loads it the first time.
func Load(config *configfile.Wrapper) *PrinterButtons {
	return host.Of(config).LoadObject(config, "buttons").(*PrinterButtons)
}

// RegisterADCButton adds a button of a resistance range on an analog pin.
func (p *PrinterButtons) RegisterADCButton(pin string, minVal, maxVal, pullup float64, cb Callback) {
	b, ok := p.adcButtons[pin]
	if !ok {
		b = newMCUADCButtons(p.printer, pin, pullup)
		p.adcButtons[pin] = b
	}
	b.setupButton(minVal, maxVal, cb)
}

// RegisterDebounceButton adds a button with the debounce_delay of a config section.
func (p *PrinterButtons) RegisterDebounceButton(pin string, cb Callback, config *configfile.Wrapper) {
	d := newDebounceButton(config, cb)
	p.RegisterButtons([]string{pin}, d.buttonHandler)
}

// RegisterDebounceADCButton adds an analog button with the debounce_delay of a config section.
func (p *PrinterButtons) RegisterDebounceADCButton(pin string, minVal, maxVal, pullup float64, cb Callback,
	config *configfile.Wrapper) {
	d := newDebounceButton(config, cb)
	p.RegisterADCButton(pin, minVal, maxVal, pullup, d.buttonHandler)
}

// RegisterADCButtonPush adds an analog button that calls cb when it is pressed.
func (p *PrinterButtons) RegisterADCButtonPush(pin string, minVal, maxVal, pullup float64, cb func(eventtime float64)) {
	p.RegisterADCButton(pin, minVal, maxVal, pullup, func(e float64, state int) {
		if state != 0 {
			cb(e)
		}
	})
}

// RegisterButtons adds buttons of one board, with one callback for all of them.
func (p *PrinterButtons) RegisterButtons(pinDescs []string, cb Callback) {
	// Parse pins
	ppins := p.printer.LookupObject("pins").(*pins.Pins)
	var m *mcu.MCU
	mcuName := ""
	var params []*pins.Params
	for _, pin := range pinDescs {
		pp := ppins.LookupPin(pin, true, true, "")
		pm, ok := pp.Chip.(*mcu.MCU)
		if !ok || (m != nil && pm != m) {
			panic(&pins.Error{Msg: "button pins must be on same mcu"})
		}
		m, mcuName = pm, pp.ChipName
		params = append(params, pp)
	}
	// Register pins and callback with the appropriate MCU
	b, ok := p.mcuButtons[mcuName]
	if !ok || len(b.pinList)+len(params) > 8 {
		b = newMCUButtons(p.printer, m)
		p.mcuButtons[mcuName] = b
	}
	b.setupButtons(params, cb)
}

// RegisterRotaryEncoder adds a rotary encoder with 2 or 4 steps per detent.
func (p *PrinterButtons) RegisterRotaryEncoder(pin1, pin2 string, cw, ccw func(eventtime float64), stepsPerDetent int) {
	var states [][4]int
	switch stepsPerDetent {
	case 2:
		states = halfStepStates
	case 4:
		states = fullStepStates
	default:
		configfile.Raise("%d steps per detent not supported", stepsPerDetent)
	}
	re := &rotaryEncoder{states: states, cwCallback: cw, ccwCallback: ccw}
	p.RegisterButtons([]string{pin1, pin2}, re.encoderCallback)
}

// RegisterButtonPush adds a button that calls cb when it is pressed.
func (p *PrinterButtons) RegisterButtonPush(pin string, cb func(eventtime float64)) {
	p.RegisterButtons([]string{pin}, func(e float64, state int) {
		if state != 0 {
			cb(e)
		}
	})
}
