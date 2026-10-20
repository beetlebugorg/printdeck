// Package extruder is the printer's extruder: its hotend heater, its move queue, its stepper with
// pressure advance, and its commands (M104, M109, SET_PRESSURE_ADVANCE, ...).
//
// Ported to Go from klippy/kinematics/extruder.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
// DummyExtruder is in the toolhead package.
//
//	Copyright (C) 2016-2025  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package extruder

import (
	"fmt"
	"log"
	"math"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/heaters"
	"printdeck/internal/klipper/extras/motion_queuing"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/motion"
	"printdeck/internal/klipper/stepper"
	"printdeck/internal/klipper/toolhead"
)

// ExtruderStepper is the stepper of an extruder, with pressure advance.
type ExtruderStepper struct {
	printer                   *host.Printer
	name                      string
	pressureAdvance           float64
	pressureAdvanceSmoothTime float64
	configPA                  float64
	configSmoothTime          float64
	Stepper                   *stepper.MCUStepper
	skExtruder                *motion.StepperKinematics
	motionQueue               string
}

// NewExtruderStepper makes the stepper of a config section.
func NewExtruderStepper(config *configfile.Wrapper) *ExtruderStepper {
	f := strings.Fields(config.Name())
	es := &ExtruderStepper{printer: host.Of(config), name: f[len(f)-1]}
	es.configPA = config.GetFloat("pressure_advance", configfile.Default(0.), configfile.Min(0.))
	es.configSmoothTime = config.GetFloat("pressure_advance_smooth_time", configfile.Default(0.040),
		configfile.Above(0.), configfile.Max(.200))
	// Setup stepper
	es.Stepper = stepper.NewPrinterStepper(config, false)
	es.skExtruder = motion.NewExtruderStepper()
	es.Stepper.SetStepperKinematics(es.skExtruder)
	// Register commands
	es.printer.RegisterEventHandler("klippy:connect", func(...any) {
		es.setPressureAdvance(es.configPA, es.configSmoothTime)
	})
	g := gcode.Of(es.printer)
	if es.name == "extruder" {
		g.RegisterMuxCommand("SET_PRESSURE_ADVANCE", "EXTRUDER", nil, es.cmdDefaultSetPressureAdvance,
			"Set pressure advance parameters")
	}
	g.RegisterMuxCommand("SET_PRESSURE_ADVANCE", "EXTRUDER", &es.name, es.cmdSetPressureAdvance,
		"Set pressure advance parameters")
	g.RegisterMuxCommand("SET_EXTRUDER_ROTATION_DISTANCE", "EXTRUDER", &es.name, es.cmdSetERotationDistance,
		"Set extruder rotation distance")
	g.RegisterMuxCommand("SYNC_EXTRUDER_MOTION", "EXTRUDER", &es.name, es.cmdSyncExtruderMotion,
		"Set extruder stepper motion queue")
	return es
}

// Status returns the pressure advance settings and the motion queue.
func (es *ExtruderStepper) Status(eventtime float64) map[string]any {
	var mq any
	if es.motionQueue != "" {
		mq = es.motionQueue
	}
	return map[string]any{"pressure_advance": es.pressureAdvance, "smooth_time": es.pressureAdvanceSmoothTime,
		"motion_queue": mq}
}

// FindPastPosition returns the extruder position at a past print time.
func (es *ExtruderStepper) FindPastPosition(printTime float64) float64 {
	return es.Stepper.MCUToCommandedPosition(es.Stepper.PastMCUPosition(printTime))
}

// SyncToExtruder moves the stepper with the move queue of an extruder. An empty name stops it.
func (es *ExtruderStepper) SyncToExtruder(extruderName string) {
	toolhead.Of(es.printer).FlushStepGeneration()
	mq := es.printer.LookupObject("motion_queuing").(*motion_queuing.PrinterMotionQueuing)
	if extruderName == "" {
		es.Stepper.SetTrapQ(nil)
		es.motionQueue = ""
		mq.CheckStepGenerationScanWindows()
		return
	}
	extruder, ok := es.printer.TryLookupObject(extruderName).(*PrinterExtruder)
	if !ok {
		panic(gcode.Errorf("'%s' is not a valid extruder.", extruderName))
	}
	es.Stepper.SetPosition([]float64{extruder.lastPosition, 0., 0.})
	es.Stepper.SetTrapQ(extruder.trapq)
	es.motionQueue = extruderName
	mq.CheckStepGenerationScanWindows()
}

