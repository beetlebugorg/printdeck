// Package host is the core of the Klipper host: the Printer, which holds the printer objects,
// sends the events between them, and loads them from the config.
//
// Ported to Go from klippy/klippy.py of Klipper (https://github.com/Klipper3d/klipper), commit
// 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f. The command line of klippy.py is in the deck's
// command.
//
// Klipper's exceptions are panics with a typed error value in the port: *configfile.Error,
// *pins.Error, *mcu.Error, *gcode.CommandError. The Printer recovers them at the same places
// where klippy.py catches the exceptions.
//
//	Copyright (C) 2016-2026  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package host

import (
	"fmt"
	"log"
	"runtime/debug"
	"strings"
	"sync"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/reactor"
)

const (
	MessageReady   = "Printer is ready"
	MessageStartup = "\nPrinter is not ready\nThe klippy host software is attempting to connect.  Please\n" +
		"retry in a few moments.\n"
	MessageRestart = "\nOnce the underlying issue is corrected, use the \"RESTART\"\n" +
		"command to reload the config and restart the host software.\nPrinter is halted\n"
)

// LoadFunc makes the printer object of a config section.
type LoadFunc func(config *configfile.Wrapper) any

type module struct{ load, loadPrefix LoadFunc }

var (
	modulesMu sync.Mutex
	modules   = map[string]module{}
)

// RegisterModule adds a module of klippy/extras. load makes the object of a section with the
// module name ("[fan]"), and loadPrefix the object of a section with a name after the module
// name ("[heater_fan my_fan]"). Each can be nil. Call it from an init function.
func RegisterModule(name string, load, loadPrefix LoadFunc) {
	modulesMu.Lock()
	defer modulesMu.Unlock()
	if _, ok := modules[name]; ok {
		panic("host: module " + name + " registered twice")
	}
	modules[name] = module{load, loadPrefix}
}

func lookupModule(name string) (module, bool) {
	modulesMu.Lock()
	defer modulesMu.Unlock()
	m, ok := modules[name]
	return m, ok
}

// EarlyObjects are the functions that add the objects that the config load needs (gcode and
// webhooks). Their packages set them in an init function.
var EarlyObjects []func(p *Printer)

// ConfigObjects are the functions that add the objects of the config before the config sections
// (pins and mcu), and AfterConfigObjects the functions after the config sections (toolhead).
var ConfigObjects, AfterConfigObjects []func(config *configfile.Wrapper)

// StartArgs are the start arguments (start_args): "config_file", "debuginput", "debugoutput",
// "dictionary", "start_reason", and others.
type StartArgs map[string]any

