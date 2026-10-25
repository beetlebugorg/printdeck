// Package endstop_phase improves the accuracy of an endstop with the phase of the stepper motor.
//
// Ported to Go from klippy/extras/endstop_phase.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2016-2021  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package endstop_phase

import (
	"fmt"
	"log"
	"maps"
	"math"
	"slices"
	"sort"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/homing"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/stepper"
)

var TrinamicDrivers = []string{"tmc2130", "tmc2208", "tmc2209", "tmc2240", "tmc2660", "tmc5160"}

// tmcModule is a Trinamic driver with a phase offset.
type tmcModule interface {
	PhaseOffset() (*int64, int64)
}

// PhaseCalc calculates the trigger phase of a stepper motor.
type PhaseCalc struct {
	printer   *host.Printer
	name      string
	phases    int64 // 0 for unknown
	tmcModule tmcModule
	// Statistics tracking for ENDSTOP_PHASE_CALIBRATE
	phaseHistory    []int64 // nil for none
	lastPhase       *int64
	lastMCUPosition *int64
	isPrimary       bool
	statsOnly       bool
}

func newPhaseCalc(printer *host.Printer, name string, phases int64) *PhaseCalc {
	return &PhaseCalc{printer: printer, name: name, phases: phases}
}

func (p *PhaseCalc) lookupTMC() {
	for _, driver := range TrinamicDrivers {
		driverName := fmt.Sprintf("%s %s", driver, p.name)
		module := p.printer.TryLookupObject(driverName)
		if module != nil {
			p.tmcModule = module.(tmcModule)
			if p.phases == 0 {
				_, p.phases = p.tmcModule.PhaseOffset()
			}
			break
		}
	}
	if p.phases != 0 {
		p.phaseHistory = make([]int64, p.phases)
	}
}

func (p *PhaseCalc) convertPhase(driverPhase, driverPhases int64) int64 {
	phases := p.phases
	return pyMod(int64(float64(driverPhase)/float64(driverPhases)*float64(phases)+.5), phases)
}

func (p *PhaseCalc) calcPhase(s mcu.Stepper, trigMCUPos int64) int64 {
	var mcuPhaseOffset int64
	if p.tmcModule != nil {
		offset, _ := p.tmcModule.PhaseOffset()
		if offset == nil {
			if _, ok := p.printer.StartArgs().Get("debugoutput"); !ok {
				panic(gcode.Errorf("Stepper %s phase unknown", p.name))
			}
		} else {
			mcuPhaseOffset = *offset
		}
	}
	phase := pyMod(trigMCUPos+mcuPhaseOffset, p.phases)
	p.phaseHistory[phase]++
	p.lastPhase = &phase
	p.lastMCUPosition = &trigMCUPos
	return phase
}

// pyMod is the Python % operator: the result has the sign of b.
func pyMod(a, b int64) int64 {
	m := a % b
	if m != 0 && (m < 0) != (b < 0) {
		m += b
	}
	return m
}

// EndstopPhase is an [endstop_phase stepper] section: it adjusts the endstop trigger positions.
type EndstopPhase struct {
	printer              *host.Printer
	name                 string
	stepDist             float64
	phases               int64
	phaseCalc            *PhaseCalc
	endstopPhase         *int64
	endstopAlignZero     bool
	endstopAccuracy      *float64
	endstopPhaseAccuracy int64
}

func loadPrefix(config *configfile.Wrapper) any {
	e := &EndstopPhase{printer: host.Of(config)}
	e.name = strings.Join(strings.Fields(config.Name())[1:], " ")
	// Obtain step_distance and microsteps from stepper config section
	sconfig := config.GetSection(e.name)
	rotationDist, stepsPerRotation := stepper.ParseStepDistance(sconfig, nil, true)
	e.stepDist = rotationDist / stepsPerRotation
	e.phases = int64(sconfig.GetInt("microsteps", configfile.NoteValid(false))) * 4
	e.phaseCalc = newPhaseCalc(e.printer, e.name, e.phases)
	// Register event handlers
	e.printer.RegisterEventHandler("klippy:connect", func(...any) { e.phaseCalc.lookupTMC() })
	e.printer.RegisterEventHandler("homing:home_rails_end", func(args ...any) {
		e.handleHomeRailsEnd(args[0].(*homing.Homing), args[1].([]*stepper.Rail))
	})
	e.printer.LoadObject(config, "endstop_phase")
	// Read config
	if triggerPhase, ok := config.GetOptional("trigger_phase", configfile.Default(nil)); ok {
		pps := config.GetIntList("trigger_phase", "/", 2)
		p, ps := int64(pps[0]), int64(pps[1])
		if p >= ps {
			configfile.Raise("Invalid trigger_phase '%s'", triggerPhase)
		}
		ep := e.phaseCalc.convertPhase(p, ps)
		e.endstopPhase = &ep
	}
	e.endstopAlignZero = config.GetBool("endstop_align_zero", configfile.Default(false))
	if v, ok := config.GetFloatOptional("endstop_accuracy", configfile.Default(nil), configfile.Above(0.)); ok {
		e.endstopAccuracy = &v
	}
	// Determine endstop accuracy
	if e.endstopAccuracy == nil {
		e.endstopPhaseAccuracy = e.phases/2 - 1
	} else if e.endstopPhase != nil {
		e.endstopPhaseAccuracy = int64(math.Ceil(*e.endstopAccuracy * .5 / e.stepDist))
	} else {
		e.endstopPhaseAccuracy = int64(math.Ceil(*e.endstopAccuracy / e.stepDist))
	}
	if e.endstopPhaseAccuracy >= e.phases/2 {
		configfile.Raise("Endstop for %s is not accurate enough for stepper phase adjustment", e.name)
	}
	if _, ok := e.printer.StartArgs().Get("debugoutput"); ok {
		e.endstopPhaseAccuracy = e.phases
	}
	return e
}

