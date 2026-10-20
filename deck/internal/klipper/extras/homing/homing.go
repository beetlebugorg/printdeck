// Package homing has the homing and probing moves: the moves to the endstops, the second home
// after a retract, Z homing with the probe, and the G28 command.
//
// Ported to Go from klippy/extras/homing.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
// A position of NaN is a coordinate that is not set (None in Klipper).
//
//	Copyright (C) 2016-2026  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package homing

import (
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/pyfmt"
	"printdeck/internal/klipper/reactor"
	"printdeck/internal/klipper/stepper"
	"printdeck/internal/klipper/toolhead"
)

const (
	homingStartDelay   = 0.001
	endstopSampleTime  = .000015
	endstopSampleCount = 4
)

// Endstop is an endstop that can home: a pin of a board, or the probe.
type Endstop interface {
	Steppers() []mcu.Stepper
	HomeStart(printTime, sampleTime float64, sampleCount int, restTime float64, triggered bool) *reactor.Completion
	HomeWait(homeEndTime float64) float64
}

// NamedEndstop is an endstop and its name.
type NamedEndstop struct {
	Endstop Endstop
	Name    string
}

// positionEndstop is an endstop with its own position: the probe's virtual endstop.
type positionEndstop interface{ PositionEndstop() float64 }

func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case int:
		return x != 0
	case nil:
		return false
	}
	return true
}

// multiComplete returns a completion that completes when all completions complete, or early
// when one of them reports an error (a true result).
func multiComplete(printer *host.Printer, completions []*reactor.Completion) *reactor.Completion {
	if len(completions) == 1 {
		return completions[0]
	}
	// Build completion that waits for all completions
	r := printer.Reactor()
	cp := r.RegisterCallback(func(float64) any {
		res := make([]any, len(completions))
		for i, c := range completions {
			res[i] = c.Wait(reactor.Never, nil)
		}
		return res
	}, reactor.Now)
	// If any completion indicates an error, then exit main completion early
	for _, c := range completions {
		c := c
		r.RegisterCallback(func(float64) any {
			if truthy(c.Wait(reactor.Never, nil)) {
				cp.Complete(1)
			}
			return 0
		}, reactor.Now)
	}
	return cp
}

// StepperPosition tracks the position of a stepper during a homing or probing move.
type StepperPosition struct {
	Stepper     *stepper.MCUStepper
	EndstopName string
	StepperName string
	StartPos    int64
	startCmdPos float64
	HaltPos     int64
	TrigPos     int64
}

func newStepperPosition(s *stepper.MCUStepper, endstopName string) *StepperPosition {
	sp := &StepperPosition{Stepper: s, EndstopName: endstopName, StepperName: s.Name(), StartPos: s.MCUPosition()}
	sp.startCmdPos = s.MCUToCommandedPosition(sp.StartPos)
	return sp
}

func (sp *StepperPosition) noteHomeEnd(triggerTime float64) {
	sp.HaltPos = sp.Stepper.MCUPosition()
	sp.TrigPos = sp.Stepper.PastMCUPosition(triggerTime)
}

func (sp *StepperPosition) verifyNoProbeSkew() {
	newStartPos := sp.Stepper.MCUPositionOf(sp.startCmdPos)
	if newStartPos != sp.StartPos {
		log.Printf("Stepper '%s' position skew after probe: pos %d now %d", sp.Stepper.Name(), sp.StartPos, newStartPos)
	}
}

// HomingMove is a homing or probing move.
type HomingMove struct {
	printer          *host.Printer
	endstops         []NamedEndstop
	toolhead         *toolhead.ToolHead
	StepperPositions []*StepperPosition
}

