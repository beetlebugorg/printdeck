// Package configfile reads the Klipper config file (printer.cfg): includes, the SAVE_CONFIG block,
// the option getters with their limits, and the check for options that no module reads.
//
// Ported to Go from klippy/configfile.py of Klipper (https://github.com/Klipper3d/klipper), commit
// 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f. The parser of Python's configparser, on which
// configfile.py is built, is in parser.go. The SAVE_CONFIG command is in the gcode port.
//
// The getters panic with *Error, as configfile.py raises config errors: the code that loads the
// printer objects recovers them.
//
//	Copyright (C) 2016-2024  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package configfile

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Error is a config error, for example a missing option or a value out of range.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

func errorf(format string, args ...any) *Error { return &Error{fmt.Sprintf(format, args...)} }

// Raise panics with a config error.
func Raise(format string, args ...any) { panic(errorf(format, args...)) }

// Recover turns a config error panic into an error. Use it in a defer.
func Recover(err *error) {
	if r := recover(); r != nil {
		if e, ok := r.(*Error); ok {
			*err = e
			return
		}
		panic(r)
	}
}

type accessKey struct{ section, option string }

// Opt is an option of a getter.
type Opt func(*getOpts)

type getOpts struct {
	def                    any
	hasDef                 bool
	min, max, above, below *float64
	noteValid              bool
}

// Default gives the value when the option is not in the file. nil means none (Python's None).
func Default(v any) Opt { return func(o *getOpts) { o.def, o.hasDef = v, true } }

// Min, Max, Above and Below are the limits of a number.
func Min(v float64) Opt   { return func(o *getOpts) { o.min = &v } }
func Max(v float64) Opt   { return func(o *getOpts) { o.max = &v } }
func Above(v float64) Opt { return func(o *getOpts) { o.above = &v } }
func Below(v float64) Opt { return func(o *getOpts) { o.below = &v } }

// NoteValid(false) reads an option without marking it as used.
func NoteValid(v bool) Opt { return func(o *getOpts) { o.noteValid = v } }

func options(opts []Opt) getOpts {
	o := getOpts{noteValid: true}
	for _, f := range opts {
		f(&o)
	}
	return o
}

// Wrapper is one section of the config (ConfigWrapper).
type Wrapper struct {
	printer        any
	fileconfig     *FileConfig
	accessTracking map[accessKey]any
	section        string
}

// Printer returns the printer that loads the config.
func (c *Wrapper) Printer() any { return c.printer }

// Name is the name of the section, for example "stepper_x" or "heater_fan heatbreak_cooling_fan".
func (c *Wrapper) Name() string { return c.section }

// Error makes a config error.
func (c *Wrapper) Error(format string, args ...any) *Error { return errorf(format, args...) }

// get reads an option with a parser, the default and the limits (_get_wrapper).
func (c *Wrapper) get(parse func(string) (any, error), option string, o getOpts) any {
	raw, ok := c.fileconfig.Get(c.section, option)
	if !ok {
		if o.hasDef {
			if o.noteValid && o.def != nil {
				c.accessTracking[accessKey{strings.ToLower(c.section), strings.ToLower(option)}] = o.def
			}
			return o.def
		}
		Raise("Option '%s' in section '%s' must be specified", option, c.section)
	}
	v, err := parse(raw)
	if err != nil {
		if e, ok := err.(*Error); ok {
			panic(e)
		}
		Raise("Unable to parse option '%s' in section '%s'", option, c.section)
	}
	if o.noteValid {
		c.accessTracking[accessKey{strings.ToLower(c.section), strings.ToLower(option)}] = v
	}
	var num float64
	isNum := false
	switch x := v.(type) {
	case int:
		num, isNum = float64(x), true
	case float64:
		num, isNum = x, true
	}
	if isNum {
		if o.min != nil && num < *o.min {
			Raise("Option '%s' in section '%s' must have minimum of %s", option, c.section, pyNum(*o.min))
		}
		if o.max != nil && num > *o.max {
			Raise("Option '%s' in section '%s' must have maximum of %s", option, c.section, pyNum(*o.max))
		}
		if o.above != nil && num <= *o.above {
			Raise("Option '%s' in section '%s' must be above %s", option, c.section, pyNum(*o.above))
		}
		if o.below != nil && num >= *o.below {
			Raise("Option '%s' in section '%s' must be below %s", option, c.section, pyNum(*o.below))
		}
	}
	return v
}

