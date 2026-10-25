// Package gcode_macro has the G-code macros of the config ([gcode_macro NAME]) and the templates
// of the other modules: the template context with the printer status, the action functions, and
// the macro variables.
//
// Ported to Go from klippy/extras/gcode_macro.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f. The templates use the jinja package in place
// of Jinja2.
//
//	Copyright (C) 2018-2021  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package gcode_macro

import (
	"fmt"
	"log"
	"reflect"
	"sort"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/jinja"
	"printdeck/internal/klipper/webhooks"
)

// Template handling

// statuser is a printer object with a status.
type statuser interface {
	Status(eventtime float64) map[string]any
}

// GetStatusWrapper is the "printer" of the templates: the status of each printer object.
type GetStatusWrapper struct {
	printer   *host.Printer
	eventtime float64
	hasTime   bool
	cache     map[string]any
}

func NewGetStatusWrapper(printer *host.Printer) *GetStatusWrapper {
	return &GetStatusWrapper{printer: printer, cache: map[string]any{}}
}

// Attr is the attribute of the wrapper. It has none: printer.name is the item name.
func (w *GetStatusWrapper) Attr(name string) (any, bool) { return nil, false }

// Item returns the status of a printer object.
func (w *GetStatusWrapper) Item(key any) (any, bool) {
	sval := strings.TrimSpace(jinja.Str(key))
	if v, ok := w.cache[sval]; ok {
		return v, true
	}
	po, ok := w.printer.TryLookupObject(sval).(statuser)
	if !ok {
		return nil, false
	}
	if !w.hasTime {
		w.eventtime, w.hasTime = w.printer.Reactor().Monotonic(), true
	}
	var sts map[string]any
	w.printer.Reactor().AssertNoPause(func() { sts = po.Status(w.eventtime) })
	res := ToJinja(sts)
	w.cache[sval] = res
	return res, true
}

// newGetStatusWrapperAt returns a wrapper that gets the status at eventtime.
func newGetStatusWrapperAt(printer *host.Printer, eventtime float64) *GetStatusWrapper {
	return &GetStatusWrapper{printer: printer, eventtime: eventtime, hasTime: true, cache: map[string]any{}}
}

// Iter lists the names of the objects with a status.
func (w *GetStatusWrapper) Iter() []any {
	var out []any
	for _, o := range w.printer.LookupObjects("") {
		if _, ok := o.Obj.(statuser); ok {
			out = append(out, o.Name)
		}
	}
	return out
}

func (w *GetStatusWrapper) PyStr() string { return "<GetStatusWrapper>" }

// Coord is a position of the status: a tuple with the attributes x, y, z and e.
type Coord struct{ jinja.Tuple }

func (c Coord) Attr(name string) (any, bool) {
	i := strings.Index("xyze", name)
	if len(name) != 1 || i < 0 || i >= len(c.Tuple) {
		return nil, false
	}
	return c.Tuple[i], true
}

func (c Coord) Item(key any) (any, bool) {
	if n, ok := key.(int64); ok {
		if n < 0 {
			n += int64(len(c.Tuple))
		}
		if n >= 0 && n < int64(len(c.Tuple)) {
			return c.Tuple[n], true
		}
	}
	return nil, false
}

func (c Coord) Iter() []any   { return c.Tuple }
func (c Coord) PyStr() string { return jinja.Repr(c.Tuple) }

// ToJinja converts a status value to the values of the templates: maps are dicts (in key order),
// slices are lists, and a gcode.Coord is a Coord.
func ToJinja(v any) any {
	switch x := v.(type) {
	case nil, bool, int64, float64, string, *jinja.Dict, jinja.Tuple, jinja.Func, jinja.Object:
		return x
	case int:
		return int64(x)
	case gcode.Coord:
		t := make(jinja.Tuple, len(x))
		for i, f := range x {
			t[i] = f
		}
		return Coord{t}
	case map[string]any:
		d := jinja.NewDict()
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			d.Set(k, ToJinja(x[k]))
		}
		return d
	case []any:
		out := make([]any, len(x))
		for i, it := range x {
			out[i] = ToJinja(it)
		}
		return out
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map:
		d := jinja.NewDict()
		keys := rv.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return fmt.Sprint(keys[i]) < fmt.Sprint(keys[j]) })
		for _, k := range keys {
			d.Set(fmt.Sprint(k.Interface()), ToJinja(rv.MapIndex(k).Interface()))
		}
		return d
	case reflect.Slice, reflect.Array:
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = ToJinja(rv.Index(i).Interface())
		}
		return out
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int64(rv.Uint())
	case reflect.Float32, reflect.Float64:
		return rv.Float()
	case reflect.String:
		return rv.String()
	case reflect.Bool:
		return rv.Bool()
	}
	return fmt.Sprint(v)
}

// TemplateWrapper is a template of the config.
type TemplateWrapper struct {
	printer       *host.Printer
	name          string
	gcode         *gcode.Dispatch
	createContext func() map[string]any
	template      *jinja.Template
}

