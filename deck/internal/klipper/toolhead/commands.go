package toolhead

import (
	"fmt"
	"math"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/motion"
)

// DummyExtruder is the extruder of a printer with no [extruder] section (from
// klippy/kinematics/extruder.py).
type DummyExtruder struct{ printer *host.Printer }

func (d *DummyExtruder) CheckMove(move *Move, eaIndex int) {
	panic(move.MoveError("Extrude when no extruder present"))
}
func (d *DummyExtruder) FindPastPosition(printTime float64) float64 { return 0. }
func (d *DummyExtruder) CalcJunction(prevMove, move *Move, eaIndex int) float64 {
	return move.MaxCruiseV2
}
func (d *DummyExtruder) ProcessMove(printTime float64, move *Move, eIndex int) {}
func (d *DummyExtruder) Name() string                                          { return "" }
func (d *DummyExtruder) Heater() any                                           { panic(gcode.Errorf("Extruder not configured")) }
func (d *DummyExtruder) TrapQ() *motion.TrapQ                                  { return nil }
func (d *DummyExtruder) AxisGcodeID() string                                   { return "E" }

// commandHelper has the G-code commands of the toolhead (ToolHeadCommandHelper).
type commandHelper struct {
	printer  *host.Printer
	toolhead *ToolHead
}

func newCommandHelper(config *configfile.Wrapper) {
	printer := host.Of(config)
	h := &commandHelper{printer: printer, toolhead: printer.LookupObject("toolhead").(*ToolHead)}
	// Register commands
	g := gcode.Of(printer)
	g.RegisterCommand("G4", h.cmdG4, false, "")
	g.RegisterCommand("M400", func(*gcode.Command) { h.toolhead.WaitMoves() }, false, "")
	g.RegisterCommand("SET_VELOCITY_LIMIT", h.cmdSetVelocityLimit, false, "Set printer velocity limits")
	g.RegisterCommand("M204", h.cmdM204, false, "")
}

func (h *commandHelper) cmdG4(gcmd *gcode.Command) {
	// Dwell
	delay := gcmd.GetFloat("P", gcode.Default(0.), gcode.Min(0.)) / 1000.
	h.toolhead.Dwell(delay)
}

// optFloat returns a parameter, or NaN when it is missing.
func optFloat(gcmd *gcode.Command, name string, opts ...gcode.Opt) float64 {
	if v, ok := gcmd.GetFloatOptional(name, opts...); ok {
		return v
	}
	return math.NaN()
}

func (h *commandHelper) cmdSetVelocityLimit(gcmd *gcode.Command) {
	maxVelocity := optFloat(gcmd, "VELOCITY", gcode.Above(0.))
	maxAccel := optFloat(gcmd, "ACCEL", gcode.Above(0.))
	squareCornerVelocity := optFloat(gcmd, "SQUARE_CORNER_VELOCITY", gcode.Min(0.))
	minCruiseRatio := optFloat(gcmd, "MINIMUM_CRUISE_RATIO", gcode.Min(0.), gcode.Below(1.))
	mv, ma, scv, mcr := h.toolhead.SetMaxVelocities(maxVelocity, maxAccel, squareCornerVelocity, minCruiseRatio)
	msg := fmt.Sprintf("max_velocity: %.6f\nmax_accel: %.6f\nminimum_cruise_ratio: %.6f\nsquare_corner_velocity: %.6f",
		mv, ma, mcr, scv)
	if math.IsNaN(maxVelocity) && math.IsNaN(maxAccel) && math.IsNaN(squareCornerVelocity) && math.IsNaN(minCruiseRatio) {
		gcmd.RespondInfo(msg, false)
	}
}

func (h *commandHelper) cmdM204(gcmd *gcode.Command) {
	// Use S for accel
	accel := optFloat(gcmd, "S", gcode.Above(0.))
	if math.IsNaN(accel) {
		// Use minimum of P and T for accel
		p := optFloat(gcmd, "P", gcode.Above(0.))
		t := optFloat(gcmd, "T", gcode.Above(0.))
		if math.IsNaN(p) || math.IsNaN(t) {
			gcmd.RespondInfo(fmt.Sprintf("Invalid M204 command \"%s\"", gcmd.Commandline()), true)
			return
		}
		accel = math.Min(p, t)
	}
	h.toolhead.SetMaxVelocities(math.NaN(), accel, math.NaN(), math.NaN())
}

// ExtruderObjects makes the extruders of the config (add_printer_objects of
// kinematics/extruder.py). The extruder package sets it.
var ExtruderObjects func(config *configfile.Wrapper)

func init() {
	host.AfterConfigObjects = append(host.AfterConfigObjects, func(config *configfile.Wrapper) {
		printer := host.Of(config)
		printer.AddObject("toolhead", newToolHead(config))
		newCommandHelper(config)
		// Load default extruder objects
		if ExtruderObjects != nil {
			ExtruderObjects(config)
		}
		// Load some default modules
		for _, name := range []string{"gcode_move", "homing", "idle_timeout", "statistics", "manual_probe",
			"tuning_tower", "garbage_collection"} {
			printer.LoadObject(config, name)
		}
	})
}

// Of returns the toolhead of a printer.
func Of(p *host.Printer) *ToolHead { return p.LookupObject("toolhead").(*ToolHead) }
