// Package led is the common code of the LEDs (SET_LED, SET_LED_TEMPLATE) and the [led] section of
// PWM driven LEDs.
//
// Ported to Go from klippy/extras/led.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2019-2025  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package led

import (
	"log"
	"math"
	"slices"
	"strconv"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/output_pin"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/jinja"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/pins"
	"printdeck/internal/klipper/reactor"
	"printdeck/internal/klipper/toolhead"
)

// Color is the red, green, blue and white of an LED, from 0 to 1. A template can give a fifth
// value, as in Klipper.
type Color []float64

// UpdateFunc sends the colors of the LEDs at a print time (NaN for now).
type UpdateFunc func(ledState []Color, printTime float64)

// LEDHelper is the common LED initialization and control.
type LEDHelper struct {
	printer      *host.Printer
	mutex        *reactor.Mutex
	updateFunc   UpdateFunc
	ledCount     int
	needTransmit bool
	ledState     []Color
	templateEval *output_pin.PrinterTemplateEvaluator
	tcallbacks   []func(text string)
}

type tkey struct {
	h     *LEDHelper
	index int
}

func NewLEDHelper(config *configfile.Wrapper, updateFunc UpdateFunc, ledCount int) *LEDHelper {
	h := &LEDHelper{printer: host.Of(config), updateFunc: updateFunc, ledCount: ledCount}
	h.mutex = h.printer.Reactor().NewMutex(false)
	h.needTransmit = false
	// Initial color
	get := func(name string) float64 {
		return config.GetFloat(name, configfile.Default(0.), configfile.Min(0.), configfile.Max(1.))
	}
	red := get("initial_RED")
	green := get("initial_GREEN")
	blue := get("initial_BLUE")
	white := get("initial_WHITE")
	for range ledCount {
		h.ledState = append(h.ledState, Color{red, green, blue, white})
	}
	// Support setting an led template
	h.templateEval = output_pin.LookupTemplateEval(config)
	for i := range ledCount {
		index := i + 1
		h.tcallbacks = append(h.tcallbacks, func(text string) { h.templateUpdate(index, text) })
	}
	// Register commands
	parts := strings.Fields(config.Name())
	name := parts[len(parts)-1]
	g := gcode.Of(h.printer)
	g.RegisterMuxCommand("SET_LED", "LED", &name, h.cmdSetLED, "Set the color of an LED")
	g.RegisterMuxCommand("SET_LED_TEMPLATE", "LED", &name, h.cmdSetLEDTemplate,
		"Assign a display_template to an LED")
	return h
}

// LEDState returns the colors of the LEDs.
func (h *LEDHelper) LEDState() []Color { return h.ledState }

func (h *LEDHelper) Status(eventtime float64) map[string]any {
	data := make([]any, len(h.ledState))
	for i, c := range h.ledState {
		t := make(jinja.Tuple, len(c))
		for j, v := range c {
			t[j] = v
		}
		data[i] = t
	}
	return map[string]any{"color_data": data}
}

// setColor sets the color of an LED (index from 1), or of all LEDs with index 0.
func (h *LEDHelper) setColor(index int, color Color) {
	var newLEDState []Color
	if index == 0 {
		for range h.ledCount {
			newLEDState = append(newLEDState, color)
		}
		if slices.EqualFunc(h.ledState, newLEDState, func(a, b Color) bool { return slices.Equal(a, b) }) {
			return
		}
	} else {
		if slices.Equal(h.ledState[index-1], color) {
			return
		}
		newLEDState = slices.Clone(h.ledState)
		newLEDState[index-1] = color
	}
	h.ledState = newLEDState
	h.needTransmit = true
}

// pyClamp is max(0., min(1., v)) with the NaN results of Python's min and max.
func pyClamp(v float64) float64 {
	if !(v < 1.) {
		v = 1. // min(1., v) keeps 1. unless v < 1.
	}
	if 0. > v || math.IsNaN(v) {
		return 0.
	}
	return v
}

func (h *LEDHelper) templateUpdate(index int, text string) {
	var parts []float64
	for _, f := range strings.SplitN(text, ",", 5) {
		v, err := strconv.ParseFloat(strings.TrimSpace(f), 64)
		if err != nil {
			log.Printf("led template render error: %v", err)
			parts = nil
			break
		}
		parts = append(parts, pyClamp(v))
	}
	for len(parts) < 4 {
		parts = append(parts, 0.)
	}
	h.setColor(index, Color(parts))
}

// CheckTransmit sends the colors when they changed, at a print time (NaN for now).
func (h *LEDHelper) CheckTransmit(printTime float64) {
	if !h.needTransmit {
		return
	}
	// Just avoid any race conditions
	ledState := h.ledState
	h.needTransmit = false
	reactorCB := func(eventtime float64) any {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(*gcode.CommandError); !ok {
					panic(r)
				}
				log.Printf("led update transmit error: %v", r)
			}
		}()
		h.mutex.Lock()
		defer h.mutex.Unlock()
		h.updateFunc(ledState, printTime)
		return nil
	}
	h.printer.Reactor().RegisterCallback(reactorCB, reactor.Now)
}

