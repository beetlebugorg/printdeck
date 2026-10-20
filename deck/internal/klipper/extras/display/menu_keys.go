// Support for menu button press tracking.
//
// Ported to Go from klippy/extras/display/menu_keys.py of Klipper
// (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018  Janar Sööt <janar.soot@gmail.com>
//	Copyright (C) 2020  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.

package display

import (
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/buttons"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
)

const longPressDuration = 0.800

// menuKeys sends the button events of the menu to a callback.
type menuKeys struct {
	printer                 *host.Printer
	reactor                 *reactor.Reactor
	callback                func(key string, eventtime float64)
	encoderFastRate         float64
	lastEncoderCWEventtime  float64
	lastEncoderCCWEventtime float64
	isShortClick            bool
	clickTimer              *reactor.Timer
}

func newMenuKeys(config *configfile.Wrapper, callback func(key string, eventtime float64)) *menuKeys {
	printer := host.Of(config)
	k := &menuKeys{printer: printer, reactor: printer.Reactor(), callback: callback}
	b := buttons.Load(config)
	// Register rotary encoder
	encoderPins, hasEncoder := config.GetOptional("encoder_pins", configfile.Default(nil))
	stepsPerDetent := config.GetIntChoice("encoder_steps_per_detent", map[int]any{2: 2, 4: 4},
		configfile.Default(4)).(int)
	if hasEncoder {
		pins := strings.Split(encoderPins, ",")
		if len(pins) != 2 {
			configfile.Raise("Unable to parse encoder_pins")
		}
		b.RegisterRotaryEncoder(strings.TrimSpace(pins[0]), strings.TrimSpace(pins[1]),
			k.encoderCWCallback, k.encoderCCWCallback, stepsPerDetent)
	}
	k.encoderFastRate = config.GetFloat("encoder_fast_rate", configfile.Default(.030), configfile.Above(0.))
	// Register click button
	k.clickTimer = k.reactor.RegisterTimer(k.longClickEvent, reactor.Never)
	k.registerButton(config, "click_pin", nil, k.clickCallback)
	// Register other buttons
	k.registerButton(config, "back_pin", func(e float64) { k.callback("back", e) }, nil)
	k.registerButton(config, "up_pin", func(e float64) { k.callback("up", e) }, nil)
	k.registerButton(config, "down_pin", func(e float64) { k.callback("down", e) }, nil)
	k.registerButton(config, "kill_pin", k.killCallback, nil)
	return k
}

// registerButton registers a button. A push callback is a push only button. Else the callback
// gets each state.
func (k *menuKeys) registerButton(config *configfile.Wrapper, name string, push func(float64), cb buttons.Callback) {
	pin, ok := config.GetOptional(name, configfile.Default(nil))
	if !ok {
		return
	}
	b := k.printer.LookupObject("buttons").(*buttons.PrinterButtons)
	if _, ok := config.GetOptional("analog_range_"+name, configfile.Default(nil)); !ok {
		if push != nil {
			b.RegisterButtonPush(pin, push)
		} else {
			b.RegisterButtons([]string{pin}, cb)
		}
		return
	}
	r := config.GetFloatList("analog_range_"+name, ",", 2)
	pullup := config.GetFloat("analog_pullup_resistor", configfile.Default(4700.), configfile.Above(0.))
	if push != nil {
		b.RegisterADCButtonPush(pin, r[0], r[1], pullup, push)
	} else {
		b.RegisterADCButton(pin, r[0], r[1], pullup, cb)
	}
}

// Rotary encoder callbacks

func (k *menuKeys) encoderCWCallback(eventtime float64) {
	fastRate := eventtime-k.lastEncoderCWEventtime <= k.encoderFastRate
	k.lastEncoderCWEventtime = eventtime
	if fastRate {
		k.callback("fast_up", eventtime)
	} else {
		k.callback("up", eventtime)
	}
}

func (k *menuKeys) encoderCCWCallback(eventtime float64) {
	fastRate := eventtime-k.lastEncoderCCWEventtime <= k.encoderFastRate
	k.lastEncoderCCWEventtime = eventtime
	if fastRate {
		k.callback("fast_down", eventtime)
	} else {
		k.callback("down", eventtime)
	}
}

// Click handling

func (k *menuKeys) longClickEvent(eventtime float64) float64 {
	k.isShortClick = false
	k.callback("long_click", eventtime)
	return reactor.Never
}

func (k *menuKeys) clickCallback(eventtime float64, state int) {
	if state != 0 {
		k.isShortClick = true
		k.reactor.UpdateTimer(k.clickTimer, eventtime+longPressDuration)
	} else if k.isShortClick {
		k.reactor.UpdateTimer(k.clickTimer, reactor.Never)
		k.callback("click", eventtime)
	}
}

func (k *menuKeys) killCallback(eventtime float64) {
	k.printer.InvokeShutdown("Shutdown due to kill button!", nil)
}
