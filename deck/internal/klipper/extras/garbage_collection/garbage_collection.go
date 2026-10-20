// Package garbage_collection logs the reactor callbacks that keep the reactor busy for too long.
//
// Ported to Go from klippy/extras/garbage_collection.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f. The
// Python module also runs Python's garbage collector when the reactor is idle. The Go runtime has
// its own collector, so the port has only the latency warning.
//
//	Copyright (C) 2025  Branden Cash <ammmze@gmail.com>
//	Copyright (C) 2016-2026  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package garbage_collection

import (
	"log"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
)

// threshold is the time of a reactor callback that gets a warning.
const threshold = 0.05

// GarbageCollection is the module object.
type GarbageCollection struct{ printer *host.Printer }

func load(config *configfile.Wrapper) any {
	gc := &GarbageCollection{printer: host.Of(config)}
	gc.printer.RegisterEventHandler("klippy:ready", func(...any) {
		r := gc.printer.Reactor()
		r.RegisterCallback(func(float64) any {
			r.SetLatencyNotifier(threshold, func(eventtime, prevEventtime float64) {
				log.Printf("Reactor %.3f busy for %.3f", eventtime, eventtime-prevEventtime)
			})
			return nil
		}, reactor.Now)
	})
	return gc
}

func init() { host.RegisterModule("garbage_collection", load, nil) }