func newTemplateWrapper(printer *host.Printer, pgm *PrinterGCodeMacro, name, script string) *TemplateWrapper {
	tw := &TemplateWrapper{printer: printer, name: name, gcode: gcode.Of(printer), createContext: pgm.CreateTemplateContext}
	t, err := jinja.Parse(script)
	if err != nil {
		if se, ok := err.(*jinja.SyntaxError); ok {
			lines := strings.Split(script, "\n")
			line := ""
			if se.Line >= 1 && se.Line <= len(lines) {
				line = lines[se.Line-1]
			}
			msg := fmt.Sprintf("Error loading template '%s'\nline %d: %s # %s", name, se.Line, line, se.Msg)
			log.Print(msg)
			panic(gcode.Errorf("%s", msg))
		}
		configfile.Raise("Error loading template '%s': %v", name, err)
	}
	tw.template = t
	return tw
}

// pyTypeName is the type of an error in the messages, as traceback.format_exception_only writes
// it.
func pyTypeName(t string) string {
	switch t {
	case "UndefinedError", "TemplateRuntimeError", "FilterArgumentError":
		return "jinja2.exceptions." + t
	}
	return t
}

// Render renders the template. A nil context is the context of CreateTemplateContext. An error
// is a command error.
func (tw *TemplateWrapper) Render(context map[string]any) string {
	if context == nil {
		context = tw.createContext()
	}
	out, err := tw.template.Render(context)
	if err != nil {
		msg := err.Error()
		if e, ok := err.(*jinja.Error); ok {
			msg = pyTypeName(e.Type)
			if e.Msg != "" {
				msg += ": " + e.Msg
			}
		}
		msg = fmt.Sprintf("Error evaluating '%s': %s\n", tw.name, msg)
		log.Print(msg)
		panic(gcode.Errorf("%s", msg))
	}
	return out
}

// CreateTemplateContext returns the context of CreateTemplateContextAt of the template.
func (tw *TemplateWrapper) CreateTemplateContext(eventtime float64) map[string]any {
	ctx := tw.createContext()
	ctx["printer"] = newGetStatusWrapperAt(tw.printer, eventtime)
	return ctx
}

// CreateContext returns the context of the template with no event time (create_template_context
// with no eventtime).
func (tw *TemplateWrapper) CreateContext() map[string]any { return tw.createContext() }

// RunGcodeFromCommand renders the template and runs it from a command handler.
func (tw *TemplateWrapper) RunGcodeFromCommand(context map[string]any) {
	tw.gcode.RunScriptFromCommand(tw.Render(context))
}

// Main gcode macro template tracking

// PrinterGCodeMacro loads the templates.
type PrinterGCodeMacro struct{ printer *host.Printer }

func load(config *configfile.Wrapper) any { return &PrinterGCodeMacro{printer: host.Of(config)} }

// Load returns the gcode_macro object, and loads it the first time.
func Load(config *configfile.Wrapper) *PrinterGCodeMacro {
	return host.Of(config).LoadObject(config, "gcode_macro").(*PrinterGCodeMacro)
}

// LoadTemplate loads the template of an option. def is the default script, or nil for none.
func (p *PrinterGCodeMacro) LoadTemplate(config *configfile.Wrapper, option string, def *string) *TemplateWrapper {
	name := fmt.Sprintf("%s:%s", config.Name(), option)
	var script string
	if def == nil {
		script = config.Get(option)
	} else {
		script = config.Get(option, configfile.Default(*def))
	}
	return newTemplateWrapper(p.printer, p, name, script)
}

// commandError is a command error as a template error, so the message has its Python type.
type commandError struct{ *gcode.CommandError }

func (e commandError) PyType() string { return "gcode.CommandError" }

func strArg(args []any, kwargs map[string]any, i int, name, def string) string {
	if i < len(args) {
		return jinja.Str(args[i])
	}
	if v, ok := kwargs[name]; ok {
		return jinja.Str(v)
	}
	return def
}

// CreateTemplateContext returns the context of a template: the printer status and the action
// functions.
func (p *PrinterGCodeMacro) CreateTemplateContext() map[string]any {
	return map[string]any{
		"printer": NewGetStatusWrapper(p.printer),
		"action_emergency_stop": jinja.Func(func(args []any, kwargs map[string]any) any {
			msg := strArg(args, kwargs, 0, "msg", "action_emergency_stop")
			p.printer.InvokeShutdown("Shutdown due to "+msg, nil)
			return ""
		}),
		"action_respond_info": jinja.Func(func(args []any, kwargs map[string]any) any {
			gcode.Of(p.printer).RespondInfo(strArg(args, kwargs, 0, "msg", ""), true)
			return ""
		}),
		"action_raise_error": jinja.Func(func(args []any, kwargs map[string]any) any {
			panic(commandError{gcode.Errorf("%s", strArg(args, kwargs, 0, "msg", ""))})
		}),
		"action_call_remote_method": jinja.Func(func(args []any, kwargs map[string]any) any {
			method := strArg(args, nil, 0, "method", "")
			func() {
				defer func() {
					if r := recover(); r != nil {
						if _, ok := r.(*gcode.CommandError); !ok {
							panic(r)
						}
						log.Printf("Remote Call Error: %v", r)
					}
				}()
				webhooks.Of(p.printer).CallRemoteMethod(method, kwargs)
			}()
			return ""
		}),
	}
}

