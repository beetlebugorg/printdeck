// Package bltouch is the BLTouch probe: a pin that a servo signal lowers and raises.
//
// Ported to Go from klippy/extras/bltouch.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018-2026  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package bltouch

import (
	"fmt"
	"log"
	"math"
	"sort"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/homing"
	"printdeck/internal/klipper/extras/manual_probe"
	"printdeck/internal/klipper/extras/probe"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/pins"
	"printdeck/internal/klipper/reactor"
	"printdeck/internal/klipper/toolhead"
)

const (
	SignalPeriod = 0.020
	MinCmdTime   = 5 * SignalPeriod

	TestTime           = 5 * 60.
	RetryResetTime     = 1.
	EndstopRestTime    = .001
	EndstopSampleTime  = .000015
	EndstopSampleCount = 4
)

var Commands = map[string]float64{
	"pin_down": 0.000650, "touch_mode": 0.001165,
	"pin_up": 0.001475, "self_test": 0.001780, "reset": 0.002190,
	"set_5V_output_mode": 0.001988, "set_OD_output_mode": 0.002091,
	"output_mode_store": 0.001884,
}

// BLTouchProbe is the BLTouch "endstop" wrapper.
type BLTouchProbe struct {
	printer             *host.Printer
	stowOnEachSample    bool
	probeTouchMode      bool
	mcuPWM              *mcu.PWM
	nextCmdTime         float64
	actionEndTime       float64
	finishHomeComplete  *reactor.Completion
	waitTriggerComplete *reactor.Completion
	mcuEndstop          *mcu.Endstop
	outputMode          any // "5V", "OD" or nil
	nextTestTime        float64
	pinUpNotTriggered   bool
	pinUpTouchTriggered bool
	pinMoveTime         float64
	homingHelper        *probe.DescendToEndstopHelper
	multi               string
	gcode               *gcode.Dispatch
}

func newBLTouchProbe(config *configfile.Wrapper, probeOffsets *probe.OffsetsHelper,
	paramHelper *probe.ParameterHelper) *BLTouchProbe {
	b := &BLTouchProbe{printer: host.Of(config)}
	b.stowOnEachSample = config.GetBool("stow_on_each_sample", configfile.Default(true))
	b.probeTouchMode = config.GetBool("probe_with_touch_mode", configfile.Default(false))
	// Create a pwm object to handle the control pin
	ppins := b.printer.LookupObject("pins").(*pins.Pins)
	b.mcuPWM = ppins.SetupPin("pwm", config.Get("control_pin")).(*mcu.PWM)
	b.mcuPWM.SetupMaxDuration(0.)
	b.mcuPWM.SetupCycleTime(SignalPeriod, false)
	// Command timing
	b.nextCmdTime, b.actionEndTime = 0., 0.
	// Create an "endstop" object to handle the sensor pin
	b.mcuEndstop = ppins.SetupPin("endstop", config.Get("sensor_pin")).(*mcu.Endstop)
	// output mode
	omodes := map[string]any{"5V": "5V", "OD": "OD", "None": nil}
	b.outputMode = config.GetChoice("set_output_mode", omodes, configfile.Default(nil))
	// Setup for sensor test
	b.nextTestTime = 0.
	b.pinUpNotTriggered = config.GetBool("pin_up_reports_not_triggered", configfile.Default(true))
	b.pinUpTouchTriggered = config.GetBool("pin_up_touch_mode_reports_triggered",
		configfile.Default(true))
	// Calculate pin move time
	b.pinMoveTime = config.GetFloat("pin_move_time", configfile.Default(0.680), configfile.Above(0.))
	// Probing via homing to endstop
	b.homingHelper = probe.NewDescendToEndstopHelper(config, b, probeOffsets, paramHelper, true)
	// multi probes state
	b.multi = "OFF"
	// Register BLTOUCH_DEBUG command
	b.gcode = gcode.Of(b.printer)
	b.gcode.RegisterCommand("BLTOUCH_DEBUG", b.cmdBLTouchDebug, false,
		"Send a command to the bltouch for debugging")
	b.gcode.RegisterCommand("BLTOUCH_STORE", b.cmdBLTouchStore, false,
		"Store an output mode in the BLTouch EEPROM")
	// Register events
	b.printer.RegisterEventHandler("klippy:connect", func(...any) { b.handleConnect() })
	return b
}

// Wrappers

