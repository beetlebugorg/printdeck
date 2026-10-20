// Package stepper_enable switches the enable pins of the stepper drivers: on at the first move of
// a stepper, and off with M18/M84.
//
// Ported to Go from klippy/extras/stepper_enable.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2019-2025  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package stepper_enable

import (
	"log"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/pins"
	"printdeck/internal/klipper/stepper"
)

const disableStallTime = 0.100

// EnablePin is a stepper enable pin, shared or dedicated (StepperEnablePin).
type EnablePin struct {
	mcuEnable   *mcu.DigitalOut
	enableCount int
	isDedicated bool
}

// SetEnable turns the pin on, for one more stepper.
func (p *EnablePin) SetEnable(printTime float64) {
	if p.enableCount == 0 {
		p.mcuEnable.SetDigital(printTime, true)
	}
	p.enableCount++
}

// SetDisable turns the pin off when no stepper uses it.
func (p *EnablePin) SetDisable(printTime float64) {
	p.enableCount--
	if p.enableCount == 0 {
		p.mcuEnable.SetDigital(printTime, false)
	}
}

// SetupEnablePin returns the enable pin of a pin description. An empty pin is a stepper that is
// always on.
func SetupEnablePin(printer *host.Printer, pin string) *EnablePin {
	if pin == "" {
		// No enable line (stepper always enabled)
		return &EnablePin{enableCount: 9999}
	}
	ppins := printer.LookupObject("pins").(*pins.Pins)
	params := ppins.LookupPin(pin, true, false, "stepper_enable")
	if enable, ok := params.Class.(*EnablePin); ok {
		// Shared enable line
		enable.isDedicated = false
		return enable
	}
	mcuEnable := params.Chip.SetupPin("digital_out", params).(*mcu.DigitalOut)
	mcuEnable.SetupMaxDuration(0.)
	enable := &EnablePin{mcuEnable: mcuEnable, isDedicated: true}
	params.Class = enable
	return enable
}

// EnableTracking is the enable state of one stepper.
type EnableTracking struct {
	stepper   *stepper.MCUStepper
	enable    *EnablePin
	callbacks []func(printTime float64, enabled bool)
	isEnabled bool
}

func newEnableTracking(s *stepper.MCUStepper, enable *EnablePin) *EnableTracking {
	et := &EnableTracking{stepper: s, enable: enable}
	s.AddActiveCallback(et.MotorEnable)
	return et
}

// RegisterStateCallback adds a function that gets each change of the enable state.
func (et *EnableTracking) RegisterStateCallback(cb func(printTime float64, enabled bool)) {
	et.callbacks = append(et.callbacks, cb)
}

// MotorEnable turns the stepper on.
func (et *EnableTracking) MotorEnable(printTime float64) {
	if !et.isEnabled {
		for _, cb := range et.callbacks {
			cb(printTime, true)
		}
		et.enable.SetEnable(printTime)
		et.isEnabled = true
	}
}

// MotorDisable turns the stepper off, until its next move.
func (et *EnableTracking) MotorDisable(printTime float64) {
	if et.isEnabled {
		// Enable stepper on future stepper movement
		for _, cb := range et.callbacks {
			cb(printTime, false)
		}
		et.enable.SetDisable(printTime)
		et.isEnabled = false
		et.stepper.AddActiveCallback(et.MotorEnable)
	}
}

func (et *EnableTracking) IsMotorEnabled() bool     { return et.isEnabled }
func (et *EnableTracking) HasDedicatedEnable() bool { return et.enable.isDedicated }

// toolhead is the part of the toolhead that the enable changes use.
type toolhead interface {
	FlushStepGeneration()
	Dwell(delay float64)
	LastMoveTime() float64
	Kinematics() any
}

type homingStateClearer interface{ ClearHomingState(axes string) }

// PrinterStepperEnable holds the enable state of the steppers.
type PrinterStepperEnable struct {
	printer     *host.Printer
	enableLines map[string]*EnableTracking
	names       []string
}

