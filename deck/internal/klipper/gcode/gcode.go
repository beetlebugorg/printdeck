// Package gcode reads G-code lines, finds the handler of each command, and sends the responses.
//
// Ported to Go from klippy/gcode.py of Klipper (https://github.com/Klipper3d/klipper), commit
// 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f. GCodeIO (gcodeio.go) reads only the G-code file of
// the batch mode. Klipper also reads a pseudo-terminal, but the deck sends G-code to the
// dispatcher directly.
//
// A handler gives an error with a panic of *CommandError, as gcode.py raises CommandError.
//
//	Copyright (C) 2016-2025  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package gcode

import (
	"fmt"
	"log"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/pyfmt"
	"printdeck/internal/klipper/reactor"
)

// CommandError is the error of a G-code command. The dispatcher sends it to the user as "!! msg".
type CommandError struct{ Msg string }

func (e *CommandError) Error() string { return e.Msg }

// Errorf makes a command error.
func Errorf(format string, args ...any) *CommandError {
	return &CommandError{fmt.Sprintf(format, args...)}
}

// Coord is a position: X, Y, Z and E.
type Coord [4]float64

func (c Coord) X() float64 { return c[0] }
func (c Coord) Y() float64 { return c[1] }
func (c Coord) Z() float64 { return c[2] }
func (c Coord) E() float64 { return c[3] }

// Handler runs a G-code command.
type Handler func(gcmd *Command)

// Command is a G-code command and its parameters (GCodeCommand).
type Command struct {
	gcode       *Dispatch
	command     string
	commandline string
	params      map[string]string
	keys        []string
	needAck     bool
}

// addParam sets a parameter. A new name goes at the end of the order, as in a Python dict.
func addParam(params map[string]string, keys []string, k, v string) []string {
	if _, ok := params[k]; !ok {
		keys = append(keys, k)
	}
	params[k] = v
	return keys
}

// ParamNames are the names of the parameters, in the order of the Python dict of
// get_command_parameters().
func (c *Command) ParamNames() []string { return append([]string(nil), c.keys...) }

func (c *Command) Command() string     { return c.command }
func (c *Command) Commandline() string { return c.commandline }

// Params returns the parameters, by upper case name.
func (c *Command) Params() map[string]string { return c.params }

// RespondInfo sends a "// msg" response, and logs it.
func (c *Command) RespondInfo(msg string, logIt bool) { c.gcode.RespondInfo(msg, logIt) }

// RespondRaw sends a response.
func (c *Command) RespondRaw(msg string) { c.gcode.RespondRaw(msg) }

// Error makes a command error.
func (c *Command) Error(format string, args ...any) *CommandError { return Errorf(format, args...) }

