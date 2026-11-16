// Package printdeck_jog is the [printdeck_jog] section, a module of Printdeck: Klipper has no such
// module. It moves the toolhead while the user holds an arrow key of the deck.
//
//	[printdeck_jog]
//
//	PRINTDECK_JOG ID=<n> AXIS=<X|Y|Z> DIR=<1|-1> SPEED=<mm/s>
//
// PRINTDECK_JOG moves one axis at SPEED, until the deck releases the key or the axis gets to its
// limit. The deck holds the key with the webhook printdeck/jog {"id": n, "hold": true} at least
// each holdTime, and releases it with {"id": n, "hold": false}. A release can come before the
// command: then the command does not move. With no hold for holdTime, for example when the deck
// stops, the move stops too.
//
//	Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package printdeck_jog

import (
	"strings"
	"sync"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
	"printdeck/internal/klipper/toolhead"
	"printdeck/internal/klipper/webhooks"
)

// holdTime is the longest time between two holds of the deck, in s. The first hold of a move also
// gets the start time of the toolhead (about 0.25 s).
const holdTime = 0.5

// PrintdeckJog is the [printdeck_jog] section.
type PrintdeckJog struct {
	printer *host.Printer
	reactor *reactor.Reactor

	mu       sync.Mutex
	id       int     // the move that runs
	released int     // the highest move that the deck released
	until    float64 // the end of the last hold
}

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	j := &PrintdeckJog{printer: printer, reactor: printer.Reactor()}
	gcode.Of(printer).RegisterCommand("PRINTDECK_JOG", j.cmdJog, false,
		"Move an axis while the deck holds an arrow key")
	webhooks.Of(printer).RegisterEndpoint("printdeck/jog", j.handleHold)
	return j
}

func init() { host.RegisterModule("printdeck_jog", load, nil) }

func (j *PrintdeckJog) handleHold(req *webhooks.WebRequest) {
	id := toInt(req.GetInt("id"))
	hold, _ := req.Get("hold", false).(bool)
	j.mu.Lock()
	defer j.mu.Unlock()
	if !hold {
		j.released = max(j.released, id)
	} else if id == j.id {
		j.until = j.reactor.Monotonic() + holdTime
	}
	req.Send(map[string]any{})
}

func (j *PrintdeckJog) held(id int) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return id > j.released && j.reactor.Monotonic() < j.until
}

func (j *PrintdeckJog) cmdJog(gcmd *gcode.Command) {
	id := gcmd.GetInt("ID", configfile.Above(0))
	axis := strings.ToUpper(gcmd.GetString("AXIS"))
	dir := gcmd.GetFloat("DIR")
	speed := gcmd.GetFloat("SPEED", configfile.Above(0.))
	i := strings.Index("XYZ", axis)
	if len(axis) != 1 || i < 0 || (dir != 1 && dir != -1) {
		panic(gcmd.Error("PRINTDECK_JOG needs AXIS=X, Y or Z, and DIR=1 or -1"))
	}
	j.mu.Lock()
	if id <= j.released {
		j.mu.Unlock()
		return
	}
	j.id, j.until = id, j.reactor.Monotonic()+holdTime+0.25
	j.mu.Unlock()

	th := toolhead.Of(j.printer)
	st := th.Kin().Status(j.reactor.Monotonic())
	if !strings.Contains(st["homed_axes"].(string), strings.ToLower(axis)) {
		panic(gcmd.Error("Home %s first", axis))
	}
	pos := th.Position()[i]
	limit := st["axis_maximum"].(gcode.Coord)[i]
	if dir < 0 {
		limit = st["axis_minimum"].(gcode.Coord)[i]
	}
	var d [3]float64
	d[i] = dir
	th.HoldMove(d, speed, dir*(limit-pos), func() bool { return j.held(id) })
}

func toInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	}
	return 0
}
