// Package board_pins is the [board_pins] section: aliases and reserved pins of a board.
//
// Ported to Go from klippy/extras/board_pins.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2019-2021  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package board_pins

import (
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/pins"
)

// PrinterBoardAliases is a [board_pins] section.
type PrinterBoardAliases struct{}

func load(config *configfile.Wrapper) any {
	ppins := host.Of(config).LookupObject("pins").(*pins.Pins)
	mcuNames := config.GetList("mcu", ",", 0, configfile.Default([]string{"mcu"}))
	var pinResolvers []*pins.Resolver
	for _, n := range mcuNames {
		pinResolvers = append(pinResolvers, ppins.PinResolver(n))
	}
	options := append([]string{"aliases"}, config.PrefixOptions("aliases_")...)
	for _, opt := range options {
		aliases := config.GetLists(opt, []string{"=", ","}, 2)
		for _, a := range aliases {
			name, value := a[0], a[1]
			if strings.HasPrefix(value, "<") && strings.HasSuffix(value, ">") {
				for _, r := range pinResolvers {
					r.ReservePin(name, value)
				}
			} else {
				for _, r := range pinResolvers {
					r.AliasPin(name, value)
				}
			}
		}
	}
	return &PrinterBoardAliases{}
}

func init() { host.RegisterModule("board_pins", load, load) }
