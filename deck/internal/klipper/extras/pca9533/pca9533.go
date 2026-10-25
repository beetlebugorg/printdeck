// Package pca9533 is the PCA9533 LED driver chip on I2C.
//
// Ported to Go from klippy/extras/pca9533.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2021  Marc-Andre Denis <marcadenis@msn.com>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package pca9533

import (
	"fmt"
	"math"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/bus"
	"printdeck/internal/klipper/extras/led"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/serialqueue"
)

var backgroundPriorityClock = int64(serialqueue.BackgroundPriorityClock)

const (
	PCA9533PWM0 = 0b010
	PCA9533PWM1 = 0b100
	PCA9533PLS0 = 0b101
)

// PCA9533 is a [pca9533] section.
type PCA9533 struct {
	printer   *host.Printer
	i2c       *bus.MCUI2C
	ledHelper *led.LEDHelper
}

func loadPrefix(config *configfile.Wrapper) any {
	p := &PCA9533{printer: host.Of(config)}
	p.i2c = bus.MCUI2CFromConfig(config, 98, 100000, false)
	p.ledHelper = led.NewLEDHelper(config, p.updateLEDs, 1)
	p.printer.RegisterEventHandler("klippy:connect", func(...any) { p.handleConnect() })
	return p
}

func init() { host.RegisterModule("pca9533", nil, loadPrefix) }

func (p *PCA9533) handleConnect() {
	p.i2c.I2CWrite([]byte{PCA9533PWM0, 85}, 0, 0, true)
	p.i2c.I2CWrite([]byte{PCA9533PWM1, 170}, 0, 0, true)
	p.updateLEDs(p.ledHelper.LEDState(), math.NaN())
}

func (p *PCA9533) updateLEDs(ledState []led.Color, printTime float64) {
	rmap := []int{0, 2, 3, 1, 1}
	color := ledState[0]
	if len(color) != 4 {
		panic(fmt.Errorf("ValueError: too many values to unpack (expected 4)"))
	}
	var v [4]int
	for i, c := range color {
		v[i] = rmap[int(c*4.)]
	}
	red, green, blue, white := v[0], v[1], v[2], v[3]
	ls0 := (white << 6) | (blue << 4) | (green << 2) | red
	var minclock int64
	if !math.IsNaN(printTime) {
		minclock = p.i2c.MCU().PrintTimeToClock(printTime)
	}
	p.i2c.I2CWrite([]byte{PCA9533PLS0, byte(ls0)}, minclock, backgroundPriorityClock, true)
}

func (p *PCA9533) Status(eventtime float64) map[string]any { return p.ledHelper.Status(eventtime) }
