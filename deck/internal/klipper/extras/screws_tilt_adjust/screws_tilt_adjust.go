// Package screws_tilt_adjust probes the bed at each bed screw, and reports the turns of each
// screw that level the bed.
//
// Ported to Go from klippy/extras/screws_tilt_adjust.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2019  Rui Caridade <rui.mcbc@gmail.com>
//	Copyright (C) 2021  Matthew Lloyd <github@matthewlloyd.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package screws_tilt_adjust

import (
	"fmt"
	"math"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/manual_probe"
	"printdeck/internal/klipper/extras/probe"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/jinja"
	"printdeck/internal/klipper/mathutil"
	"printdeck/internal/klipper/pyfmt"
)

type screw struct {
	coord []float64
	name  string
}

// ScrewsTiltAdjust is the [screws_tilt_adjust] section.
type ScrewsTiltAdjust struct {
	printer      *host.Printer
	screws       []screw
	results      *jinja.Dict
	maxDiff      *float64
	maxDiffError bool
	thread       int
	direction    string // "" for none
	probeHelper  *probe.PointsHelper
	gcode        *gcode.Dispatch
}

func load(config *configfile.Wrapper) any {
	s := &ScrewsTiltAdjust{printer: host.Of(config), results: jinja.NewDict()}
	// Read config
	for i := range 99 {
		prefix := fmt.Sprintf("screw%d", i+1)
		if _, ok := config.GetOptional(prefix, configfile.Default(nil)); !ok {
			break
		}
		screwCoord := config.GetFloatList(prefix, ",", 2)
		screwName := fmt.Sprintf("screw at %.3f,%.3f", screwCoord[0], screwCoord[1])
		screwName = config.Get(prefix+"_name", configfile.Default(screwName))
		s.screws = append(s.screws, screw{screwCoord, screwName})
	}
	if len(s.screws) < 3 {
		configfile.Raise("screws_tilt_adjust: Must have at least three screws")
	}
	threads := map[string]any{"CW-M3": 0, "CCW-M3": 1, "CW-M4": 2, "CCW-M4": 3,
		"CW-M5": 4, "CCW-M5": 5, "CW-M6": 6, "CCW-M6": 7}
	s.thread = config.GetChoice("screw_thread", threads, configfile.Default("CW-M3")).(int)
	// Initialize ProbePointsHelper
	var points [][]float64
	for _, sc := range s.screws {
		points = append(points, sc.coord)
	}
	s.probeHelper = probe.NewPointsHelper(config, func(results []*manual_probe.ProbeResult) string {
		s.probeFinalize(results)
		return ""
	}, points)
	s.probeHelper.MinimumPoints(3)
	// Register command
	s.gcode = gcode.Of(s.printer)
	s.gcode.RegisterCommand("SCREWS_TILT_CALCULATE", s.cmdScrewsTiltCalculate, false,
		"Tool to help adjust bed leveling screws by calculating the number of turns to level it.")
	return s
}

func init() { host.RegisterModule("screws_tilt_adjust", load, nil) }

func (s *ScrewsTiltAdjust) cmdScrewsTiltCalculate(gcmd *gcode.Command) {
	s.maxDiff = nil
	if v, ok := gcmd.GetFloatOptional("MAX_DEVIATION"); ok {
		s.maxDiff = &v
	}
	// Option to force all turns to be in the given direction (CW or CCW)
	direction, ok := gcmd.Get("DIRECTION", gcode.Default(nil))
	if ok {
		direction = strings.ToUpper(direction)
		if direction != "CW" && direction != "CCW" {
			panic(gcmd.Error("Error on '%s': DIRECTION must be either CW or CCW", gcmd.Commandline()))
		}
	}
	s.direction = direction
	s.probeHelper.StartProbe(gcmd)
}

func (s *ScrewsTiltAdjust) Status(eventtime float64) map[string]any {
	var maxDiff any
	if s.maxDiff != nil {
		maxDiff = *s.maxDiff
	}
	return map[string]any{"error": s.maxDiffError, "max_deviation": maxDiff, "results": s.results}
}