func (h *LEDHelper) cmdSetLED(gcmd *gcode.Command) {
	// Parse parameters
	get := func(name string) float64 {
		return gcmd.GetFloat(name, gcode.Default(0.), gcode.Min(0.), gcode.Max(1.))
	}
	red := get("RED")
	green := get("GREEN")
	blue := get("BLUE")
	white := get("WHITE")
	index := 0
	if _, ok := gcmd.Get("INDEX", gcode.Default(nil)); ok {
		index = gcmd.GetInt("INDEX", gcode.Min(1), gcode.Max(float64(h.ledCount)))
	}
	transmit := gcmd.GetInt("TRANSMIT", gcode.Default(1))
	sync := gcmd.GetInt("SYNC", gcode.Default(1))
	color := Color{red, green, blue, white}
	// Update and transmit data
	lookaheadBgfunc := func(printTime float64) {
		h.setColor(index, color)
		if transmit != 0 {
			h.CheckTransmit(printTime)
		}
	}
	if sync != 0 {
		//Sync LED Update with print time and send
		toolhead.Of(h.printer).RegisterLookaheadCallback(lookaheadBgfunc)
	} else {
		//Send update now (so as not to wake toolhead and reset idle_timeout)
		lookaheadBgfunc(math.NaN())
	}
}

func (h *LEDHelper) cmdSetLEDTemplate(gcmd *gcode.Command) {
	flush := func() { h.CheckTransmit(math.NaN()) }
	if _, ok := gcmd.Get("INDEX", gcode.Default(nil)); ok {
		index := gcmd.GetInt("INDEX", gcode.Min(1), gcode.Max(float64(h.ledCount)))
		h.templateEval.SetTemplate(gcmd, tkey{h, index}, h.tcallbacks[index-1], flush, h)
	} else {
		for i := range h.ledCount {
			h.templateEval.SetTemplate(gcmd, tkey{h, i + 1}, h.tcallbacks[i], flush, h)
		}
	}
}

// ######################################################################
// Handler for PWM controlled LEDs
// ######################################################################

type pwmPin struct {
	idx int
	pin mcu.PWMPin
}

// PrinterPWMLED is an [led] section.
type PrinterPWMLED struct {
	printer       *host.Printer
	pins          []pwmPin
	lastPrintTime float64
	ledHelper     *LEDHelper
	prevColor     Color
}

func loadPrefix(config *configfile.Wrapper) any {
	p := &PrinterPWMLED{printer: host.Of(config)}
	// Configure pwm pins
	ppins := p.printer.LookupObject("pins").(*pins.Pins)
	maxDuration := p.printer.LookupObject("mcu").(*mcu.MCU).MaxNominalDuration()
	cycleTime := config.GetFloat("cycle_time", configfile.Default(0.010), configfile.Above(0.),
		configfile.Max(maxDuration))
	hardwarePWM := config.GetBool("hardware_pwm", configfile.Default(false))
	for i, name := range []string{"red", "green", "blue", "white"} {
		pinName, ok := config.GetOptional(name+"_pin", configfile.Default(nil))
		if !ok {
			continue
		}
		mcuPin := ppins.SetupPin("pwm", pinName).(mcu.PWMPin)
		mcuPin.SetupMaxDuration(0.)
		mcuPin.SetupCycleTime(cycleTime, hardwarePWM)
		p.pins = append(p.pins, pwmPin{i, mcuPin})
	}
	if len(p.pins) == 0 {
		configfile.Raise("No LED pin definitions found in '%s'", config.Name())
	}
	p.lastPrintTime = 0.
	// Initialize color data
	p.ledHelper = NewLEDHelper(config, p.updateLEDs, 1)
	color := p.ledHelper.LEDState()[0]
	p.prevColor = color
	for _, pp := range p.pins {
		pp.pin.SetupStartValue(color[pp.idx], 0.)
	}
	return p
}

func init() { host.RegisterModule("led", nil, loadPrefix) }

func (p *PrinterPWMLED) updateLEDs(ledState []Color, printTime float64) {
	m := p.pins[0].pin.MCU()
	minSchedTime := m.MinScheduleTime()
	if math.IsNaN(printTime) {
		eventtime := p.printer.Reactor().Monotonic()
		printTime = m.EstimatedPrintTime(eventtime) + minSchedTime
	}
	printTime = math.Max(printTime, p.lastPrintTime+minSchedTime)
	color := ledState[0]
	for _, pp := range p.pins {
		if p.prevColor[pp.idx] != color[pp.idx] {
			pp.pin.SetPWM(printTime, color[pp.idx])
			p.lastPrintTime = printTime
		}
	}
	p.prevColor = color
}

func (p *PrinterPWMLED) Status(eventtime float64) map[string]any {
	return p.ledHelper.Status(eventtime)
}
