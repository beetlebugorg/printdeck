// Package printdeck_homing is the [printdeck_homing] section, a module of Printdeck: Klipper has no
// such module.
//
//	[printdeck_homing]
//	max_skew: 3
//	home_xy_together: False
//
// max_skew limits the skew of the steppers of one rail with several endstops, for example two Z
// motors with a switch each. When the first endstop triggers, the others must trigger within
// max_skew mm of more travel. Else the homing move stops with an error, before a stepper with a
// failed switch drives into the frame. The steppers stop a short time after the limit (the drip of
// the move), so the real skew can be up to about 0.1 s of the homing speed more.
//
// home_xy_together homes X and Y in one move, when a G28 homes both (G28, or G28 X Y). The toolhead
// goes to the corner of the two endstops, and each stepper stops at its own endstop. Each axis moves
// at or below its homing_speed. The home then lasts as long as the longer axis. Without the option,
// it lasts as long as the two axes added. Use it only on a cartesian printer where the toolhead can
// go straight to that corner.
//
//	Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package printdeck_homing

import (
	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/homing"
	"printdeck/internal/klipper/host"
)

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	skew := config.GetFloat("max_skew", configfile.Default(3.), configfile.Min(0.))
	together := config.GetBool("home_xy_together", configfile.Default(false))
	ph := printer.LoadObject(config, "homing").(*homing.PrinterHoming)
	ph.SetMaxSkew(skew)
	ph.SetHomeXY(together)
	return &struct{}{}
}

func init() { host.RegisterModule("printdeck_homing", load, nil) }
