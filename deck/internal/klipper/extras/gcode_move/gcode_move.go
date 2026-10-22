// Package gcode_move has the G-code move commands (G0, G1, G92, M220, ...) and the G-code
// coordinate state: absolute or relative moves, the offsets, and the speed and extrude factors.
//
// Ported to Go from klippy/extras/gcode_move.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2016-2025  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package gcode_move

import (
	"fmt"
	"log"

	"strings"
	"unicode"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/pyfmt"
)

// MoveTransform changes the moves before the toolhead (bed_tilt, bed_mesh, ...).
type MoveTransform interface {
	Move(newpos []float64, speed float64)
	Position() []float64
}

type axis struct {
	name  string
	index int
}

type savedState struct {
	absoluteCoord, allowAbsoluteExtrude        bool
	basePosition, lastPosition, homingPosition []float64
	speed, speedFactor, extrudeFactor          float64
}

// GCodeMove is the G-code coordinate state.
type GCodeMove struct {
	printer                             *host.Printer
	isPrinterReady                      bool
	absoluteCoord, allowAbsoluteExtrude bool
	basePosition                        []float64
	lastPosition                        []float64
	homingPosition                      []float64
	axisMap                             []axis
	speed, speedFactor, extrudeFactor   float64
	savedStates                         map[string]savedState
	moveTransform                       MoveTransform
	moveWithTransform                   func(newpos []float64, speed float64)
	positionWithTransform               func() []float64
	speedHook                           SpeedHook
}

// SpeedHook returns the speed of a G1 move from the position from to the position to, in mm/s
// before the speed factor, or 0 for the F speed. It is an addition of Printdeck for max mode.
type SpeedHook func(from, to []float64) float64

// SetSpeedHook sets the function that gives the speed of each G1 move.
func (m *GCodeMove) SetSpeedHook(h SpeedHook) { m.speedHook = h }

func defaultAxisMap() []axis { return []axis{{"X", 0}, {"Y", 1}, {"Z", 2}, {"E", 3}} }

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	m := &GCodeMove{printer: printer, absoluteCoord: true, allowAbsoluteExtrude: true,
		basePosition: []float64{0., 0., 0., 0.}, lastPosition: []float64{0., 0., 0., 0.},
		homingPosition: []float64{0., 0., 0., 0.}, axisMap: defaultAxisMap(), speed: 25., speedFactor: 1. / 60.,
		extrudeFactor: 1., savedStates: map[string]savedState{},
		positionWithTransform: func() []float64 { return []float64{0., 0., 0., 0.} }}
	// Register g-code commands
	g := gcode.Of(printer)
	for _, c := range []struct {
		name string
		fn   gcode.Handler
		desc string
	}{
		{"G1", m.CmdG1, ""}, {"G20", m.cmdG20, ""}, {"G21", func(*gcode.Command) {}, ""},
		{"M82", func(*gcode.Command) { m.allowAbsoluteExtrude = true }, ""},
		{"M83", func(*gcode.Command) { m.allowAbsoluteExtrude = false }, ""},
		{"G90", func(*gcode.Command) { m.absoluteCoord = true }, ""},
		{"G91", func(*gcode.Command) { m.absoluteCoord = false }, ""},
		{"G92", m.cmdG92, ""}, {"M220", m.cmdM220, ""}, {"M221", m.cmdM221, ""},
		{"SET_GCODE_OFFSET", m.cmdSetGcodeOffset, "Set a virtual offset to g-code positions"},
		{"SAVE_GCODE_STATE", m.cmdSaveGcodeState, "Save G-Code coordinate state"},
		{"RESTORE_GCODE_STATE", m.cmdRestoreGcodeState, "Restore a previously saved G-Code state"},
	} {
		g.RegisterCommand(c.name, c.fn, false, c.desc)
	}
	g.RegisterCommand("G0", m.CmdG1, false, "")
	g.RegisterCommand("M114", m.cmdM114, true, "")
	g.RegisterCommand("GET_POSITION", m.cmdGetPosition, true,
		"Return information on the current location of the toolhead")
	// Register callbacks
	printer.RegisterEventHandler("klippy:ready", func(...any) { m.handleReady() })
	printer.RegisterEventHandler("klippy:shutdown", func(...any) { m.isPrinterReady = false })
	printer.RegisterEventHandler("klippy:analyze_shutdown", func(...any) { m.handleAnalyzeShutdown() })
	printer.RegisterEventHandler("toolhead:set_position", func(...any) { m.ResetLastPosition() })
	printer.RegisterEventHandler("toolhead:manual_move", func(...any) { m.ResetLastPosition() })
	printer.RegisterEventHandler("toolhead:update_extra_axes", func(...any) { m.updateExtraAxes() })
	printer.RegisterEventHandler("gcode:command_error", func(...any) { m.ResetLastPosition() })
	printer.RegisterEventHandler("extruder:activate_extruder", func(...any) { m.handleActivateExtruder() })
	printer.RegisterEventHandler("homing:home_rails_end", func(args ...any) {
		m.handleHomeRailsEnd(args[0].(homingState))
	})
	return m
}

