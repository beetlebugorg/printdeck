// Package pins reads the pin names of the config ("^!PA1", "mcu:PB2"), finds the chip of each pin,
// and changes the pin names of board commands to the names that the board knows.
//
// Ported to Go from klippy/pins.py of Klipper (https://github.com/Klipper3d/klipper), commit
// 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
// The functions panic with *Error, as pins.py raises its errors during the config load: the code
// that loads the printer objects recovers them.
//
//	Copyright (C) 2016-2021  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package pins

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Error is a pin error, for example a pin that two config sections use.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

func raise(format string, args ...any) { panic(&Error{fmt.Sprintf(format, args...)}) }

// Command translation

var rePin = regexp.MustCompile(`([ _]pin=)([^ ]*)`)

// Resolver changes the pin names of the commands of one chip (PinResolver).
type Resolver struct {
	validateAliases bool
	reserved        map[string]string
	aliases         map[string]string
	activePins      map[string]string
}

func NewResolver(validateAliases bool) *Resolver {
	return &Resolver{validateAliases: validateAliases, reserved: map[string]string{},
		aliases: map[string]string{}, activePins: map[string]string{}}
}

// badPinName reports a name with a modifier character or whitespace.
func badPinName(pin string) bool {
	return strings.ContainsAny(pin, "^~!:") || strings.Join(strings.FieldsFunc(pin, unicode.IsSpace), "") != pin
}

// ReservePin reserves a pin for a function of the board, for example a USB pin.
func (r *Resolver) ReservePin(pin, reserveName string) {
	if old, ok := r.reserved[pin]; ok && old != reserveName {
		raise("Pin %s reserved for %s - can't reserve for %s", pin, old, reserveName)
	}
	r.reserved[pin] = reserveName
}

// AliasPin makes alias a second name of pin.
func (r *Resolver) AliasPin(alias, pin string) {
	if old, ok := r.aliases[alias]; ok && old != pin {
		raise("Alias %s mapped to %s - can't alias to %s", alias, old, pin)
	}
	if badPinName(pin) {
		raise("Invalid pin alias '%s'\n", pin)
	}
	if p, ok := r.aliases[pin]; ok {
		pin = p
	}
	r.aliases[alias] = pin
	for existingAlias, existingPin := range r.aliases {
		if existingPin == alias {
			r.aliases[existingAlias] = pin
		}
	}
}

// UpdateCommand changes the pin names of a command ("pin=" and "_pin=" parameters) to the board's
// names.
func (r *Resolver) UpdateCommand(cmd string) string {
	return rePin.ReplaceAllStringFunc(cmd, func(s string) string {
		m := rePin.FindStringSubmatch(s)
		name := m[2]
		pinID, ok := r.aliases[name]
		if !ok {
			pinID = name
		}
		active, ok := r.activePins[pinID]
		if !ok {
			active = name
			r.activePins[pinID] = name
		}
		if name != active && r.validateAliases {
			raise("pin %s is an alias for %s", name, active)
		}
		if res, ok := r.reserved[pinID]; ok {
			raise("pin %s is reserved for %s", name, res)
		}
		return m[1] + pinID
	})
}

// Pin to chip mapping

// Chip is a chip that has pins: a board, or a chip on a bus of a board.
type Chip interface {
	SetupPin(pinType string, params *Params) any
}

// Params is a pin of the config.
type Params struct {
	Chip      Chip
	ChipName  string
	Pin       string
	Invert    int
	Pullup    int
	ShareType string
}

// Pins are the chips and the pins in use (PrinterPins).
type Pins struct {
	chips             map[string]Chip
	activePins        map[string]*Params
	pinResolvers      map[string]*Resolver
	allowMultiUsePins map[string]bool
}

func New() *Pins {
	return &Pins{chips: map[string]Chip{}, activePins: map[string]*Params{},
		pinResolvers: map[string]*Resolver{}, allowMultiUsePins: map[string]bool{}}
}

// ParsePin reads a pin description.
func (p *Pins) ParsePin(pinDesc string, canInvert, canPullup bool) *Params {
	desc := strings.TrimSpace(pinDesc)
	pullup, invert := 0, 0
	if canPullup && (strings.HasPrefix(desc, "^") || strings.HasPrefix(desc, "~")) {
		pullup = 1
		if strings.HasPrefix(desc, "~") {
			pullup = -1
		}
		desc = strings.TrimSpace(desc[1:])
	}
	if canInvert && strings.HasPrefix(desc, "!") {
		invert = 1
		desc = strings.TrimSpace(desc[1:])
	}
	chipName, pin := "mcu", desc
	if c, pn, ok := strings.Cut(desc, ":"); ok {
		chipName, pin = strings.TrimSpace(c), strings.TrimSpace(pn)
	}
	chip, ok := p.chips[chipName]
	if !ok {
		raise("Unknown pin chip name '%s'", chipName)
	}
	if badPinName(pin) {
		format := ""
		if canPullup {
			format += "[^~] "
		}
		if canInvert {
			format += "[!] "
		}
		raise("Invalid pin description '%s'\nFormat is: %s[chip_name:] pin_name", pinDesc, format)
	}
	return &Params{Chip: chip, ChipName: chipName, Pin: pin, Invert: invert, Pullup: pullup}
}

// LookupPin reads a pin description and marks the pin as in use. A pin that is in use already is
// an error, except for a shared pin of the same share type and polarity.
func (p *Pins) LookupPin(pinDesc string, canInvert, canPullup bool, shareType string) *Params {
	params := p.ParsePin(pinDesc, canInvert, canPullup)
	shareName := params.ChipName + ":" + params.Pin
	if share, ok := p.activePins[shareName]; ok {
		switch {
		case p.allowMultiUsePins[shareName]:
		case shareType == "" || shareType != share.ShareType:
			raise("pin %s used multiple times in config", params.Pin)
		case params.Invert != share.Invert || params.Pullup != share.Pullup:
			raise("Shared pin %s must have same polarity", params.Pin)
		}
		return share
	}
	params.ShareType = shareType
	p.activePins[shareName] = params
	return params
}

// SetupPin makes the object of a pin: pinType is "endstop", "digital_out", "pwm" or "adc".
func (p *Pins) SetupPin(pinType, pinDesc string) any {
	canInvert := pinType == "endstop" || pinType == "digital_out" || pinType == "pwm"
	canPullup := pinType == "endstop"
	params := p.LookupPin(pinDesc, canInvert, canPullup, "")
	return params.Chip.SetupPin(pinType, params)
}

// ResetPinSharing marks a pin as free.
func (p *Pins) ResetPinSharing(params *Params) {
	delete(p.activePins, params.ChipName+":"+params.Pin)
}

// PinResolver returns the resolver of a chip.
func (p *Pins) PinResolver(chipName string) *Resolver {
	r, ok := p.pinResolvers[chipName]
	if !ok {
		raise("Unknown chip name '%s'", chipName)
	}
	return r
}

// RegisterChip adds a chip.
func (p *Pins) RegisterChip(chipName string, chip Chip) {
	chipName = strings.TrimSpace(chipName)
	if _, ok := p.chips[chipName]; ok {
		raise("Duplicate chip name '%s'", chipName)
	}
	p.chips[chipName] = chip
	p.pinResolvers[chipName] = NewResolver(true)
}

// AllowMultiUsePin lets more than one config section use a pin.
func (p *Pins) AllowMultiUsePin(pinDesc string) {
	params := p.ParsePin(pinDesc, false, false)
	p.allowMultiUsePins[params.ChipName+":"+params.Pin] = true
}
