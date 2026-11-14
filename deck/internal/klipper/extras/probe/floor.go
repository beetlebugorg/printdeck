package probe

// Printdeck: a floor for the descent of the probe. Klipper's probe goes down to position_min of Z.
// A probe of a small part off the bed (the calibration cube of [printdeck_cube]) can miss it, and
// then the nozzle must stop above the bed.

import "printdeck/internal/klipper/host"

var floors = map[*host.Printer]float64{}

// WithFloor runs f. During f, a descent of the probe stops at Z floor (a nozzle position) when the
// probe does not trigger, with the error of no trigger.
func WithFloor(p *host.Printer, floor float64, f func()) {
	floors[p] = floor
	defer delete(floors, p)
	f()
}
