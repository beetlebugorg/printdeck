package homing

// Printdeck addition to the port of Klipper: a limit on the skew of the steppers of one rail with
// several endstops (two Z motors with a switch each). Klipper stops each stepper at its own endstop
// and moves on until all the endstops trigger. When a switch or its wire fails, its stepper then
// drives into the frame while the other side waits at its switch. With a limit (the max_skew of
// [printdeck_homing]), the move stops when the other endstops do not trigger within max_skew mm
// after the first one.

import (
	"fmt"
	"strings"

	"printdeck/internal/klipper/reactor"
)

// SetMaxSkew sets the limit in mm. 0 is no limit.
func (ph *PrinterHoming) SetMaxSkew(mm float64) { ph.maxSkew = mm }

// skewGuard returns a completion for the homing move: it completes with all the endstops, or with 1
// when the first endstop triggered and the others did not trigger within maxSkew mm at speed.
// *stopped then has the message of the stop.
func (hm *HomingMove) skewGuard(all *reactor.Completion, triggers []*reactor.Completion, speed float64) (*reactor.Completion, *string) {
	stopped := new(string)
	ph, _ := hm.printer.TryLookupObject("homing").(*PrinterHoming)
	if ph == nil || ph.maxSkew <= 0 || len(triggers) < 2 || speed <= 0 {
		return all, stopped
	}
	r := hm.printer.Reactor()
	out := r.NewCompletion()
	r.RegisterCallback(func(float64) any {
		v := all.Wait(reactor.Never, nil)
		if !out.Test() {
			out.Complete(v)
		}
		return 0
	}, reactor.Now)
	first := false
	for i, c := range triggers {
		i, c := i, c
		r.RegisterCallback(func(float64) any {
			if truthy(c.Wait(reactor.Never, nil)) || first || out.Test() {
				return 0
			}
			first = true
			deadline := r.Monotonic() + ph.maxSkew/speed
			if all.Wait(deadline, "timeout") != "timeout" || out.Test() {
				return 0
			}
			var missing []string
			for j, o := range triggers {
				if !o.Test() {
					missing = append(missing, hm.endstops[j].Name)
				}
			}
			*stopped = fmt.Sprintf("Homing stopped: %s triggered, but %s did not trigger within %.1f mm. "+
				"Check that switch and its wire.", hm.endstops[i].Name, strings.Join(missing, ", "), ph.maxSkew)
			out.Complete(1)
			return 0
		}, reactor.Now)
	}
	return out, stopped
}