// threadsFactor are the factors of CW-M3, CCW-M3, CW-M4, CCW-M4, CW-M5, CCW-M5, CW-M6 and CCW-M6.
var threadsFactor = map[int]float64{0: 0.5, 1: 0.5, 2: 0.7, 3: 0.7, 4: 0.8, 5: 0.8, 6: 1.0, 7: 1.0}

// screwAdjust returns the turn of a screw for a height difference: the direction, the full turns
// and the minutes.
func screwAdjust(diff float64, thread int) (string, int64, int64) {
	isClockwiseThread := (thread & 1) == 0
	adjust := 0.
	if math.Abs(diff) >= 0.001 {
		factor, ok := threadsFactor[thread]
		if !ok {
			factor = 0.5
		}
		adjust = diff / factor
	}
	var sign string
	if isClockwiseThread {
		sign = "CCW"
		if adjust >= 0 {
			sign = "CW"
		}
	} else {
		sign = "CW"
		if adjust >= 0 {
			sign = "CCW"
		}
	}
	adjust = math.Abs(adjust)
	fullTurns := math.Trunc(adjust)
	decimalPart := adjust - fullTurns
	minutes := mathutil.Round(decimalPart*60, 0)
	return sign, int64(fullTurns), int64(minutes)
}

func (s *ScrewsTiltAdjust) probeFinalize(positions []*manual_probe.ProbeResult) {
	s.results = jinja.NewDict()
	s.maxDiffError = false
	isClockwiseThread := (s.thread & 1) == 0
	var screwDiff []float64
	// Process the read Z values
	var iBase int
	var zBase float64
	if s.direction != "" {
		// Lowest or highest screw is the base position used for comparison
		useMax := (isClockwiseThread && s.direction == "CW") ||
			(!isClockwiseThread && s.direction == "CCW")
		iBase, zBase = 0, positions[0].BedZ()
		for i, pos := range positions {
			z := pos.BedZ()
			if (useMax && z > zBase) || (!useMax && z < zBase) {
				iBase, zBase = i, z
			}
		}
	} else {
		// First screw is the base position used for comparison
		iBase, zBase = 0, positions[0].BedZ()
	}
	// Provide the user some information on how to read the results
	s.gcode.RespondInfo("01:20 means 1 full turn and 20 minutes, "+
		"CW=clockwise, CCW=counter-clockwise", true)
	for i, sc := range s.screws {
		z := positions[i].BedZ()
		coord, name := sc.coord, sc.name
		result := jinja.NewDict()
		if i == iBase {
			// Show the results
			s.gcode.RespondInfo(fmt.Sprintf("%s : x=%.1f, y=%.1f, z=%.5f",
				name+" (base)", coord[0], coord[1], z), true)
			sign := "CCW"
			if isClockwiseThread {
				sign = "CW"
			}
			result.Set("z", z)
			result.Set("sign", sign)
			result.Set("adjust", "00:00")
			result.Set("is_base", true)
		} else {
			// Calculate how knob must be adjusted for other positions
			diff := zBase - z
			screwDiff = append(screwDiff, math.Abs(diff))
			sign, fullTurns, minutes := screwAdjust(diff, s.thread)
			// Show the results
			s.gcode.RespondInfo(fmt.Sprintf("%s : x=%.1f, y=%.1f, z=%.5f : adjust %s %02d:%02d",
				name, coord[0], coord[1], z, sign, fullTurns, minutes), true)
			result.Set("z", z)
			result.Set("sign", sign)
			result.Set("adjust", fmt.Sprintf("%02d:%02d", fullTurns, minutes))
			result.Set("is_base", false)
		}
		s.results.Set(fmt.Sprintf("screw%d", i+1), result)
	}
	if s.maxDiff != nil && *s.maxDiff != 0 {
		for _, d := range screwDiff {
			if d > *s.maxDiff {
				s.maxDiffError = true
				panic(gcode.Errorf("bed level exceeds configured limits (%smm)! "+
					"Adjust screws and restart print.", pyfmt.Float(*s.maxDiff)))
			}
		}
	}
}
