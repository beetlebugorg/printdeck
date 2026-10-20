package stepper

// Stepper controlled rails (GenericPrinterRail, LookupRail and LookupMultiRail of stepper.py).

import (
	"fmt"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/motion"
	"printdeck/internal/klipper/pins"
)

// Endstop is the endstop of a rail: a pin of a board, or a virtual endstop (the probe).
type Endstop interface {
	AddStepper(s mcu.Stepper)
}

// positionEndstop is an endstop with its own position, as the probe's virtual endstop.
type positionEndstop interface {
	PositionEndstop() float64
}

// endstopRegistrar is the query_endstops module.
type endstopRegistrar interface {
	RegisterEndstop(endstop any, name string)
}

// NamedEndstop is an endstop and its name.
type NamedEndstop struct {
	Endstop Endstop
	Name    string
}

type endstopEntry struct {
	endstop        Endstop
	invert, pullup int
}

// HomingInfo are the homing settings of a rail.
type HomingInfo struct {
	Speed, PositionEndstop, RetractSpeed, RetractDist float64
	PositiveDir                                       bool
	SecondHomingSpeed                                 float64
}

// Rail is a motor control carriage with one (or more) steppers and one (or more) endstops
// (GenericPrinterRail).
type Rail struct {
	stepperUnitsInRadians bool
	printer               *host.Printer
	name                  string
	steppers              []*MCUStepper
	endstops              []NamedEndstop
	endstopMap            map[string]endstopEntry
	endstopPin            string
	queryEndstops         endstopRegistrar
	positionEndstop       float64
	positionMin           float64
	positionMax           float64
	homingSpeed           float64
	secondHomingSpeed     float64
	homingRetractSpeed    float64
	homingRetractDist     float64
	homingPositiveDir     bool
}

// NewRail makes a rail from a config section. defaultPositionEndstop is nil for none.
func NewRail(config *configfile.Wrapper, needPositionMinmax bool, defaultPositionEndstop *float64,
	unitsInRadians bool) *Rail {
	r := &Rail{stepperUnitsInRadians: unitsInRadians, printer: host.Of(config), name: config.Name(),
		endstopMap: map[string]endstopEntry{}}
	r.endstopPin = config.Get("endstop_pin")
	// Primary endstop position
	r.queryEndstops = r.printer.LoadObject(config, "query_endstops").(endstopRegistrar)
	mcuEndstop := r.lookupEndstop(r.endstopPin, r.name)
	if pe, ok := mcuEndstop.(positionEndstop); ok {
		r.positionEndstop = pe.PositionEndstop()
	} else if defaultPositionEndstop == nil {
		r.positionEndstop = config.GetFloat("position_endstop")
	} else {
		r.positionEndstop = config.GetFloat("position_endstop", configfile.Default(*defaultPositionEndstop))
	}
	// Axis range
	if needPositionMinmax {
		r.positionMin = config.GetFloat("position_min", configfile.Default(0.))
		r.positionMax = config.GetFloat("position_max", configfile.Above(r.positionMin))
	} else {
		r.positionMin = 0.
		r.positionMax = r.positionEndstop
	}
	if r.positionEndstop < r.positionMin || r.positionEndstop > r.positionMax {
		configfile.Raise("position_endstop in section '%s' must be between position_min and position_max",
			config.Name())
	}
	// Homing mechanics
	r.homingSpeed = config.GetFloat("homing_speed", configfile.Default(5.0), configfile.Above(0.))
	r.secondHomingSpeed = config.GetFloat("second_homing_speed", configfile.Default(r.homingSpeed/2.),
		configfile.Above(0.))
	r.homingRetractSpeed = config.GetFloat("homing_retract_speed", configfile.Default(r.homingSpeed),
		configfile.Above(0.))
	r.homingRetractDist = config.GetFloat("homing_retract_dist", configfile.Default(5.), configfile.Min(0.))
	dir, ok := config.GetBoolOptional("homing_positive_dir", configfile.Default(nil))
	if !ok {
		axisLen := r.positionMax - r.positionMin
		switch {
		case r.positionEndstop <= r.positionMin+axisLen/4.:
			r.homingPositiveDir = false
		case r.positionEndstop >= r.positionMax-axisLen/4.:
			r.homingPositiveDir = true
		default:
			configfile.Raise("Unable to infer homing_positive_dir in section '%s'", config.Name())
		}
		config.GetBool("homing_positive_dir", configfile.Default(r.homingPositiveDir))
	} else {
		r.homingPositiveDir = dir
		if (dir && r.positionEndstop == r.positionMin) || (!dir && r.positionEndstop == r.positionMax) {
			configfile.Raise("Invalid homing_positive_dir / position_endstop in '%s'", config.Name())
		}
	}
	return r
}

// Name is the name of the config section.
func (r *Rail) Name() string { return r.name }

// ShortName is the name without "stepper_" (or the last word of the name).
func (r *Rail) ShortName() string {
	if strings.HasPrefix(r.name, "stepper") && len(r.name) > 8 {
		// Skip an extra symbol after 'stepper'
		return r.name[8:]
	}
	f := strings.Fields(r.name)
	return f[len(f)-1]
}

