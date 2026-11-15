// Package printdeck_pad is the [printdeck_pad] section, a module of Printdeck: Klipper has no such
// module. During a print, the probe measures the top of a small solid pad that the print makes
// next to the part, one layer at a time. The nozzle sets the top of each layer, and the probe
// measures that top. So the difference is the error of the nozzle height: an error of z_offset,
// a growth of the hot nozzle, or too little plastic.
//
//	[printdeck_pad]
//	clearance: 1.0   # the nozzle stays this much over the top of the print during the probe
//	gain: 0.5        # the part of the error that a correction removes
//	max_step: 0.05   # the most change of the correction after one check, in mm
//	max_adjust: 0.5  # the most correction, in mm
//
// PAD_CHECK X=<x> Y=<y> [CORRECT=1]: the nozzle is over the middle of the pad at the height of the
// layer, at X and Y. The pin probes the middle of the pad, and the nozzle goes back. The error is
// the top that the probe reads, less the height of the nozzle. With CORRECT=1 the command then
// changes the Z of the G-code offset (SET_GCODE_OFFSET Z_ADJUST) for the next layers.
//
// The error stays when the nozzle moves: the nozzle moves the top of the plastic with it. So the
// correction goes to a target: the negative of the error. It moves gain of the way to the target
// after each check, and no more than max_step. The height of the print is wrong by the error
// plus the correction: "print_error" in the results.
//
// The descent of the probe stops when the nozzle gets to clearance mm under the probe trigger
// height of the layer, that is z_offset less clearance over the top. A pin that misses the pad
// then gives an error, and the nozzle stays over the print. clearance must be less than z_offset.
//
// PAD_FIND_OFFSET X=<x> Y=<y> SIDE=<side> HEIGHT=<height> finds x_offset and y_offset of the
// probe on a square pad that the print made: the middle at X and Y, the side and the height in
// mm. The pin probes the middle, then bisects each edge to 0.1 mm: on the pad the probe reads
// about the height, off the pad about 0. The middle of the edges that the pin finds, less the
// middle that the nozzle printed, is the error of the offsets. The width of the pin moves the two
// edges of a side by the same amount, so the middle stays. If the pin misses the middle, it tries
// 8 places around it. The status has "found_x_offset" and "found_y_offset".
//
// PAD_RESET [RESULTS=0] removes the correction, and clears the results. With RESULTS=0 the
// results stay. The status has the results of the checks:
// "checks", a list of [z, error, correction] with z the height of the nozzle and the correction
// of the layer, and "adjust", the correction now.
//
//	Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package printdeck_pad

import (
	"fmt"
	"math"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/probe"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/toolhead"
)

// above is the height of the probe start over its trigger height, in mm.
const above = 2.0

// Pad is the [printdeck_pad] section.
type Pad struct {
	printer           *host.Printer
	clearance, gain   float64
	maxStep, maxAdj   float64
	travelSpeed, lift float64
	adjust            float64      // the correction now, in mm
	checks            [][3]float64 // z, error, correction
	found             []float64    // x_offset and y_offset of PAD_FIND_OFFSET, or nil
}

func load(config *configfile.Wrapper) any {
	p := &Pad{printer: host.Of(config)}
	p.clearance = config.GetFloat("clearance", configfile.Default(1.), configfile.Above(0.))
	p.gain = config.GetFloat("gain", configfile.Default(0.5), configfile.Above(0.), configfile.Max(1.))
	p.maxStep = config.GetFloat("max_step", configfile.Default(0.05), configfile.Above(0.))
	p.maxAdj = config.GetFloat("max_adjust", configfile.Default(0.5), configfile.Above(0.))
	p.travelSpeed = config.GetFloat("travel_speed", configfile.Default(150.), configfile.Above(0.))
	p.lift = config.GetFloat("lift_speed", configfile.Default(15.), configfile.Above(0.))
	g := gcode.Of(p.printer)
	g.RegisterCommand("PAD_CHECK", p.cmdCheck, false, "Probe the check pad, and correct the Z of the print")
	g.RegisterCommand("PAD_FIND_OFFSET", p.cmdFindOffset, false, "Find x_offset and y_offset of the probe on a printed pad")
	g.RegisterCommand("PAD_RESET", p.cmdReset, false, "Clear the results and the correction of the pad checks")
	return p
}

func init() { host.RegisterModule("printdeck_pad", load, nil) }

