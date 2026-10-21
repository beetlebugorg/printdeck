// Package display is the basic LCD display support: the display templates, groups and glyphs,
// the screen updates, and the menu. Only the ST7920 chips are ported. The other lcd_type choices
// are a config error.
//
// Ported to Go from klippy/extras/display/__init__.py and display.py of Klipper
// (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018-2022  Kevin O'Connor <kevin@koconnor.net>
//	Copyright (C) 2018  Aleph Objects, Inc <marcio@alephobjects.com>
//	Copyright (C) 2018  Eric Callahan <arksine.code@gmail.com>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package display

import (
	_ "embed"
	"fmt"
	"log"
	"sort"
	"strings"
	"unicode/utf8"

	"printdeck/internal/klipper/configfile"
	_ "printdeck/internal/klipper/extras/display_status"
	"printdeck/internal/klipper/extras/gcode_macro"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/jinja"
	"printdeck/internal/klipper/pyfmt"
	"printdeck/internal/klipper/reactor"
)

//go:embed display.cfg
var displayCfg string

var fmtf = fmt.Sprintf

const (
	// Normal time between each screen redraw
	redrawTime = 0.500
	// Minimum time between screen redraws
	redrawMinTime = 0.100
)

// LCDChip is the low level handler of a display.
type LCDChip interface {
	Init()
	Flush()
	Clear()
	SetGlyphs(glyphs map[string]Glyph)
	WriteText(x, y int, data []byte)
	WriteGraphics(x, y int, data []byte)
	WriteGlyph(x, y int, glyphName string) int
	Dimensions() (int, int)
}

// lcdChips are the lcd_type choices. A nil constructor is a chip that is not ported.
var lcdChips = map[string]any{
	"st7920": newST7920, "emulated_st7920": newEmulatedST7920,
	"hd44780": nil, "uc1701": nil, "ssd1306": nil, "sh1106": nil,
	"hd44780_spi": nil, "aip31068_spi": nil,
}

// Glyph is a [display_glyph] icon. A nil field is a missing icon.
type Glyph struct {
	Icon16x16 *[2][]byte
	Icon5x8   *Icon5x8
}

// Icon5x8 is the hd44780 icon of a glyph.
type Icon5x8 struct {
	Slot int
	Data []int
}

// pyError is an error with the name of its Python type, for the error messages of the templates.
type pyError struct {
	*gcode.CommandError
}

func (e pyError) PyType() string { return "gcode.CommandError" }

// DisplayTemplate is a [display_template my_template] config section.
type DisplayTemplate struct {
	printer  *host.Printer
	name     string
	params   map[string]any
	template *gcode_macro.TemplateWrapper
}

func newDisplayTemplate(config *configfile.Wrapper) *DisplayTemplate {
	t := &DisplayTemplate{printer: host.Of(config), params: map[string]any{}}
	nameParts := strings.Fields(config.Name())
	if len(nameParts) != 2 {
		configfile.Raise("Section name '%s' is not valid", config.Name())
	}
	t.name = nameParts[1]
	for _, option := range config.PrefixOptions("param_") {
		v, err := jinja.LiteralEval(config.Get(option))
		if err != nil {
			if le, ok := err.(*jinja.LiteralError); ok && le.Type == "ValueError" {
				configfile.Raise("Option '%s' in section '%s' is not a valid literal", option, config.Name())
			}
			panic(err)
		}
		t.params[option] = v
	}
	t.template = gcode_macro.Load(config).LoadTemplate(config, "text", nil)
	return t
}

// Params returns the parameters of the template.
func (t *DisplayTemplate) Params() map[string]any { return t.params }

// Render renders the template with the context and the parameters.
func (t *DisplayTemplate) Render(context map[string]any, kwargs map[string]any) string {
	params := map[string]any{}
	for k, v := range t.params {
		params[k] = v
	}
	for k, v := range kwargs {
		params[k] = v
	}
	if len(params) != len(t.params) {
		panic(pyError{gcode.Errorf("Invalid parameter to display_template %s", t.name)})
	}
	ctx := map[string]any{}
	for k, v := range context {
		ctx[k] = v
	}
	for k, v := range params {
		ctx[k] = v
	}
	return t.template.Render(ctx)
}

