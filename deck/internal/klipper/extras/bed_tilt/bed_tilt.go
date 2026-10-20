// Package bed_tilt compensates a tilted bed: a move transform that adds a plane to Z, and the
// BED_TILT_CALIBRATE command that probes the plane.
//
// Ported to Go from klippy/extras/bed_tilt.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package bed_tilt

import (
	"fmt"
	"log"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/gcode_move"
	"printdeck/internal/klipper/extras/manual_probe"
	"printdeck/internal/klipper/extras/probe"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mathutil"
	"printdeck/internal/klipper/toolhead"
)

// BedTilt is the move transform of the bed tilt.
type BedTilt struct {
	printer                   *host.Printer
	xAdjust, yAdjust, zAdjust float64
	toolhead                  *toolhead.ToolHead
}

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	bt := &BedTilt{printer: printer}
	printer.RegisterEventHandler("klippy:connect", func(...any) { bt.toolhead = toolhead.Of(printer) })
	bt.xAdjust = config.GetFloat("x_adjust", configfile.Default(0.))
	bt.yAdjust = config.GetFloat("y_adjust", configfile.Default(0.))
	bt.zAdjust = config.GetFloat("z_adjust", configfile.Default(0.))
	if _, ok := config.GetOptional("points", configfile.Default(nil)); ok {
		newCalibrate(config, bt)
	}
	// Register move transform with g-code class
	printer.LoadObject(config, "gcode_move").(*gcode_move.GCodeMove).SetMoveTransform(bt, false)
	return bt
}

func init() { host.RegisterModule("bed_tilt", load, nil) }

// Position returns the toolhead position without the tilt.
func (bt *BedTilt) Position() []float64 {
	pos := bt.toolhead.Position()
	pos[2] -= pos[0]*bt.xAdjust + pos[1]*bt.yAdjust + bt.zAdjust
	return pos
}

// Move moves the toolhead with the tilt added to Z.
func (bt *BedTilt) Move(newpos []float64, speed float64) {
	pos := append([]float64(nil), newpos...)
	pos[2] += pos[0]*bt.xAdjust + pos[1]*bt.yAdjust + bt.zAdjust
	bt.toolhead.Move(pos, speed)
}

// UpdateAdjust sets the tilt, and stages it for SAVE_CONFIG.
func (bt *BedTilt) UpdateAdjust(x, y, z float64) {
	bt.xAdjust, bt.yAdjust, bt.zAdjust = x, y, z
	bt.printer.LookupObject("gcode_move").(*gcode_move.GCodeMove).ResetLastPosition()
	pconfig := bt.printer.LookupObject("configfile").(*configfile.PrinterConfig)
	pconfig.Set("bed_tilt", "x_adjust", fmt.Sprintf("%.6f", x))
	pconfig.Set("bed_tilt", "y_adjust", fmt.Sprintf("%.6f", y))
	pconfig.Set("bed_tilt", "z_adjust", fmt.Sprintf("%.6f", z))
}

// calibrate is the BED_TILT_CALIBRATE command.
type calibrate struct {
	printer     *host.Printer
	bedtilt     *BedTilt
	probeHelper *probe.PointsHelper
	gcode       *gcode.Dispatch
}

func newCalibrate(config *configfile.Wrapper, bt *BedTilt) {
	c := &calibrate{printer: host.Of(config), bedtilt: bt}
	c.probeHelper = probe.NewPointsHelper(config, c.probeFinalize, nil)
	c.probeHelper.MinimumPoints(3)
	c.gcode = gcode.Of(c.printer)
	c.gcode.RegisterCommand("BED_TILT_CALIBRATE", func(gcmd *gcode.Command) { c.probeHelper.StartProbe(gcmd) },
		false, "Bed tilt calibration script")
}

func (c *calibrate) probeFinalize(positions []*manual_probe.ProbeResult) string {
	log.Printf("Calculating bed_tilt with: %v", positions)
	// Find best fit for: a + b*bed_x + c*bed_y = bed_z
	var eqs, ans [][]float64
	for _, p := range positions {
		eqs = append(eqs, []float64{1., p.BedX_, p.BedY_})
		ans = append(ans, []float64{p.BedZ_})
	}
	res := mathutil.SolveLinearEquations(eqs, ans, false)
	if res == nil {
		panic(gcode.Errorf("Unable to calculate tilt"))
	}
	zAdjust, xAdjust, yAdjust := res[0][0], res[1][0], res[2][0]
	// Update current bed_tilt calculations
	c.bedtilt.UpdateAdjust(xAdjust, yAdjust, zAdjust)
	// Log and report results
	msg := fmt.Sprintf("x_adjust: %.6f y_adjust: %.6f z_adjust: %.6f", xAdjust, yAdjust, zAdjust)
	c.gcode.RespondInfo(msg+"\nThe above parameters have been applied to the current\n"+
		"session. The SAVE_CONFIG command will update the printer\n"+
		"config file and restart the printer.", true)
	return ""
}
