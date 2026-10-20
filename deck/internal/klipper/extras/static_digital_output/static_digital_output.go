// Package static_digital_output sets a list of digital output pins once, at the board config.
//
// Ported to Go from klippy/extras/static_digital_output.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2017-2018  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package static_digital_output

import (
	"fmt"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/pins"
)

// PrinterStaticDigitalOut is the module object. It has no state.
type PrinterStaticDigitalOut struct{}

func loadPrefix(config *configfile.Wrapper) any {
	ppins := host.Of(config).LookupObject("pins").(*pins.Pins)
	for _, pinDesc := range config.GetList("pins", ",", 0) {
		params := ppins.LookupPin(pinDesc, true, false, "")
		m, ok := params.Chip.(*mcu.MCU)
		if !ok {
			configfile.Raise("pin %s is not a pin of a board", pinDesc)
		}
		value := 0
		if params.Invert == 0 {
			value = 1
		}
		m.AddConfigCmd(fmt.Sprintf("set_digital_out pin=%s value=%d", params.Pin, value), false, false)
	}
	return &PrinterStaticDigitalOut{}
}

func init() { host.RegisterModule("static_digital_output", nil, loadPrefix) }