func (e *EndstopPhase) alignEndstop(rail *stepper.Rail) float64 {
	if !e.endstopAlignZero || e.endstopPhase == nil {
		return 0.
	}
	// Adjust the endstop position so 0.0 is always at a full step
	microsteps := e.phases / 4
	halfMicrosteps := microsteps / 2
	phaseOffset := float64(pyMod(*e.endstopPhase+halfMicrosteps, microsteps)-halfMicrosteps) * e.stepDist
	fullStep := float64(microsteps) * e.stepDist
	pe := rail.HomingInfo().PositionEndstop
	return float64(int64(pe/fullStep+.5))*fullStep - pe + phaseOffset
}

func (e *EndstopPhase) homedOffset(s mcu.Stepper, trigMCUPos int64) float64 {
	phase := e.phaseCalc.calcPhase(s, trigMCUPos)
	if e.endstopPhase == nil {
		log.Printf("Setting %s endstop phase to %d", e.name, phase)
		e.endstopPhase = &phase
		return 0.
	}
	delta := pyMod(phase-*e.endstopPhase, e.phases)
	if delta >= e.phases-e.endstopPhaseAccuracy {
		delta -= e.phases
	} else if delta > e.endstopPhaseAccuracy {
		panic(gcode.Errorf("Endstop %s incorrect phase (got %d vs %d)", e.name, phase, *e.endstopPhase))
	}
	return float64(delta) * e.stepDist
}

// endstopSteppers is an endstop that lists its steppers.
type endstopSteppers interface{ Steppers() []mcu.Stepper }

func (e *EndstopPhase) handleHomeRailsEnd(homingState *homing.Homing, rails []*stepper.Rail) {
	for _, rail := range rails {
		s := rail.Endstops()[0].Endstop.(endstopSteppers).Steppers()[0]
		if s.Name() == e.name {
			trigMCUPos := homingState.TriggerPosition(e.name)
			align := e.alignEndstop(rail)
			offset := e.homedOffset(s, trigMCUPos)
			homingState.SetStepperAdjustment(e.name, align+offset)
			return
		}
	}
}

// EndstopPhases is the [endstop_phase] object: it has the ENDSTOP_PHASE_CALIBRATE command.
type EndstopPhases struct {
	printer  *host.Printer
	tracking map[string]*PhaseCalc
	gcode    *gcode.Dispatch
}

func load(config *configfile.Wrapper) any {
	e := &EndstopPhases{printer: host.Of(config), tracking: map[string]*PhaseCalc{}}
	// Register handlers
	e.printer.RegisterEventHandler("homing:home_rails_end", func(args ...any) {
		e.handleHomeRailsEnd(args[0].(*homing.Homing), args[1].([]*stepper.Rail))
	})
	e.gcode = gcode.Of(e.printer)
	e.gcode.RegisterCommand("ENDSTOP_PHASE_CALIBRATE", e.cmdEndstopPhaseCalibrate, false,
		"Calibrate stepper phase")
	return e
}

func init() { host.RegisterModule("endstop_phase", load, loadPrefix) }

