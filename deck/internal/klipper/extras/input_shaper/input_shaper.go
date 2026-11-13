// Package input_shaper shapes the motion of the X and Y axes to reduce the ringing of the
// printer (SET_INPUT_SHAPER). The config section is [input_shaper].
//
// Ported to Go from klippy/extras/input_shaper.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f. The step generation of the shaper
// (klippy/chelper/kin_shaper.c) is in motion/kin_shaper.go.
//
//	Copyright (C) 2019-2020  Kevin O'Connor <kevin@koconnor.net>
//	Copyright (C) 2020-2025  Dmitry Butyugin <dmbutyugin@google.com>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package input_shaper

import (
	"fmt"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/motion_queuing"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/motion"
	"printdeck/internal/klipper/stepper"
	"printdeck/internal/klipper/toolhead"
)

// InputShaperParams are the parameters of the shaper of one axis.
type InputShaperParams struct {
	axis         string
	ShaperType   string
	DampingRatio float64
	ShaperFreq   float64
}

func newInputShaperParams(axis string, config *configfile.Wrapper) *InputShaperParams {
	p := &InputShaperParams{axis: axis}
	shaperType := config.Get("shaper_type", configfile.Default("mzv"))
	p.ShaperType = config.Get("shaper_type_"+axis, configfile.Default(shaperType))
	sconfig := GetShaperCfg(p.ShaperType)
	if sconfig == nil {
		configfile.Raise("Unsupported shaper type: %s", p.ShaperType)
	}
	p.DampingRatio = config.GetFloat("damping_ratio_"+axis, configfile.Default(DefaultDampingRatio),
		configfile.Min(0.), configfile.Max(sconfig.MaxDampingRatio))
	p.ShaperFreq = config.GetFloat("shaper_freq_"+axis, configfile.Default(0.), configfile.Min(0.))
	// Validate input shaper
	if _, _, _, err := p.getShaper("", 0, 0, configErrorf); err != nil {
		panic(err)
	}
	return p
}

func configErrorf(format string, args ...any) error {
	return &configfile.Error{Msg: fmt.Sprintf(format, args...)}
}

func commandErrorf(format string, args ...any) error { return gcode.Errorf(format, args...) }

func (p *InputShaperParams) update(gcmd *gcode.Command) {
	axis := strings.ToUpper(p.axis)
	shaperType, ok := gcmd.Get("SHAPER_TYPE", gcode.Default(nil))
	if !ok {
		shaperType = gcmd.GetString("SHAPER_TYPE_"+axis, gcode.Default(p.ShaperType))
	}
	sconfig := GetShaperCfg(strings.ToLower(shaperType))
	if sconfig == nil {
		panic(gcmd.Error("Unsupported shaper type: %s", shaperType))
	}
	dampingRatio := gcmd.GetFloat("DAMPING_RATIO_"+axis, gcode.Default(p.DampingRatio), gcode.Min(0.))
	if dampingRatio > sconfig.MaxDampingRatio {
		panic(gcmd.Error("Too high value of damping_ratio=%.3f for shaper %s on axis %s",
			dampingRatio, shaperType, axis))
	}
	shaperFreq := gcmd.GetFloat("SHAPER_FREQ_"+axis, gcode.Default(p.ShaperFreq), gcode.Min(0.))
	// Validate input shaper
	if _, _, _, err := p.getShaper(strings.ToLower(shaperType), shaperFreq, dampingRatio, commandErrorf); err != nil {
		panic(err)
	}
	p.DampingRatio = dampingRatio
	p.ShaperType = strings.ToLower(shaperType)
	p.ShaperFreq = shaperFreq
}

// getShaper returns the pulses of the shaper. "" and 0 are the values of the parameters (Python's
// None and the "or" of get_shaper).
func (p *InputShaperParams) getShaper(shaperType string, shaperFreq, dampingRatio float64,
	errorf func(string, ...any) error) (int, []float64, []float64, error) {
	var A, T []float64
	if p.ShaperFreq == 0 {
		A, T = GetNoneShaper()
	} else {
		if shaperType == "" {
			shaperType = p.ShaperType
		}
		if shaperFreq == 0 {
			shaperFreq = p.ShaperFreq
		}
		if dampingRatio == 0 {
			dampingRatio = p.DampingRatio
		}
		var err error
		A, T, err = InitShaper(shaperType, shaperFreq, dampingRatio, errorf)
		if err != nil {
			return 0, nil, nil, err
		}
	}
	return len(A), A, T, nil
}