// Get returns a start argument as a string, and false when it is not set.
func (a StartArgs) Get(name string) (string, bool) {
	v, ok := a[name]
	if !ok || v == nil {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

type object struct {
	name string
	obj  any
}

// Printer is the printer (the Printer class of klippy.py).
type Printer struct {
	startArgs      StartArgs
	reactor        *reactor.Reactor
	stateMessage   string
	inShutdown     bool
	runResult      string
	eventHandlers  map[string][]func(args ...any)
	objects        []object
	objectIndex    map[string]int
	configFileName string
}

// New makes a printer, and registers its connect callback.
func New(r *reactor.Reactor, startArgs StartArgs) *Printer {
	p := &Printer{startArgs: startArgs, reactor: r, stateMessage: MessageStartup,
		eventHandlers: map[string][]func(args ...any){}, objectIndex: map[string]int{}}
	r.RegisterCallback(func(float64) any { p.connect(); return nil }, reactor.Now)
	// Init printer components that must be setup prior to config
	for _, f := range EarlyObjects {
		f(p)
	}
	return p
}

// Of returns the printer of a config.
func Of(config *configfile.Wrapper) *Printer { return config.Printer().(*Printer) }

func (p *Printer) StartArgs() StartArgs      { return p.startArgs }
func (p *Printer) Reactor() *reactor.Reactor { return p.reactor }
func (p *Printer) IsShutdown() bool          { return p.inShutdown }

// StateMessage returns the state message and its category: "ready", "startup", "shutdown" or
// "error".
func (p *Printer) StateMessage() (string, string) {
	switch {
	case p.stateMessage == MessageReady:
		return p.stateMessage, "ready"
	case p.stateMessage == MessageStartup:
		return p.stateMessage, "startup"
	case p.inShutdown:
		return p.stateMessage, "shutdown"
	}
	return p.stateMessage, "error"
}

func (p *Printer) setState(msg string) {
	if p.stateMessage == MessageReady || p.stateMessage == MessageStartup {
		p.stateMessage = msg
	}
	if _, ok := p.startArgs.Get("debuginput"); msg != MessageReady && ok {
		p.RequestExit("error_exit")
	}
}

// UpdateErrorMsg changes an error message.
func (p *Printer) UpdateErrorMsg(oldmsg, newmsg string) {
	if p.stateMessage != oldmsg || p.stateMessage == MessageReady || p.stateMessage == MessageStartup ||
		newmsg == MessageReady || newmsg == MessageStartup {
		return
	}
	p.stateMessage = newmsg
	log.Print(newmsg)
}

// AddObject adds a printer object.
func (p *Printer) AddObject(name string, obj any) {
	if _, ok := p.objectIndex[name]; ok {
		configfile.Raise("Printer object '%s' already created", name)
	}
	p.objectIndex[name] = len(p.objects)
	p.objects = append(p.objects, object{name, obj})
}

// LookupObject returns a printer object. A missing object is a config error.
func (p *Printer) LookupObject(name string) any {
	if i, ok := p.objectIndex[name]; ok {
		return p.objects[i].obj
	}
	configfile.Raise("Unknown config object '%s'", name)
	return nil
}

// TryLookupObject returns a printer object, or nil.
func (p *Printer) TryLookupObject(name string) any {
	if i, ok := p.objectIndex[name]; ok {
		return p.objects[i].obj
	}
	return nil
}

// NamedObject is a printer object and its name.
type NamedObject struct {
	Name string
	Obj  any
}

// LookupObjects returns the objects of a module: the object with the module name, and then the
// objects with names that start with the module name and a space. An empty module gives all
// objects.
func (p *Printer) LookupObjects(module string) []NamedObject {
	var out []NamedObject
	if module == "" {
		for _, o := range p.objects {
			out = append(out, NamedObject{o.name, o.obj})
		}
		return out
	}
	if i, ok := p.objectIndex[module]; ok {
		out = append(out, NamedObject{module, p.objects[i].obj})
	}
	for _, o := range p.objects {
		if strings.HasPrefix(o.name, module+" ") {
			out = append(out, NamedObject{o.name, o.obj})
		}
	}
	return out
}

// LoadObject returns the object of a section, and makes it the first time. A section with no
// module is a config error.
func (p *Printer) LoadObject(config *configfile.Wrapper, section string) any {
	obj, ok := p.loadObject(config, section)
	if !ok {
		configfile.Raise("Unable to load module '%s'", section)
	}
	return obj
}

// TryLoadObject is LoadObject that returns nil for a section with no module.
func (p *Printer) TryLoadObject(config *configfile.Wrapper, section string) any {
	obj, _ := p.loadObject(config, section)
	return obj
}

func (p *Printer) loadObject(config *configfile.Wrapper, section string) (any, bool) {
	if i, ok := p.objectIndex[section]; ok {
		return p.objects[i].obj, true
	}
	parts := strings.Fields(section)
	if len(parts) == 0 {
		return nil, false
	}
	m, ok := lookupModule(parts[0])
	if !ok {
		return nil, false
	}
	load := m.load
	if len(parts) > 1 {
		load = m.loadPrefix
	}
	if load == nil {
		return nil, false
	}
	obj := load(config.GetSection(section))
	p.objectIndex[section] = len(p.objects)
	p.objects = append(p.objects, object{section, obj})
	return obj, true
}

// objectNames lists the names of the objects, for the check of the unused options.
func (p *Printer) objectNames() []string {
	names := make([]string, len(p.objects))
	for i, o := range p.objects {
		names[i] = o.name
	}
	return names
}

func (p *Printer) readConfig() {
	pconfig := configfile.NewPrinterConfig(p)
	p.AddObject("configfile", pconfig)
	filename, _ := p.startArgs.Get("config_file")
	config, err := pconfig.ReadMainConfig(filename)
	if err != nil {
		panic(err)
	}
	// Create printer components
	for _, f := range ConfigObjects {
		f(config)
	}
	for _, section := range config.PrefixSections("") {
		p.TryLoadObject(config, section.Name())
	}
	for _, f := range AfterConfigObjects {
		f(config)
	}
	// Validate that there are no undefined parameters in the config file
	if err := pconfig.CheckUnusedOptions(config, p.objectNames()); err != nil {
		panic(err)
	}
}

// kinded is an error that tells its kind for the connect phase: "config" (a config or a pin
// error), "protocol" (an error of the board's protocol), or "mcu" (an error of a board).
type kinded interface{ ErrorKind() string }

func (p *Printer) connect() {
	if !p.connectPhase() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Unhandled exception during ready callback: %v\n%s", r, debug.Stack())
			p.InvokeShutdown(fmt.Sprintf("Internal error during ready callback: %v", r), nil)
		}
	}()
	p.setState(MessageReady)
	p.reactor.AssertNoPause(func() {
		for _, cb := range p.eventHandlers["klippy:ready"] {
			if p.stateMessage != MessageReady {
				return
			}
			cb()
		}
	})
}