func (b *BLTouchProbe) MCU() *mcu.MCU            { return b.mcuEndstop.MCU() }
func (b *BLTouchProbe) AddStepper(s mcu.Stepper) { b.mcuEndstop.AddStepper(s) }
func (b *BLTouchProbe) Steppers() []mcu.Stepper  { return b.mcuEndstop.Steppers() }
func (b *BLTouchProbe) HomeWait(homeEndTime float64) float64 {
	return b.mcuEndstop.HomeWait(homeEndTime)
}
func (b *BLTouchProbe) QueryEndstop(printTime float64) int {
	return b.mcuEndstop.QueryEndstop(printTime)
}

// isCommandError reports that a recovered value is a command error.
func isCommandError(r any) bool {
	_, ok := r.(*gcode.CommandError)
	return ok
}

func (b *BLTouchProbe) handleConnect() {
	b.syncMCUPrintTime()
	b.nextCmdTime += 0.200
	b.setOutputMode(b.outputMode)
	defer func() {
		if r := recover(); r != nil {
			if !isCommandError(r) {
				panic(r)
			}
			log.Printf("BLTouch raise probe error: %s", r.(*gcode.CommandError).Msg)
		}
	}()
	b.raiseProbe()
	b.verifyRaiseProbe()
}

func (b *BLTouchProbe) syncMCUPrintTime() {
	curtime := b.printer.Reactor().Monotonic()
	estTime := b.mcuPWM.MCU().EstimatedPrintTime(curtime)
	b.nextCmdTime = math.Max(b.nextCmdTime, estTime+MinCmdTime)
}

func (b *BLTouchProbe) syncPrintTime() {
	th := toolhead.Of(b.printer)
	printTime := th.LastMoveTime()
	if b.nextCmdTime > printTime {
		th.Dwell(b.nextCmdTime - printTime)
	} else {
		b.nextCmdTime = printTime
	}
}

func (b *BLTouchProbe) sendCmd(cmd string, duration float64) {
	// Translate duration to ticks to avoid any secondary mcu clock skew
	m := b.mcuPWM.MCU()
	cmdClock := m.PrintTimeToClock(b.nextCmdTime)
	pulse := float64(int64((duration-MinCmdTime)/SignalPeriod)) * SignalPeriod
	cmdClock += m.SecondsToClock(math.Max(MinCmdTime, pulse))
	endTime := m.ClockToPrintTime(float64(cmdClock))
	// Schedule command followed by PWM disable
	b.mcuPWM.SetPWM(b.nextCmdTime, Commands[cmd]/SignalPeriod)
	b.mcuPWM.SetPWM(endTime, 0.)
	// Update time tracking
	b.actionEndTime = b.nextCmdTime + duration
	b.nextCmdTime = math.Max(b.actionEndTime, endTime+MinCmdTime)
}

func (b *BLTouchProbe) verifyState(triggered bool) (ok bool) {
	// Perform endstop check to verify bltouch reports desired state
	b.mcuEndstop.HomeStart(b.actionEndTime, EndstopSampleTime, EndstopSampleCount,
		EndstopRestTime, triggered)
	defer func() {
		if r := recover(); r != nil {
			if !isCommandError(r) {
				panic(r)
			}
			ok = false
		}
	}()
	triggerTime := b.mcuEndstop.HomeWait(b.actionEndTime + 0.100)
	return triggerTime > 0.
}

func (b *BLTouchProbe) raiseProbe() {
	b.syncMCUPrintTime()
	if !b.pinUpNotTriggered {
		b.sendCmd("reset", MinCmdTime)
	}
	b.sendCmd("pin_up", b.pinMoveTime)
}

func (b *BLTouchProbe) verifyRaiseProbe() {
	if !b.pinUpNotTriggered {
		// No way to verify raise attempt
		return
	}
	for retry := range 3 {
		success := b.verifyState(false)
		if success {
			// The "probe raised" test completed successfully
			break
		}
		if retry >= 2 {
			panic(gcode.Errorf("BLTouch failed to raise probe"))
		}
		msg := "Failed to verify BLTouch probe is raised; retrying."
		b.gcode.RespondInfo(msg, true)
		b.syncMCUPrintTime()
		b.sendCmd("reset", RetryResetTime)
		b.sendCmd("pin_up", b.pinMoveTime)
	}
}