// NewHomingMove makes a homing move with endstops. The endstops with no steppers are left out. A
// nil toolhead is the toolhead of the printer.
func NewHomingMove(printer *host.Printer, endstops []NamedEndstop, th *toolhead.ToolHead) *HomingMove {
	hm := &HomingMove{printer: printer, toolhead: th}
	for _, es := range endstops {
		if len(es.Endstop.Steppers()) > 0 {
			hm.endstops = append(hm.endstops, es)
		}
	}
	if hm.toolhead == nil {
		hm.toolhead = toolhead.Of(printer)
	}
	return hm
}

// MCUEndstops lists the endstops of the move.
func (hm *HomingMove) MCUEndstops() []Endstop {
	out := make([]Endstop, len(hm.endstops))
	for i, es := range hm.endstops {
		out[i] = es.Endstop
	}
	return out
}

func mcuStepper(s mcu.Stepper) *stepper.MCUStepper { return s.(*stepper.MCUStepper) }

func (hm *HomingMove) calcEndstopRate(endstop Endstop, movepos []float64, speed float64) float64 {
	startpos := hm.toolhead.Position()
	var sum float64
	for i := 0; i < 3; i++ {
		d := movepos[i] - startpos[i]
		sum += d * d
	}
	moveT := math.Sqrt(sum) / speed
	maxSteps := math.Inf(-1)
	for _, s := range endstop.Steppers() {
		ms := mcuStepper(s)
		steps := math.Abs(ms.CalcPositionFromCoord(startpos)-ms.CalcPositionFromCoord(movepos)) / ms.StepDist()
		maxSteps = math.Max(maxSteps, steps)
	}
	if maxSteps <= 0. {
		return .001
	}
	return moveT / maxSteps
}

// CalcToolheadPos returns the toolhead position of stepper positions, with offsets in steps.
func (hm *HomingMove) CalcToolheadPos(kinSpos map[string]float64, offsets map[string]int64) []float64 {
	spos := map[string]float64{}
	for k, v := range kinSpos {
		spos[k] = v
	}
	kin := hm.toolhead.Kin()
	for _, s := range kin.Steppers() {
		ms := s.(*stepper.MCUStepper)
		spos[ms.Name()] += float64(offsets[ms.Name()]) * ms.StepDist()
	}
	thpos := hm.toolhead.Position()
	cpos := kin.CalcPosition(spos)
	out := make([]float64, len(thpos))
	copy(out, thpos)
	for i := 0; i < 3 && i < len(cpos); i++ {
		if !math.IsNaN(cpos[i]) {
			out[i] = cpos[i]
		}
	}
	return out
}

func commandPositions(kin toolhead.Kinematics) map[string]float64 {
	out := map[string]float64{}
	for _, s := range kin.Steppers() {
		ms := s.(*stepper.MCUStepper)
		out[ms.Name()] = ms.CommandedPosition()
	}
	return out
}

// catchCommand runs f, and returns the message of a command error.
func catchCommand(f func()) (msg string, failed bool) {
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(*gcode.CommandError)
			if !ok {
				panic(r)
			}
			msg, failed = e.Msg, true
		}
	}()
	f()
	return "", false
}

