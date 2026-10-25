// Package homing_heaters turns heaters off during homing moves, for printers with steppers that
// the heaters disturb.
//
// Ported to Go from klippy/extras/homing_heaters.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2016-2018  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package homing_heaters

import (
	"slices"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/heaters"
	"printdeck/internal/klipper/extras/homing"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/jinja"
	"printdeck/internal/klipper/toolhead"
)

type named interface{ Name() string }

type mcuEndstopper interface{ MCUEndstops() []homing.Endstop }

// HomingHeaters is the [homing_heaters] section.
type HomingHeaters struct {
	printer        *host.Printer
	disableHeaters []string // nil for all heaters
	flakySteppers  []string // nil for all steppers
	pheaters       *heaters.PrinterHeaters
	targetSave     map[string]float64
}

func load(config *configfile.Wrapper) any {
	h := &HomingHeaters{printer: host.Of(config), targetSave: map[string]float64{}}
	h.printer.RegisterEventHandler("klippy:connect", func(...any) { h.handleConnect() })
	h.printer.RegisterEventHandler("homing:homing_move_begin", func(args ...any) {
		h.handleHomingMoveBegin(args[0].(mcuEndstopper))
	})
	h.printer.RegisterEventHandler("homing:homing_move_end", func(args ...any) {
		h.handleHomingMoveEnd(args[0].(mcuEndstopper))
	})
	h.disableHeaters = config.GetList("heaters", ",", 0, configfile.Default(nil))
	h.flakySteppers = config.GetList("steppers", ",", 0, configfile.Default(nil))
	h.pheaters = heaters.Load(config)
	return h
}

func init() { host.RegisterModule("homing_heaters", load, nil) }

func reprList(l []string) string {
	items := make([]any, len(l))
	for i, s := range l {
		items[i] = s
	}
	return jinja.Repr(jinja.Tuple(items))
}

func (h *HomingHeaters) handleConnect() {
	// heaters to disable
	allHeaters := h.pheaters.AllHeaters()
	if h.disableHeaters == nil {
		h.disableHeaters = allHeaters
	} else {
		for _, x := range h.disableHeaters {
			if !slices.Contains(allHeaters, x) {
				configfile.Raise("One or more of these heaters are unknown: %s", reprList(h.disableHeaters))
			}
		}
	}
	// steppers valid?
	kin := toolhead.Of(h.printer).Kinematics().(toolhead.Kinematics)
	var allSteppers []string
	for _, s := range kin.Steppers() {
		allSteppers = append(allSteppers, s.(named).Name())
	}
	if h.flakySteppers == nil {
		return
	}
	for _, x := range h.flakySteppers {
		if !slices.Contains(allSteppers, x) {
			configfile.Raise("One or more of these steppers are unknown: %s", reprList(h.flakySteppers))
		}
	}
}

func (h *HomingHeaters) checkEligible(endstops []homing.Endstop) bool {
	if h.flakySteppers == nil {
		return true
	}
	for _, es := range endstops {
		for _, s := range es.Steppers() {
			if slices.Contains(h.flakySteppers, s.(named).Name()) {
				return true
			}
		}
	}
	return false
}

func (h *HomingHeaters) handleHomingMoveBegin(hmove mcuEndstopper) {
	if !h.checkEligible(hmove.MCUEndstops()) {
		return
	}
	for _, heaterName := range h.disableHeaters {
		heater := h.pheaters.LookupHeater(heaterName)
		_, target := heater.GetTemp(0)
		h.targetSave[heaterName] = target
		heater.SetTemp(0.)
	}
}

func (h *HomingHeaters) handleHomingMoveEnd(hmove mcuEndstopper) {
	if !h.checkEligible(hmove.MCUEndstops()) {
		return
	}
	for _, heaterName := range h.disableHeaters {
		heater := h.pheaters.LookupHeater(heaterName)
		heater.SetTemp(h.targetSave[heaterName])
	}
}