func (e *EndstopPhases) updateStepper(s mcu.Stepper, trigMCUPos int64, isPrimary bool) {
	stepperName := s.Name()
	phaseCalc, ok := e.tracking[stepperName]
	if !ok {
		// Check if stepper has an endstop_phase config section defined
		modName := fmt.Sprintf("endstop_phase %s", stepperName)
		if m := e.printer.TryLookupObject(modName); m != nil {
			phaseCalc = m.(*EndstopPhase).phaseCalc
		} else {
			// Create new PhaseCalc tracker
			phaseCalc = newPhaseCalc(e.printer, stepperName, 0)
			phaseCalc.statsOnly = true
			phaseCalc.lookupTMC()
		}
		e.tracking[stepperName] = phaseCalc
	}
	if phaseCalc.phaseHistory == nil {
		return
	}
	if isPrimary {
		phaseCalc.isPrimary = true
	}
	if phaseCalc.statsOnly {
		phaseCalc.calcPhase(s, trigMCUPos)
	}
}

func (e *EndstopPhases) handleHomeRailsEnd(homingState *homing.Homing, rails []*stepper.Rail) {
	for _, rail := range rails {
		isPrimary := true
		for _, s := range rail.Steppers() {
			sname := s.Name()
			trigMCUPos := homingState.TriggerPosition(sname)
			e.updateStepper(s, trigMCUPos, isPrimary)
			isPrimary = false
		}
	}
}

func (e *EndstopPhases) cmdEndstopPhaseCalibrate(gcmd *gcode.Command) {
	stepperName, ok := gcmd.Get("STEPPER", gcode.Default(nil))
	if !ok {
		e.reportStats()
		return
	}
	phaseCalc := e.tracking[stepperName]
	if phaseCalc == nil || phaseCalc.phaseHistory == nil {
		panic(gcmd.Error("Stats not available for stepper %s", stepperName))
	}
	endstopPhase, phases := e.generateStats(stepperName, phaseCalc)
	if !phaseCalc.isPrimary {
		return
	}
	pconfig := e.printer.LookupObject("configfile").(*configfile.PrinterConfig)
	section := fmt.Sprintf("endstop_phase %s", stepperName)
	pconfig.RemoveSection(section)
	pconfig.Set(section, "trigger_phase", fmt.Sprintf("%d/%d", endstopPhase, phases))
	gcmd.RespondInfo("The SAVE_CONFIG command will update the printer config\n"+
		"file with these parameters and restart the printer.", true)
}

func (e *EndstopPhases) generateStats(stepperName string, phaseCalc *PhaseCalc) (int64, int64) {
	phaseHistory := phaseCalc.phaseHistory
	wph := append(slices.Clone(phaseHistory), phaseHistory...)
	phases := int64(len(phaseHistory))
	halfPhases := phases / 2
	type costPhase struct{ cost, phase int64 }
	var res []costPhase
	for i := range phases {
		phase := i + halfPhases
		var cost int64
		for j := i; j < i+phases; j++ {
			d := j - phase
			if d < 0 {
				d = -d
			}
			cost += wph[j] * d
		}
		res = append(res, costPhase{cost, phase})
	}
	sort.Slice(res, func(a, b int) bool {
		if res[a].cost != res[b].cost {
			return res[a].cost < res[b].cost
		}
		return res[a].phase < res[b].phase
	})
	best := res[0].phase
	var found []int64
	for j := best - halfPhases; j < best+halfPhases; j++ {
		// Python indexes from the end of the list for a negative j
		k := j
		if k < 0 {
			k += int64(len(wph))
		}
		if wph[k] != 0 {
			found = append(found, j)
		}
	}
	bestPhase := pyMod(best, phases)
	lo, hi := pyMod(found[0], phases), pyMod(found[len(found)-1], phases)
	e.gcode.RespondInfo(fmt.Sprintf("%s: trigger_phase=%d/%d (range %d to %d)",
		stepperName, bestPhase, phases, lo, hi), true)
	return bestPhase, phases
}

func (e *EndstopPhases) reportStats() {
	if len(e.tracking) == 0 {
		e.gcode.RespondInfo("No steppers found. (Be sure to home at least once.)", true)
		return
	}
	for _, stepperName := range slices.Sorted(maps.Keys(e.tracking)) {
		phaseCalc := e.tracking[stepperName]
		if phaseCalc == nil || !phaseCalc.isPrimary {
			continue
		}
		e.generateStats(stepperName, phaseCalc)
	}
}

func (e *EndstopPhases) Status(eventtime float64) map[string]any {
	lh := map[string]any{}
	for name, pc := range e.tracking {
		if pc.phaseHistory == nil {
			continue
		}
		var phase, mcuPos any
		if pc.lastPhase != nil {
			phase = *pc.lastPhase
		}
		if pc.lastMCUPosition != nil {
			mcuPos = *pc.lastMCUPosition
		}
		lh[name] = map[string]any{"phase": phase, "phases": pc.phases, "mcu_position": mcuPos}
	}
	return map[string]any{"last_home": lh}
}
