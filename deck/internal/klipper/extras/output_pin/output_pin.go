// Ported to Go from klippy/extras/output_pin.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2017-2025  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package output_pin

import (
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/display"
	"printdeck/internal/klipper/extras/gcode_macro"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/jinja"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/pins"
	"printdeck/internal/klipper/reactor"
)

// ######################################################################
// Template evaluation helper
// ######################################################################

// Time between each template update
const RenderTime = 0.500

type activeTemplate struct {
	key           any
	uid           string // "" when the parameters are not static
	template      *display.DisplayTemplate
	lparams       map[string]any
	callback      func(text string)
	flushCallback func()
	flushKey      any
}

// PrinterTemplateEvaluator renders the display templates of SET_PIN TEMPLATE and SET_LED
// TEMPLATE every RenderTime.
type PrinterTemplateEvaluator struct {
	printer         *host.Printer
	activeTemplates []*activeTemplate // in the order of their first activation (a dict)
	renderTimer     *reactor.Timer
	templates       map[string]*display.DisplayTemplate
	gcodeMacro      *gcode_macro.PrinterGCodeMacro
}

func newPrinterTemplateEvaluator(config *configfile.Wrapper) *PrinterTemplateEvaluator {
	te := &PrinterTemplateEvaluator{printer: host.Of(config)}
	// Load templates
	dtemplates := display.LookupDisplayTemplates(config)
	te.templates = dtemplates.DisplayTemplates()
	te.gcodeMacro = gcode_macro.Load(config)
	return te
}

func (te *PrinterTemplateEvaluator) activateTimer() {
	if te.renderTimer != nil || len(te.activeTemplates) == 0 {
		return
	}
	rc := te.printer.Reactor()
	te.renderTimer = rc.RegisterTimer(te.render, reactor.Now)
}

// staticUID returns the id of a template and its parameters, or "" when a parameter is a list or
// a dict (not hashable in Python).
func staticUID(template *display.DisplayTemplate, lparams map[string]any) string {
	keys := make([]string, 0, len(lparams))
	for k, v := range lparams {
		switch v.(type) {
		case []any, map[string]any, map[any]any:
			// lparams is not static, so disable caching
			return ""
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	fmt.Fprintf(&b, "%p", template)
	for _, k := range keys {
		fmt.Fprintf(&b, "\x00%s=%s", k, jinja.Repr(lparams[k]))
	}
	return b.String()
}

func (te *PrinterTemplateEvaluator) activateTemplate(key any, callback func(string),
	template *display.DisplayTemplate, lparams map[string]any, flushCallback func(), flushKey any) {
	idx := -1
	for i, at := range te.activeTemplates {
		if at.key == key {
			idx = i
		}
	}
	if template != nil {
		// Build a unique id to make it possible to cache duplicate rendering
		at := &activeTemplate{key, staticUID(template, lparams), template, lparams, callback,
			flushCallback, flushKey}
		if idx >= 0 {
			te.activeTemplates[idx] = at
		} else {
			te.activeTemplates = append(te.activeTemplates, at)
		}
		return
	}
	if idx >= 0 {
		te.activeTemplates = append(te.activeTemplates[:idx], te.activeTemplates[idx+1:]...)
	}
}

func (te *PrinterTemplateEvaluator) render(eventtime float64) float64 {
	if len(te.activeTemplates) == 0 {
		// Nothing to do - unregister timer
		rc := te.printer.Reactor()
		rc.UnregisterTimer(te.renderTimer)
		te.renderTimer = nil
		return reactor.Never
	}
	// Setup gcode_macro template context
	context := te.gcodeMacro.CreateTemplateContextAt(eventtime)
	context["render"] = jinja.Func(func(args []any, kwargs map[string]any) any {
		if len(args) != 1 {
			panic(&jinja.Error{Type: "TypeError", Msg: "render() takes 1 positional argument"})
		}
		t, ok := te.templates[jinja.Str(args[0])]
		if !ok {
			panic(&jinja.Error{Type: "KeyError", Msg: jinja.Repr(args[0])})
		}
		return t.Render(context, kwargs)
	})
	// Render all templates
	var flushCallbacks []func()
	flushSeen := map[any]bool{}
	renderCache := map[string]string{}
	for _, at := range append([]*activeTemplate(nil), te.activeTemplates...) {
		text, ok := "", false
		if at.uid != "" {
			text, ok = renderCache[at.uid]
		}
		if !ok {
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("display template render error: %v", r)
						text = ""
					}
				}()
				text = at.template.Render(context, at.lparams)
			}()
			if at.uid != "" {
				renderCache[at.uid] = text
			}
		}
		if at.flushCallback != nil {
			if !flushSeen[at.flushKey] {
				flushSeen[at.flushKey] = true
				flushCallbacks = append(flushCallbacks, at.flushCallback)
			}
		}
		at.callback(text)
	}
	// Invoke optional flush callbacks
	for _, fc := range flushCallbacks {
		fc()
	}
	return eventtime + RenderTime
}