func init() { host.RegisterModule("gcode_move", load, nil) }

// Of returns the gcode_move object of a printer.
func Of(p *host.Printer) *GCodeMove { return p.LookupObject("gcode_move").(*GCodeMove) }

type homingState interface{ Axes() []int }

func (m *GCodeMove) handleReady() {
	m.isPrinterReady = true
	if m.moveTransform == nil {
		th := m.printer.LookupObject("toolhead").(MoveTransform)
		m.moveWithTransform = th.Move
		m.positionWithTransform = th.Position
	}
	m.ResetLastPosition()
}

func (m *GCodeMove) handleAnalyzeShutdown() {
	log.Printf("gcode state: absolute_coord=%v absolute_extrude=%v base_position=%v last_position=%v"+
		" homing_position=%v speed_factor=%v extrude_factor=%v speed=%v", m.absoluteCoord,
		m.allowAbsoluteExtrude, m.basePosition, m.lastPosition, m.homingPosition, m.speedFactor,
		m.extrudeFactor, m.speed)
}

func (m *GCodeMove) handleActivateExtruder() {
	m.ResetLastPosition()
	m.extrudeFactor = 1.
	m.basePosition[3] = m.lastPosition[3]
}

func (m *GCodeMove) handleHomeRailsEnd(hs homingState) {
	m.ResetLastPosition()
	for _, a := range hs.Axes() {
		m.basePosition[a] = m.homingPosition[a]
	}
}

// SetMoveTransform sets the transform of the moves, and returns the old one (the toolhead for
// none). A second transform is a config error, except with force.
func (m *GCodeMove) SetMoveTransform(transform MoveTransform, force bool) MoveTransform {
	if m.moveTransform != nil && !force {
		configfile.Raise("G-Code move transform already specified")
	}
	old := m.moveTransform
	if old == nil {
		old, _ = m.printer.TryLookupObject("toolhead").(MoveTransform)
	}
	m.moveTransform = transform
	m.moveWithTransform = transform.Move
	m.positionWithTransform = transform.Position
	return old
}

func (m *GCodeMove) gcodePosition() []float64 {
	p := make([]float64, len(m.lastPosition))
	for i := range p {
		p[i] = m.lastPosition[i] - m.basePosition[i]
	}
	p[3] /= m.extrudeFactor
	return p
}

func coord(p []float64) gcode.Coord {
	var c gcode.Coord
	copy(c[:], p)
	return c
}

// Status returns the G-code state.
func (m *GCodeMove) Status(eventtime float64) map[string]any {
	axisMap := map[string]int{}
	for _, a := range m.axisMap {
		axisMap[a.name] = a.index
	}
	return map[string]any{"speed_factor": m.speedFactor * 60., "speed": m.speed / m.speedFactor,
		"extrude_factor": m.extrudeFactor, "absolute_coordinates": m.absoluteCoord,
		"absolute_extrude": m.allowAbsoluteExtrude, "homing_origin": coord(m.homingPosition),
		"position": coord(m.lastPosition), "gcode_position": coord(m.gcodePosition()), "axis_map": axisMap}
}

// HomingZ is the Z of the G-code offset (SET_GCODE_OFFSET), in mm. Printdeck uses it to compare a
// Z of the file with a Z of the toolhead.
func (m *GCodeMove) HomingZ() float64 { return m.homingPosition[2] }

// AdjustSavedZ adds dz to the Z offset of a saved G-code state (SAVE_GCODE_STATE), so that the
// print goes on dz higher after RESTORE_GCODE_STATE. It reports false when the state is not saved.
// Printdeck uses it after a height check on a part (printdeck_pause).
func (m *GCodeMove) AdjustSavedZ(name string, dz float64) bool {
	state, ok := m.savedStates[name]
	if !ok {
		return false
	}
	// As SET_GCODE_OFFSET: the offset and the base position of the moves.
	state.homingPosition = append([]float64(nil), state.homingPosition...)
	state.homingPosition[2] += dz
	state.basePosition = append([]float64(nil), state.basePosition...)
	state.basePosition[2] += dz
	m.savedStates[name] = state
	return true
}

// ResetLastPosition sets the last position from the transform (or the toolhead).
func (m *GCodeMove) ResetLastPosition() {
	if m.isPrinterReady {
		m.lastPosition = m.positionWithTransform()
	}
}