type dataItem struct {
	row, col int
	template *gcode_macro.TemplateWrapper
}

// DisplayGroup is the [display_data my_group my_item] sections of one group name.
type DisplayGroup struct {
	dataItems []dataItem
}

func newDisplayGroup(config *configfile.Wrapper, name string, dataConfigs []*configfile.Wrapper) *DisplayGroup {
	// Load and parse the position of display_data items
	type item struct {
		row, col int
		name     string
	}
	var items []item
	for _, c := range dataConfigs {
		pos := c.Get("position")
		parts := strings.Split(pos, ",")
		var vals []int
		for _, v := range parts {
			n, err := pyfmt.ParseInt(strings.TrimSpace(v))
			if err != nil {
				break
			}
			vals = append(vals, int(n))
		}
		if len(vals) != 2 || len(parts) != 2 {
			configfile.Raise("Unable to parse 'position' in section '%s'", c.Name())
		}
		items = append(items, item{vals[0], vals[1], c.Name()})
	}
	// Load all templates and store sorted by display position
	configsByName := map[string]*configfile.Wrapper{}
	for _, c := range dataConfigs {
		configsByName[c.Name()] = c
	}
	gm := gcode_macro.Load(config)
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.row != b.row {
			return a.row < b.row
		}
		if a.col != b.col {
			return a.col < b.col
		}
		return a.name < b.name
	})
	g := &DisplayGroup{}
	for _, it := range items {
		c := configsByName[it.name]
		if c.Get("text") != "" {
			g.dataItems = append(g.dataItems, dataItem{it.row, it.col, gm.LoadTemplate(c, "text", nil)})
		}
	}
	return g
}

// Show draws the items of the group on the display.
func (g *DisplayGroup) Show(display *PrinterLCD, templates map[string]*DisplayTemplate, eventtime float64) {
	if len(g.dataItems) == 0 {
		panic(fmt.Errorf("IndexError: list index out of range"))
	}
	context := g.dataItems[0].template.CreateTemplateContext(eventtime)
	context["draw_progress_bar"] = jinja.Func(func(args []any, kwargs map[string]any) any {
		a := callArgs(args, kwargs, "row", "col", "width", "value")
		return display.DrawProgressBar(pyInt(a[0]), pyInt(a[1]), pyInt(a[2]), pyFloat(a[3]))
	})
	context["render"] = jinja.Func(func(args []any, kwargs map[string]any) any {
		if len(args) != 1 {
			panic(&jinja.Error{Type: "TypeError", Msg: "render() takes 1 positional argument"})
		}
		name := jinja.Str(args[0])
		t, ok := templates[name]
		if !ok {
			panic(&jinja.Error{Type: "KeyError", Msg: jinja.Repr(args[0])})
		}
		return t.Render(context, kwargs)
	})
	for _, it := range g.dataItems {
		text := it.template.Render(context)
		display.DrawText(it.row, it.col, strings.ReplaceAll(text, "\n", ""), eventtime)
	}
}

// callArgs returns the arguments of a call by position, then by name.
func callArgs(args []any, kwargs map[string]any, names ...string) []any {
	out := make([]any, len(names))
	for i, name := range names {
		if i < len(args) {
			out[i] = args[i]
		} else if v, ok := kwargs[name]; ok {
			out[i] = v
		} else {
			panic(&jinja.Error{Type: "TypeError", Msg: fmt.Sprintf("missing required argument: '%s'", name)})
		}
	}
	return out
}

func pyInt(v any) int {
	switch x := v.(type) {
	case int64:
		return int(x)
	case bool:
		if x {
			return 1
		}
		return 0
	}
	panic(&jinja.Error{Type: "TypeError", Msg: "an integer is required"})
}

