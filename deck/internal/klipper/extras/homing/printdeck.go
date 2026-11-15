package homing

// Printdeck additions to the port of Klipper:
//
//   - A limit on the skew of the steppers of one rail with several endstops (two Z motors with a
//     switch each). Klipper stops each stepper at its own endstop and moves on until all the endstops
//     trigger. When a switch or its wire fails, its stepper then drives into the frame while the
//     other side waits at its switch. With a limit (the max_skew of [printdeck_homing]), the move
//     stops when the other endstops of the rail do not trigger within max_skew mm after the first
//     one.
//   - The home of X and Y in one move (the home_xy_together of [printdeck_homing]). The kinematics
//     gives the two rails to one HomeRails. The move goes to the corner of the two endstops, and each
//     stepper stops at its own endstop, as the two Z motors do. The speed of the move keeps each axis
//     at or below its homing_speed.

import (
	"fmt"
	"math"
	"strings"

	"printdeck/internal/klipper/reactor"
	"printdeck/internal/klipper/stepper"
)

// SetMaxSkew sets the limit in mm. 0 is no limit.
func (ph *PrinterHoming) SetMaxSkew(mm float64) { ph.maxSkew = mm }

// SetHomeXY sets the home of X and Y in one move.
func (ph *PrinterHoming) SetHomeXY(on bool) { ph.homeXY = on }

// HomeXYTogether reports the home of X and Y in one move (stepper.XYHomer).
func (h *Homing) HomeXYTogether() bool {
	ph, _ := h.printer.TryLookupObject("homing").(*PrinterHoming)
	return ph != nil && ph.homeXY
}

// railGroups gives the index of the rail of each endstop name of the rails.
func railGroups(rails []*stepper.Rail) map[string]int {
	out := map[string]int{}
	for i, r := range rails {
		for _, e := range r.Endstops() {
			out[e.Name] = i
		}
	}
	return out
}

// railsSpeed is the speed of a homing move of the rails from forcepos to movepos: the homing speed
// of the first rail (as in Klipper), or the second homing speed. When the rails home several axes in
// one move, rail i homes the axis of the i-th position in forcepos that is not NaN. The speed then
// keeps each axis at or below the speed of its rail, and the axis with the longest time at its speed.
func railsSpeed(rails []*stepper.Rail, forcepos, movepos []float64, second bool) float64 {
	speedOf := func(r *stepper.Rail) float64 {
		if second {
			return r.HomingInfo().SecondHomingSpeed
		}
		return r.HomingInfo().Speed
	}
	speed := speedOf(rails[0])
	var axes []int
	for i := 0; i < 3 && i < len(forcepos); i++ {
		if !math.IsNaN(forcepos[i]) {
			axes = append(axes, i)
		}
	}
	if len(rails) < 2 || len(axes) != len(rails) {
		return speed
	}
	var sq float64
	for _, a := range axes {
		sq += (movepos[a] - forcepos[a]) * (movepos[a] - forcepos[a])
	}
	length := math.Sqrt(sq)
	speed = math.Inf(1)
	for i, a := range axes {
		if d := math.Abs(movepos[a] - forcepos[a]); d > 0 {
			speed = math.Min(speed, speedOf(rails[i])*length/d)
		}
	}
	if math.IsInf(speed, 1) {
		return speedOf(rails[0])
	}
	return speed
}

// skewGuard returns a completion for the homing move: it completes with all the endstops, or with 1
// when the first endstop of a rail triggered and the other endstops of that rail did not trigger
// within maxSkew mm at speed. *stopped then has the message of the stop. The endstops of other rails
// (X and Y in one move) trigger at any time.
func (hm *HomingMove) skewGuard(all *reactor.Completion, triggers []*reactor.Completion, speed float64) (*reactor.Completion, *string) {
	stopped := new(string)
	ph, _ := hm.printer.TryLookupObject("homing").(*PrinterHoming)
	if ph == nil || ph.maxSkew <= 0 || len(triggers) < 2 || speed <= 0 {
		return all, stopped
	}
	// The endstops of each rail. With no groups, all the endstops are of one rail.
	byRail := map[int][]int{}
	for i, es := range hm.endstops {
		byRail[hm.groups[es.Name]] = append(byRail[hm.groups[es.Name]], i)
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
	for _, rail := range byRail {
		if len(rail) < 2 {
			continue
		}
		var railTriggers []*reactor.Completion
		for _, i := range rail {
			railTriggers = append(railTriggers, triggers[i])
		}
		railAll := multiComplete(hm.printer, railTriggers)
		first := false
		for _, i := range rail {
			i, c := i, triggers[i]
			r.RegisterCallback(func(float64) any {
				if truthy(c.Wait(reactor.Never, nil)) || first || out.Test() {
					return 0
				}
				first = true
				deadline := r.Monotonic() + ph.maxSkew/speed
				if railAll.Wait(deadline, "timeout") != "timeout" || out.Test() {
					return 0
				}
				var missing []string
				for _, j := range rail {
					if !triggers[j].Test() {
						missing = append(missing, hm.endstops[j].Name)
					}
				}
				*stopped = fmt.Sprintf("Homing stopped: %s triggered, but %s did not trigger within %.1f mm. "+
					"Check that switch and its wire.", hm.endstops[i].Name, strings.Join(missing, ", "), ph.maxSkew)
				out.Complete(1)
				return 0
			}, reactor.Now)
		}
	}
	return out, stopped
}