// Status is the results of the checks.
func (p *Pad) Status(eventtime float64) map[string]any {
	checks := make([]any, len(p.checks))
	for i, c := range p.checks {
		checks[i] = []any{c[0], c[1], c[2]}
	}
	st := map[string]any{"checks": checks, "adjust": p.adjust}
	if p.found != nil {
		st["found_x_offset"], st["found_y_offset"] = p.found[0], p.found[1]
	}
	return st
}

func (p *Pad) cmdReset(gcmd *gcode.Command) {
	if gcmd.GetInt("RESULTS", gcode.Default(1)) != 0 {
		p.checks = nil
	}
	if p.adjust != 0 {
		gcode.Of(p.printer).RunScriptFromCommand(fmt.Sprintf("SET_GCODE_OFFSET Z_ADJUST=%.4f MOVE=0", -p.adjust))
	}
	p.adjust = 0
}

// Step is the change of the correction after a check: gain of the way from the correction now
// to the negative of the error, no more than maxStep, and the correction no more than maxAdj.
func Step(err, adjust, gain, maxStep, maxAdj float64) float64 {
	d := gain * (-err - adjust)
	d = math.Max(-maxStep, math.Min(maxStep, d))
	next := math.Max(-maxAdj, math.Min(maxAdj, adjust+d))
	return next - adjust
}

func (p *Pad) cmdCheck(gcmd *gcode.Command) {
	pr, ok := p.printer.TryLookupObject("probe").(probe.Probe)
	if !ok {
		panic(gcode.Errorf("PAD_CHECK needs a probe"))
	}
	x := gcmd.GetFloat("X")
	y := gcmd.GetFloat("Y")
	correct := gcmd.GetInt("CORRECT", gcode.Default(0)) != 0
	off := pr.Offsets(nil)
	if p.clearance >= off[2] {
		panic(gcode.Errorf("clearance of [printdeck_pad] must be less than z_offset of the probe"))
	}
	th := toolhead.Of(p.printer)
	start := th.Position()
	z := start[2] // the nozzle at the height of the layer, over the middle of the pad
	nan := math.NaN()
	top := z + off[2] + above
	th.ManualMove([]float64{nan, nan, top}, p.lift)
	th.ManualMove([]float64{x - off[0], y - off[1], nan}, p.travelSpeed)
	var bedZ float64
	session := pr.StartProbeSession(gcmd)
	func() {
		defer session.EndProbeSession()
		probe.WithFloor(p.printer, z+off[2]-p.clearance, func() {
			func() {
				defer func() {
					if r := recover(); r != nil {
						if e, ok := r.(*gcode.CommandError); ok && strings.Contains(e.Msg, "No trigger") {
							panic(gcode.Errorf("The probe did not find the check pad at Z %.2f: the pin missed "+
								"the pad, or the pad is %.1f mm low.", z, p.clearance))
						}
						panic(r)
					}
				}()
				session.RunProbe(gcmd)
			}()
			rs := session.PullProbedResults()
			bedZ = rs[len(rs)-1].BedZ()
		})
		th.ManualMove([]float64{nan, nan, top}, p.lift)
	}()
	th.ManualMove([]float64{start[0], start[1], nan}, p.travelSpeed)
	th.ManualMove([]float64{nan, nan, z}, p.lift)
	err := bedZ - z
	// The correction of this layer: the result keeps it, and the change is for the next layers.
	p.checks = append(p.checks, [3]float64{z, err, p.adjust})
	msg := fmt.Sprintf("Pad check at Z %.2f: error %+.3f mm, height of the print %+.3f mm", z, err, err+p.adjust)
	if correct {
		if d := Step(err, p.adjust, p.gain, p.maxStep, p.maxAdj); d != 0 {
			p.adjust += d
			gcode.Of(p.printer).RunScriptFromCommand(fmt.Sprintf("SET_GCODE_OFFSET Z_ADJUST=%.4f MOVE=0", d))
		}
		msg += fmt.Sprintf(", correction now %+.3f mm", p.adjust)
	}
	gcmd.RespondInfo(msg, true)
}

// findAbove is the height of the probe start over the trigger height on the pad, in mm, for
// PAD_FIND_OFFSET.
const findAbove = 4.0

// Bisect returns the place of an edge between in (on the pad) and out (off the pad), to tol mm.
// on probes a place, and returns true when it is on the pad.
func Bisect(in, out, tol float64, on func(v float64) bool) float64 {
	for math.Abs(out-in) > tol {
		mid := (in + out) / 2
		if on(mid) {
			in = mid
		} else {
			out = mid
		}
	}
	return (in + out) / 2
}