// HomingMove moves to movepos until the endstops trigger, and returns the trigger position. With
// probePos, the toolhead position is the position of the trigger.
func (hm *HomingMove) HomingMove(movepos []float64, speed float64, probePos, triggered, checkTriggered bool) []float64 {
	// Notify start of homing/probing move
	hm.printer.SendEvent("homing:homing_move_begin", hm)
	// Note start location
	hm.toolhead.FlushStepGeneration()
	kin := hm.toolhead.Kin()
	kinSpos := commandPositions(kin)
	hm.StepperPositions = nil
	for _, es := range hm.endstops {
		for _, s := range es.Endstop.Steppers() {
			hm.StepperPositions = append(hm.StepperPositions, newStepperPosition(mcuStepper(s), es.Name))
		}
	}
	// Start endstop checking
	printTime := hm.toolhead.LastMoveTime()
	var endstopTriggers []*reactor.Completion
	for _, es := range hm.endstops {
		restTime := hm.calcEndstopRate(es.Endstop, movepos, speed)
		wait := es.Endstop.HomeStart(printTime, endstopSampleTime, endstopSampleCount, restTime, triggered)
		endstopTriggers = append(endstopTriggers, wait)
	}
	allEndstopTrigger := multiComplete(hm.printer, endstopTriggers)
	hm.toolhead.Dwell(homingStartDelay)
	// Issue move
	errMsg := ""
	if msg, failed := catchCommand(func() { hm.toolhead.DripMove(movepos, speed, allEndstopTrigger) }); failed {
		errMsg = "Error during homing move: " + msg
	}
	// Wait for endstops to trigger
	triggerTimes := map[string]float64{}
	moveEndPrintTime := hm.toolhead.LastMoveTime()
	for _, es := range hm.endstops {
		var triggerTime float64
		if msg, failed := catchCommand(func() { triggerTime = es.Endstop.HomeWait(moveEndPrintTime) }); failed {
			if errMsg == "" {
				errMsg = fmt.Sprintf("Error during homing %s: %s", es.Name, msg)
			}
			continue
		}
		if triggerTime > 0. {
			triggerTimes[es.Name] = triggerTime
		} else if checkTriggered && errMsg == "" {
			errMsg = fmt.Sprintf("No trigger on %s after full movement", es.Name)
		}
	}
	// Determine stepper halt positions
	hm.toolhead.FlushStepGeneration()
	for _, sp := range hm.StepperPositions {
		tt, ok := triggerTimes[sp.EndstopName]
		if !ok {
			tt = moveEndPrintTime
		}
		sp.noteHomeEnd(tt)
	}
	var haltpos, trigpos []float64
	if probePos {
		haltSteps, trigSteps := map[string]int64{}, map[string]int64{}
		same := true
		for _, sp := range hm.StepperPositions {
			haltSteps[sp.StepperName] = sp.HaltPos - sp.StartPos
			trigSteps[sp.StepperName] = sp.TrigPos - sp.StartPos
		}
		for k, v := range haltSteps {
			if trigSteps[k] != v {
				same = false
			}
		}
		trigpos = hm.CalcToolheadPos(kinSpos, trigSteps)
		haltpos = trigpos
		if !same {
			haltpos = hm.CalcToolheadPos(kinSpos, haltSteps)
		}
		hm.toolhead.SetPosition(haltpos, "")
		for _, sp := range hm.StepperPositions {
			sp.verifyNoProbeSkew()
		}
	} else {
		haltpos, trigpos = movepos, movepos
		overSteps := map[string]int64{}
		anyOver := false
		for _, sp := range hm.StepperPositions {
			overSteps[sp.StepperName] = sp.HaltPos - sp.TrigPos
			if overSteps[sp.StepperName] != 0 {
				anyOver = true
			}
		}
		if anyOver {
			hm.toolhead.SetPosition(movepos, "")
			haltpos = hm.CalcToolheadPos(commandPositions(kin), overSteps)
		}
		hm.toolhead.SetPosition(haltpos, "")
	}
	// Signal homing/probing move complete
	if msg, failed := catchCommand(func() { hm.printer.SendEvent("homing:homing_move_end", hm) }); failed && errMsg == "" {
		errMsg = msg
	}
	if errMsg != "" {
		panic(gcode.Errorf("%s", errMsg))
	}
	return trigpos
}

// CheckNoMovement returns the name of an endstop whose stepper did not move, or "".
func (hm *HomingMove) CheckNoMovement() string {
	if _, ok := hm.printer.StartArgs().Get("debuginput"); ok {
		return ""
	}
	for _, sp := range hm.StepperPositions {
		if sp.StartPos == sp.TrigPos {
			return sp.EndstopName
		}
	}
	return ""
}

