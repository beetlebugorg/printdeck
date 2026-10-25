// Package multi_pin is a virtual pin that sends its changes to several output pins.
//
// Ported to Go from klippy/extras/multi_pin.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2017-2021  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package multi_pin

import (
	"fmt"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/pins"
)

// PrinterMultiPin is a [multi_pin] section.
type PrinterMultiPin struct {
	printer *host.Printer
	pinType string
	pinList []string
	mcuPins []any
}

func loadPrefix(config *configfile.Wrapper) any {
	m := &PrinterMultiPin{printer: host.Of(config)}
	ppins := m.printer.LookupObject("pins").(*pins.Pins)
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(*pins.Error); !ok {
					panic(r)
				}
			}
		}()
		ppins.RegisterChip("multi_pin", m)
	}()
	m.pinType = ""
	m.pinList = config.GetList("pins", ",", 0)
	return m
}

func init() { host.RegisterModule("multi_pin", nil, loadPrefix) }

func pinError(format string, args ...any) { panic(&pins.Error{Msg: fmt.Sprintf(format, args...)}) }

// SetupPin sets up the output pins of the multi_pin of the pin name.
func (m *PrinterMultiPin) SetupPin(pinType string, pinParams *pins.Params) any {
	ppins := m.printer.LookupObject("pins").(*pins.Pins)
	pinName := pinParams.Pin
	pin, _ := m.printer.TryLookupObject("multi_pin " + pinName).(*PrinterMultiPin)
	if pin != m {
		if pin == nil {
			pinError("multi_pin %s not configured", pinName)
		}
		return pin.SetupPin(pinType, pinParams)
	}
	if m.pinType != "" {
		pinError("Can't setup multi_pin %s twice", pinName)
	}
	m.pinType = pinType
	invert := ""
	if pinParams.Invert != 0 {
		invert = "!"
	}
	for _, pinDesc := range m.pinList {
		m.mcuPins = append(m.mcuPins, ppins.SetupPin(pinType, invert+pinDesc))
	}
	return m
}

type mcuPin interface {
	MCU() *mcu.MCU
	SetupMaxDuration(maxDuration float64)
	SetupStartValue(startValue, shutdownValue float64)
}

func (m *PrinterMultiPin) MCU() *mcu.MCU { return m.mcuPins[0].(mcuPin).MCU() }

func (m *PrinterMultiPin) SetupMaxDuration(maxDuration float64) {
	for _, p := range m.mcuPins {
		p.(mcuPin).SetupMaxDuration(maxDuration)
	}
}

func (m *PrinterMultiPin) SetupStartValue(startValue, shutdownValue float64) {
	for _, p := range m.mcuPins {
		p.(mcuPin).SetupStartValue(startValue, shutdownValue)
	}
}

func (m *PrinterMultiPin) SetupCycleTime(cycleTime float64, hardwarePWM bool) {
	for _, p := range m.mcuPins {
		p.(mcu.PWMPin).SetupCycleTime(cycleTime, hardwarePWM)
	}
}

func (m *PrinterMultiPin) SetDigital(printTime float64, value bool) {
	for _, p := range m.mcuPins {
		p.(mcu.DigitalOutPin).SetDigital(printTime, value)
	}
}

func (m *PrinterMultiPin) NextAlignedPrintTime(printTime, allowEarly float64) float64 {
	return printTime
}

func (m *PrinterMultiPin) SetPWM(printTime, value float64) {
	for _, p := range m.mcuPins {
		p.(mcu.PWMPin).SetPWM(printTime, value)
	}
}
