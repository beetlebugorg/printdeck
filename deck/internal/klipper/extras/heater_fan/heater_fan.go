// Package heater_fan runs a fan while a heater is on or hot ([heater_fan NAME]).
//
// Ported to Go from klippy/extras/heater_fan.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2016-2020  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package heater_fan

import (
	"math"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/fan"
	"printdeck/internal/klipper/extras/heaters"
	"printdeck/internal/klipper/host"
)

const pinMinTime = 0.100

// PrinterHeaterFan is a fan of heaters.
type PrinterHeaterFan struct {
	printer     *host.Printer
	heaterNames []string
	heaterTemp  float64
	heaters     []*heaters.Heater
	fan         *fan.Fan
	fanSpeed    float64
	lastSpeed   float64
}

func loadPrefix(config *configfile.Wrapper) any {
	printer := host.Of(config)
	hf := &PrinterHeaterFan{printer: printer}
	heaters.Load(config)
	printer.RegisterEventHandler("klippy:ready", func(...any) { hf.handleReady() })
	hf.heaterNames = config.GetList("heater", ",", 0, configfile.Default([]string{"extruder"}))
	hf.heaterTemp = config.GetFloat("heater_temp", configfile.Default(50.0))
	hf.fan = fan.NewFan(config, 1.)
	hf.fanSpeed = config.GetFloat("fan_speed", configfile.Default(1.), configfile.Min(0.), configfile.Max(1.))
	return hf
}

func init() { host.RegisterModule("heater_fan", nil, loadPrefix) }

func (hf *PrinterHeaterFan) handleReady() {
	pheaters := heaters.Of(hf.printer)
	hf.heaters = nil
	for _, n := range hf.heaterNames {
		hf.heaters = append(hf.heaters, pheaters.LookupHeater(n))
	}
	r := hf.printer.Reactor()
	r.RegisterTimer(hf.callback, r.Monotonic()+pinMinTime)
}

func (hf *PrinterHeaterFan) Status(eventtime float64) map[string]any { return hf.fan.Status(eventtime) }

func (hf *PrinterHeaterFan) callback(eventtime float64) float64 {
	speed := 0.
	for _, h := range hf.heaters {
		currentTemp, targetTemp := h.GetTemp(eventtime)
		if targetTemp != 0 || currentTemp > hf.heaterTemp {
			speed = hf.fanSpeed
		}
	}
	if speed != hf.lastSpeed {
		hf.lastSpeed = speed
		hf.fan.SetSpeed(speed, math.NaN())
	}
	return eventtime + 1.
}