type extraAxis interface{ AxisGcodeID() string }

type extraAxesOwner interface{ ExtraAxes() []any }

func (m *GCodeMove) updateExtraAxes() {
	th := m.printer.LookupObject("toolhead").(extraAxesOwner)
	axisMap := defaultAxisMap()
	has := map[string]bool{"X": true, "Y": true, "Z": true, "E": true}
	extraAxes := th.ExtraAxes()
	for index, ea := range extraAxes {
		e, ok := ea.(extraAxis)
		if ea == nil || !ok {
			continue
		}
		id := e.AxisGcodeID()
		if len(id) != 1 || !unicode.IsUpper(rune(id[0])) || has[id] || strings.Contains("FN", id) {
			continue
		}
		has[id] = true
		axisMap = append(axisMap, axis{id, index})
	}
	m.axisMap = axisMap
	n := len(extraAxes)
	base := append([]float64(nil), m.basePosition[:4]...)
	for len(base) < n {
		base = append(base, 0.)
	}
	m.basePosition = base
	m.ResetLastPosition()
}

// G-Code movement commands

// CmdG1 is the G1 command (and G0).
func (m *GCodeMove) CmdG1(gcmd *gcode.Command) {
	// Move
	params := gcmd.Params()
	unable := func() { panic(gcmd.Error("Unable to parse move '%s'", gcmd.Commandline())) }
	var from []float64
	if m.speedHook != nil {
		from = append(from, m.lastPosition...)
	}
	for _, a := range m.axisMap {
		s, ok := params[a.name]
		if !ok {
			continue
		}
		v, err := pyfmt.ParseFloat(s)
		if err != nil {
			unable()
		}
		absoluteCoord := m.absoluteCoord
		if a.name == "E" {
			v *= m.extrudeFactor
			if !m.allowAbsoluteExtrude {
				absoluteCoord = false
			}
		}
		if !absoluteCoord {
			// value relative to position of last move
			m.lastPosition[a.index] += v
		} else {
			// value relative to base coordinate position
			m.lastPosition[a.index] = v + m.basePosition[a.index]
		}
	}
	if s, ok := params["F"]; ok {
		gcodeSpeed, err := pyfmt.ParseFloat(s)
		if err != nil {
			unable()
		}
		if gcodeSpeed <= 0. {
			panic(gcmd.Error("Invalid speed in '%s'", gcmd.Commandline()))
		}
		m.speed = gcodeSpeed * m.speedFactor
	}
	speed := m.speed
	if m.speedHook != nil {
		// Max mode: the speed of the hook in place of the F speed. The speed factor (M220)
		// still applies.
		if v := m.speedHook(from, m.lastPosition); v > 0 {
			speed = v * m.speedFactor * 60.
		}
	}
	m.moveWithTransform(m.lastPosition, speed)
}

// G-Code coordinate manipulation

func (m *GCodeMove) cmdG20(gcmd *gcode.Command) {
	// Set units to inches
	panic(gcmd.Error("Machine does not support G20 (inches) command"))
}

func (m *GCodeMove) cmdG92(gcmd *gcode.Command) {
	// Set position
	any := false
	for i, a := range "XYZE" {
		offset, ok := gcmd.GetFloatOptional(string(a))
		if !ok {
			continue
		}
		any = true
		if i == 3 {
			offset *= m.extrudeFactor
		}
		m.basePosition[i] = m.lastPosition[i] - offset
	}
	if !any {
		copy(m.basePosition[:4], m.lastPosition[:4])
	}
}

func (m *GCodeMove) cmdM114(gcmd *gcode.Command) {
	// Get Current Position
	p := m.gcodePosition()
	gcmd.RespondRaw(fmt.Sprintf("X:%.3f Y:%.3f Z:%.3f E:%.3f", p[0], p[1], p[2], p[3]))
}

func (m *GCodeMove) cmdM220(gcmd *gcode.Command) {
	// Set speed factor override percentage
	value := gcmd.GetFloat("S", gcode.Default(100.), gcode.Above(0.)) / (60. * 100.)
	m.speed = m.speed / m.speedFactor * value
	m.speedFactor = value
}

func (m *GCodeMove) cmdM221(gcmd *gcode.Command) {
	// Set extrude factor override percentage
	newExtrudeFactor := gcmd.GetFloat("S", gcode.Default(100.), gcode.Above(0.)) / 100.
	lastEPos := m.lastPosition[3]
	eValue := (lastEPos - m.basePosition[3]) / m.extrudeFactor
	m.basePosition[3] = lastEPos - eValue*newExtrudeFactor
	m.extrudeFactor = newExtrudeFactor
}