// pyFloat is Python's float() of a value.
func pyFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int64:
		return float64(x)
	case bool:
		if x {
			return 1
		}
		return 0
	case string:
		f, err := pyfmt.ParseFloat(x)
		if err != nil {
			panic(&jinja.Error{Type: "ValueError", Msg: fmt.Sprintf("could not convert string to float: %s", jinja.Repr(x))})
		}
		return f
	}
	panic(&jinja.Error{Type: "TypeError", Msg: "float() argument must be a string or a real number"})
}

// PrinterDisplayTemplate is the global cache of the templates, the groups, and the glyphs.
type PrinterDisplayTemplate struct {
	printer           *host.Printer
	displayTemplates  map[string]*DisplayTemplate
	displayDataGroups map[string]*DisplayGroup
	displayGlyphs     map[string]Glyph
}

func newPrinterDisplayTemplate(config *configfile.Wrapper) *PrinterDisplayTemplate {
	t := &PrinterDisplayTemplate{printer: host.Of(config), displayTemplates: map[string]*DisplayTemplate{},
		displayDataGroups: map[string]*DisplayGroup{}, displayGlyphs: map[string]Glyph{}}
	t.loadConfig(config)
	return t
}

func parseGlyph(glyphName, data string, width, height int) []int {
	var glyphData []int
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		line = strings.ReplaceAll(strings.ReplaceAll(line, ".", "0"), "*", "1")
		if line == "" {
			continue
		}
		if len(line) != width || strings.Trim(line, "01") != "" {
			configfile.Raise("Invalid glyph line in %s", glyphName)
		}
		v := 0
		for _, c := range line {
			v = v<<1 | int(c-'0')
		}
		glyphData = append(glyphData, v)
	}
	if len(glyphData) != height {
		configfile.Raise("Glyph %s incorrect lines", glyphName)
	}
	return glyphData
}

// mainAndDefault returns the sections of the main config with a prefix, then the sections of
// the default config that the main config does not have.
func mainAndDefault(config, dconfig *configfile.Wrapper, prefix string) []*configfile.Wrapper {
	main := config.PrefixSections(prefix)
	names := map[string]bool{}
	for _, c := range main {
		names[c.Name()] = true
	}
	out := append([]*configfile.Wrapper(nil), main...)
	for _, c := range dconfig.PrefixSections(prefix) {
		if !names[c.Name()] {
			out = append(out, c)
		}
	}
	return out
}

func (t *PrinterDisplayTemplate) loadConfig(config *configfile.Wrapper) {
	// Load default display config file
	pconfig := t.printer.LookupObject("configfile").(*configfile.PrinterConfig)
	dconfig, err := pconfig.ReadConfigData(displayCfg, "display.cfg")
	if err != nil {
		configfile.Raise("Cannot load config '%s'", "display.cfg")
	}
	// Load display_template sections
	for _, c := range mainAndDefault(config, dconfig, "display_template ") {
		dt := newDisplayTemplate(c)
		t.displayTemplates[dt.name] = dt
	}
	// Load display_data sections
	groups := map[string][]*configfile.Wrapper{}
	var groupNames []string
	for _, c := range mainAndDefault(config, dconfig, "display_data ") {
		nameParts := strings.Fields(c.Name())
		if len(nameParts) != 3 {
			configfile.Raise("Section name '%s' is not valid", c.Name())
		}
		if _, ok := groups[nameParts[1]]; !ok {
			groupNames = append(groupNames, nameParts[1])
		}
		groups[nameParts[1]] = append(groups[nameParts[1]], c)
	}
	for _, groupName := range groupNames {
		t.displayDataGroups[groupName] = newDisplayGroup(config, groupName, groups[groupName])
	}
	// Load display glyphs
	const dgPrefix = "display_glyph "
	for _, dg := range mainAndDefault(config, dconfig, dgPrefix) {
		glyphName := dg.Name()[len(dgPrefix):]
		if data, ok := dg.GetOptional("data", configfile.Default(nil)); ok {
			idata := parseGlyph(glyphName, data, 16, 16)
			var icon1, icon2 []byte
			for _, bits := range idata {
				icon1 = append(icon1, byte(bits>>8&0xff))
				icon2 = append(icon2, byte(bits&0xff))
			}
			g := t.displayGlyphs[glyphName]
			g.Icon16x16 = &[2][]byte{icon1, icon2}
			t.displayGlyphs[glyphName] = g
		}
		if data, ok := dg.GetOptional("hd44780_data", configfile.Default(nil)); ok {
			slot := dg.GetInt("hd44780_slot", configfile.Min(0), configfile.Max(7))
			idata := parseGlyph(glyphName, data, 5, 8)
			g := t.displayGlyphs[glyphName]
			g.Icon5x8 = &Icon5x8{slot, idata}
			t.displayGlyphs[glyphName] = g
		}
	}
}

