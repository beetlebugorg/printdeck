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
	return map[string]any{"checks": checks, "adjust": p.adjust}
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