// pyNum writes a limit as Python's str() of a float or an int.
func pyNum(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

func parseStr(s string) (any, error) { return s, nil }

// parseInt reads an integer as Python's int().
func parseInt(s string) (any, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), "_", "")
	n, err := strconv.ParseInt(s, 10, 64)
	return int(n), err
}

// parseFloat reads a number as Python's float().
func parseFloat(s string) (any, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), "_", "")
	switch strings.ToLower(strings.TrimLeft(s, "+-")) {
	case "inf", "infinity", "nan":
	default:
		if strings.ContainsAny(s, "xXpP") {
			return nil, fmt.Errorf("not a float: %q", s)
		}
	}
	return strconv.ParseFloat(s, 64)
}

var booleanStates = map[string]bool{"1": true, "yes": true, "true": true, "on": true,
	"0": false, "no": false, "false": false, "off": false}

func parseBool(s string) (any, error) {
	v, ok := booleanStates[strings.ToLower(s)]
	if !ok {
		return nil, fmt.Errorf("not a boolean: %s", s)
	}
	return v, nil
}

// Get returns a string option. With Default(nil), a missing option gives "" and false.
func (c *Wrapper) Get(option string, opts ...Opt) string {
	v, _ := c.GetOptional(option, opts...)
	return v
}

// GetOptional returns a string option, and false when it is missing and the default is nil.
func (c *Wrapper) GetOptional(option string, opts ...Opt) (string, bool) {
	v := c.get(parseStr, option, options(opts))
	s, ok := v.(string)
	return s, ok
}

// GetInt returns an integer option.
func (c *Wrapper) GetInt(option string, opts ...Opt) int {
	v, _ := c.GetIntOptional(option, opts...)
	return v
}

// GetIntOptional returns an integer option, and false when it is missing and the default is nil.
func (c *Wrapper) GetIntOptional(option string, opts ...Opt) (int, bool) {
	v := c.get(parseInt, option, options(opts))
	n, ok := v.(int)
	return n, ok
}

// GetFloat returns a number option.
func (c *Wrapper) GetFloat(option string, opts ...Opt) float64 {
	v, _ := c.GetFloatOptional(option, opts...)
	return v
}

// GetFloatOptional returns a number option, and false when it is missing and the default is nil.
func (c *Wrapper) GetFloatOptional(option string, opts ...Opt) (float64, bool) {
	o := options(opts)
	if d, ok := o.def.(int); ok {
		o.def = float64(d)
	}
	v := c.get(parseFloat, option, o)
	f, ok := v.(float64)
	return f, ok
}

// GetBool returns a boolean option.
func (c *Wrapper) GetBool(option string, opts ...Opt) bool {
	v, _ := c.GetBoolOptional(option, opts...)
	return v
}

// GetBoolOptional returns a boolean option, and false when it is missing and the default is nil.
func (c *Wrapper) GetBoolOptional(option string, opts ...Opt) (bool, bool) {
	v := c.get(parseBool, option, options(opts))
	b, ok := v.(bool)
	return b, ok
}

// GetChoice returns the choice of a string option: the value of the choices map for the option
// value.
func (c *Wrapper) GetChoice(option string, choices map[string]any, opts ...Opt) any {
	o := options(opts)
	v := c.get(parseStr, option, o)
	s, _ := v.(string)
	if v == nil {
		s = "None"
	}
	r, ok := choices[s]
	if !ok {
		Raise("Choice '%s' for option '%s' in section '%s' is not a valid choice", s, option, c.section)
	}
	return r
}

// GetIntChoice returns the choice of an integer option.
func (c *Wrapper) GetIntChoice(option string, choices map[int]any, opts ...Opt) any {
	n := c.GetInt(option, opts...)
	r, ok := choices[n]
	if !ok {
		Raise("Choice '%d' for option '%s' in section '%s' is not a valid choice", n, option, c.section)
	}
	return r
}