// SetTemplate starts or stops the template of a command (TEMPLATE=name PARAM_x=value) for the
// object key. callback gets each text. flushCallback runs once after each render for each
// flushKey, or is nil.
func (te *PrinterTemplateEvaluator) SetTemplate(gcmd *gcode.Command, key any, callback func(string),
	flushCallback func(), flushKey any) {
	var template *display.DisplayTemplate
	lparams := map[string]any{}
	tplName := gcmd.GetString("TEMPLATE")
	if tplName != "" {
		var ok bool
		template, ok = te.templates[tplName]
		if !ok {
			panic(gcmd.Error("Unknown display_template '%s'", tplName))
		}
		tparams := template.Params()
		params := gcmd.Params()
		for _, p := range gcmd.ParamNames() {
			v := params[p]
			if !strings.HasPrefix(p, "PARAM_") {
				continue
			}
			p = strings.ToLower(p)
			if _, ok := tparams[p]; !ok {
				panic(gcmd.Error("Invalid display_template parameter: %s", p))
			}
			lv, err := jinja.LiteralEval(v)
			if err != nil {
				if le, ok := err.(*jinja.LiteralError); ok && le.Type == "ValueError" {
					panic(gcmd.Error("Unable to parse '%s' as a literal", v))
				}
				panic(err)
			}
			lparams[p] = lv
		}
	}
	te.activateTemplate(key, callback, template, lparams, flushCallback, flushKey)
	te.activateTimer()
}

// LookupTemplateEval returns the template evaluator of the printer.
func LookupTemplateEval(config *configfile.Wrapper) *PrinterTemplateEvaluator {
	printer := host.Of(config)
	te, ok := printer.TryLookupObject("template_evaluator").(*PrinterTemplateEvaluator)
	if !ok {
		te = newPrinterTemplateEvaluator(config)
		printer.AddObject("template_evaluator", te)
	}
	return te
}

// ######################################################################
// Main output pin handling
// ######################################################################

type outputPin interface {
	MCU() *mcu.MCU
	SetupMaxDuration(maxDuration float64)
	SetupStartValue(startValue, shutdownValue float64)
}

// PrinterOutputPin is an [output_pin] section.
type PrinterOutputPin struct {
	printer       *host.Printer
	isPWM         bool
	mcuPin        outputPin
	scale         float64
	lastValue     float64
	shutdownValue float64
	gcrq          *GCodeRequestQueue
	templateEval  *PrinterTemplateEvaluator
}

func loadPrefix(config *configfile.Wrapper) any {
	p := &PrinterOutputPin{printer: host.Of(config)}
	ppins := p.printer.LookupObject("pins").(*pins.Pins)
	// Determine pin type
	p.isPWM = config.GetBool("pwm", configfile.Default(false))
	if p.isPWM {
		pwm := ppins.SetupPin("pwm", config.Get("pin")).(*mcu.PWM)
		p.mcuPin = pwm
		maxDuration := pwm.MCU().MaxNominalDuration()
		cycleTime := config.GetFloat("cycle_time", configfile.Default(0.100), configfile.Above(0.),
			configfile.Max(maxDuration))
		hardwarePWM := config.GetBool("hardware_pwm", configfile.Default(false))
		pwm.SetupCycleTime(cycleTime, hardwarePWM)
		p.scale = config.GetFloat("scale", configfile.Default(1.), configfile.Above(0.))
	} else {
		p.mcuPin = ppins.SetupPin("digital_out", config.Get("pin")).(*mcu.DigitalOut)
		p.scale = 1.
	}
	p.mcuPin.SetupMaxDuration(0.)
	// Determine start and shutdown values
	p.lastValue = config.GetFloat("value", configfile.Default(0.), configfile.Min(0.),
		configfile.Max(p.scale)) / p.scale
	p.shutdownValue = config.GetFloat("shutdown_value", configfile.Default(0.), configfile.Min(0.),
		configfile.Max(p.scale)) / p.scale
	p.mcuPin.SetupStartValue(p.lastValue, p.shutdownValue)
	// Create gcode request queue
	p.gcrq = NewGCodeRequestQueue(config, p.mcuPin.MCU(), p.setPin)
	// Template handling
	p.templateEval = LookupTemplateEval(config)
	// Register commands
	pinName := strings.Fields(config.Name())[1]
	g := gcode.Of(p.printer)
	g.RegisterMuxCommand("SET_PIN", "PIN", &pinName, p.cmdSetPin, "Set the value of an output pin")
	return p
}

func init() { host.RegisterModule("output_pin", nil, loadPrefix) }

func (p *PrinterOutputPin) Status(eventtime float64) map[string]any {
	return map[string]any{"value": p.lastValue}
}

func (p *PrinterOutputPin) setPin(printTime, value float64) (string, float64) {
	if value == p.lastValue {
		return ActionDiscard, 0.
	}
	p.lastValue = value
	if p.isPWM {
		p.mcuPin.(*mcu.PWM).SetPWM(printTime, value)
	} else {
		p.mcuPin.(*mcu.DigitalOut).SetDigital(printTime, value != 0)
	}
	return ActionNone, 0.
}

func (p *PrinterOutputPin) templateUpdate(text string) {
	value, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil {
		log.Printf("output_pin template render error: %v", err)
		value = 0.
	}
	p.gcrq.SendAsyncRequest(value, math.NaN())
}

func (p *PrinterOutputPin) cmdSetPin(gcmd *gcode.Command) {
	value, hasValue := gcmd.GetFloatOptional("VALUE", gcode.Min(0.), gcode.Max(p.scale))
	_, hasTemplate := gcmd.Get("TEMPLATE", gcode.Default(nil))
	if hasValue == hasTemplate {
		panic(gcmd.Error("SET_PIN command must specify VALUE or TEMPLATE"))
	}
	// Check for template setting
	if hasTemplate {
		p.templateEval.SetTemplate(gcmd, p, p.templateUpdate, nil, nil)
		return
	}
	// Read requested value
	value /= p.scale
	if !p.isPWM && value != 0. && value != 1. {
		panic(gcmd.Error("Invalid pin value"))
	}
	// Queue requested value
	p.gcrq.QueueGcodeRequest(value)
}
