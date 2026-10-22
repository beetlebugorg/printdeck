// Package max_mode is max mode, an experiment: the printer ignores the F speeds of the G-code, and
// gives each move a top speed from the flow limit of the hotend, the feature type comments of the
// slicer and the limits of the printer (package maxmode). It is a module of Printdeck, not a port
// of a Klipper module. The config section is [max_mode]. Max mode is off unless the section has
// enable: True, or until the command MAX_MODE ENABLE=1.
//
// The options and their defaults:
//
//	[max_mode]
//	enable: False
//	max_flow: 12             # mm³/s
//	filament_diameter: 1.75  # mm; the default is filament_diameter of [extruder]
//	first_layer_speed: 30    # mm/s; 0 is no limit
//	travel_speed: 0          # mm/s; 0 is max_velocity
//	min_layer_time: 8        # s; 0 is no minimum
//	min_speed: 10            # mm/s
//	outer_wall_speed: 80     # and inner_wall, overhang (30), bridge (30), top_surface (100),
//	                         # solid_infill, infill, support, skirt and other: 0 is no limit
//
//	Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package max_mode

import (
	"fmt"
	"log"
	"os"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/gcode_move"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/toolhead"
	"printdeck/internal/maxmode"
)

// MaxMode is the max_mode object.
type MaxMode struct {
	printer   *host.Printer
	enabled   bool
	sh        *maxmode.Shaper
	toolhead  *toolhead.ToolHead
	gcodeMove *gcode_move.GCodeMove
}

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	s := maxmode.Defaults()
	if config.HasSection("extruder") {
		s.FilamentDiameter = config.GetSection("extruder").GetFloat("filament_diameter",
			configfile.Default(s.FilamentDiameter))
	}
	s.FilamentDiameter = config.GetFloat("filament_diameter", configfile.Default(s.FilamentDiameter),
		configfile.Above(0.))
	s.Load(func(option string, def float64) float64 {
		return config.GetFloat(option, configfile.Default(def), configfile.Min(0.))
	})
	m := &MaxMode{printer: printer, enabled: config.GetBool("enable", configfile.Default(false)),
		sh: maxmode.NewShaper(s)}
	m.gcodeMove = printer.LoadObject(config, "gcode_move").(*gcode_move.GCodeMove)
	g := gcode.Of(printer)
	g.RegisterCommand("MAX_MODE", m.cmdMaxMode, false,
		"Turn max mode on or off: the move speeds come from the flow limit and the feature speeds")
	g.RegisterCommentHandler(m.comment)
	printer.RegisterEventHandler("klippy:connect", func(...any) {
		m.toolhead = toolhead.Of(printer)
		m.gcodeMove.SetSpeedHook(m.speed)
	})
	return m
}

func init() { host.RegisterModule("max_mode", load, nil) }

// speed is the speed hook of gcode_move. The shaper follows the moves also when max mode is off,
// so that the layer is correct after MAX_MODE ENABLE=1 during a print.
func (m *MaxMode) speed(from, to []float64) float64 {
	m.sh.S.MaxVelocity, _ = m.toolhead.MaxVelocity()
	v := m.sh.Speed([]float64{to[0] - from[0], to[1] - from[1], to[2] - from[2], to[3] - from[3]})
	if !m.enabled {
		return 0
	}
	return v
}

// comment reads the feature and layer comments. At a new layer of a file print it reads the layer
// ahead in the file for the min_layer_time factor.
func (m *MaxMode) comment(c string) {
	if !m.sh.Comment(c) || !m.enabled {
		return
	}
	f := m.layerFactor()
	m.sh.SetFactor(f)
	if f < 1 {
		log.Printf("max_mode: layer %d factor %.3f for min_layer_time", m.sh.Layer(), f)
	}
}

// layerScanMax is the most bytes that the scan of a layer reads.
const layerScanMax = 8 << 20

type sdcard interface {
	FilePath() any
	Status(eventtime float64) map[string]any
}

// layerFactor scans the file of the virtual_sdcard print from the layer comment to the next one.
// The file position is the start of the line of the layer comment while it runs. It returns 1
// without a file print.
func (m *MaxMode) layerFactor() float64 {
	sd, ok := m.printer.TryLookupObject("virtual_sdcard").(sdcard)
	if !ok {
		return 1
	}
	path, ok := sd.FilePath().(string)
	if !ok {
		return 1
	}
	pos, _ := sd.Status(0)["file_position"].(int64)
	f, err := os.Open(path)
	if err != nil {
		log.Printf("max_mode: %v", err)
		return 1
	}
	defer f.Close()
	if _, err := f.Seek(pos, 0); err != nil {
		return 1
	}
	gs := m.gcodeMove.Status(0)
	st := maxmode.State{Layer: m.sh.Layer() - 1}
	if p, ok := gs["gcode_position"].(gcode.Coord); ok {
		st.Pos = p
	}
	st.AbsXYZ, _ = gs["absolute_coordinates"].(bool)
	st.AbsE, _ = gs["absolute_extrude"].(bool)
	if v, ok := gs["speed"].(float64); ok {
		st.Speed = v / 60.
	}
	return maxmode.NextLayerFactor(&limitReader{f, layerScanMax}, m.sh.S, st)
}

type limitReader struct {
	f *os.File
	n int64
}

func (r *limitReader) Read(p []byte) (int, error) {
	if r.n <= 0 {
		return 0, fmt.Errorf("max_mode: layer scan over %d bytes", layerScanMax)
	}
	p = p[:min(int64(len(p)), r.n)]
	n, err := r.f.Read(p)
	r.n -= int64(n)
	return n, err
}

func (m *MaxMode) cmdMaxMode(gcmd *gcode.Command) {
	if v, ok := gcmd.GetFloatOptional("MAX_FLOW", gcode.Above(0.)); ok {
		m.sh.S.MaxFlow = v
	}
	if gcmd.GetInt("ENABLE", gcode.Default(boolInt(m.enabled))) != 0 {
		m.enabled = true
	} else {
		m.enabled = false
		m.sh.SetFactor(1)
	}
	state := "off"
	if m.enabled {
		state = "on"
	}
	gcmd.RespondInfo(fmt.Sprintf("Max mode %s: max_flow=%.1f layer=%d feature=%s layer_factor=%.3f",
		state, m.sh.S.MaxFlow, m.sh.Layer(), m.sh.Feature(), m.sh.Factor()), true)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Status returns the state of max mode.
func (m *MaxMode) Status(eventtime float64) map[string]any {
	return map[string]any{"enabled": m.enabled, "max_flow": m.sh.S.MaxFlow, "layer": m.sh.Layer(),
		"feature": m.sh.Feature().String(), "layer_factor": m.sh.Factor()}
}