// Homing is the state of a homing request (G28).
type Homing struct {
	printer       *host.Printer
	toolhead      *toolhead.ToolHead
	changedAxes   []int
	triggerMCUPos map[string]int64
	adjustPos     map[string]float64
}

func newHoming(printer *host.Printer) *Homing {
	return &Homing{printer: printer, toolhead: toolhead.Of(printer), triggerMCUPos: map[string]int64{},
		adjustPos: map[string]float64{}}
}

func (h *Homing) SetAxes(axes []int) { h.changedAxes = axes }
func (h *Homing) Axes() []int        { return h.changedAxes }

// TriggerPosition returns the board position of a stepper at the trigger.
func (h *Homing) TriggerPosition(stepperName string) int64 { return h.triggerMCUPos[stepperName] }

// SetStepperAdjustment adds an offset to a stepper after the homing (the endstop phase).
func (h *Homing) SetStepperAdjustment(stepperName string, adjustment float64) {
	h.adjustPos[stepperName] = adjustment
}

// fillCoord fills the NaN entries of coord with the toolhead position.
func (h *Homing) fillCoord(coord []float64) []float64 {
	thcoord := h.toolhead.Position()
	for i, c := range coord {
		if !math.IsNaN(c) {
			thcoord[i] = c
		}
	}
	return thcoord
}

// SetHomedPosition sets the toolhead position of the coordinates that are not NaN.
func (h *Homing) SetHomedPosition(pos []float64) { h.toolhead.SetPosition(h.fillCoord(pos), "") }

func (h *Homing) setStartPosition(forcepos []float64) {
	var axes strings.Builder
	for axis := 0; axis < 3; axis++ {
		if !math.IsNaN(forcepos[axis]) {
			axes.WriteByte("xyz"[axis])
		}
	}
	h.toolhead.SetPosition(h.fillCoord(forcepos), axes.String())
}

func (h *Homing) retractMove(hi stepper.HomingInfo, forcepos, movepos []float64) []float64 {
	// Determine retract position and move to it
	startpos := h.fillCoord(forcepos)
	homepos := h.fillCoord(movepos)
	axesD := make([]float64, len(homepos))
	var sum float64
	for i := range homepos {
		axesD[i] = homepos[i] - startpos[i]
		if i < 3 {
			sum += axesD[i] * axesD[i]
		}
	}
	retractR := math.Min(1., hi.RetractDist/math.Sqrt(sum))
	retractpos := make([]float64, len(homepos))
	for i := range homepos {
		retractpos[i] = homepos[i] - axesD[i]*retractR
	}
	h.toolhead.Move(retractpos, hi.RetractSpeed)
	// Force new kinematic position
	newStart := make([]float64, len(retractpos))
	for i := range retractpos {
		newStart[i] = retractpos[i] - axesD[i]*retractR
	}
	h.toolhead.SetPosition(newStart, "")
	return homepos
}

func railEndstops(rails []*stepper.Rail) []NamedEndstop {
	var out []NamedEndstop
	for _, r := range rails {
		for _, e := range r.Endstops() {
			out = append(out, NamedEndstop{e.Endstop.(Endstop), e.Name})
		}
	}
	return out
}