// Status is the status of the parameters, in the order of Klipper.
func (p *InputShaperParams) status() [][2]string {
	return [][2]string{{"shaper_type", p.ShaperType},
		{"shaper_freq", fmt.Sprintf("%.3f", p.ShaperFreq)},
		{"damping_ratio", fmt.Sprintf("%.6f", p.DampingRatio)}}
}

// AxisInputShaper is the shaper of one axis.
type AxisInputShaper struct {
	axis   string
	Params *InputShaperParams
	n      int
	A, T   []float64
	saved  *savedShaper
}

type savedShaper struct {
	n    int
	A, T []float64
}

func newAxisInputShaper(axis string, config *configfile.Wrapper) *AxisInputShaper {
	s := &AxisInputShaper{axis: axis, Params: newInputShaperParams(axis, config)}
	s.n, s.A, s.T, _ = s.Params.getShaper("", 0, 0, nil)
	return s
}

func (s *AxisInputShaper) Name() string { return "shaper_" + s.axis }

// Shaper returns the pulses of the shaper.
func (s *AxisInputShaper) Shaper() (int, []float64, []float64) { return s.n, s.A, s.T }

func (s *AxisInputShaper) update(gcmd *gcode.Command) {
	s.Params.update(gcmd)
	s.n, s.A, s.T, _ = s.Params.getShaper("", 0, 0, nil)
}

func (s *AxisInputShaper) setShaperKinematics(sk *motion.StepperKinematics) bool {
	success := motion.InputShaperSetShaperParams(sk, s.axis[0], s.n, s.A, s.T) == 0
	if !success {
		s.disableShaping()
		motion.InputShaperSetShaperParams(sk, s.axis[0], s.n, s.A, s.T)
	}
	return success
}

// IsEnabled reports that the shaper has pulses.
func (s *AxisInputShaper) IsEnabled() bool { return s.n > 0 }

func (s *AxisInputShaper) disableShaping() {
	if s.saved == nil && s.n != 0 {
		s.saved = &savedShaper{s.n, s.A, s.T}
	}
	A, T := GetNoneShaper()
	s.n, s.A, s.T = len(A), A, T
}

func (s *AxisInputShaper) enableShaping() {
	if s.saved == nil {
		// Input shaper was not disabled
		return
	}
	s.n, s.A, s.T = s.saved.n, s.saved.A, s.saved.T
	s.saved = nil
}

func (s *AxisInputShaper) report(gcmd *gcode.Command) {
	var parts []string
	for _, kv := range s.Params.status() {
		parts = append(parts, fmt.Sprintf("%s_%s:%s", kv[0], s.axis, kv[1]))
	}
	gcmd.RespondInfo(strings.Join(parts, " "), true)
}

// InputShaper is the [input_shaper] section.
type InputShaper struct {
	printer                      *host.Printer
	toolhead                     *toolhead.ToolHead
	motionQueuing                *motion_queuing.PrinterMotionQueuing
	shapers                      []*AxisInputShaper
	inputShaperStepperKinematics []*motion.StepperKinematics
	origStepperKinematics        []*motion.StepperKinematics
}

func load(config *configfile.Wrapper) any {
	is := &InputShaper{printer: host.Of(config), motionQueuing: motion_queuing.Load(config)}
	is.printer.RegisterEventHandler("klippy:connect", func(...any) { is.connect() })
	is.printer.RegisterEventHandler("dual_carriage:update_kinematics", func(...any) { is.updateKinematics() })
	is.shapers = []*AxisInputShaper{newAxisInputShaper("x", config), newAxisInputShaper("y", config),
		newAxisInputShaper("z", config)}
	// Register gcode commands
	gcode.Of(is.printer).RegisterCommand("SET_INPUT_SHAPER", is.cmdSetInputShaper, false,
		"Set cartesian parameters for input shaper")
	return is
}

func init() { host.RegisterModule("input_shaper", load, nil) }

