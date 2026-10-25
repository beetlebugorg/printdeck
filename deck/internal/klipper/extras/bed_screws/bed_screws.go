// Package bed_screws moves the head to each bed screw, so the user can adjust the screws.
//
// Ported to Go from klippy/extras/bed_screws.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2019-2021  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package bed_screws

import (
	"fmt"
	"math"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/toolhead"
)

type screw struct {
	coord []float64
	name  string
}

// BedScrews is the [bed_screws] section.
type BedScrews struct {
	printer         *host.Printer
	state           string // "" for no state
	currentScrew    int
	acceptedScrews  int
	numberOfScrews  int
	states          map[string][]screw
	speed           float64
	liftSpeed       float64
	horizontalMoveZ float64
	probeZ          float64
	gcode           *gcode.Dispatch
}

func load(config *configfile.Wrapper) any {
	b := &BedScrews{printer: host.Of(config)}
	b.reset()
	b.numberOfScrews = 0
	// Read config
	var screws, fineAdjust []screw
	for i := range 99 {
		prefix := fmt.Sprintf("screw%d", i+1)
		if _, ok := config.GetOptional(prefix, configfile.Default(nil)); !ok {
			break
		}
		screwCoord := config.GetFloatList(prefix, ",", 2)
		screwName := fmt.Sprintf("screw at %.3f,%.3f", screwCoord[0], screwCoord[1])
		screwName = config.Get(prefix+"_name", configfile.Default(screwName))
		screws = append(screws, screw{screwCoord, screwName})
		pfa := prefix + "_fine_adjust"
		if _, ok := config.GetOptional(pfa, configfile.Default(nil)); ok {
			fineCoord := config.GetFloatList(pfa, ",", 2)
			fineAdjust = append(fineAdjust, screw{fineCoord, screwName})
		}
	}
	if len(screws) < 3 {
		configfile.Raise("bed_screws: Must have at least three screws")
	}
	b.numberOfScrews = len(screws)
	b.states = map[string][]screw{"adjust": screws, "fine": fineAdjust}
	b.speed = config.GetFloat("speed", configfile.Default(50.), configfile.Above(0.))
	b.liftSpeed = config.GetFloat("probe_speed", configfile.Default(5.), configfile.Above(0.))
	b.horizontalMoveZ = config.GetFloat("horizontal_move_z", configfile.Default(5.))
	b.probeZ = config.GetFloat("probe_height", configfile.Default(0.))
	// Register command
	b.gcode = gcode.Of(b.printer)
	b.gcode.RegisterCommand("BED_SCREWS_ADJUST", b.cmdBedScrewsAdjust, false,
		"Tool to help adjust bed leveling screws")
	return b
}

func init() { host.RegisterModule("bed_screws", load, nil) }

func (b *BedScrews) reset() {
	b.state = ""
	b.currentScrew = 0
	b.acceptedScrews = 0
}

func (b *BedScrews) move(coord []float64, speed float64) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(*gcode.CommandError); ok {
				b.unregisterCommands()
				b.reset()
			}
			panic(r)
		}
	}()
	toolhead.Of(b.printer).ManualMove(coord, speed)
}

func (b *BedScrews) moveToScrew(state string, i int) {
	// Move up, over, and then down
	none := math.NaN()
	b.move([]float64{none, none, b.horizontalMoveZ}, b.liftSpeed)
	s := b.states[state][i]
	b.move([]float64{s.coord[0], s.coord[1], b.horizontalMoveZ}, b.speed)
	b.move([]float64{s.coord[0], s.coord[1], b.probeZ}, b.liftSpeed)
	// Update state
	b.state = state
	b.currentScrew = i
	// Register commands
	b.gcode.RespondInfo(fmt.Sprintf("Adjust %s. Then run ACCEPT, ADJUSTED, or ABORT\n"+
		"Use ADJUSTED if a significant screw adjustment is made", s.name), true)
	b.gcode.RegisterCommand("ACCEPT", b.cmdAccept, false, "Accept bed screw position")
	b.gcode.RegisterCommand("ADJUSTED", b.cmdAdjusted, false,
		"Accept bed screw position after notable adjustment")
	b.gcode.RegisterCommand("ABORT", b.cmdAbort, false, "Abort bed screws tool")
}

func (b *BedScrews) unregisterCommands() {
	b.gcode.UnregisterCommand("ACCEPT")
	b.gcode.UnregisterCommand("ADJUSTED")
	b.gcode.UnregisterCommand("ABORT")
}

func (b *BedScrews) Status(eventtime float64) map[string]any {
	var state any
	if b.state != "" {
		state = b.state
	}
	return map[string]any{
		"is_active":       b.state != "",
		"state":           state,
		"current_screw":   b.currentScrew,
		"accepted_screws": b.acceptedScrews,
	}
}

func (b *BedScrews) cmdBedScrewsAdjust(gcmd *gcode.Command) {
	if b.state != "" {
		panic(gcmd.Error("Already in bed_screws helper; use ABORT to exit"))
	}
	// reset accepted screws
	b.acceptedScrews = 0
	none := math.NaN()
	b.move([]float64{none, none, b.horizontalMoveZ}, b.speed)
	b.moveToScrew("adjust", 0)
}

func (b *BedScrews) cmdAccept(gcmd *gcode.Command) {
	b.unregisterCommands()
	b.acceptedScrews = b.acceptedScrews + 1
	if b.currentScrew+1 < len(b.states[b.state]) && b.acceptedScrews < b.numberOfScrews {
		// Continue with next screw
		b.moveToScrew(b.state, b.currentScrew+1)
		return
	}
	if b.acceptedScrews < b.numberOfScrews {
		// Retry coarse adjustments
		b.moveToScrew("adjust", 0)
		return
	}
	if b.state == "adjust" && len(b.states["fine"]) > 0 {
		// Reset accepted screws for fine adjustment
		b.acceptedScrews = 0
		// Perform fine screw adjustments
		b.moveToScrew("fine", 0)
		return
	}
	// Done
	b.reset()
	none := math.NaN()
	b.move([]float64{none, none, b.horizontalMoveZ}, b.liftSpeed)
	gcmd.RespondInfo("Bed screws tool completed successfully", true)
}

func (b *BedScrews) cmdAdjusted(gcmd *gcode.Command) {
	b.unregisterCommands()
	b.acceptedScrews = -1
	b.cmdAccept(gcmd)
}

func (b *BedScrews) cmdAbort(gcmd *gcode.Command) {
	b.unregisterCommands()
	b.reset()
}