func (b *BLTouchProbe) lowerProbe() {
	b.testSensor()
	b.syncPrintTime()
	b.sendCmd("pin_down", b.pinMoveTime)
	if b.probeTouchMode {
		b.sendCmd("touch_mode", MinCmdTime)
	}
}

func (b *BLTouchProbe) testSensor() {
	if !b.pinUpTouchTriggered {
		// Nothing to test
		return
	}
	th := toolhead.Of(b.printer)
	printTime := th.LastMoveTime()
	if printTime < b.nextTestTime {
		b.nextTestTime = printTime + TestTime
		return
	}
	// Raise the bltouch probe and test if probe is raised
	b.syncPrintTime()
	for retry := range 3 {
		b.sendCmd("pin_up", b.pinMoveTime)
		b.sendCmd("touch_mode", MinCmdTime)
		success := b.verifyState(true)
		b.syncPrintTime()
		if success {
			// The "bltouch connection" test completed successfully
			b.nextTestTime = printTime + TestTime
			return
		}
		msg := "BLTouch failed to verify sensor state"
		if retry >= 2 {
			panic(gcode.Errorf("%s", msg))
		}
		b.gcode.RespondInfo(msg+"; retrying.", true)
		b.sendCmd("reset", RetryResetTime)
	}
}

// Probe session

func (b *BLTouchProbe) StartProbeSession(gcmd *gcode.Command) probe.HWSession {
	b.homingHelper.ClearTriggerPositions()
	if !b.stowOnEachSample {
		b.multi = "FIRST"
	}
	return b
}

func (b *BLTouchProbe) probePrepare() {
	if b.multi == "OFF" || b.multi == "FIRST" {
		b.lowerProbe()
		if b.multi == "FIRST" {
			b.multi = "ON"
		}
	}
	b.syncPrintTime()
}

func (b *BLTouchProbe) HomeStart(printTime, sampleTime float64, sampleCount int, restTime float64,
	triggered bool) *reactor.Completion {
	restTime = math.Min(restTime, EndstopRestTime)
	b.finishHomeComplete = b.mcuEndstop.HomeStart(printTime, sampleTime, sampleCount, restTime,
		triggered)
	// Schedule wait_for_trigger callback
	r := b.printer.Reactor()
	b.waitTriggerComplete = r.RegisterCallback(b.waitForTrigger, reactor.Now)
	return b.finishHomeComplete
}

func (b *BLTouchProbe) waitForTrigger(eventtime float64) any {
	b.finishHomeComplete.Wait(reactor.Never, nil)
	if b.multi == "OFF" {
		b.raiseProbe()
	}
	return nil
}

func (b *BLTouchProbe) probeFinish() {
	b.waitTriggerComplete.Wait(reactor.Never, nil)
	if b.multi == "OFF" {
		b.verifyRaiseProbe()
	}
	b.syncPrintTime()
}

func (b *BLTouchProbe) RunProbe(gcmd *gcode.Command) {
	b.probePrepare()
	func() {
		defer func() {
			if r := recover(); r != nil {
				if isCommandError(r) {
					b.probeFinish()
				}
				panic(r)
			}
		}()
		b.homingHelper.DescendUntilTrigger(gcmd)
	}()
	b.probeFinish()
}

func (b *BLTouchProbe) PullProbedResults() []*manual_probe.ProbeResult {
	return b.homingHelper.PullTriggerPositions()
}

func (b *BLTouchProbe) EndProbeSession() {
	b.homingHelper.ClearTriggerPositions()
	if !b.stowOnEachSample {
		b.syncPrintTime()
		b.raiseProbe()
		b.verifyRaiseProbe()
		b.syncPrintTime()
		b.multi = "OFF"
	}
}

// Output mode setting

func (b *BLTouchProbe) setOutputMode(mode any) {
	// If this is inadvertently/purposely issued for a
	// BLTOUCH pre V3.0 and clones:
	//   No reaction at all.
	// BLTOUCH V3.0 and V3.1:
	//   This will set the mode.
	if mode == nil {
		return
	}
	log.Printf("BLTouch set output mode: %s", mode)
	b.syncMCUPrintTime()
	if mode == "5V" {
		b.sendCmd("set_5V_output_mode", MinCmdTime)
	}
	if mode == "OD" {
		b.sendCmd("set_OD_output_mode", MinCmdTime)
	}
}