// Shapers returns the shapers of the X, Y and Z axes.
func (is *InputShaper) Shapers() []*AxisInputShaper { return is.shapers }

func (is *InputShaper) connect() {
	is.toolhead = toolhead.Of(is.printer)
	if is.printer.TryLookupObject("dual_carriage") != nil {
		for _, shaper := range is.shapers {
			if shaper.IsEnabled() {
				configfile.Raise("Input shaper parameters cannot be configured via [input_shaper] " +
					"section with dual_carriage(s)  enabled. Refer to Klipper documentation on how  " +
					"to configure input shaper for dual_carriage(s).")
			}
		}
		return
	}
	// Configure initial values
	is.updateInputShaping(configErrorf)
}

func (is *InputShaper) getInputShaperStepperKinematics(s *stepper.MCUStepper) *motion.StepperKinematics {
	// Lookup stepper kinematics
	sk := s.StepperKinematics()
	for _, x := range is.inputShaperStepperKinematics {
		if x == sk {
			return sk
		}
	}
	isSK := motion.InputShaperAlloc()
	s.SetStepperKinematics(isSK)
	if res := motion.InputShaperSetSK(isSK, sk); res < 0 {
		s.SetStepperKinematics(sk)
		return nil
	}
	is.origStepperKinematics = append(is.origStepperKinematics, sk)
	is.inputShaperStepperKinematics = append(is.inputShaperStepperKinematics, isSK)
	return isSK
}

func (is *InputShaper) steppers() []*stepper.MCUStepper {
	var out []*stepper.MCUStepper
	for _, s := range is.toolhead.Kin().Steppers() {
		if ms, ok := s.(*stepper.MCUStepper); ok {
			out = append(out, ms)
		}
	}
	return out
}

func (is *InputShaper) updateKinematics() {
	if is.toolhead == nil {
		// Klipper initialization is not yet completed
		return
	}
	is.toolhead.FlushStepGeneration()
	for _, s := range is.steppers() {
		if s.TrapQ() == nil {
			continue
		}
		isSK := is.getInputShaperStepperKinematics(s)
		if isSK == nil {
			continue
		}
		motion.InputShaperUpdateSK(isSK)
	}
	is.motionQueuing.CheckStepGenerationScanWindows()
}

func (is *InputShaper) updateInputShaping(errorf func(string, ...any) error) {
	is.toolhead.FlushStepGeneration()
	var failedShapers []*AxisInputShaper
	failed := func(s *AxisInputShaper) bool {
		for _, f := range failedShapers {
			if f == s {
				return true
			}
		}
		return false
	}
	for _, s := range is.steppers() {
		if s.TrapQ() == nil {
			continue
		}
		isSK := is.getInputShaperStepperKinematics(s)
		if isSK == nil {
			continue
		}
		for _, shaper := range is.shapers {
			if failed(shaper) {
				continue
			}
			if !shaper.setShaperKinematics(isSK) {
				failedShapers = append(failedShapers, shaper)
			}
		}
	}
	is.motionQueuing.CheckStepGenerationScanWindows()
	if len(failedShapers) > 0 {
		if errorf == nil {
			errorf = commandErrorf
		}
		var names []string
		for _, s := range failedShapers {
			names = append(names, s.Name())
		}
		panic(errorf("Failed to configure shaper(s) %s with given parameters", strings.Join(names, ", ")))
	}
}

// DisableShaping turns the shapers off, and keeps their pulses for EnableShaping.
func (is *InputShaper) DisableShaping() {
	for _, shaper := range is.shapers {
		shaper.disableShaping()
	}
	is.updateInputShaping(nil)
}

// EnableShaping turns the shapers on again.
func (is *InputShaper) EnableShaping() {
	for _, shaper := range is.shapers {
		shaper.enableShaping()
	}
	is.updateInputShaping(nil)
}

func (is *InputShaper) cmdSetInputShaper(gcmd *gcode.Command) {
	if len(gcmd.Params()) > 0 {
		for _, shaper := range is.shapers {
			shaper.update(gcmd)
		}
		is.updateInputShaping(nil)
	}
	for ind, shaper := range is.shapers {
		if ind < 2 || shaper.IsEnabled() {
			shaper.report(gcmd)
		}
	}
}