func (m *GCodeMove) cmdSetGcodeOffset(gcmd *gcode.Command) {
	moveDelta := []float64{0., 0., 0., 0.}
	for pos, a := range "XYZE" {
		offset, ok := gcmd.GetFloatOptional(string(a))
		if !ok {
			offset, ok = gcmd.GetFloatOptional(string(a) + "_ADJUST")
			if !ok {
				continue
			}
			offset += m.homingPosition[pos]
		}
		delta := offset - m.homingPosition[pos]
		moveDelta[pos] = delta
		m.basePosition[pos] += delta
		m.homingPosition[pos] = offset
	}
	// Move the toolhead the given offset if requested
	if gcmd.GetInt("MOVE", gcode.Default(0)) != 0 {
		speed := gcmd.GetFloat("MOVE_SPEED", gcode.Default(m.speed), gcode.Above(0.))
		for pos, delta := range moveDelta {
			m.lastPosition[pos] += delta
		}
		m.moveWithTransform(m.lastPosition, speed)
	}
}

func (m *GCodeMove) cmdSaveGcodeState(gcmd *gcode.Command) {
	stateName := gcmd.GetString("NAME", gcode.Default("default"))
	m.savedStates[stateName] = savedState{m.absoluteCoord, m.allowAbsoluteExtrude,
		append([]float64(nil), m.basePosition...), append([]float64(nil), m.lastPosition...),
		append([]float64(nil), m.homingPosition...), m.speed, m.speedFactor, m.extrudeFactor}
}

func (m *GCodeMove) cmdRestoreGcodeState(gcmd *gcode.Command) {
	stateName := gcmd.GetString("NAME", gcode.Default("default"))
	state, ok := m.savedStates[stateName]
	if !ok {
		panic(gcmd.Error("Unknown g-code state: %s", stateName))
	}
	// Restore state
	m.absoluteCoord = state.absoluteCoord
	m.allowAbsoluteExtrude = state.allowAbsoluteExtrude
	copy(m.basePosition[:4], state.basePosition[:4])
	m.homingPosition = append([]float64(nil), state.homingPosition...)
	m.speed = state.speed
	m.speedFactor = state.speedFactor
	m.extrudeFactor = state.extrudeFactor
	// Restore the relative E position
	eDiff := m.lastPosition[3] - state.lastPosition[3]
	m.basePosition[3] += eDiff
	// Move the toolhead back if requested
	if gcmd.GetInt("MOVE", gcode.Default(0)) != 0 {
		speed := gcmd.GetFloat("MOVE_SPEED", gcode.Default(m.speed), gcode.Above(0.))
		copy(m.lastPosition[:3], state.lastPosition[:3])
		m.moveWithTransform(m.lastPosition, speed)
	}
}

type positionStepper interface {
	Name() string
	MCUPosition() int64
	CommandedPosition() float64
}

type kinematics interface {
	Steppers() []any
	CalcPosition(stepperPositions map[string]float64) []float64
}

type kinOwner interface {
	Kinematics() any
	Position() []float64
}

func joinPos(names string, v []float64) string {
	var parts []string
	for i, a := range names {
		if i >= len(v) {
			break
		}
		parts = append(parts, fmt.Sprintf("%c:%.6f", a, v[i]))
	}
	return strings.Join(parts, " ")
}

func (m *GCodeMove) cmdGetPosition(gcmd *gcode.Command) {
	th, ok := m.printer.TryLookupObject("toolhead").(kinOwner)
	if !ok {
		panic(gcmd.Error("Printer not ready"))
	}
	kin := th.Kinematics().(kinematics)
	var mcuPos, stepperPos []string
	cinfo := map[string]float64{}
	for _, s := range kin.Steppers() {
		ps := s.(positionStepper)
		mcuPos = append(mcuPos, fmt.Sprintf("%s:%d", ps.Name(), ps.MCUPosition()))
		stepperPos = append(stepperPos, fmt.Sprintf("%s:%.6f", ps.Name(), ps.CommandedPosition()))
		cinfo[ps.Name()] = ps.CommandedPosition()
	}
	kinPos := joinPos("XYZ", kin.CalcPosition(cinfo))
	gcmd.RespondInfo(fmt.Sprintf("mcu: %s\nstepper: %s\nkinematic: %s\ntoolhead: %s\ngcode: %s\ngcode base: %s\ngcode homing: %s",
		strings.Join(mcuPos, " "), strings.Join(stepperPos, " "), kinPos, joinPos("XYZE", th.Position()),
		joinPos("XYZE", m.lastPosition), joinPos("XYZE", m.basePosition), joinPos("XYZ", m.homingPosition)), true)
}