// CreateTemplateContextAt returns the context of CreateTemplateContext, with the printer status at
// eventtime.
func (p *PrinterGCodeMacro) CreateTemplateContextAt(eventtime float64) map[string]any {
	ctx := p.CreateTemplateContext()
	ctx["printer"] = newGetStatusWrapperAt(p.printer, eventtime)
	return ctx
}

func init() { host.RegisterModule("gcode_macro", load, loadPrefix) }

// GCode macro

// GCodeMacro is a macro of the config ([gcode_macro NAME]).
type GCodeMacro struct {
	alias          string
	printer        *host.Printer
	template       *TemplateWrapper
	gcode          *gcode.Dispatch
	renameExisting string
	cmdDesc        string
	inScript       bool
	variables      *jinja.Dict
}

func loadPrefix(config *configfile.Wrapper) any {
	f := strings.Fields(config.Name())
	if len(f) > 2 {
		configfile.Raise("Name of section '%s' contains illegal whitespace", config.Name())
	}
	name := f[1]
	printer := host.Of(config)
	m := &GCodeMacro{alias: strings.ToUpper(name), printer: printer, variables: jinja.NewDict()}
	m.template = Load(config).LoadTemplate(config, "gcode", nil)
	m.gcode = gcode.Of(printer)
	m.renameExisting, _ = config.GetOptional("rename_existing", configfile.Default(nil))
	m.cmdDesc = config.Get("description", configfile.Default("G-Code macro"))
	if m.renameExisting != "" {
		if gcode.IsTraditionalGcode(m.alias) != gcode.IsTraditionalGcode(m.renameExisting) {
			configfile.Raise("G-Code macro rename of different types ('%s' vs '%s')", m.alias, m.renameExisting)
		}
		printer.RegisterEventHandler("klippy:connect", func(...any) { m.handleConnect() })
	} else {
		m.gcode.RegisterCommand(m.alias, m.cmd, false, m.cmdDesc)
	}
	m.gcode.RegisterMuxCommand("SET_GCODE_VARIABLE", "MACRO", &name, m.cmdSetGcodeVariable,
		"Set the value of a G-Code macro variable")
	const prefix = "variable_"
	for _, option := range config.PrefixOptions(prefix) {
		literal, err := jinja.LiteralEval(config.Get(option))
		if err != nil {
			configfile.Raise("Option '%s' in section '%s' is not a valid literal: %v", option, config.Name(), err)
		}
		m.variables.Set(option[len(prefix):], literal)
	}
	return m
}

func (m *GCodeMacro) handleConnect() {
	prev := m.gcode.UnregisterCommand(m.alias)
	if prev == nil {
		configfile.Raise("Existing command '%s' not found in gcode_macro rename", m.alias)
	}
	m.gcode.RegisterCommand(m.renameExisting, prev, false, fmt.Sprintf("Renamed builtin of '%s'", m.alias))
	m.gcode.RegisterCommand(m.alias, m.cmd, false, m.cmdDesc)
}

// Status returns the macro variables.
func (m *GCodeMacro) Status(eventtime float64) map[string]any {
	out := map[string]any{}
	for _, k := range m.variables.Keys() {
		out[k], _ = m.variables.Get(k)
	}
	return out
}

func (m *GCodeMacro) cmdSetGcodeVariable(gcmd *gcode.Command) {
	variable := gcmd.GetString("VARIABLE")
	value := gcmd.GetString("VALUE")
	if _, ok := m.variables.Get(variable); !ok {
		panic(gcmd.Error("Unknown gcode_macro variable '%s'", variable))
	}
	literal, err := jinja.LiteralEval(value)
	if err != nil {
		panic(gcmd.Error("Unable to parse '%s' as a literal: %v in '%s'", value, err, gcmd.Commandline()))
	}
	v := jinja.NewDict()
	for _, k := range m.variables.Keys() {
		val, _ := m.variables.Get(k)
		v.Set(k, val)
	}
	v.Set(variable, literal)
	m.variables = v
}

func (m *GCodeMacro) cmd(gcmd *gcode.Command) {
	if m.inScript {
		panic(gcmd.Error("Macro %s called recursively", m.alias))
	}
	kwparams := map[string]any{}
	for _, k := range m.variables.Keys() {
		kwparams[k], _ = m.variables.Get(k)
	}
	for k, v := range m.template.createContext() {
		kwparams[k] = v
	}
	params := jinja.NewDict()
	keys := make([]string, 0, len(gcmd.Params()))
	for k := range gcmd.Params() {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		params.Set(k, gcmd.Params()[k])
	}
	kwparams["params"] = params
	kwparams["rawparams"] = gcmd.RawParams()
	m.inScript = true
	defer func() { m.inScript = false }()
	m.template.RunGcodeFromCommand(kwparams)
}
