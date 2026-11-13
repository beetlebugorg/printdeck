// Package z_tilt levels the gantry with several Z steppers: Z_TILT_ADJUST probes the bed, fits a
// plane, and moves each Z stepper by the height of the plane at its position.
//
// Ported to Go from klippy/extras/z_tilt.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018-2019  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package z_tilt

import (
	"fmt"
	"log"
	"math"
	"sort"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/manual_probe"
	"printdeck/internal/klipper/extras/probe"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mathutil"
	"printdeck/internal/klipper/stepper"
	"printdeck/internal/klipper/toolhead"
)

// AdjustHelper moves each Z stepper by its own amount (ZAdjustHelper).
type AdjustHelper struct {
	printer   *host.Printer
	name      string
	zCount    int
	zSteppers []*stepper.MCUStepper
}

// NewAdjustHelper checks at connect that the kinematics have zCount Z steppers.
func NewAdjustHelper(config *configfile.Wrapper, zCount int) *AdjustHelper {
	h := &AdjustHelper{printer: host.Of(config), name: config.Name(), zCount: zCount}
	h.printer.RegisterEventHandler("klippy:connect", func(...any) { h.handleConnect() })
	return h
}

func (h *AdjustHelper) handleConnect() {
	kin := toolhead.Of(h.printer).Kin()
	var zs []*stepper.MCUStepper
	for _, s := range kin.Steppers() {
		if st, ok := s.(*stepper.MCUStepper); ok && st.IsActiveAxis("z") {
			zs = append(zs, st)
		}
	}
	if len(zs) != h.zCount {
		configfile.Raise("%s z_positions needs exactly %d items", h.name, len(zs))
	}
	if len(zs) < 2 {
		configfile.Raise("%s requires multiple z steppers", h.name)
	}
	h.zSteppers = zs
}

// AdjustSteppers moves each Z stepper by minus its adjustment, from the lowest to the highest.
func (h *AdjustHelper) AdjustSteppers(adjustments []float64, speed float64) {
	th := toolhead.Of(h.printer)
	g := gcode.Of(h.printer)
	curpos := th.Position()
	// Report on movements
	var stepstrs []string
	for i, s := range h.zSteppers {
		stepstrs = append(stepstrs, fmt.Sprintf("%s = %.6f", s.Name(), adjustments[i]))
	}
	g.RespondInfo("Making the following Z adjustments:\n"+strings.Join(stepstrs, "\n"), true)
	// Disable Z stepper movements
	th.FlushStepGeneration()
	for _, s := range h.zSteppers {
		s.SetTrapQ(nil)
	}
	// Move each z stepper (sorted from lowest to highest) until they match
	type pos struct {
		offset float64
		s      *stepper.MCUStepper
	}
	positions := make([]pos, len(h.zSteppers))
	for i, s := range h.zSteppers {
		positions[i] = pos{-adjustments[i], s}
	}
	sort.SliceStable(positions, func(i, j int) bool { return positions[i].offset < positions[j].offset })
	firstOffset := positions[0].offset
	zLow := curpos[2] - firstOffset
	for i := 0; i < len(positions)-1; i++ {
		s, next := positions[i].s, positions[i+1].offset
		th.FlushStepGeneration()
		s.SetTrapQ(th.TrapQ())
		curpos[2] = zLow + next
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("ZAdjustHelper adjust_steppers: %v", r)
					th.FlushStepGeneration()
					for _, s := range h.zSteppers {
						s.SetTrapQ(th.TrapQ())
					}
					panic(r)
				}
			}()
			th.Move(curpos, speed)
			th.SetPosition(curpos, "")
		}()
	}
	// Z should now be level - do final cleanup
	last := positions[len(positions)-1].s
	th.FlushStepGeneration()
	last.SetTrapQ(th.TrapQ())
	curpos[2] += firstOffset
	th.SetPosition(curpos, "")
}

// AdjustStatus is "applied": true after a successful adjust, until the motors turn off.
type AdjustStatus struct{ applied bool }

// NewAdjustStatus resets the status when the motors turn off.
func NewAdjustStatus(printer *host.Printer) *AdjustStatus {
	st := &AdjustStatus{}
	printer.RegisterEventHandler("stepper_enable:motor_off", func(...any) { st.Reset() })
	return st
}

// CheckRetryResult notes a "done" result.
func (st *AdjustStatus) CheckRetryResult(result string) string {
	if result == "done" {
		st.applied = true
	}
	return result
}

// Reset clears the status.
func (st *AdjustStatus) Reset() { st.applied = false }

// Status is the status of the object.
func (st *AdjustStatus) Status(eventtime float64) map[string]any {
	return map[string]any{"applied": st.applied}
}

// RetryHelper repeats the probe until the range of the points is within the tolerance.
type RetryHelper struct {
	gcode                 *gcode.Dispatch
	defaultMaxRetries     int
	defaultRetryTolerance float64
	valueLabel            string
	errorMsgExtra         string
	maxRetries            int
	retryTolerance        float64
	currentRetry          int
	previous              *float64
	increasing            int
}