func (p *Pad) cmdFindOffset(gcmd *gcode.Command) {
	pr, ok := p.printer.TryLookupObject("probe").(probe.Probe)
	if !ok {
		panic(gcode.Errorf("PAD_FIND_OFFSET needs a probe"))
	}
	cx, cy := gcmd.GetFloat("X"), gcmd.GetFloat("Y")
	side := gcmd.GetFloat("SIDE", gcode.Above(0))
	height := gcmd.GetFloat("HEIGHT", gcode.Above(0))
	off := pr.Offsets(nil)
	th := toolhead.Of(p.printer)
	nan := math.NaN()
	top := height + off[2] + findAbove
	th.ManualMove([]float64{nan, nan, math.Max(th.Position()[2], top)}, p.lift)
	n := 0
	// on probes the pin place (x, y): true on the pad. Each probe deploys and stows the pin. With
	// the pin down from probe to probe, the short moves of the bisection start the next probe
	// before the BLTouch is ready after its trigger: "Probe triggered prior to movement".
	on := func(x, y float64) bool {
		th.ManualMove([]float64{x - off[0], y - off[1], nan}, p.travelSpeed)
		session := pr.StartProbeSession(gcmd)
		var z float64
		func() {
			defer session.EndProbeSession()
			session.RunProbe(gcmd)
			rs := session.PullProbedResults()
			z = rs[len(rs)-1].BedZ()
		}()
		th.ManualMove([]float64{nan, nan, top}, p.lift)
		n++
		return z > height/2
	}
	var fx, fy float64
	func() {
		var w, h float64
		var err error
		fx, fy, w, h, err = FindCenter(cx, cy, side, on)
		if err != nil {
			panic(gcode.Errorf("%s", err.Error()))
		}
		gcmd.RespondInfo(fmt.Sprintf("The pad measures %.2f by %.2f mm (%d probes)", w, h, n), true)
	}()
	// The pin found the middle at (fx, fy) with the offsets off. The nozzle printed it at (cx, cy).
	p.found = []float64{off[0] - (fx - cx), off[1] - (fy - cy)}
	gcmd.RespondInfo(fmt.Sprintf("x_offset %.2f (now %.2f), y_offset %.2f (now %.2f)",
		p.found[0], off[0], p.found[1], off[1]), true)
}

// FindCenter finds the middle of a square pad of side side, near (cx, cy), with on: true when
// the pin at a place is on the pad. It returns the middle, and the width along X and Y.
func FindCenter(cx, cy, side float64, on func(x, y float64) bool) (fx, fy, w, h float64, err error) {
	// A place on the pad: the middle, or one of 8 places around it.
	d := side * 0.3
	ix, iy, found := cx, cy, false
	for _, o := range [][2]float64{{0, 0}, {0, -d}, {0, d}, {-d, 0}, {d, 0}, {-d, -d}, {d, -d}, {-d, d}, {d, d}} {
		if on(cx+o[0], cy+o[1]) {
			ix, iy, found = cx+o[0], cy+o[1], true
			break
		}
	}
	if !found {
		return 0, 0, 0, 0, fmt.Errorf("The probe did not find the pad at 9 places around X %.1f Y %.1f. "+
			"Check x_offset and y_offset of the probe: they are more than %.0f mm wrong.", cx, cy, side*0.8)
	}
	edge := func(name string, in, out float64, at func(v float64) bool) float64 {
		if err != nil {
			return 0
		}
		if at(out) {
			err = fmt.Errorf("The probe found the pad at the %s side, %.1f mm from the place on the pad. "+
				"Check SIDE and the pad.", name, math.Abs(out-in))
			return 0
		}
		return Bisect(in, out, 0.1, at)
	}
	onX := func(v float64) bool { return on(v, iy) }
	left := edge("left", ix, ix-side, onX)
	right := edge("right", ix, ix+side, onX)
	fx = (left + right) / 2
	onY := func(v float64) bool { return on(fx, v) }
	front := edge("front", iy, iy-side, onY)
	back := edge("back", iy, iy+side, onY)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	fy = (front + back) / 2
	w, h = right-left, back-front
	if math.Abs(w-h) > 2 || math.Abs(w-side) > 3 {
		return 0, 0, 0, 0, fmt.Errorf("The pad measures %.1f by %.1f mm, and SIDE is %.1f. The edges are not clear: "+
			"check the pad.", w, h, side)
	}
	return fx, fy, w, h, nil
}