// GetLists returns a list of lists, split by seps from the outer to the inner list. count is the
// length of the inner lists (0 for any). The values are parsed by parser.
func (c *Wrapper) getLists(option string, seps []string, count int, parser func(string) (any, error), opts []Opt) any {
	var lparser func(value string, pos int) (any, error)
	lparser = func(value string, pos int) (any, error) {
		var parts []string
		if strings.TrimSpace(value) != "" {
			for _, p := range strings.Split(value, seps[pos]) {
				parts = append(parts, strings.TrimSpace(p))
			}
		}
		if pos > 0 {
			var out []any
			for _, p := range parts {
				if p == "" {
					continue
				}
				v, err := lparser(p, pos-1)
				if err != nil {
					return nil, err
				}
				out = append(out, v)
			}
			return out, nil
		}
		var res []any
		for _, p := range parts {
			v, err := parser(p)
			if err != nil {
				return nil, err
			}
			res = append(res, v)
		}
		if count > 0 && len(res) != count {
			return nil, errorf("Option '%s' in section '%s' must have %d elements", option, c.section, count)
		}
		return res, nil
	}
	return c.get(func(s string) (any, error) { return lparser(s, len(seps)-1) }, option, options(opts))
}

// GetList returns a list of strings. With Default(nil), a missing option gives nil.
func (c *Wrapper) GetList(option string, sep string, count int, opts ...Opt) []string {
	v := c.getLists(option, []string{sep}, count, parseStr, opts)
	return toStrings(v)
}

// GetIntList returns a list of integers.
func (c *Wrapper) GetIntList(option string, sep string, count int, opts ...Opt) []int {
	v := c.getLists(option, []string{sep}, count, parseInt, opts)
	if l, ok := v.([]int); ok {
		return l
	}
	var out []int
	for _, x := range anyList(v) {
		out = append(out, x.(int))
	}
	return out
}

// GetFloatList returns a list of numbers.
func (c *Wrapper) GetFloatList(option string, sep string, count int, opts ...Opt) []float64 {
	v := c.getLists(option, []string{sep}, count, parseFloat, opts)
	if l, ok := v.([]float64); ok {
		return l
	}
	var out []float64
	for _, x := range anyList(v) {
		out = append(out, x.(float64))
	}
	return out
}

// GetFloatLists returns a list of lists of numbers, for example "1, 2\n3, 4" with seps ",", "\n".
func (c *Wrapper) GetFloatLists(option string, seps []string, count int, opts ...Opt) [][]float64 {
	v := c.getLists(option, seps, count, parseFloat, opts)
	var out [][]float64
	for _, row := range anyList(v) {
		var r []float64
		for _, x := range anyList(row) {
			r = append(r, x.(float64))
		}
		out = append(out, r)
	}
	return out
}

func anyList(v any) []any {
	l, _ := v.([]any)
	return l
}

func toStrings(v any) []string {
	if l, ok := v.([]string); ok {
		return l
	}
	var out []string
	for _, x := range anyList(v) {
		out = append(out, x.(string))
	}
	return out
}

// GetSection returns another section.
func (c *Wrapper) GetSection(section string) *Wrapper {
	return &Wrapper{printer: c.printer, fileconfig: c.fileconfig, accessTracking: c.accessTracking, section: section}
}

// HasSection reports that a section is in the file.
func (c *Wrapper) HasSection(section string) bool { return c.fileconfig.HasSection(section) }

// HasOption reports that an option is in this section of the file.
func (c *Wrapper) HasOption(option string) bool { return c.fileconfig.HasOption(c.section, option) }

// PrefixSections returns the sections whose names start with prefix, in file order.
func (c *Wrapper) PrefixSections(prefix string) []*Wrapper {
	var out []*Wrapper
	for _, s := range c.fileconfig.Sections() {
		if strings.HasPrefix(s, prefix) {
			out = append(out, c.GetSection(s))
		}
	}
	return out
}

// PrefixOptions returns the options of this section that start with prefix.
func (c *Wrapper) PrefixOptions(prefix string) []string {
	var out []string
	for _, o := range c.fileconfig.Options(c.section) {
		if strings.HasPrefix(o, prefix) {
			out = append(out, o)
		}
	}
	return out
}

