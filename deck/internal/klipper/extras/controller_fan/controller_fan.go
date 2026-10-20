// Package controller_fan runs a fan while a stepper or a heater is on ([controller_fan NAME]),
// for example the fan of the board.
//
// Ported to Go from klippy/extras/controller_fan.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2019  Nils Friedchen <nils.friedchen@googlemail.com>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package controller_fan

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/fan"
	"printdeck/internal/klipper/extras/heaters"
	"printdeck/internal/klipper/extras/stepper_enable"
	"printdeck/internal/klipper/host"
)

const pinMinTime = 0.100

// ControllerFan is a fan of the steppers and heaters.
type ControllerFan struct {
	printer       *host.Printer
	stepperNames  []string
	hasSteppers   bool
	stepperEnable *stepper_enable.PrinterStepperEnable
	heaters       []*heaters.Heater
	fan           *fan.Fan
	fanSpeed      float64
	idleSpeed     float64
	idleTimeout   int
	heaterNames   []string
	lastOn        int
	lastSpeed     float64
}

func loadPrefix(config *configfile.Wrapper) any {
	printer := host.Of(config)
	cf := &ControllerFan{printer: printer}
	printer.RegisterEventHandler("klippy:ready", func(...any) { cf.handleReady() })
	printer.RegisterEventHandler("klippy:connect", func(...any) { cf.handleConnect() })
	if names := config.GetList("stepper", ",", 0, configfile.Default(nil)); names != nil {
		cf.stepperNames, cf.hasSteppers = names, true
	}
	cf.stepperEnable = printer.LoadObject(config, "stepper_enable").(*stepper_enable.PrinterStepperEnable)
	heaters.Load(config)
	cf.fan = fan.NewFan(config, 0.)
	cf.fanSpeed = config.GetFloat("fan_speed", configfile.Default(1.), configfile.Min(0.), configfile.Max(1.))
	cf.idleSpeed = config.GetFloat("idle_speed", configfile.Default(cf.fanSpeed), configfile.Min(0.), configfile.Max(1.))
	cf.idleTimeout = config.GetInt("idle_timeout", configfile.Default(30), configfile.Min(0))
	cf.heaterNames = config.GetList("heater", ",", 0, configfile.Default([]string{"extruder"}))
	cf.lastOn = cf.idleTimeout
	return cf
}

func init() { host.RegisterModule("controller_fan", nil, loadPrefix) }

func (cf *ControllerFan) handleConnect() {
	// Heater lookup
	pheaters := heaters.Of(cf.printer)
	cf.heaters = nil
	for _, n := range cf.heaterNames {
		cf.heaters = append(cf.heaters, pheaters.LookupHeater(n))
	}
	// Stepper lookup
	allSteppers := cf.stepperEnable.Steppers()
	if !cf.hasSteppers {
		cf.stepperNames = allSteppers
		return
	}
	for _, n := range cf.stepperNames {
		if !slices.Contains(allSteppers, n) {
			quoted := make([]string, len(cf.stepperNames))
			for i, s := range cf.stepperNames {
				quoted[i] = "'" + s + "'"
			}
			configfile.Raise("One or more of these steppers are unknown: %s (valid steppers are: %s)",
				fmt.Sprintf("[%s]", strings.Join(quoted, ", ")), strings.Join(allSteppers, ", "))
		}
	}
}

func (cf *ControllerFan) handleReady() {
	r := cf.printer.Reactor()
	r.RegisterTimer(cf.callback, r.Monotonic()+pinMinTime)
}

func (cf *ControllerFan) Status(eventtime float64) map[string]any { return cf.fan.Status(eventtime) }

func (cf *ControllerFan) callback(eventtime float64) float64 {
	speed := 0.
	active := false
	for _, name := range cf.stepperNames {
		active = active || cf.stepperEnable.LookupEnable(name).IsMotorEnabled()
	}
	for _, h := range cf.heaters {
		if _, target := h.GetTemp(eventtime); target != 0 {
			active = true
		}
	}
	if active {
		cf.lastOn = 0
		speed = cf.fanSpeed
	} else if cf.lastOn < cf.idleTimeout {
		speed = cf.idleSpeed
		cf.lastOn++
	}
	if speed != cf.lastSpeed {
		cf.lastSpeed = speed
		cf.fan.SetSpeed(speed, math.NaN())
	}
	return eventtime + 1.
}