func isPySpace(b byte) bool {
	return b == ' ' || (b >= '\t' && b <= '\r') || b == 0x1c || b == 0x1d || b == 0x1e || b == 0x1f
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// RawParams returns the text after the command name, without a line number and a checksum.
func (c *Command) RawParams() string {
	command, origline := c.command, c.commandline
	paramStart, paramEnd := len(command), len(origline)
	if paramStart > len(origline) || strings.ToUpper(origline[:paramStart]) != command {
		// Skip any gcode line-number and ignore any trailing checksum
		paramStart += strings.Index(strings.ToUpper(origline), command)
		if end := strings.LastIndexByte(origline, '*'); end >= 0 && isAllDigits(origline[end+1:]) {
			paramEnd = end
		}
	}
	if paramStart >= 0 && paramStart < len(origline) && isPySpace(origline[paramStart]) {
		paramStart++
	}
	if paramStart < 0 {
		paramStart = 0
	}
	if paramStart > paramEnd {
		return ""
	}
	return origline[paramStart:paramEnd]
}

// Ack sends "ok" (with msg after it), once, when the command needs an ack.
func (c *Command) Ack(msg string) bool {
	if !c.needAck {
		return false
	}
	okMsg := "ok"
	if msg != "" {
		okMsg = "ok " + msg
	}
	c.RespondRaw(okMsg)
	c.needAck = false
	return true
}

// Opt is an option of a parameter getter.
type Opt = configfile.Opt

// Default, Min, Max, Above and Below are the options of the getters, as in configfile.
var (
	Default = configfile.Default
	Min     = configfile.Min
	Max     = configfile.Max
	Above   = configfile.Above
	Below   = configfile.Below
)

type getOpts struct {
	def                    any
	hasDef                 bool
	min, max, above, below *float64
}

// readOpts reads the options with a config wrapper: configfile keeps its option type closed.
func readOpts(opts []Opt) getOpts {
	var o getOpts
	o.def, o.hasDef, o.min, o.max, o.above, o.below = configfile.ReadOpts(opts)
	return o
}

func (c *Command) get(name string, parse func(string) (any, error), o getOpts, limit func(float64) string) any {
	value, ok := c.params[name]
	if !ok {
		if !o.hasDef {
			panic(Errorf("Error on '%s': missing %s", c.commandline, name))
		}
		return o.def
	}
	v, err := parse(value)
	if err != nil {
		panic(Errorf("Error on '%s': unable to parse %s", c.commandline, value))
	}
	var num float64
	switch x := v.(type) {
	case int:
		num = float64(x)
	case float64:
		num = x
	default:
		return v
	}
	if o.min != nil && num < *o.min {
		panic(Errorf("Error on '%s': %s must have minimum of %s", c.commandline, name, limit(*o.min)))
	}
	if o.max != nil && num > *o.max {
		panic(Errorf("Error on '%s': %s must have maximum of %s", c.commandline, name, limit(*o.max)))
	}
	if o.above != nil && num <= *o.above {
		panic(Errorf("Error on '%s': %s must be above %s", c.commandline, name, limit(*o.above)))
	}
	if o.below != nil && num >= *o.below {
		panic(Errorf("Error on '%s': %s must be below %s", c.commandline, name, limit(*o.below)))
	}
	return v
}

func intLimit(f float64) string { return strconv.FormatInt(int64(f), 10) }

// Get returns a string parameter. With Default(nil), a missing parameter gives "" and false.
func (c *Command) Get(name string, opts ...Opt) (string, bool) {
	v := c.get(name, func(s string) (any, error) { return s, nil }, readOpts(opts), intLimit)
	s, ok := v.(string)
	return s, ok
}

// GetString returns a string parameter, or the default.
func (c *Command) GetString(name string, opts ...Opt) string {
	s, _ := c.Get(name, opts...)
	return s
}

// GetInt returns an integer parameter.
func (c *Command) GetInt(name string, opts ...Opt) int {
	v := c.get(name, func(s string) (any, error) {
		n, err := pyfmt.ParseInt(s)
		return int(n), err
	}, readOpts(opts), intLimit)
	n, _ := v.(int)
	return n
}

// GetFloat returns a number parameter.
func (c *Command) GetFloat(name string, opts ...Opt) float64 {
	o := readOpts(opts)
	if d, ok := o.def.(int); ok {
		o.def = float64(d)
	}
	v := c.get(name, func(s string) (any, error) { return pyfmt.ParseFloat(s) }, o, pyfmt.Float)
	f, _ := v.(float64)
	return f
}

// GetFloatOptional returns a number parameter, and false when it is missing.
func (c *Command) GetFloatOptional(name string, opts ...Opt) (float64, bool) {
	if _, ok := c.params[name]; !ok {
		return 0, false
	}
	return c.GetFloat(name, opts...), true
}

type mux struct {
	key    string
	values map[string]Handler
	order  []string
	hasNil bool
}

// Dispatch reads G-code and runs the handlers (GCodeDispatch).
type Dispatch struct {
	printer            *host.Printer
	isFileinput        bool
	isPrinterReady     bool
	mutex              *reactor.Mutex
	outputCallbacks    []func(msg string)
	baseGcodeHandlers  map[string]Handler
	readyGcodeHandlers map[string]Handler
	gcodeHandlers      map[string]Handler
	muxCommands        map[string]*mux
	gcodeHelp          map[string]string
	statusCommands     map[string]map[string]string
	defaultHandler     Handler

	// The command stack of printdeck.go.
	stackMu sync.Mutex
	stack   []string
}

func New(printer *host.Printer) *Dispatch {
	_, fileinput := printer.StartArgs().Get("debuginput")
	g := &Dispatch{printer: printer, isFileinput: fileinput, mutex: printer.Reactor().NewMutex(false),
		baseGcodeHandlers: map[string]Handler{}, readyGcodeHandlers: map[string]Handler{},
		muxCommands: map[string]*mux{}, gcodeHelp: map[string]string{}}
	g.gcodeHandlers = g.baseGcodeHandlers
	g.defaultHandler = g.cmdDefault
	printer.RegisterEventHandler("klippy:ready", func(...any) { g.handleReady() })
	printer.RegisterEventHandler("klippy:shutdown", func(...any) { g.handleShutdown() })
	printer.RegisterEventHandler("klippy:disconnect", func(...any) { g.respondState("Disconnect") })
	// Register commands needed before config file is loaded
	g.RegisterCommand("M110", func(*Command) {}, true, "")
	g.RegisterCommand("M112", g.cmdM112, true, "")
	g.RegisterCommand("M115", g.cmdM115, true, "")
	g.RegisterCommand("RESTART", func(*Command) { g.RequestRestart("restart") }, true,
		"Reload config file and restart host software")
	g.RegisterCommand("FIRMWARE_RESTART", func(*Command) { g.RequestRestart("firmware_restart") }, true,
		"Restart firmware, host, and reload config")
	g.RegisterCommand("ECHO", func(gcmd *Command) { gcmd.RespondInfo(gcmd.Commandline(), false) }, true, "")
	g.RegisterCommand("STATUS", g.cmdStatus, true, "Report the printer status")
	g.RegisterCommand("HELP", g.cmdHelp, true, "Report the list of available extended G-Code commands")
	g.RegisterCommand("SAVE_CONFIG", g.cmdSaveConfig, false, "Overwrite config file and restart")
	return g
}

func init() {
	host.EarlyObjects = append(host.EarlyObjects, func(p *host.Printer) { p.AddObject("gcode", New(p)) })
}

// Of returns the gcode object of a printer.
func Of(p *host.Printer) *Dispatch { return p.LookupObject("gcode").(*Dispatch) }

// IsTraditionalGcode reports a command of a letter and a number, for example "G1" or "M104".
func IsTraditionalGcode(cmd string) bool {
	f := strings.Fields(strings.ToUpper(cmd))
	if len(f) == 0 || len(f[0]) < 2 {
		return false
	}
	c := f[0]
	if _, err := pyfmt.ParseFloat(c[1:]); err != nil {
		return false
	}
	return unicode.IsUpper(rune(c[0])) && c[1] >= '0' && c[1] <= '9'
}

func isAlnum(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// RegisterCommand adds a command. An extended command (not a letter and a number) has its
// parameters as NAME=VALUE. desc is the help text, or "".
func (g *Dispatch) RegisterCommand(cmd string, fn Handler, whenNotReady bool, desc string) {
	if _, ok := g.readyGcodeHandlers[cmd]; ok {
		configfile.Raise("gcode command %s already registered", cmd)
	}
	if !IsTraditionalGcode(cmd) {
		if strings.ToUpper(cmd) != cmd || !isAlnum(strings.ReplaceAll(cmd, "_", "A")) ||
			unicode.IsDigit(rune(cmd[0])) || (len(cmd) > 1 && unicode.IsDigit(rune(cmd[1]))) {
			configfile.Raise("Can't register '%s' as it is an invalid name", cmd)
		}
		orig := fn
		fn = func(gcmd *Command) { orig(g.extendedParams(gcmd)) }
	}
	g.readyGcodeHandlers[cmd] = fn
	if whenNotReady {
		g.baseGcodeHandlers[cmd] = fn
	}
	if desc != "" {
		g.gcodeHelp[cmd] = desc
	}
	g.buildStatusCommands()
}

// UnregisterCommand removes a command, and returns its handler, or nil.
func (g *Dispatch) UnregisterCommand(cmd string) Handler {
	old := g.readyGcodeHandlers[cmd]
	delete(g.readyGcodeHandlers, cmd)
	delete(g.baseGcodeHandlers, cmd)
	g.buildStatusCommands()
	return old
}

// RegisterMuxCommand adds a command that runs the handler of a value of its key parameter, for
// example "SET_FAN_SPEED FAN=name". The value "" without hasValue is the handler for no key.
func (g *Dispatch) RegisterMuxCommand(cmd, key string, value *string, fn Handler, desc string) {
	prev, ok := g.muxCommands[cmd]
	if !ok {
		g.RegisterCommand(cmd, func(gcmd *Command) { g.cmdMux(cmd, gcmd) }, false, desc)
		prev = &mux{key: key, values: map[string]Handler{}}
		g.muxCommands[cmd] = prev
	}
	vs := "None"
	if value != nil {
		vs = *value
	}
	if prev.key != key {
		configfile.Raise("mux command %s %s %s may have only one key (%s)", cmd, key, vs, prev.key)
	}
	if value == nil {
		if prev.hasNil {
			configfile.Raise("mux command %s %s %s already registered", cmd, key, vs)
		}
		prev.hasNil = true
		prev.values["\x00none"] = fn
		return
	}
	if _, ok := prev.values[*value]; ok {
		configfile.Raise("mux command %s %s %s already registered", cmd, key, vs)
	}
	prev.values[*value] = fn
	prev.order = append(prev.order, *value)
}

// CommandHelp returns the help texts.
func (g *Dispatch) CommandHelp() map[string]string {
	out := map[string]string{}
	for k, v := range g.gcodeHelp {
		out[k] = v
	}
	return out
}

// Status returns the status of the gcode object.
func (g *Dispatch) Status(eventtime float64) map[string]any {
	return map[string]any{"commands": g.statusCommands}
}

func (g *Dispatch) buildStatusCommands() {
	commands := map[string]map[string]string{}
	for cmd := range g.gcodeHandlers {
		commands[cmd] = map[string]string{}
		if h, ok := g.gcodeHelp[cmd]; ok {
			commands[cmd]["help"] = h
		}
	}
	g.statusCommands = commands
}

// RegisterOutputHandler adds a function that gets each response.
func (g *Dispatch) RegisterOutputHandler(cb func(msg string)) {
	g.outputCallbacks = append(g.outputCallbacks, cb)
}

func (g *Dispatch) handleShutdown() {
	if !g.isPrinterReady {
		return
	}
	g.isPrinterReady = false
	g.gcodeHandlers = g.baseGcodeHandlers
	g.buildStatusCommands()
	g.respondState("Shutdown")
}

func (g *Dispatch) handleReady() {
	g.isPrinterReady = true
	g.gcodeHandlers = g.readyGcodeHandlers
	g.buildStatusCommands()
	g.respondState("Ready")
}

// Parse input into commands

var argsRE = regexp.MustCompile(`([A-Z_]+|[A-Z*])`)

// pySplit splits s as Python's re.split with one group: the text, the match, the text, ...
func pySplit(s string) []string {
	var parts []string
	last := 0
	for _, m := range argsRE.FindAllStringIndex(s, -1) {
		parts = append(parts, s[last:m[0]], s[m[0]:m[1]])
		last = m[1]
	}
	return append(parts, s[last:])
}

func joinRange(parts []string, from, to int) string {
	if from > len(parts) {
		from = len(parts)
	}
	if to > len(parts) {
		to = len(parts)
	}
	return strings.Join(parts[from:to], "")
}

func (g *Dispatch) processCommands(commands []string, needAck bool) {
	for _, line := range commands {
		// Ignore comments and leading/trailing spaces
		line = strings.TrimSpace(line)
		origline := line
		if cpos := strings.IndexByte(line, ';'); cpos >= 0 {
			line = line[:cpos]
		}
		// Break line into parts and determine command
		parts := pySplit(strings.ToUpper(line))
		var cmd string
		if joinRange(parts, 0, 2) == "N" {
			// Skip line number at start of command
			cmd = strings.TrimSpace(joinRange(parts, 3, 5))
		} else {
			cmd = strings.TrimSpace(joinRange(parts, 0, 3))
		}
		// Build gcode "params" dictionary
		params := map[string]string{}
		var keys []string
		for i := 1; i+1 < len(parts); i += 2 {
			keys = addParam(params, keys, parts[i], strings.TrimSpace(parts[i+1]))
		}
		gcmd := &Command{gcode: g, command: cmd, commandline: origline, params: params, keys: keys, needAck: needAck}
		// Invoke handler for command
		handler, ok := g.gcodeHandlers[cmd]
		if !ok {
			handler = g.defaultHandler
		}
		g.runHandler(handler, gcmd, cmd, needAck)
		gcmd.Ack("")
	}
}

func (g *Dispatch) runHandler(handler Handler, gcmd *Command, cmd string, needAck bool) {
	g.pushCommand(gcmd.commandline)
	defer g.popCommand()
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		if e, ok := r.(*CommandError); ok {
			g.respondError(e.Msg)
			g.printer.SendEvent("gcode:command_error")
			if !needAck {
				panic(e)
			}
			return
		}
		msg := fmt.Sprintf("Internal error on command:\"%s\"", cmd)
		log.Printf("%s: %v\n%s", msg, r, debug.Stack())
		g.printer.InvokeShutdown(msg, nil)
		g.respondError(msg)
		if !needAck {
			panic(r)
		}
	}()
	handler(gcmd)
}

// RunScriptFromCommand runs a script from a command handler, with the G-code mutex held.
func (g *Dispatch) RunScriptFromCommand(script string) {
	g.processCommands(strings.Split(script, "\n"), false)
}

// RunScript runs a script. A command error panics.
func (g *Dispatch) RunScript(script string) {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	g.processCommands(strings.Split(script, "\n"), false)
}

// ProcessCommands runs lines of G-code that need an "ok" each (as from a G-code terminal), with
// the G-code mutex held.
func (g *Dispatch) ProcessCommands(commands []string) {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	g.processCommands(commands, true)
}

// Mutex is the G-code mutex.
func (g *Dispatch) Mutex() *reactor.Mutex { return g.mutex }

// CreateCommand makes a command with no ack, for a module that runs a handler directly.
func (g *Dispatch) CreateCommand(command, commandline string, params map[string]string) *Command {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return &Command{gcode: g, command: command, commandline: commandline, params: params, keys: keys}
}

// Response handling

// RespondRaw sends a response to the output handlers.
func (g *Dispatch) RespondRaw(msg string) {
	for _, cb := range g.outputCallbacks {
		cb(msg)
	}
}

// RespondInfo sends a "// msg" response, with "// " on each line.
func (g *Dispatch) RespondInfo(msg string, logIt bool) {
	if logIt {
		log.Print(msg)
	}
	lines := strings.Split(strings.TrimSpace(msg), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	g.RespondRaw("// " + strings.Join(lines, "\n// "))
}

func (g *Dispatch) respondError(msg string) {
	log.Print(msg)
	lines := strings.Split(strings.TrimSpace(msg), "\n")
	if len(lines) > 1 {
		g.RespondInfo(strings.Join(lines, "\n"), false)
	}
	g.RespondRaw("!! " + strings.TrimSpace(lines[0]))
	if g.isFileinput {
		g.printer.RequestExit("error_exit")
	}
}

func (g *Dispatch) respondState(state string) {
	g.RespondInfo("Klipper state: "+state, false)
}

// Parameter parsing helpers

func (g *Dispatch) extendedParams(gcmd *Command) *Command {
	args, err := shlexSplit(gcmd.RawParams())
	if err != nil {
		panic(Errorf("Malformed command '%s'", gcmd.Commandline()))
	}
	eparams := map[string]string{}
	var keys []string
	for _, a := range args {
		k, v, ok := strings.Cut(a, "=")
		if !ok {
			panic(Errorf("Malformed command '%s'", gcmd.Commandline()))
		}
		keys = addParam(eparams, keys, strings.ToUpper(k), v)
	}
	// Update gcmd with new parameters
	gcmd.params, gcmd.keys = eparams, keys
	return gcmd
}

// G-Code special command handlers

func (g *Dispatch) cmdDefault(gcmd *Command) {
	cmd := gcmd.Command()
	switch cmd {
	case "M105":
		// Don't warn about temperature requests when not ready
		gcmd.Ack("T:0")
		return
	case "M21":
		// Don't warn about sd card init when not ready
		return
	}
	if !g.isPrinterReady {
		msg, _ := g.printer.StateMessage()
		panic(Errorf("%s", msg))
	}
	if cmd == "" {
		// Klipper logs the comment lines at the debug level. The Go host has no debug log.
		return
	}
	if strings.Contains(cmd, " ") {
		// Handle M117/M118 gcode with numeric and special characters
		realcmd := strings.Fields(cmd)[0]
		if realcmd == "M117" || realcmd == "M118" || realcmd == "M23" {
			if handler, ok := g.gcodeHandlers[realcmd]; ok {
				gcmd.command = realcmd
				handler(gcmd)
				return
			}
		}
	} else if (cmd == "M140" || cmd == "M104") && gcmd.GetFloat("S", Default(0.)) == 0 {
		// Don't warn about requests to turn off heaters when not present
		return
	} else if cmd == "M107" || (cmd == "M106" && (gcmd.GetFloat("S", Default(1.)) == 0 || g.isFileinput)) {
		// Don't warn about requests to turn off fan when fan not present
		return
	}
	gcmd.RespondInfo(fmt.Sprintf("Unknown command:\"%s\"", cmd), true)
}

func (g *Dispatch) cmdMux(command string, gcmd *Command) {
	m := g.muxCommands[command]
	var keyParam string
	var has bool
	if m.hasNil {
		keyParam, has = gcmd.Get(m.key, Default(nil))
	} else {
		keyParam, has = gcmd.Get(m.key)
	}
	lookup := keyParam
	if !has {
		lookup = "\x00none"
	}
	fn, ok := m.values[lookup]
	if !ok {
		var keys []string
		guess := ""
		for _, value := range m.order {
			keys = append(keys, "'"+value+"'")
			if keyParam != "" && strings.Contains(value, keyParam) {
				guess = ". Did you mean '" + value + "'?"
			}
		}
		if guess == "" {
			guess = ". Options: " + strings.Join(keys, ", ")
		}
		panic(Errorf("The value '%s' is not valid for %s%s", keyParam, m.key, guess))
	}
	fn(gcmd)
}

// Low-level G-Code commands that are needed before the config file is loaded

func (g *Dispatch) cmdM112(gcmd *Command) {
	// Emergency Stop
	g.printer.InvokeShutdown("Shutdown due to M112 command", nil)
}

// CmdM112 runs an emergency stop.
func (g *Dispatch) CmdM112() { g.cmdM112(nil) }

func (g *Dispatch) cmdM115(gcmd *Command) {
	// Get Firmware Version and Capabilities
	version, _ := g.printer.StartArgs().Get("software_version")
	if version == "" {
		version = "None"
	}
	msg := "FIRMWARE_NAME:Klipper FIRMWARE_VERSION:" + version
	if !gcmd.Ack(msg) {
		gcmd.RespondInfo(msg, true)
	}
}

// Restarter is the part of the toolhead that a restart needs.
type Restarter interface {
	LastMoveTime() float64
	Dwell(delay float64)
	WaitMoves()
}

// RequestRestart stops the printer for a restart, after the moves in the queue.
func (g *Dispatch) RequestRestart(result string) {
	if g.isPrinterReady {
		if toolhead, ok := g.printer.TryLookupObject("toolhead").(Restarter); ok {
			printTime := toolhead.LastMoveTime()
			if result == "exit" {
				log.Printf("Exiting (print time %.3fs)", printTime)
			}
			g.printer.SendEvent("gcode:request_restart", printTime)
			toolhead.Dwell(0.500)
			toolhead.WaitMoves()
		}
	}
	g.printer.RequestExit(result)
}

func (g *Dispatch) cmdStatus(gcmd *Command) {
	if g.isPrinterReady {
		g.respondState("Ready")
		return
	}
	msg, _ := g.printer.StateMessage()
	msg = strings.TrimRightFunc(msg, unicode.IsSpace) + "\nKlipper state: Not ready"
	panic(Errorf("%s", msg))
}

func (g *Dispatch) cmdHelp(gcmd *Command) {
	var cmdhelp []string
	if !g.isPrinterReady {
		cmdhelp = append(cmdhelp, "Printer is not ready - not all commands available.")
	}
	cmdhelp = append(cmdhelp, "Available extended commands:")
	cmds := make([]string, 0, len(g.gcodeHandlers))
	for cmd := range g.gcodeHandlers {
		cmds = append(cmds, cmd)
	}
	sort.Strings(cmds)
	for _, cmd := range cmds {
		if h, ok := g.gcodeHelp[cmd]; ok {
			cmdhelp = append(cmdhelp, fmt.Sprintf("%-10s: %s", cmd, h))
		}
	}
	gcmd.RespondInfo(strings.Join(cmdhelp, "\n"), false)
}

// cmdSaveConfig writes the SAVE_CONFIG changes to the config file and restarts (the
// cmd_SAVE_CONFIG of configfile.py).
func (g *Dispatch) cmdSaveConfig(gcmd *Command) {
	pconfig := g.printer.LookupObject("configfile").(*configfile.PrinterConfig)
	cfgname, _ := g.printer.StartArgs().Get("config_file")
	restart, err := pconfig.SaveConfig(cfgname)
	if err != nil {
		panic(gcmd.Error("%s", err.Error()))
	}
	if restart {
		g.RequestRestart("restart")
	}
}

// Registered reports that a command is registered.
func (g *Dispatch) Registered(cmd string) bool {
	_, ok := g.readyGcodeHandlers[cmd]
	return ok
}