// FileConfig returns the parsed file.
func (c *Wrapper) FileConfig() *FileConfig { return c.fileconfig }

// Deprecate notes a deprecated option that is in the file.
func (c *Wrapper) Deprecate(pconfig *PrinterConfig, option string, value *string) {
	if !c.fileconfig.HasOption(c.section, option) {
		return
	}
	pconfig.Deprecate(c.section, option, value, "")
}

// Config file parsing (with include file support)

// ReadConfigFile reads a config file, with Windows line ends changed to "\n".
func ReadConfigFile(filename string) (string, error) {
	b, err := os.ReadFile(filename)
	if err != nil {
		msg := fmt.Sprintf("Unable to open config file %s", filename)
		log.Print(msg)
		return "", errorf("%s", msg)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n"), nil
}

// stripComments cuts each line at its first '#', as configfile.py does before configparser.
func stripComments(data string) string {
	lines := strings.Split(data, "\n")
	for i, line := range lines {
		if pos := strings.IndexByte(line, '#'); pos >= 0 {
			lines[i] = line[:pos]
		}
	}
	return strings.Join(lines, "\n")
}

func appendFileconfig(fc *FileConfig, data, filename string) error {
	if data == "" {
		return nil
	}
	return fc.Read(stripComments(data), filename)
}

// BuildFileconfig parses data with no includes.
func BuildFileconfig(data, filename string) (*FileConfig, error) {
	fc := NewFileConfig()
	return fc, appendFileconfig(fc, data, filename)
}

// hasMagic reports that a path has glob characters (glob.has_magic).
func hasMagic(s string) bool { return strings.ContainsAny(s, "*?[") }

func resolveInclude(sourceFilename, includeSpec string, fc *FileConfig, visited map[string]bool) error {
	dirname := filepath.Dir(sourceFilename)
	includeGlob := filepath.Join(dirname, strings.TrimSpace(includeSpec))
	if filepath.IsAbs(strings.TrimSpace(includeSpec)) {
		includeGlob = strings.TrimSpace(includeSpec)
	}
	names, _ := filepath.Glob(includeGlob)
	if len(names) == 0 && !hasMagic(includeGlob) {
		return errorf("Include file '%s' does not exist", includeGlob)
	}
	sort.Strings(names)
	for _, name := range names {
		data, err := ReadConfigFile(name)
		if err != nil {
			return err
		}
		if err := parseConfig(data, name, fc, visited); err != nil {
			return err
		}
	}
	return nil
}

var includeSectRE = regexp.MustCompile(`^\[(.+)\]`)

// parseConfig parses data and its includes. The lines between includes parse as one unit, so the
// overrides in includes apply in order.
func parseConfig(data, filename string, fc *FileConfig, visited map[string]bool) error {
	path, _ := filepath.Abs(filename)
	if visited[path] {
		return errorf("Recursive include of config file '%s'", filename)
	}
	visited[path] = true
	defer delete(visited, path)
	var buf []string
	for _, line := range strings.Split(data, "\n") {
		// Strip trailing comment
		if pos := strings.IndexByte(line, '#'); pos >= 0 {
			line = line[:pos]
		}
		if m := includeSectRE.FindStringSubmatch(line); m != nil && strings.HasPrefix(m[1], "include ") {
			if err := appendFileconfig(fc, strings.Join(buf, "\n"), filename); err != nil {
				return err
			}
			buf = buf[:0]
			if err := resolveInclude(filename, m[1][8:], fc, visited); err != nil {
				return err
			}
			continue
		}
		buf = append(buf, line)
	}
	return appendFileconfig(fc, strings.Join(buf, "\n"), filename)
}

// BuildFileconfigWithIncludes parses data and the files that it includes.
func BuildFileconfigWithIncludes(data, filename string) (*FileConfig, error) {
	fc := NewFileConfig()
	return fc, parseConfig(data, filename, fc, map[string]bool{})
}

// Config auto save

const autosaveHeader = `
#*# <---------------------- SAVE_CONFIG ---------------------->
#*# DO NOT EDIT THIS BLOCK OR BELOW. The contents are auto-generated.
#*#
`

// findAutosaveData splits the regular data and the SAVE_CONFIG block, without its "#*# " line
// prefixes.
func findAutosaveData(data string) (string, string) {
	regular, autosave := data, ""
	if pos := strings.Index(data, autosaveHeader); pos >= 0 {
		regular = data[:pos]
		autosave = strings.TrimSpace(data[pos+len(autosaveHeader):])
	}
	// Check for errors and strip line prefixes
	if strings.Contains(regular, "\n#*# ") || strings.Contains(autosave, autosaveHeader) {
		log.Print("Can't read autosave from config file - autosave state corrupted")
		return data, ""
	}
	out := []string{""}
	for _, line := range strings.Split(autosave, "\n") {
		if (!strings.HasPrefix(line, "#*#") || (len(line) >= 4 && !strings.HasPrefix(line, "#*# "))) && autosave != "" {
			log.Print("Can't read autosave from config file - modifications after header")
			return data, ""
		}
		if len(line) > 4 {
			out = append(out, line[4:])
		} else {
			out = append(out, "")
		}
	}
	out = append(out, "")
	return regular, strings.Join(out, "\n")
}

var (
	commentRE = regexp.MustCompile(`[#;].*$`)
	valueRE   = regexp.MustCompile(`[^A-Za-z0-9_].*$`)
)

// stripDuplicates comments out the fields of data that fc defines.
func stripDuplicates(data string, fc *FileConfig) string {
	lines := strings.Split(data, "\n")
	section := ""
	isDupField := false
	for i, line := range lines {
		pruned := strings.TrimRightFunc(commentRE.ReplaceAllString(line, ""), unicode.IsSpace)
		if pruned == "" {
			continue
		}
		if unicode.IsSpace(rune(pruned[0])) {
			if isDupField {
				lines[i] = "#" + lines[i]
			}
			continue
		}
		isDupField = false
		if pruned[0] == '[' {
			section = strings.TrimSpace(pruned[1 : len(pruned)-1])
			continue
		}
		field := valueRE.ReplaceAllString(pruned, "")
		if fc.HasOption(section, field) {
			isDupField = true
			lines[i] = "#" + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

// LoadMainConfig reads the main config file and its SAVE_CONFIG block. It returns the whole
// config, and the config of the SAVE_CONFIG block alone.
func LoadMainConfig(filename string) (*FileConfig, *FileConfig, error) {
	data, err := ReadConfigFile(filename)
	if err != nil {
		return nil, nil, err
	}
	regularData, autosaveData := findAutosaveData(data)
	regular, err := BuildFileconfigWithIncludes(regularData, filename)
	if err != nil {
		return nil, nil, err
	}
	autosaveData = stripDuplicates(autosaveData, regular)
	autosave, err := BuildFileconfig(autosaveData, filename)
	if err != nil {
		return nil, nil, err
	}
	if err := appendFileconfig(regular, autosaveData, "*AUTOSAVE*"); err != nil {
		return nil, nil, err
	}
	return regular, autosave, nil
}

// Config validation (check for undefined options)

// PrinterConfig is the config of the printer (the PrinterConfig class).
type PrinterConfig struct {
	printer         any
	accessTracking  map[accessKey]any
	autosaveOptions map[accessKey]bool
	statusSettings  map[string]map[string]any
	statusRawConfig map[string]map[string]string
	deprecated      map[string]bool
	warnings        []map[string]any
	autosave        *FileConfig
}

func NewPrinterConfig(printer any) *PrinterConfig {
	return &PrinterConfig{printer: printer, deprecated: map[string]bool{}}
}

// ReadMainConfig reads the main config file and returns its "printer" section.
func (pc *PrinterConfig) ReadMainConfig(filename string) (*Wrapper, error) {
	fc, autosave, err := LoadMainConfig(filename)
	if err != nil {
		return nil, err
	}
	pc.autosave = autosave
	pc.autosaveOptions = map[accessKey]bool{}
	for _, s := range autosave.Sections() {
		for _, o := range autosave.Options(s) {
			pc.autosaveOptions[accessKey{strings.ToLower(s), strings.ToLower(o)}] = true
		}
	}
	pc.accessTracking = map[accessKey]any{}
	config := &Wrapper{printer: pc.printer, fileconfig: fc, accessTracking: pc.accessTracking, section: "printer"}
	pc.buildStatusConfig(config)
	return config, nil
}

// ReadConfig reads another config file, with no includes and no tracking.
func (pc *PrinterConfig) ReadConfig(filename string) (*Wrapper, error) {
	data, err := ReadConfigFile(filename)
	if err != nil {
		return nil, err
	}
	fc, err := BuildFileconfig(data, filename)
	if err != nil {
		return nil, err
	}
	return &Wrapper{printer: pc.printer, fileconfig: fc, accessTracking: map[accessKey]any{}, section: "printer"}, nil
}

// CheckUnusedOptions returns an error for a section or an option that no module read.
// objectNames are the names of the printer objects.
func (pc *PrinterConfig) CheckUnusedOptions(config *Wrapper, objectNames []string) error {
	access := map[accessKey]bool{}
	for k := range pc.accessTracking {
		access[k] = true
	}
	for k := range pc.autosaveOptions {
		access[k] = true
	}
	valid := map[string]bool{}
	for _, n := range objectNames {
		valid[n] = true
	}
	for k := range access {
		valid[k.section] = true
	}
	fc := config.fileconfig
	for _, sectionName := range fc.Sections() {
		section := strings.ToLower(sectionName)
		if !valid[section] {
			return errorf("Section '%s' is not a valid config section", section)
		}
		for _, option := range fc.Options(sectionName) {
			option = strings.ToLower(option)
			if !access[accessKey{section, option}] {
				return errorf("Option '%s' is not valid in section '%s'", option, section)
			}
		}
	}
	pc.statusSettings = map[string]map[string]any{}
	for k, v := range pc.accessTracking {
		if pc.statusSettings[k.section] == nil {
			pc.statusSettings[k.section] = map[string]any{}
		}
		pc.statusSettings[k.section][k.option] = v
	}
	pc.accessTracking = map[accessKey]any{}
	pc.autosaveOptions = map[accessKey]bool{}
	return nil
}

func (pc *PrinterConfig) buildStatusConfig(config *Wrapper) {
	pc.statusRawConfig = map[string]map[string]string{}
	for _, section := range config.PrefixSections("") {
		st := map[string]string{}
		for _, option := range section.PrefixOptions("") {
			st[option] = section.Get(option, NoteValid(false))
		}
		pc.statusRawConfig[section.Name()] = st
	}
}

func (pc *PrinterConfig) addDeprecated(data map[string]any) bool {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var key strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&key, "%s=%v;", k, data[k])
	}
	if pc.deprecated[key.String()] {
		return false
	}
	pc.deprecated[key.String()] = true
	pc.warnings = append(pc.warnings, data)
	return true
}

// RuntimeWarning adds a warning, and logs it the first time.
func (pc *PrinterConfig) RuntimeWarning(msg string) {
	if pc.addDeprecated(map[string]any{"type": "runtime_warning", "message": msg}) {
		log.Print(msg)
	}
}

// Deprecate notes a deprecated option, or a deprecated value of an option.
func (pc *PrinterConfig) Deprecate(section, option string, value *string, msg string) {
	res := map[string]any{"type": "deprecated_option"}
	defmsg := fmt.Sprintf("Option '%s' in section '%s' is deprecated.", option, section)
	if value != nil {
		res = map[string]any{"type": "deprecated_value", "value": *value}
		defmsg = fmt.Sprintf("Value '%s' in option '%s' in section '%s' is deprecated.", *value, option, section)
	}
	if msg == "" {
		msg = defmsg
	}
	res["message"], res["section"], res["option"] = msg, section, option
	pc.addDeprecated(res)
}

// Warnings returns the warnings and deprecations.
func (pc *PrinterConfig) Warnings() []map[string]any { return pc.warnings }

// Status returns the status of the config, for the API (get_status).
func (pc *PrinterConfig) Status() map[string]any {
	return map[string]any{"config": pc.statusRawConfig, "warnings": pc.warnings,
		"settings": pc.statusSettings, "save_config_pending": false}
}

// Autosave is the config of the SAVE_CONFIG block.
func (pc *PrinterConfig) Autosave() *FileConfig { return pc.autosave }