func (es *ExtruderStepper) setPressureAdvance(pressureAdvance, smoothTime float64) {
	oldSmoothTime := es.pressureAdvanceSmoothTime
	if es.pressureAdvance == 0 {
		oldSmoothTime = 0.
	}
	newSmoothTime := smoothTime
	if pressureAdvance == 0 {
		newSmoothTime = 0.
	}
	th := toolhead.Of(es.printer)
	if newSmoothTime != oldSmoothTime {
		// Need full kinematic flush to change the smooth time
		th.FlushStepGeneration()
		motion.ExtruderSetPressureAdvance(es.skExtruder, 0., pressureAdvance, newSmoothTime)
		mq := es.printer.LookupObject("motion_queuing").(*motion_queuing.PrinterMotionQueuing)
		mq.CheckStepGenerationScanWindows()
	} else {
		th.RegisterLookaheadCallback(func(printTime float64) {
			motion.ExtruderSetPressureAdvance(es.skExtruder, printTime, pressureAdvance, newSmoothTime)
		})
	}
	es.pressureAdvance = pressureAdvance
	es.pressureAdvanceSmoothTime = smoothTime
}

func (es *ExtruderStepper) cmdDefaultSetPressureAdvance(gcmd *gcode.Command) {
	extruder, ok := toolhead.Of(es.printer).Extruder().(*PrinterExtruder)
	if !ok || extruder.ExtruderStepper == nil {
		panic(gcmd.Error("Active extruder does not have a stepper"))
	}
	if extruder.ExtruderStepper.Stepper.TrapQ() != extruder.trapq {
		panic(gcmd.Error("Unable to infer active extruder stepper"))
	}
	extruder.ExtruderStepper.cmdSetPressureAdvance(gcmd)
}

func (es *ExtruderStepper) cmdSetPressureAdvance(gcmd *gcode.Command) {
	pressureAdvance := gcmd.GetFloat("ADVANCE", gcode.Default(es.pressureAdvance), gcode.Min(0.))
	smoothTime := gcmd.GetFloat("SMOOTH_TIME", gcode.Default(es.pressureAdvanceSmoothTime), gcode.Min(0.),
		gcode.Max(.200))
	es.setPressureAdvance(pressureAdvance, smoothTime)
	msg := fmt.Sprintf("pressure_advance: %.6f\npressure_advance_smooth_time: %.6f", pressureAdvance, smoothTime)
	gcmd.RespondInfo(msg, false)
}

func (es *ExtruderStepper) cmdSetERotationDistance(gcmd *gcode.Command) {
	var rotationDist float64
	if rd, ok := gcmd.GetFloatOptional("DISTANCE"); ok {
		rotationDist = rd
		if rotationDist == 0 {
			panic(gcmd.Error("Rotation distance can not be zero"))
		}
		_, origInvertDir := es.Stepper.DirInverted()
		nextInvertDir := origInvertDir
		if rotationDist < 0. {
			nextInvertDir = !origInvertDir
			rotationDist = -rotationDist
		}
		toolhead.Of(es.printer).FlushStepGeneration()
		es.Stepper.SetRotationDistance(rotationDist)
		es.Stepper.SetDirInverted(nextInvertDir)
	} else {
		rotationDist, _ = es.Stepper.RotationDistance()
	}
	invertDir, origInvertDir := es.Stepper.DirInverted()
	if invertDir != origInvertDir {
		rotationDist = -rotationDist
	}
	gcmd.RespondInfo(fmt.Sprintf("Extruder '%s' rotation distance set to %0.6f", es.name, rotationDist), true)
}

func (es *ExtruderStepper) cmdSyncExtruderMotion(gcmd *gcode.Command) {
	ename := gcmd.GetString("MOTION_QUEUE")
	es.SyncToExtruder(ename)
	gcmd.RespondInfo(fmt.Sprintf("Extruder '%s' now syncing with '%s'", es.name, ename), true)
}

// PrinterExtruder tracks the hotend heater, the extrusion motion queue, and the extruder stepper.
type PrinterExtruder struct {
	printer         *host.Printer
	name            string
	lastPosition    float64
	heater          *heaters.Heater
	nozzleDiameter  float64
	filamentArea    float64
	maxExtrudeRatio float64
	maxEVelocity    float64
	maxEAccel       float64
	maxEDist        float64
	instantCornerV  float64
	motionQueuing   *motion_queuing.PrinterMotionQueuing
	trapq           *motion.TrapQ
	ExtruderStepper *ExtruderStepper
}

