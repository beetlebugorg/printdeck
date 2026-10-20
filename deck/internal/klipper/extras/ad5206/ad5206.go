// Package ad5206 sets the channels of an AD5206 digital potentiometer, for example the stepper
// currents of a RAMBo.
//
// Ported to Go from klippy/extras/ad5206.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2017,2018  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package ad5206

import (
	"fmt"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/bus"
	"printdeck/internal/klipper/host"
)

// AD5206 is a digital potentiometer on SPI.
type AD5206 struct{ spi *bus.MCUSPI }

func loadPrefix(config *configfile.Wrapper) any {
	d := &AD5206{spi: bus.MCUSPIFromConfig(config, 0, "enable_pin", 25000000, "", false)}
	scale := config.GetFloat("scale", configfile.Default(1.), configfile.Above(0.))
	for i := 0; i < 6; i++ {
		if val, ok := config.GetFloatOptional(fmt.Sprintf("channel_%d", i+1), configfile.Default(nil),
			configfile.Min(0.), configfile.Max(scale)); ok {
			d.SetRegister(i, int(val*256./scale+.5))
		}
	}
	return d
}

func init() { host.RegisterModule("ad5206", nil, loadPrefix) }

// SetRegister sets a channel register.
func (d *AD5206) SetRegister(reg, value int) {
	d.spi.SPISend([]byte{byte(reg), byte(value)}, 0, 0)
}