func (h *Homing) doHomeRails(rails []*stepper.Rail, forcepos, movepos []float64) {
	// Notify of upcoming homing operation
	h.printer.SendEvent("homing:home_rails_begin", h, rails)
	// Alter kinematics class to think printer is at forcepos
	homepos := h.fillCoord(movepos)
	h.setStartPosition(forcepos)
	// Perform first home
	endstops := railEndstops(rails)
	hi := rails[0].HomingInfo()
	hmove := NewHomingMove(h.printer, endstops, nil)
	hmove.HomingMove(homepos, hi.Speed, false, true, true)
	// Perform second home
	if hi.RetractDist != 0 {
		homepos = h.retractMove(hi, forcepos, movepos)
		// Home again
		hmove = NewHomingMove(h.printer, endstops, nil)
		hmove.HomingMove(homepos, hi.SecondHomingSpeed, false, true, true)
		if name := hmove.CheckNoMovement(); name != "" {
			panic(gcode.Errorf("Endstop %s still triggered after retract", name))
		}
	}
	// Signal home operation complete
	h.toolhead.FlushStepGeneration()
	h.triggerMCUPos = map[string]int64{}
	for _, sp := range hmove.StepperPositions {
		h.triggerMCUPos[sp.StepperName] = sp.TrigPos
	}
	h.adjustPos = map[string]float64{}
	h.printer.SendEvent("homing:home_rails_end", h, rails)
	anyAdjust := false
	for _, v := range h.adjustPos {
		if v != 0 {
			anyAdjust = true
		}
	}
	if anyAdjust {
		// Apply any homing offsets
		kin := h.toolhead.Kin()
		homepos = h.toolhead.Position()
		kinSpos := commandPositions(kin)
		for name := range kinSpos {
			kinSpos[name] += h.adjustPos[name]
		}
		newpos := kin.CalcPosition(kinSpos)
		for axis := 0; axis < 3; axis++ {
			if !math.IsNaN(forcepos[axis]) {
				if math.IsNaN(newpos[axis]) {
					panic(gcode.Errorf("Cannot determine position of toolhead on axis %c after homing", "xyz"[axis]))
				}
				homepos[axis] = newpos[axis]
			}
		}
		h.toolhead.SetPosition(homepos, "")
	}
}

// ProbeResult is a result of the probe.
type ProbeResult interface{ BedZ() float64 }

// ProbeSession is a probing session of the probe.
type ProbeSession interface {
	RunProbe(gcmd *gcode.Command)
	PullProbedResults() []ProbeResult
	EndProbeSession()
}

// Prober is the probe object, as the Z homing with the probe uses it.
type Prober interface {
	StartProbeSession(gcmd *gcode.Command) ProbeSession
}

func (h *Homing) createProbeGcmd(attemptNum int, probeSpeed float64) *gcode.Command {
	params := map[string]string{"HOME_ATTEMPT_NUM": strconv.Itoa(attemptNum), "SAMPLES": "1",
		"PROBE_SPEED": pyfmt.Float(probeSpeed)}
	return gcode.Of(h.printer).CreateCommand("G28", "", params)
}

func (h *Homing) probingHome(session ProbeSession, attemptNum int, probeSpeed float64) {
	// Run probe
	session.RunProbe(h.createProbeGcmd(attemptNum, probeSpeed))
	ppos := session.PullProbedResults()[0]
	// Update toolhead position
	curpos := h.toolhead.Position()
	curpos[2] -= ppos.BedZ()
	h.toolhead.SetPosition(curpos, "")
}

func (h *Homing) doHomeZViaProbe(rails []*stepper.Rail, forcepos, movepos []float64) {
	if !math.IsNaN(movepos[0]) || !math.IsNaN(movepos[1]) {
		panic(gcode.Errorf("Can only home Z with probe:z_virtual_endstop"))
	}
	hi := rails[0].HomingInfo()
	// Create probe session
	probe := h.printer.LookupObject("probe").(Prober)
	session := probe.StartProbeSession(h.createProbeGcmd(1, hi.Speed))
	// Alter kinematics class to think printer is at forcepos
	h.setStartPosition(forcepos)
	// Perform first home
	h.probingHome(session, 1, hi.Speed)
	// Perform second home
	if hi.RetractDist != 0 {
		h.retractMove(hi, forcepos, movepos)
		// Home again
		h.probingHome(session, 2, hi.SecondHomingSpeed)
	}
	session.EndProbeSession()
}

