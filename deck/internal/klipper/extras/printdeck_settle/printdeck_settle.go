// Package printdeck_settle is the [printdeck_settle] section, a module of Printdeck: Klipper has no
// such module. A heated bed changes its shape for many minutes after it gets to its temperature.
// A mesh from that time is wrong when the print starts. BED_SETTLE probes one place of the bed
// again and again, and stops when the bed stops moving.
//
//	[printdeck_settle]
//	x: 142            # the place of the probe (the pin), in mm
//	y: 140
//	interval: 30      # the time from one probe to the next, in s
//	window: 180       # the time of the probes for the rate, in s
//	tau: 900          # the time constant of the bed, in s
//	tolerance: 0.02   # the most movement that is still to come, in mm
//	timeout: 900      # the most time of BED_SETTLE, in s
//
// BED_SETTLE: probes the place every interval seconds. The rate of the bed is the slope of a line
// through the probes of the last window seconds. A bed comes to its shape as an exponential with
// the time constant tau, so the movement that is still to come is the rate times tau. When it is
// tolerance or less, the bed is settled and the command ends. After timeout seconds, the command
// ends with a message.
//
// BED_SETTLE SAMPLE=1 [RESET=1]: one probe, added to the readings. RESET=1 clears the readings
// first. A file of the drift test probes this way, with a G4 between the probes.
//
// The status has "readings", a list of [t, z] with t the seconds after the first probe, "settled",
// "rate" in mm/s and "remaining" in mm.
//
//	Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package printdeck_settle

import (
	"fmt"
	"math"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/probe"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/toolhead"
)

// above is the height of the nozzle over the probe trigger height between the probes, in mm.
const above = 3.0

// Settle is the [printdeck_settle] section.
type Settle struct {
	printer               *host.Printer
	x, y                  float64
	interval, window, tau float64
	tolerance, timeout    float64
	travelSpeed, lift     float64
	readings              [][2]float64 // t, z
	t0                    float64      // the time of the first reading
	settled               bool
	rate, remaining       float64
}

func load(config *configfile.Wrapper) any {
	s := &Settle{printer: host.Of(config)}
	s.x = config.GetFloat("x")
	s.y = config.GetFloat("y")
	s.interval = config.GetFloat("interval", configfile.Default(30.), configfile.Above(0.))
	s.window = config.GetFloat("window", configfile.Default(180.), configfile.Above(0.))
	s.tau = config.GetFloat("tau", configfile.Default(900.), configfile.Above(0.))
	s.tolerance = config.GetFloat("tolerance", configfile.Default(0.02), configfile.Above(0.))
	s.timeout = config.GetFloat("timeout", configfile.Default(900.), configfile.Min(0.))
	s.travelSpeed = config.GetFloat("travel_speed", configfile.Default(150.), configfile.Above(0.))
	s.lift = config.GetFloat("lift_speed", configfile.Default(15.), configfile.Above(0.))
	gcode.Of(s.printer).RegisterCommand("BED_SETTLE", s.cmdSettle, false, "Probe the bed until it stops moving")
	return s
}

func init() { host.RegisterModule("printdeck_settle", load, nil) }

// Status is the readings and the last estimate.
func (s *Settle) Status(eventtime float64) map[string]any {
	rs := make([]any, len(s.readings))
	for i, r := range s.readings {
		rs[i] = []any{r[0], r[1]}
	}
	return map[string]any{"readings": rs, "settled": s.settled, "rate": s.rate, "remaining": s.remaining}
}

// Remaining is the rate of the readings of the last window seconds (the slope of a line through
// them, in mm/s), and the movement that is still to come: the rate times tau. ok is false when
// the readings do not fill the window: at least 3 readings over 90 % of it.
func Remaining(readings [][2]float64, window, tau float64) (rate, remaining float64, ok bool) {
	if len(readings) < 3 {
		return 0, 0, false
	}
	last := readings[len(readings)-1][0]
	var pts [][2]float64
	for _, r := range readings {
		if r[0] >= last-window-1e-9 {
			pts = append(pts, r)
		}
	}
	if len(pts) < 3 || last-pts[0][0] < 0.9*window {
		return 0, 0, false
	}
	var mt, mz float64
	for _, p := range pts {
		mt += p[0] / float64(len(pts))
		mz += p[1] / float64(len(pts))
	}
	var stz, stt float64
	for _, p := range pts {
		stz += (p[0] - mt) * (p[1] - mz)
		stt += (p[0] - mt) * (p[0] - mt)
	}
	rate = stz / stt
	return rate, rate * tau, true
}

// probe probes the place once, and adds the reading.
func (s *Settle) probe(gcmd *gcode.Command) float64 {
	pr, ok := s.printer.TryLookupObject("probe").(probe.Probe)
	if !ok {
		panic(gcode.Errorf("BED_SETTLE needs a probe"))
	}
	off := pr.Offsets(nil)
	th := toolhead.Of(s.printer)
	nan := math.NaN()
	// Each probe starts 3 mm over the trigger height. After G28 the nozzle is at the top of Z (the
	// TAZ homes Z up), and a probe from there drops 290 mm at the probe speed: about 100 s.
	top := off[2] + above
	th.ManualMove([]float64{nan, nan, top}, s.lift)
	th.ManualMove([]float64{s.x - off[0], s.y - off[1], nan}, s.travelSpeed)
	session := pr.StartProbeSession(gcmd)
	var z float64
	func() {
		defer session.EndProbeSession()
		session.RunProbe(gcmd)
		rs := session.PullProbedResults()
		for _, r := range rs {
			z += r.BedZ() / float64(len(rs))
		}
	}()
	th.ManualMove([]float64{nan, nan, top}, s.lift)
	now := s.printer.Reactor().Monotonic()
	if len(s.readings) == 0 {
		s.t0 = now
	}
	s.readings = append(s.readings, [2]float64{now - s.t0, z})
	s.rate, s.remaining, s.settled = 0, 0, false
	if rate, rem, ok := Remaining(s.readings, s.window, s.tau); ok {
		s.rate, s.remaining = rate, rem
		s.settled = math.Abs(rem) <= s.tolerance
	}
	return z
}

func (s *Settle) cmdSettle(gcmd *gcode.Command) {
	if gcmd.GetInt("SAMPLE", gcode.Default(0)) != 0 {
		if gcmd.GetInt("RESET", gcode.Default(0)) != 0 {
			s.readings = nil
		}
		z := s.probe(gcmd)
		gcmd.RespondInfo(fmt.Sprintf("Bed at %.0f s: z %.4f", s.readings[len(s.readings)-1][0], z), true)
		return
	}
	s.readings = nil
	th := toolhead.Of(s.printer)
	for {
		z := s.probe(gcmd)
		t := s.readings[len(s.readings)-1][0]
		if s.rate != 0 || s.settled {
			gcmd.RespondInfo(fmt.Sprintf("Bed at %.0f s: z %.4f, %+.4f mm/min, %+.3f mm to come", t, z, s.rate*60, s.remaining), true)
		} else {
			gcmd.RespondInfo(fmt.Sprintf("Bed at %.0f s: z %.4f", t, z), true)
		}
		if s.settled {
			gcmd.RespondInfo(fmt.Sprintf("The bed is settled after %.0f s", t), true)
			return
		}
		if t+s.interval > s.timeout {
			gcmd.RespondInfo(fmt.Sprintf("The bed did not settle in %.0f s: %+.3f mm to come", t, s.remaining), true)
			return
		}
		th.Dwell(s.interval)
		th.WaitMoves()
	}
}