func lookupDisplayTemplates(config *configfile.Wrapper) *PrinterDisplayTemplate {
	printer := host.Of(config)
	dt, ok := printer.TryLookupObject("display_template").(*PrinterDisplayTemplate)
	if !ok {
		dt = newPrinterDisplayTemplate(config)
		printer.AddObject("display_template", dt)
	}
	return dt
}

// PrinterLCD is a [display] section.
type PrinterLCD struct {
	printer              *host.Printer
	reactor              *reactor.Reactor
	lcdChip              LCDChip
	menu                 *MenuManager
	displayTemplates     map[string]*DisplayTemplate
	displayDataGroups    map[string]*DisplayGroup
	showDataGroup        *DisplayGroup
	screenUpdateTimer    *reactor.Timer
	redrawRequestPending bool
	redrawTime           float64

	// The hooks of Printdeck (printdeck.go). nil: the display works as in Klipper.
	screenHook func(eventtime float64) bool
	keyHook    func(key string, eventtime float64) bool
}

func newPrinterLCD(config *configfile.Wrapper) *PrinterLCD {
	printer := host.Of(config)
	d := &PrinterLCD{printer: printer, reactor: printer.Reactor()}
	// Load low-level lcd handler
	lcdType := config.Get("lcd_type")
	ctor := config.GetChoice("lcd_type", lcdChips)
	if ctor == nil {
		configfile.Raise("lcd_type '%s' in section '%s' is not supported", lcdType, config.Name())
	}
	d.lcdChip = ctor.(func(*configfile.Wrapper) LCDChip)(config)
	// Load menu and display_status
	name := config.Name()
	if name == "display" {
		// only load menu for primary display
		d.menu = NewMenuManager(config, d)
	}
	printer.LoadObject(config, "display_status")
	// Configurable display
	templates := lookupDisplayTemplates(config)
	d.displayTemplates = templates.displayTemplates
	d.displayDataGroups = templates.displayDataGroups
	d.lcdChip.SetGlyphs(templates.displayGlyphs)
	dgroup := "_default_16x4"
	if cols, _ := d.lcdChip.Dimensions(); cols == 20 {
		dgroup = "_default_20x4"
	}
	dgroup = config.Get("display_group", configfile.Default(dgroup))
	d.showDataGroup = d.displayDataGroups[dgroup]
	if d.showDataGroup == nil {
		configfile.Raise("Unknown display_data group '%s'", dgroup)
	}
	// Screen updating
	printer.RegisterEventHandler("klippy:ready", func(...any) { d.handleReady() })
	d.screenUpdateTimer = d.reactor.RegisterTimer(d.screenUpdateEvent, reactor.Never)
	// Register g-code commands
	g := gcode.Of(printer)
	g.RegisterMuxCommand("SET_DISPLAY_GROUP", "DISPLAY", &name, d.cmdSetDisplayGroup, "Set the active display group")
	if name == "display" {
		g.RegisterMuxCommand("SET_DISPLAY_GROUP", "DISPLAY", nil, d.cmdSetDisplayGroup, "")
	}
	return d
}