// connectPhase reads the config and connects to the boards. It returns false after an error.
func (p *Printer) connectPhase() (ok bool) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		ok = false
		err, _ := r.(error)
		kind := ""
		if k, ok := r.(kinded); ok {
			kind = k.ErrorKind()
		}
		switch kind {
		case "config":
			log.Printf("Config error: %v", err)
			p.setState(fmt.Sprintf("%v\n%s", err, MessageRestart))
		case "protocol", "mcu":
			msg := "Protocol error"
			if kind == "mcu" {
				msg = "MCU error during connect"
			}
			log.Printf("%s: %v", msg, err)
			p.setState(msg)
			p.SendEvent("klippy:notify_mcu_error", msg, map[string]any{"error": err.Error()})
		default:
			log.Printf("Unhandled exception during connect: %v\n%s", r, debug.Stack())
			p.setState(fmt.Sprintf("Internal error during connect: %v\n%s", r, MessageRestart))
		}
	}()
	p.readConfig()
	p.SendEvent("klippy:mcu_identify")
	for _, cb := range p.eventHandlers["klippy:connect"] {
		if p.stateMessage != MessageStartup {
			return false
		}
		cb()
	}
	return true
}

// Run runs the reactor until an exit request, and returns the run result: "exit", "error_exit",
// "restart" or "firmware_restart".
func (p *Printer) Run() string {
	log.Printf("Start printer at %.1f", p.reactor.Monotonic())
	func() {
		defer func() {
			if r := recover(); r != nil {
				msg := "Unhandled exception during run"
				log.Printf("%s: %v\n%s", msg, r, debug.Stack())
				func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("Repeat unhandled exception during run: %v", r)
							p.runResult = "error_exit"
						}
					}()
					p.reactor.RegisterCallback(func(float64) any { p.InvokeShutdown(msg, nil); return nil }, reactor.Now)
					p.reactor.Run()
				}()
			}
		}()
		p.reactor.Run()
	}()
	// Check restart flags
	runResult := p.runResult
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Unhandled exception during post run: %v", r)
			}
		}()
		if runResult == "firmware_restart" {
			p.SendEvent("klippy:firmware_restart")
		}
		p.SendEvent("klippy:disconnect")
	}()
	return runResult
}

// InvokeShutdown moves the printer to the shutdown state, and runs the shutdown handlers.
func (p *Printer) InvokeShutdown(msg string, details map[string]any) {
	if p.inShutdown {
		return
	}
	log.Printf("Transition to shutdown state: %s", msg)
	p.inShutdown = true
	p.setState(msg)
	p.reactor.AssertNoPause(func() {
		for _, cb := range p.eventHandlers["klippy:shutdown"] {
			runLogged("Exception during shutdown handler", func() { cb() })
		}
		for _, cb := range p.eventHandlers["klippy:analyze_shutdown"] {
			runLogged("Exception in analyze_shutdown handler", func() { cb(msg, details) })
		}
	})
}

func runLogged(msg string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("%s: %v\n%s", msg, r, debug.Stack())
		}
	}()
	f()
}

// InvokeAsyncShutdown runs InvokeShutdown on the reactor. Call it from any goroutine.
func (p *Printer) InvokeAsyncShutdown(msg string, details map[string]any) {
	p.reactor.RegisterAsyncCallback(func(float64) any { p.InvokeShutdown(msg, details); return nil }, reactor.Now)
}

// RegisterEventHandler adds a handler of an event. The handlers run in the order that they are
// registered.
func (p *Printer) RegisterEventHandler(event string, callback func(args ...any)) {
	p.eventHandlers[event] = append(p.eventHandlers[event], callback)
}

// SendEvent runs the handlers of an event.
func (p *Printer) SendEvent(event string, args ...any) {
	for _, cb := range p.eventHandlers[event] {
		cb(args...)
	}
}

// RequestExit stops the reactor. The first result is the run result.
func (p *Printer) RequestExit(result string) {
	if p.runResult == "" {
		p.runResult = result
	}
	p.reactor.End()
}

// SoftwareVersion is the "software_version" start argument, or "None".
func (p *Printer) SoftwareVersion() string {
	if v, ok := p.startArgs.Get("software_version"); ok {
		return v
	}
	return "None"
}