// Range returns the minimum and maximum positions.
func (r *Rail) Range() (float64, float64) { return r.positionMin, r.positionMax }

// HomingInfo returns the homing settings.
func (r *Rail) HomingInfo() HomingInfo {
	return HomingInfo{r.homingSpeed, r.positionEndstop, r.homingRetractSpeed, r.homingRetractDist,
		r.homingPositiveDir, r.secondHomingSpeed}
}

// Steppers lists the steppers.
func (r *Rail) Steppers() []*MCUStepper { return append([]*MCUStepper(nil), r.steppers...) }

// Endstops lists the endstops and their names.
func (r *Rail) Endstops() []NamedEndstop { return append([]NamedEndstop(nil), r.endstops...) }

func (r *Rail) lookupEndstop(endstopPin, name string) Endstop {
	ppins := r.printer.LookupObject("pins").(*pins.Pins)
	pinParams := ppins.ParsePin(endstopPin, true, true)
	// Normalize pin name
	pinName := fmt.Sprintf("%s:%s", pinParams.ChipName, pinParams.Pin)
	// Look for already-registered endstop
	e, ok := r.endstopMap[pinName]
	if !ok {
		// New endstop, register it
		mcuEndstop := ppins.SetupPin("endstop", endstopPin).(Endstop)
		r.endstopMap[pinName] = endstopEntry{mcuEndstop, pinParams.Invert, pinParams.Pullup}
		r.endstops = append(r.endstops, NamedEndstop{mcuEndstop, name})
		r.queryEndstops.RegisterEndstop(mcuEndstop, name)
		return mcuEndstop
	}
	if pinParams.Invert != e.invert || pinParams.Pullup != e.pullup {
		configfile.Raise("Printer rail %s shared endstop pin %s must specify the same pullup/invert settings",
			r.name, pinName)
	}
	return e.endstop
}

// AddStepper adds a stepper, with its own endstop pin or the endstop of the rail.
func (r *Rail) AddStepper(s *MCUStepper, endstopPin, endstopName string) {
	r.steppers = append(r.steppers, s)
	var mcuEndstop Endstop
	if endstopPin != "" {
		if endstopName == "" {
			endstopName = s.ShortName()
		}
		mcuEndstop = r.lookupEndstop(endstopPin, endstopName)
	} else {
		mcuEndstop = r.lookupEndstop(r.endstopPin, r.name)
	}
	mcuEndstop.AddStepper(s)
}

// AddStepperFromConfig adds the stepper of a config section.
func (r *Rail) AddStepperFromConfig(config *configfile.Wrapper) {
	s := NewPrinterStepper(config, r.stepperUnitsInRadians)
	pin, _ := config.GetOptional("endstop_pin", configfile.Default(nil))
	r.AddStepper(s, pin, "")
}

// CommandedPosition is the commanded position of the first stepper.
func (r *Rail) CommandedPosition() float64 { return r.steppers[0].CommandedPosition() }

// CalcPositionFromCoord is the position of the first stepper at an XYZ position.
func (r *Rail) CalcPositionFromCoord(coord []float64) float64 {
	return r.steppers[0].CalcPositionFromCoord(coord)
}

// SetupItersolve sets a step solver for each stepper. alloc makes one solver.
func (r *Rail) SetupItersolve(alloc func() *motion.StepperKinematics) {
	for _, s := range r.steppers {
		s.SetupItersolve(alloc())
	}
}

// SetTrapQ sets the move queue of each stepper.
func (r *Rail) SetTrapQ(tq *motion.TrapQ) {
	for _, s := range r.steppers {
		s.SetTrapQ(tq)
	}
}

// SetPosition sets the position of each stepper.
func (r *Rail) SetPosition(coord []float64) {
	for _, s := range r.steppers {
		s.SetPosition(coord)
	}
}

// LookupRail makes the rail of a config section, with its stepper.
func LookupRail(config *configfile.Wrapper, needPositionMinmax bool, defaultPositionEndstop *float64,
	unitsInRadians bool) *Rail {
	r := NewRail(config, needPositionMinmax, defaultPositionEndstop, unitsInRadians)
	r.AddStepperFromConfig(config)
	return r
}

// LookupMultiRail makes a rail with the steppers of the section and of the sections with a
// number after its name ("stepper_z1").
func LookupMultiRail(config *configfile.Wrapper, needPositionMinmax bool, defaultPositionEndstop *float64,
	unitsInRadians bool) *Rail {
	r := LookupRail(config, needPositionMinmax, defaultPositionEndstop, unitsInRadians)
	for i := 1; i < 99; i++ {
		name := fmt.Sprintf("%s%d", config.Name(), i)
		if !config.HasSection(name) {
			break
		}
		r.AddStepperFromConfig(config.GetSection(name))
	}
	return r
}

// HomingState is the homing of a G28 command, as the kinematics use it. A NaN position in
// HomeRails is an axis that does not move (None in Klipper).
type HomingState interface {
	Axes() []int
	HomeRails(rails []*Rail, forcepos, homepos []float64)
}