// HomeRails homes rails: forcepos is the position before the move, and movepos the endstop
// position.
func (h *Homing) HomeRails(rails []*stepper.Rail, forcepos, movepos []float64) {
	// Check if this is actually a Z home via probe request
	var isProbe []bool
	anyProbe := false
	for _, r := range rails {
		for _, e := range r.Endstops() {
			_, ok := e.Endstop.(positionEndstop)
			isProbe = append(isProbe, ok)
			anyProbe = anyProbe || ok
		}
	}
	if len(isProbe) == 1 && isProbe[0] {
		// Perform Z home via probe
		h.doHomeZViaProbe(rails, forcepos, movepos)
		return
	}
	if anyProbe {
		panic(gcode.Errorf("Can't home to mix of probe:z_virtual_endstop rails"))
	}
	// Perform normal home to endstops
	h.doHomeRails(rails, forcepos, movepos)
}

// PrinterHoming has the G28 command and the homing moves of other modules.
type PrinterHoming struct{ printer *host.Printer }

func load(config *configfile.Wrapper) any {
	ph := &PrinterHoming{printer: host.Of(config)}
	gcode.Of(ph.printer).RegisterCommand("G28", ph.cmdG28, false, "")
	return ph
}

func init() { host.RegisterModule("homing", load, nil) }

// shutdownError turns a command error into the error of Klipper at a shutdown.
func (ph *PrinterHoming) rethrow(what string) {
	if r := recover(); r != nil {
		if _, ok := r.(*gcode.CommandError); ok && ph.printer.IsShutdown() {
			panic(gcode.Errorf("%s failed due to printer shutdown", what))
		}
		panic(r)
	}
}

// ManualHome is a homing move with endstops, for the modules that home by themselves.
func (ph *PrinterHoming) ManualHome(th *toolhead.ToolHead, endstops []NamedEndstop, pos []float64, speed float64,
	probePos, triggered, checkTriggered bool) []float64 {
	hmove := NewHomingMove(ph.printer, endstops, th)
	defer ph.rethrow("Homing")
	return hmove.HomingMove(pos, speed, probePos, triggered, checkTriggered)
}

// CheckProbeFirstHome reports a G28 command of the first probe attempt.
func (ph *PrinterHoming) CheckProbeFirstHome(gcmd *gcode.Command) bool {
	v, _ := gcmd.Get("HOME_ATTEMPT_NUM", gcode.Default(nil))
	return gcmd.Command() == "G28" && v == "1"
}

// ProbingMove moves until the probe triggers, and returns the trigger position.
func (ph *PrinterHoming) ProbingMove(mcuProbe Endstop, pos []float64, speed float64, checkMovement bool) []float64 {
	hmove := NewHomingMove(ph.printer, []NamedEndstop{{mcuProbe, "probe"}}, nil)
	var epos []float64
	func() {
		defer ph.rethrow("Probing")
		epos = hmove.HomingMove(pos, speed, true, true, true)
	}()
	if checkMovement && hmove.CheckNoMovement() != "" {
		panic(gcode.Errorf("Probe triggered prior to movement"))
	}
	return epos
}

type homer interface{ Home(hs stepper.HomingState) }

type motorOffer interface{ MotorOff() }

func (ph *PrinterHoming) cmdG28(gcmd *gcode.Command) {
	// Move to origin
	var axes []int
	for pos, axis := range "XYZ" {
		if _, ok := gcmd.Get(string(axis), gcode.Default(nil)); ok {
			axes = append(axes, pos)
		}
	}
	if len(axes) == 0 {
		axes = []int{0, 1, 2}
	}
	hs := newHoming(ph.printer)
	hs.SetAxes(axes)
	kin := toolhead.Of(ph.printer).Kinematics()
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(*gcode.CommandError); ok {
				if ph.printer.IsShutdown() {
					panic(gcode.Errorf("Homing failed due to printer shutdown"))
				}
				ph.printer.LookupObject("stepper_enable").(motorOffer).MotorOff()
			}
			panic(r)
		}
	}()
	kin.(homer).Home(hs)
}