func (b *BLTouchProbe) storeOutputMode(mode string) {
	// If this command is inadvertently/purposely issued for a
	// BLTOUCH pre V3.0 and clones:
	//   No reaction at all to this sequence apart from a pin-down/pin-up
	// BLTOUCH V3.0:
	//   This will set the mode (twice) and sadly, a pin-up is needed at
	//   the end, because of the pin-down
	// BLTOUCH V3.1:
	//   This will set the mode and store it in the eeprom.
	//   The pin-up is not needed but does not hurt
	log.Printf("BLTouch store output mode: %s", mode)
	b.syncPrintTime()
	b.sendCmd("pin_down", MinCmdTime)
	if mode == "5V" {
		b.sendCmd("set_5V_output_mode", MinCmdTime)
	} else {
		b.sendCmd("set_OD_output_mode", MinCmdTime)
	}
	b.sendCmd("output_mode_store", MinCmdTime)
	if mode == "5V" {
		b.sendCmd("set_5V_output_mode", MinCmdTime)
	} else {
		b.sendCmd("set_OD_output_mode", MinCmdTime)
	}
	b.sendCmd("pin_up", MinCmdTime)
}

func (b *BLTouchProbe) cmdBLTouchDebug(gcmd *gcode.Command) {
	cmd, ok := gcmd.Get("COMMAND", gcode.Default(nil))
	if _, known := Commands[cmd]; !ok || !known {
		names := make([]string, 0, len(Commands))
		for c := range Commands {
			names = append(names, c)
		}
		sort.Strings(names)
		gcmd.RespondInfo(fmt.Sprintf("BLTouch commands: %s", strings.Join(names, ", ")), true)
		return
	}
	gcmd.RespondInfo(fmt.Sprintf("Sending BLTOUCH_DEBUG COMMAND=%s", cmd), true)
	b.syncPrintTime()
	b.sendCmd(cmd, b.pinMoveTime)
	b.syncPrintTime()
}

func (b *BLTouchProbe) cmdBLTouchStore(gcmd *gcode.Command) {
	cmd, ok := gcmd.Get("MODE", gcode.Default(nil))
	if !ok || (cmd != "5V" && cmd != "OD") {
		gcmd.RespondInfo("BLTouch output modes: 5V, OD", true)
		return
	}
	gcmd.RespondInfo(fmt.Sprintf("Storing BLTouch output mode: %s", cmd), true)
	b.syncPrintTime()
	b.storeOutputMode(cmd)
	b.syncPrintTime()
}

// PrinterBLTouch is the [bltouch] section: the probe of the printer.
type PrinterBLTouch struct {
	printer      *host.Printer
	probeOffsets *probe.OffsetsHelper
	paramHelper  *probe.ParameterHelper
	mcuProbe     *BLTouchProbe
	probeSession *probe.SampleAveragingHelper
	cmdHelper    *probe.CommandHelper
}

func load(config *configfile.Wrapper) any {
	p := &PrinterBLTouch{printer: host.Of(config)}
	p.probeOffsets = probe.NewOffsetsHelper(config)
	p.paramHelper = probe.NewParameterHelper(config)
	p.mcuProbe = newBLTouchProbe(config, p.probeOffsets, p.paramHelper)
	p.probeSession = probe.NewSampleAveragingHelper(config, p.paramHelper, p.mcuProbe.StartProbeSession)
	queryEndstop := p.mcuProbe.QueryEndstop
	p.cmdHelper = probe.NewCommandHelper(config, p, queryEndstop, true)
	probe.NewHomingViaProbeHelper(config, p.probeOffsets.Offsets(nil)[2], queryEndstop)
	p.printer.AddObject("probe", p)
	return p
}

func init() { host.RegisterModule("bltouch", load, nil) }

func (p *PrinterBLTouch) ProbeParams(gcmd *gcode.Command) probe.Params {
	return p.paramHelper.ProbeParams(gcmd)
}
func (p *PrinterBLTouch) Offsets(gcmd *gcode.Command) [3]float64 {
	return p.probeOffsets.Offsets(gcmd)
}
func (p *PrinterBLTouch) Status(eventtime float64) map[string]any {
	return p.cmdHelper.Status(eventtime)
}
func (p *PrinterBLTouch) StartProbeSession(gcmd *gcode.Command) homing.ProbeSession {
	return p.probeSession.StartProbeSession(gcmd)
}