func load(config *configfile.Wrapper) any { return newPrinterLCD(config) }

func loadPrefix(config *configfile.Wrapper) any {
	if !config.HasSection("display") {
		configfile.Raise("A primary [display] section must be defined in printer.cfg to use auxiliary displays")
	}
	parts := strings.Fields(config.Name())
	if parts[len(parts)-1] == "display" {
		configfile.Raise("Section name [display display] is not valid. Please choose a different postfix.")
	}
	return newPrinterLCD(config)
}

func init() { host.RegisterModule("display", load, loadPrefix) }

// Dimensions are the columns and the rows of the display.
func (d *PrinterLCD) Dimensions() (int, int) { return d.lcdChip.Dimensions() }

func (d *PrinterLCD) handleReady() {
	d.lcdChip.Init()
	// Start screen update timer
	d.reactor.UpdateTimer(d.screenUpdateTimer, reactor.Now)
}

// Screen updating

func (d *PrinterLCD) screenUpdateEvent(eventtime float64) float64 {
	if d.redrawRequestPending {
		d.redrawRequestPending = false
		d.redrawTime = eventtime + redrawMinTime
	}
	d.lcdChip.Clear()
	if d.screenHook != nil && d.screenHook(eventtime) {
		d.lcdChip.Flush()
		return eventtime + redrawTime
	}
	// update menu component
	if d.menu != nil {
		if d.menu.ScreenUpdateEvent(eventtime) {
			d.lcdChip.Flush()
			return eventtime + redrawTime
		}
	}
	// Update normal display
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Error during display screen update: %v", r)
			}
		}()
		d.showDataGroup.Show(d, d.displayTemplates, eventtime)
	}()
	d.lcdChip.Flush()
	if d.redrawRequestPending {
		return d.redrawTime
	}
	return eventtime + redrawTime
}

// RequestRedraw draws the screen again soon.
func (d *PrinterLCD) RequestRedraw() {
	if d.redrawRequestPending {
		return
	}
	d.redrawRequestPending = true
	d.reactor.UpdateTimer(d.screenUpdateTimer, d.redrawTime)
}

// DrawText draws text with "~glyph~" names at a row and a column, and returns the next column.
func (d *PrinterLCD) DrawText(row, col int, mixedText string, eventtime float64) int {
	pos := col
	for i, text := range strings.Split(mixedText, "~") {
		if i&1 == 0 {
			// write text
			d.lcdChip.WriteText(pos, row, []byte(text))
			pos += utf8.RuneCountInString(text)
		} else {
			// write glyph
			pos += d.lcdChip.WriteGlyph(pos, row, text)
		}
	}
	return pos
}

// DrawProgressBar draws a progress bar of width columns at a row and a column.
func (d *PrinterLCD) DrawProgressBar(row, col, width int, value float64) string {
	shift := int(float64(width)*8*(1.-value) + .5)
	top := width*8 - 1
	if shift < 0 || top < 0 {
		panic(&jinja.Error{Type: "ValueError", Msg: "negative shift count"})
	}
	// The bits are -1 << shift, the top bit, and the bottom bit.
	pixel := func(k int) bool { return k >= shift || k == top || k == 0 }
	for i := 0; i < width; i++ {
		var b byte
		for bit := 0; bit < 8; bit++ {
			if pixel(i*8 + bit) {
				b |= 1 << bit
			}
		}
		data := make([]byte, 16)
		data[0], data[15] = 0xff, 0xff
		for j := 1; j < 15; j++ {
			data[j] = b
		}
		d.lcdChip.WriteGraphics(col+width-1-i, row, data)
	}
	return ""
}

func (d *PrinterLCD) cmdSetDisplayGroup(gcmd *gcode.Command) {
	group := gcmd.GetString("GROUP")
	newDG := d.displayDataGroups[group]
	if newDG == nil {
		panic(gcmd.Error("Unknown display_data group '%s'", group))
	}
	d.showDataGroup = newDG
}