func newPrinterExtruder(config *configfile.Wrapper, extruderNum int) *PrinterExtruder {
	printer := host.Of(config)
	e := &PrinterExtruder{printer: printer, name: config.Name()}
	// Setup hotend heater
	pheaters := heaters.Load(config)
	e.heater = pheaters.SetupHeater(config, fmt.Sprintf("T%d", extruderNum))
	// Setup kinematic checks
	e.nozzleDiameter = config.GetFloat("nozzle_diameter", configfile.Above(0.))
	filamentDiameter := config.GetFloat("filament_diameter", configfile.Min(e.nozzleDiameter))
	e.filamentArea = math.Pi * math.Pow(filamentDiameter*.5, 2)
	defMaxCrossSection := 4. * e.nozzleDiameter * e.nozzleDiameter
	defMaxExtrudeRatio := defMaxCrossSection / e.filamentArea
	maxCrossSection := config.GetFloat("max_extrude_cross_section", configfile.Default(defMaxCrossSection),
		configfile.Above(0.))
	e.maxExtrudeRatio = maxCrossSection / e.filamentArea
	log.Printf("Extruder max_extrude_ratio=%.6f", e.maxExtrudeRatio)
	th := toolhead.Of(printer)
	maxVelocity, maxAccel := th.MaxVelocity()
	e.maxEVelocity = config.GetFloat("max_extrude_only_velocity", configfile.Default(maxVelocity*defMaxExtrudeRatio),
		configfile.Above(0.))
	e.maxEAccel = config.GetFloat("max_extrude_only_accel", configfile.Default(maxAccel*defMaxExtrudeRatio),
		configfile.Above(0.))
	e.maxEDist = config.GetFloat("max_extrude_only_distance", configfile.Default(50.), configfile.Min(0.))
	e.instantCornerV = config.GetFloat("instantaneous_corner_velocity", configfile.Default(1.), configfile.Min(0.))
	// Setup extruder trapq (trapezoidal motion queue)
	e.motionQueuing = motion_queuing.Load(config)
	e.trapq = e.motionQueuing.AllocateTrapQ()
	// Setup extruder stepper
	_, hasStep := config.GetOptional("step_pin", configfile.Default(nil))
	_, hasDir := config.GetOptional("dir_pin", configfile.Default(nil))
	_, hasRD := config.GetOptional("rotation_distance", configfile.Default(nil))
	if hasStep || hasDir || hasRD {
		e.ExtruderStepper = NewExtruderStepper(config)
		e.ExtruderStepper.Stepper.SetTrapQ(e.trapq)
	}
	// Register commands
	g := gcode.Of(printer)
	if e.name == "extruder" {
		th.SetExtruder(e, 0.)
		g.RegisterCommand("M104", func(gcmd *gcode.Command) { e.cmdM104(gcmd, false) }, false, "")
		g.RegisterCommand("M109", func(gcmd *gcode.Command) { e.cmdM104(gcmd, true) }, false, "")
	}
	g.RegisterMuxCommand("ACTIVATE_EXTRUDER", "EXTRUDER", &e.name, e.cmdActivateExtruder,
		"Change the active extruder")
	return e
}

func init() {
	toolhead.ExtruderObjects = func(config *configfile.Wrapper) {
		printer := host.Of(config)
		for i := 0; i < 99; i++ {
			section := "extruder"
			if i != 0 {
				section = fmt.Sprintf("extruder%d", i)
			}
			if !config.HasSection(section) {
				break
			}
			printer.AddObject(section, newPrinterExtruder(config.GetSection(section), i))
		}
	}
}

// Status returns the heater status, the extrude check, and the stepper settings.
func (e *PrinterExtruder) Status(eventtime float64) map[string]any {
	sts := e.heater.Status(eventtime)
	sts["can_extrude"] = e.heater.CanExtrude()
	if e.ExtruderStepper != nil {
		for k, v := range e.ExtruderStepper.Status(eventtime) {
			sts[k] = v
		}
	}
	return sts
}

func (e *PrinterExtruder) Name() string            { return e.name }
func (e *PrinterExtruder) Heater() *heaters.Heater { return e.heater }
func (e *PrinterExtruder) TrapQ() *motion.TrapQ    { return e.trapq }
func (e *PrinterExtruder) AxisGcodeID() string     { return "E" }
func (e *PrinterExtruder) LastPosition() float64   { return e.lastPosition }

// Stats returns the statistics line of the heater.
func (e *PrinterExtruder) Stats(eventtime float64) (bool, string) { return e.heater.Stats(eventtime) }

