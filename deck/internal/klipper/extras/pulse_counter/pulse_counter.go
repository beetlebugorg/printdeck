// Package pulse_counter counts the edges of an input pin on the board, for example the
// tachometer of a fan.
//
// Ported to Go from klippy/extras/pulse_counter.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2021  Adrian Keet <arkeet@gmail.com>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package pulse_counter

import (
	"fmt"

	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/pins"
	"printdeck/internal/klipper/serialhdl"
)

// MCUCounter is a GPIO input edge counter of a board.
type MCUCounter struct {
	mcu        *mcu.MCU
	oid        int
	pin        string
	pullup     int
	pollTime   float64
	pollTicks  int64
	sampleTime float64
	callback   func(time float64, count int64, countTime float64)
	lastCount  int64
}

func NewMCUCounter(printer *host.Printer, pin string, sampleTime, pollTime float64) *MCUCounter {
	ppins := printer.LookupObject("pins").(*pins.Pins)
	pinParams := ppins.LookupPin(pin, false, true, "")
	c := &MCUCounter{mcu: pinParams.Chip.(*mcu.MCU), pin: pinParams.Pin, pullup: pinParams.Pullup,
		pollTime: pollTime, sampleTime: sampleTime}
	c.oid = c.mcu.CreateOID()
	c.mcu.RegisterConfigCallback(c.buildConfig)
	return c
}

func (c *MCUCounter) buildConfig() {
	c.mcu.AddConfigCmd(fmt.Sprintf("config_counter oid=%d pin=%s pull_up=%d", c.oid, c.pin, c.pullup),
		false, false)
	clock := c.mcu.GetQuerySlot(c.oid)
	c.pollTicks = c.mcu.SecondsToClock(c.pollTime)
	sampleTicks := c.mcu.SecondsToClock(c.sampleTime)
	c.mcu.AddConfigCmd(fmt.Sprintf("query_counter oid=%d clock=%d poll_ticks=%d sample_ticks=%d",
		c.oid, clock, c.pollTicks, sampleTicks), true, false)
	c.mcu.RegisterSerialResponse(c.handleCounterState,
		"counter_state oid=%c next_clock=%u count=%u count_clock=%u", int64(c.oid))
}

// SetupCallback sets the function that gets each sample: the time, the count and the time of the
// last count.
func (c *MCUCounter) SetupCallback(cb func(time float64, count int64, countTime float64)) {
	c.callback = cb
}

func (c *MCUCounter) handleCounterState(params serialhdl.Params) {
	nextClock := c.mcu.Clock32ToClock64(uint32(params.Int("next_clock")))
	time := c.mcu.ClockToPrintTime(float64(nextClock - c.pollTicks))

	countClock := c.mcu.Clock32ToClock64(uint32(params.Int("count_clock")))
	countTime := c.mcu.ClockToPrintTime(float64(countClock))

	// handle 32-bit counter overflow
	lastCount := c.lastCount
	deltaCount := (params.Int("count") - lastCount) & 0xffffffff
	count := lastCount + deltaCount
	c.lastCount = count

	if c.callback != nil {
		c.callback(time, count, countTime)
	}
}

// FrequencyCounter is the frequency of the edges of a pin.
type FrequencyCounter struct {
	callback  func(time, freq float64)
	lastTime  *float64
	lastCount int64
	freq      float64
	counter   *MCUCounter
}

func NewFrequencyCounter(printer *host.Printer, pin string, sampleTime, pollTime float64) *FrequencyCounter {
	f := &FrequencyCounter{}
	f.counter = NewMCUCounter(printer, pin, sampleTime, pollTime)
	f.counter.SetupCallback(f.counterCallback)
	return f
}

func (f *FrequencyCounter) counterCallback(time float64, count int64, countTime float64) {
	if f.lastTime == nil { // First sample
		t := time
		f.lastTime = &t
	} else {
		deltaTime := countTime - *f.lastTime
		if deltaTime > 0 {
			t := countTime
			f.lastTime = &t
			deltaCount := count - f.lastCount
			f.freq = float64(deltaCount) / deltaTime
		} else { // No counts since last sample
			t := time
			f.lastTime = &t
			f.freq = 0.
		}
		if f.callback != nil {
			f.callback(time, f.freq)
		}
	}
	f.lastCount = count
}

// Frequency returns the last frequency.
func (f *FrequencyCounter) Frequency() float64 { return f.freq }