func load(config *configfile.Wrapper) any {
	se := &PrinterStepperEnable{printer: host.Of(config), enableLines: map[string]*EnableTracking{}}
	se.printer.RegisterEventHandler("gcode:request_restart", func(...any) { se.MotorOff() })
	// Register M18/M84 commands
	g := gcode.Of(se.printer)
	g.RegisterCommand("M18", func(*gcode.Command) { se.MotorOff() }, false, "")
	g.RegisterCommand("M84", func(*gcode.Command) { se.MotorOff() }, false, "")
	return se
}

func init() { host.RegisterModule("stepper_enable", load, nil) }

// RegisterStepper adds a stepper and its enable pin.
func (se *PrinterStepperEnable) RegisterStepper(config *configfile.Wrapper, s *stepper.MCUStepper) {
	name := s.Name()
	g := gcode.Of(se.printer)
	g.RegisterMuxCommand("SET_STEPPER_ENABLE", "STEPPER", &name, se.cmdSetStepperEnable,
		"Enable/disable individual stepper by name")
	pin, _ := config.GetOptional("enable_pin", configfile.Default(nil))
	enable := SetupEnablePin(se.printer, pin)
	if _, ok := se.enableLines[name]; !ok {
		se.names = append(se.names, name)
	}
	se.enableLines[name] = newEnableTracking(s, enable)
}

// SetMotorsEnable turns steppers on or off, and reports a change.
func (se *PrinterStepperEnable) SetMotorsEnable(stepperNames []string, enable bool) bool {
	th := se.printer.LookupObject("toolhead").(toolhead)
	// Flush steps to ensure all auto enable callbacks invoked
	th.FlushStepGeneration()
	printTime, havePrintTime := 0., false
	didChange := false
	for _, name := range stepperNames {
		el := se.enableLines[name]
		if el.IsMotorEnabled() == enable {
			continue
		}
		if !havePrintTime {
			// Dwell for sufficient delay from any previous auto enable
			if !enable {
				th.Dwell(disableStallTime)
			}
			printTime, havePrintTime = th.LastMoveTime(), true
		}
		if enable {
			el.MotorEnable(printTime)
		} else {
			el.MotorDisable(printTime)
		}
		didChange = true
	}
	// Dwell to ensure sufficient delay prior to a future auto enable
	if didChange && !enable {
		th.Dwell(disableStallTime)
	}
	return didChange
}

// MotorOff turns all steppers off, and clears the homing state.
func (se *PrinterStepperEnable) MotorOff() {
	se.SetMotorsEnable(se.Steppers(), false)
	kin := se.printer.LookupObject("toolhead").(toolhead).Kinematics()
	kin.(homingStateClearer).ClearHomingState("xyz")
	se.printer.SendEvent("stepper_enable:motor_off")
}

// Status returns the enable state of each stepper.
func (se *PrinterStepperEnable) Status(eventtime float64) map[string]any {
	steppers := map[string]any{}
	for name, et := range se.enableLines {
		steppers[name] = et.IsMotorEnabled()
	}
	return map[string]any{"steppers": steppers}
}

func (se *PrinterStepperEnable) cmdSetStepperEnable(gcmd *gcode.Command) {
	name := gcmd.GetString("STEPPER")
	enable := gcmd.GetInt("ENABLE", gcode.Default(1))
	se.SetMotorsEnable([]string{name}, enable != 0)
	if enable != 0 {
		log.Printf("%s has been manually enabled", name)
	} else {
		log.Printf("%s has been manually disabled", name)
	}
}

// LookupEnable returns the enable state of a stepper.
func (se *PrinterStepperEnable) LookupEnable(name string) *EnableTracking {
	et, ok := se.enableLines[name]
	if !ok {
		configfile.Raise("Unknown stepper '%s'", name)
	}
	return et
}

// Steppers lists the names of the steppers, in the order of their registration.
func (se *PrinterStepperEnable) Steppers() []string { return append([]string(nil), se.names...) }
