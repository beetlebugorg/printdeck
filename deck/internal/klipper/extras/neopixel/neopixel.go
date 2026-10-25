// Package neopixel is the [neopixel] section: a chain of WS2812 style LEDs on one data pin.
//
// Ported to Go from klippy/extras/neopixel.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2019-2022  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package neopixel

import (
	"bytes"
	"fmt"
	"log"
	"math"
	"slices"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/led"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/pins"
	"printdeck/internal/klipper/serialqueue"
)

var backgroundPriorityClock = int64(serialqueue.BackgroundPriorityClock)

const (
	BitMaxTime   = .000004
	ResetMinTime = .000050

	MaxMCUSize = 500 // Sanity check on LED chain length
)

type colorIndex struct{ lidx, cidx int }

// PrinterNeoPixel is a [neopixel] section.
type PrinterNeoPixel struct {
	printer           *host.Printer
	mcu               *mcu.MCU
	oid               int
	pin               string
	neopixelUpdateCmd *mcu.CommandWrapper
	neopixelSendCmd   *mcu.CommandQueryWrapper
	colorMap          []colorIndex
	ledHelper         *led.LEDHelper
	colorData         []byte
	oldColorData      []byte
}

func sortedChars(s string) string {
	b := []byte(s)
	slices.Sort(b)
	return string(b)
}

func loadPrefix(config *configfile.Wrapper) any {
	n := &PrinterNeoPixel{printer: host.Of(config)}
	// Configure neopixel
	ppins := n.printer.LookupObject("pins").(*pins.Pins)
	pinParams := ppins.LookupPin(config.Get("pin"), false, false, "")
	n.mcu = pinParams.Chip.(*mcu.MCU)
	n.oid = n.mcu.CreateOID()
	n.pin = pinParams.Pin
	n.mcu.RegisterConfigCallback(n.buildConfig)
	// Build color map
	chainCount := config.GetInt("chain_count", configfile.Default(1), configfile.Min(1))
	colorOrder := config.GetList("color_order", ",", 0, configfile.Default([]string{"GRB"}))
	if len(colorOrder) == 1 {
		co := colorOrder[0]
		colorOrder = nil
		for range chainCount {
			colorOrder = append(colorOrder, co)
		}
	}
	if len(colorOrder) != chainCount {
		configfile.Raise("color_order does not match chain_count")
	}
	for lidx, co := range colorOrder {
		if sorted := sortedChars(co); sorted != sortedChars("RGB") && sorted != sortedChars("RGBW") {
			configfile.Raise("Invalid color_order '%s'", co)
		}
		for _, c := range co {
			n.colorMap = append(n.colorMap, colorIndex{lidx, strings.IndexRune("RGBW", c)})
		}
	}
	if len(n.colorMap) > MaxMCUSize {
		configfile.Raise("neopixel chain too long")
	}
	// Initialize color data
	n.ledHelper = led.NewLEDHelper(config, n.updateLEDs, chainCount)
	n.colorData = make([]byte, len(n.colorMap))
	n.updateColorData(n.ledHelper.LEDState())
	n.oldColorData = make([]byte, len(n.colorData))
	for i, d := range n.colorData {
		n.oldColorData[i] = d ^ 1
	}
	// Register callbacks
	n.printer.RegisterEventHandler("klippy:connect", func(...any) { n.sendData(math.NaN()) })
	return n
}

func init() { host.RegisterModule("neopixel", nil, loadPrefix) }

func (n *PrinterNeoPixel) buildConfig() {
	bmt := n.mcu.SecondsToClock(BitMaxTime)
	rmt := n.mcu.SecondsToClock(ResetMinTime)
	n.mcu.AddConfigCmd(fmt.Sprintf("config_neopixel oid=%d pin=%s data_size=%d"+
		" bit_max_ticks=%d reset_min_ticks=%d", n.oid, n.pin, len(n.colorData), bmt, rmt), false, false)
	cmdQueue := n.mcu.AllocCommandQueue()
	n.neopixelUpdateCmd = n.mcu.LookupCommand("neopixel_update oid=%c pos=%hu data=%*s", cmdQueue)
	n.neopixelSendCmd = n.mcu.LookupQueryCommand("neopixel_send oid=%c",
		"neopixel_result oid=%c success=%c", int64(n.oid), cmdQueue, false)
}

func (n *PrinterNeoPixel) updateColorData(ledState []led.Color) {
	for cdidx, ci := range n.colorMap {
		n.colorData[cdidx] = byte(int(ledState[ci.lidx][ci.cidx]*255. + .5))
	}
}

func (n *PrinterNeoPixel) sendData(printTime float64) {
	oldData, newData := n.oldColorData, n.colorData
	if bytes.Equal(newData, oldData) {
		return
	}
	// Find the position of all changed bytes in this framebuffer
	var diffs [][2]int
	for i := range newData {
		if newData[i] != oldData[i] {
			diffs = append(diffs, [2]int{i, 1})
		}
	}
	// Batch together changes that are close to each other
	for i := len(diffs) - 2; i >= 0; i-- {
		pos := diffs[i][0]
		nextpos, nextcount := diffs[i+1][0], diffs[i+1][1]
		if pos+5 >= nextpos && nextcount < 16 {
			diffs[i][1] = nextcount + (nextpos - pos)
			diffs = append(diffs[:i+1], diffs[i+2:]...)
		}
	}
	// Transmit changes
	for _, d := range diffs {
		pos, count := d[0], d[1]
		n.neopixelUpdateCmd.Send([]any{n.oid, pos, append([]byte(nil), newData[pos:pos+count]...)}, 0,
			backgroundPriorityClock)
	}
	copy(oldData, newData)
	// Instruct mcu to update the LEDs
	var minclock int64
	if !math.IsNaN(printTime) {
		minclock = n.mcu.PrintTimeToClock(printTime)
	}
	if _, ok := n.printer.StartArgs().Get("debugoutput"); ok {
		return
	}
	for range 8 {
		params := n.neopixelSendCmd.Send([]any{n.oid}, minclock, backgroundPriorityClock, true)
		if params.Int("success") != 0 {
			return
		}
	}
	log.Printf("Neopixel update did not succeed")
}

func (n *PrinterNeoPixel) updateLEDs(ledState []led.Color, printTime float64) {
	n.updateColorData(ledState)
	n.sendData(printTime)
}

func (n *PrinterNeoPixel) Status(eventtime float64) map[string]any {
	return n.ledHelper.Status(eventtime)
}