// CheckMove checks the extrusion of a move, and limits the speed of an extrude only move.
func (e *PrinterExtruder) CheckMove(move *toolhead.Move, eaIndex int) {
	if !e.heater.CanExtrude() {
		panic(gcode.Errorf("Extrude below minimum temp\nSee the 'min_extrude_temp' config option for details"))
	}
	axisR := move.AxesR[eaIndex]
	axisD := move.AxesD[eaIndex]
	if (move.AxesD[0] == 0 && move.AxesD[1] == 0) || axisR < 0. {
		// Extrude only move (or retraction move) - limit accel and velocity
		if math.Abs(axisD) > e.maxEDist {
			panic(gcode.Errorf("Extrude only move too long (%.3fmm vs %.3fmm)\n"+
				"See the 'max_extrude_only_distance' config option for details", axisD, e.maxEDist))
		}
		invExtrudeR := 1. / math.Abs(axisR)
		move.LimitSpeed(e.maxEVelocity*invExtrudeR, e.maxEAccel*invExtrudeR)
	} else if axisR > e.maxExtrudeRatio {
		if axisD <= e.nozzleDiameter*e.maxExtrudeRatio {
			// Permit extrusion if amount extruded is tiny
			return
		}
		area := axisR * e.filamentArea
		panic(gcode.Errorf("Move exceeds maximum extrusion (%.3fmm^2 vs %.3fmm^2)\n"+
			"See the 'max_extrude_cross_section' config option for details", area, e.maxExtrudeRatio*e.filamentArea))
	}
}

// CalcJunction returns the maximum junction speed squared of the extrusion.
func (e *PrinterExtruder) CalcJunction(prevMove, move *toolhead.Move, eaIndex int) float64 {
	diffR := move.AxesR[eaIndex] - prevMove.AxesR[eaIndex]
	if diffR != 0 {
		return math.Pow(e.instantCornerV/math.Abs(diffR), 2)
	}
	return move.MaxCruiseV2
}

// ProcessMove queues the extrusion of a move.
func (e *PrinterExtruder) ProcessMove(printTime float64, move *toolhead.Move, eaIndex int) {
	axisR := move.AxesR[eaIndex]
	accel := move.Accel * axisR
	startV := move.StartV * axisR
	cruiseV := move.CruiseV * axisR
	canPressureAdvance := 0.
	if axisR > 0. && (move.AxesD[0] != 0 || move.AxesD[1] != 0) {
		canPressureAdvance = 1.
	}
	// Queue movement (x is extruder movement, y is pressure advance flag)
	e.trapq.Append(printTime, move.AccelT, move.CruiseT, move.DecelT, move.StartPos[eaIndex], 0., 0., 1.,
		canPressureAdvance, 0., startV, cruiseV, accel)
	e.lastPosition = move.EndPos[eaIndex]
}

// FindPastPosition returns the extruder position at a past print time.
func (e *PrinterExtruder) FindPastPosition(printTime float64) float64 {
	if e.ExtruderStepper == nil {
		return 0.
	}
	return e.ExtruderStepper.FindPastPosition(printTime)
}

type heaterOwner interface{ Heater() *heaters.Heater }

func (e *PrinterExtruder) cmdM104(gcmd *gcode.Command, wait bool) {
	// Set Extruder Temperature
	temp := gcmd.GetFloat("S", gcode.Default(0.))
	var extruder any
	if _, ok := gcmd.Params()["T"]; ok {
		index := gcmd.GetInt("T", gcode.Min(0))
		section := "extruder"
		if index != 0 {
			section = fmt.Sprintf("extruder%d", index)
		}
		extruder = e.printer.TryLookupObject(section)
		if extruder == nil {
			if temp <= 0. {
				return
			}
			panic(gcmd.Error("Extruder not configured"))
		}
	} else {
		extruder = toolhead.Of(e.printer).Extruder()
	}
	h, ok := extruder.(heaterOwner)
	if !ok {
		panic(gcode.Errorf("Extruder not configured"))
	}
	heaters.Of(e.printer).SetTemperature(h.Heater(), temp, wait)
}

func (e *PrinterExtruder) cmdActivateExtruder(gcmd *gcode.Command) {
	th := toolhead.Of(e.printer)
	if th.Extruder() == toolhead.ExtraAxis(e) {
		gcmd.RespondInfo(fmt.Sprintf("Extruder %s already active", e.name), true)
		return
	}
	gcmd.RespondInfo(fmt.Sprintf("Activating extruder %s", e.name), true)
	th.FlushStepGeneration()
	th.SetExtruder(e, e.lastPosition)
	e.printer.SendEvent("extruder:activate_extruder")
}