// NewRetryHelper reads retries and retry_tolerance.
func NewRetryHelper(config *configfile.Wrapper, errorMsgExtra string) *RetryHelper {
	return &RetryHelper{gcode: gcode.Of(host.Of(config)),
		defaultMaxRetries:     config.GetInt("retries", configfile.Default(0), configfile.Min(0)),
		defaultRetryTolerance: config.GetFloat("retry_tolerance", configfile.Default(0.), configfile.Above(0.)),
		valueLabel:            "Probed points range", errorMsgExtra: errorMsgExtra}
}

// Start starts a command.
func (r *RetryHelper) Start(gcmd *gcode.Command) {
	r.maxRetries = gcmd.GetInt("RETRIES", gcode.Default(r.defaultMaxRetries), gcode.Min(0), gcode.Max(30))
	r.retryTolerance = gcmd.GetFloat("RETRY_TOLERANCE", gcode.Default(r.defaultRetryTolerance),
		gcode.Min(0.0), gcode.Max(1.0))
	r.currentRetry = 0
	r.previous = nil
	r.increasing = 0
}

func (r *RetryHelper) checkIncrease(err float64) bool {
	if r.previous != nil && *r.previous != 0 && err > *r.previous+0.0000001 {
		r.increasing++
	} else if r.increasing > 0 {
		r.increasing--
	}
	r.previous = &err
	return r.increasing > 1
}

// CheckRetry returns "done" or "retry".
func (r *RetryHelper) CheckRetry(zPositions []float64) string {
	if r.maxRetries == 0 {
		return "done"
	}
	hi, lo := math.Inf(-1), math.Inf(1)
	for _, z := range zPositions {
		hi, lo = math.Max(hi, z), math.Min(lo, z)
	}
	err := mathutil.Round(hi-lo, 6)
	r.gcode.RespondInfo(fmt.Sprintf("Retries: %d/%d %s: %0.6f tolerance: %0.6f", r.currentRetry, r.maxRetries,
		r.valueLabel, err, r.retryTolerance), true)
	if r.checkIncrease(err) {
		panic(gcode.Errorf("Retries aborting: %s is increasing. %s", r.valueLabel, r.errorMsgExtra))
	}
	if err <= r.retryTolerance {
		return "done"
	}
	r.currentRetry++
	if r.currentRetry > r.maxRetries {
		panic(gcode.Errorf("Too many retries"))
	}
	return "retry"
}

// ZTilt is the [z_tilt] section.
type ZTilt struct {
	printer     *host.Printer
	zPositions  [][]float64
	retryHelper *RetryHelper
	probeHelper *probe.PointsHelper
	zStatus     *AdjustStatus
	zHelper     *AdjustHelper
}

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	z := &ZTilt{printer: printer}
	z.zPositions = config.GetFloatLists("z_positions", []string{",", "\n"}, 2)
	z.retryHelper = NewRetryHelper(config, "")
	z.probeHelper = probe.NewPointsHelper(config, z.probeFinalize, nil)
	z.probeHelper.MinimumPoints(2)
	z.zStatus = NewAdjustStatus(printer)
	z.zHelper = NewAdjustHelper(config, len(z.zPositions))
	gcode.Of(printer).RegisterCommand("Z_TILT_ADJUST", z.cmdZTiltAdjust, false, "Adjust the Z tilt")
	return z
}

func init() { host.RegisterModule("z_tilt", load, nil) }

func (z *ZTilt) cmdZTiltAdjust(gcmd *gcode.Command) {
	z.zStatus.Reset()
	z.retryHelper.Start(gcmd)
	z.probeHelper.StartProbe(gcmd)
}

func (z *ZTilt) probeFinalize(positions []*manual_probe.ProbeResult) string {
	log.Printf("Calculating bed tilt with: %v", positions)
	// Find best fit for: a + b*bed_x + c*bed_y = bed_z
	var eqs, ans [][]float64
	for _, p := range positions {
		eqs = append(eqs, []float64{1., p.BedX_, p.BedY_})
		ans = append(ans, []float64{p.BedZ_})
	}
	adjustments := Adjustments(eqs, ans, z.zPositions)
	speed := z.probeHelper.LiftSpeed()
	z.zHelper.AdjustSteppers(adjustments, speed)
	zs := make([]float64, len(positions))
	for i, p := range positions {
		zs[i] = p.BedZ_
	}
	return z.zStatus.CheckRetryResult(z.retryHelper.CheckRetry(zs))
}

// Status is "applied".
func (z *ZTilt) Status(eventtime float64) map[string]any { return z.zStatus.Status(eventtime) }

// Adjustments fits the plane z = a + b*x + c*y to the probe points (eqs, ans) and returns its height
// at each Z stepper position.
func Adjustments(eqs, ans, zPositions [][]float64) []float64 {
	res := mathutil.SolveLinearEquations(eqs, ans, true)
	zAdjust, xAdjust, yAdjust := res[0][0], res[1][0], res[2][0]
	// Apply results
	log.Printf("Calculated bed tilt parameters: x_adjust=%.6f y_adjust=%.6f z_adjust=%.6f",
		xAdjust, yAdjust, zAdjust)
	out := make([]float64, len(zPositions))
	for i, p := range zPositions {
		out[i] = p[0]*xAdjust + p[1]*yAdjust + zAdjust
	}
	return out
}
