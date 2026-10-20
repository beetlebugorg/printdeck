package display

// Basic LCD menu support (menu.py).
//
//	Copyright (C) 2020  Janar Sööt <janar.soot@gmail.com>
//
// Python's classes are Go types that embed the type of the base class. Each element keeps a
// pointer to itself as the element interface, so a method of the base type calls the method
// of the subclass.

import (
	_ "embed"
	"fmt"
	"log"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/gcode_macro"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/jinja"
	"printdeck/internal/klipper/reactor"
)

//go:embed menu.cfg
var menuCfg string

// kwargs are the keyword arguments of an element that has no config section.
type kwargs map[string]any

// scriptFunc is a script that is a Go function.
type scriptFunc func(el element, context map[string]any) string

// element is a menu element (MenuElement), with the methods that a subclass overrides.
type element interface {
	base() *menuElement
	init()
	isEditing() bool
	isScrollable() bool
	isEnabled() bool
	startEditing()
	stopEditing()
	getContext(cxt map[string]any) map[string]any
	heartbeat(eventtime float64)
	// handleScript is the default behaviour of a script (handle_script_<name>).
	handleScript(name string)
}

// literalEval is Python's ast.literal_eval. An error is a panic of a *jinja.Error.
func literalEval(s string) any {
	v, err := jinja.LiteralEval(s)
	if err != nil {
		le := err.(*jinja.LiteralError)
		panic(&jinja.Error{Type: le.Type, Msg: le.Msg})
	}
	return v
}

// isValueError tells if a panic value is a Python ValueError.
func isValueError(r any) bool {
	e, ok := r.(*jinja.Error)
	return ok && e.Type == "ValueError"
}

var menuIDs int

// Scriptable menu element abstract baseclass
type menuElement struct {
	self             element
	manager          *MenuManager
	cursor           string
	index            *int
	enable           bool
	name             *string
	enableTpl        *gcode_macro.TemplateWrapper
	nameTpl          *gcode_macro.TemplateWrapper
	ns               string
	lastHeartbeat    float64
	hasLastHeartbeat bool
	scrollPos        int  // -1 is None
	scrollPending    int8 // 1 is True, 0 is False, -1 is None
	scrollNext       float64
	scripts          map[string]any
}

var substituteRE = regexp.MustCompile(`\$(?:(\$)|([_a-zA-Z][_a-zA-Z0-9]*)|\{([_a-zA-Z][_a-zA-Z0-9]*)\})`)

// safeSubstitute is Python's string.Template.safe_substitute with one variable.
func safeSubstitute(s, name, value string) string {
	return substituteRE.ReplaceAllStringFunc(s, func(m string) string {
		sub := substituteRE.FindStringSubmatch(m)
		switch {
		case sub[1] != "":
			return "$"
		case sub[2] == name || sub[3] == name:
			return value
		}
		return m
	})
}

func newMenuElement(self element, manager *MenuManager, config *configfile.Wrapper, kw kwargs) *menuElement {
	e := &menuElement{self: self, manager: manager, cursor: ">", enable: true, scrollPos: -1}
	// set class defaults and attributes from arguments
	if v, ok := kw["index"].(int); ok {
		e.index = &v
	}
	if v, ok := kw["enable"].(bool); ok {
		e.enable = v
	}
	if v, ok := kw["name"].(string); ok {
		e.name = &v
	}
	if config != nil {
		// overwrite class attributes from config
		var def any
		if e.index != nil {
			def = *e.index
		}
		if v, ok := config.GetIntOptional("index", configfile.Default(def)); ok {
			e.index = &v
		} else {
			e.index = nil
		}
		e.nameTpl = manager.gcodeMacro.LoadTemplate(config, "name", e.name)
		func() {
			defer func() {
				if r := recover(); r != nil {
					if _, ok := r.(*configfile.Error); !ok {
						panic(r)
					}
					e.enableTpl = manager.gcodeMacro.LoadTemplate(config, "enable", nil)
				}
			}()
			e.enable = config.GetBool("enable", configfile.Default(e.enable))
		}()
		// item namespace - used in relative paths
		e.ns = strings.TrimSpace(strings.Join(strings.Split(config.Name(), " ")[1:], " "))
	} else {
		// ns - item namespace key, used in item relative paths
		// $__id - generated id text variable
		menuIDs++
		id := fmt.Sprintf("__menu_%x", menuIDs)
		ns, ok := kw["ns"].(string)
		if !ok {
			ns = id
		}
		e.ns = safeSubstitute("menu "+ns, "__id", id)
	}
	// menu scripts
	e.scripts = map[string]any{}
	return e
}

func (e *menuElement) base() *menuElement { return e }
func (e *menuElement) init()              {}

func (e *menuElement) renderName() string {
	if e.nameTpl != nil {
		context := e.self.getContext(nil)
		return asflat(e.nameTpl.Render(context))
	}
	return asflat(*e.name)
}

// loadScript loads a script template from the config, or a script from the keyword arguments.
func (e *menuElement) loadScript(config *configfile.Wrapper, kw kwargs, name string) {
	if _, ok := e.scripts[name]; ok {
		log.Printf("Declaration of '%s' hides previous script declaration", name)
	}
	if config == nil {
		e.scripts[name] = kw[name]
	} else {
		def := ""
		e.scripts[name] = e.manager.gcodeMacro.LoadTemplate(config, name, &def)
	}
}

func (e *menuElement) isEditing() bool     { return false }
func (e *menuElement) isScrollable() bool  { return true }
func (e *menuElement) isEnabled() bool     { return e.evalEnable(e.self.getContext(nil)) }
func (e *menuElement) startEditing()       {}
func (e *menuElement) stopEditing()        {}
func (e *menuElement) handleScript(string) {}

func (e *menuElement) getContext(cxt map[string]any) map[string]any {
	// get default menu context
	context := e.manager.getContext(cxt)
	context["menu"].(*jinja.Dict).Set("ns", e.getNS("."))
	return context
}

func (e *menuElement) evalEnable(context map[string]any) bool {
	if e.enableTpl != nil {
		return jinja.Truthy(literalEval(e.enableTpl.Render(context)))
	}
	return e.enable
}

// selectItem is called when an item is selected (select).
func (e *menuElement) selectItem() { e.resetScroller() }

func (e *menuElement) heartbeat(eventtime float64) {
	e.lastHeartbeat, e.hasLastHeartbeat = eventtime, true
	if eventtime >= e.scrollNext {
		e.scrollNext = eventtime + 0.5
		if !e.self.isEditing() {
			e.updateScroller()
		}
	}
}

func (e *menuElement) updateScroller() {
	switch {
	case e.scrollPos < 0 && e.scrollPending == 1:
		e.scrollPos = 0
	case e.scrollPending == 1:
		e.scrollPos++
		e.scrollPending = 0
	case e.scrollPending == 0:
		// hold scroll position
	case e.scrollPending == -1:
		e.resetScroller()
	}
}

func (e *menuElement) resetScroller() {
	e.scrollPos = -1
	e.scrollPending = 0
}

// needScroller controls the scroller: 1 increases the scroll position on the next update, 0
// holds the scroll position, and -1 resets the scroller.
func (e *menuElement) needScroller(value int8) { e.scrollPending = value }

var glyphRE = regexp.MustCompile(`~.*?~`)

func sliceName(name string, index int) string {
	// The characters of the text, and each glyph placeholder
	var chunks []string
	last := 0
	for _, loc := range glyphRE.FindAllStringIndex(name, -1) {
		for _, r := range name[last:loc[0]] {
			chunks = append(chunks, string(r))
		}
		chunks = append(chunks, name[loc[0]:loc[1]])
		last = loc[1]
	}
	for _, r := range name[last:] {
		chunks = append(chunks, string(r))
	}
	if index > len(chunks) {
		index = len(chunks)
	}
	return strings.Join(chunks[index:], "")
}

func (e *menuElement) renderName2(selected bool) string {
	name := e.renderName()
	if selected && e.scrollPos >= 0 {
		name = sliceName(name, e.scrollPos)
	} else {
		e.resetScroller()
	}
	return name
}

func (e *menuElement) getNS(name string) string {
	name = strings.TrimSpace(name)
	if strings.HasPrefix(name, "..") {
		parts := strings.Split(e.ns, " ")
		name = strings.Join(parts[:len(parts)-1], " ") + " " + name[2:]
	} else if strings.HasPrefix(name, ".") {
		name = e.ns + " " + name[1:]
	}
	return strings.TrimSpace(name)
}

func (e *menuElement) sendEvent(event string, args ...any) {
	e.manager.sendEvent(e.getNS(".")+":"+event, args...)
}

func (e *menuElement) runScriptOf(name string, context map[string]any) string {
	switch s := e.scripts[name].(type) {
	case *gcode_macro.TemplateWrapper:
		// check template
		return s.Render(context)
	case scriptFunc:
		// check callback
		return s(e.self, context)
	case string:
		// check static string
		return s
	}
	return ""
}

// runScript runs a script. An empty event is the name of the script. A nil context is the
// default context.
func (e *menuElement) runScript(name, event string, context map[string]any, renderOnly bool) string {
	result := ""
	// init context
	if _, ok := e.scripts[name]; ok {
		context = e.self.getContext(context)
		if event == "" {
			event = name
		}
		context["menu"].(*jinja.Dict).Set("event", event)
		result = e.runScriptOf(name, context)
	}
	if !renderOnly {
		// run result as gcode
		e.manager.queueGcode(result)
		// default behaviour
		e.self.handleScript(name)
	}
	return result
}

func (e *menuElement) cursorText() string {
	if e.cursor == "" {
		return ""
	}
	_, n := utf8.DecodeRuneInString(e.cursor)
	return e.cursor[:n]
}

// container is a menu container (MenuContainer).
type container interface {
	element
	cbase() *menuContainer
	namesAsList() []string
	populateMore()
	drawContainer(nrows int, eventtime float64)
}

type itemName struct {
	item element
	name string
}

// Menu container abstract class
type menuContainer struct {
	*menuElement
	cself      container
	populateCB func(c container)
	selected   int // -1 is None
	allitems   []itemName
	names      []string
	items      []element
	parents    []container
}

func newMenuContainer(self container, manager *MenuManager, config *configfile.Wrapper, kw kwargs) *menuContainer {
	c := &menuContainer{menuElement: newMenuElement(self, manager, config, kw), cself: self, selected: -1}
	if cb, ok := kw["populate"].(func(c container)); ok {
		c.populateCB = cb
	}
	c.cursor = ">"
	return c
}

func (c *menuContainer) cbase() *menuContainer { return c }

func (c *menuContainer) init() {
	// recursive guard
	c.parents = nil
}

func (c *menuContainer) namesAsList() []string { return nil }
func (c *menuContainer) populateMore()         {}

func (c *menuContainer) isEditing() bool {
	for _, item := range c.items {
		if item.isEditing() {
			return true
		}
	}
	return false
}

func (c *menuContainer) stopEditing() {
	for _, item := range c.items {
		if item.isEditing() {
			item.stopEditing()
		}
	}
}

func (c *menuContainer) lookupItem(item any) (element, string) {
	switch x := item.(type) {
	case string:
		name := strings.TrimSpace(x)
		ns := c.getNS(name)
		return c.manager.lookupMenuitem(ns), name
	case element:
		return x, x.base().getNS(".")
	}
	return nil, ""
}

func (c *menuContainer) indexOfLocal(item any) int {
	switch x := item.(type) {
	case string:
		s := strings.TrimSpace(x)
		for i, n := range c.names {
			if n == s {
				return i
			}
		}
	case element:
		for i, it := range c.items {
			if it == x {
				return i
			}
		}
	}
	return -1
}

// indexOf returns the index of an item, or -1.
func (c *menuContainer) indexOf(item any, lookInside bool) int {
	index := c.indexOfLocal(item)
	if index < 0 && lookInside {
		for _, con := range c.items {
			// Python tests the index as a bool, so the index 0 does not count.
			if cc, ok := con.(container); ok && cc.cbase().indexOfLocal(item) > 0 {
				index = c.indexOfLocal(con)
			}
		}
	}
	return index
}

func (c *menuContainer) assertRecursiveRelation() {
	for _, p := range c.parents {
		if p == c.cself {
			panic(fmt.Errorf("AssertionError: Recursive relation of '%s' container", c.getNS(".")))
		}
	}
}

// insertItem inserts an item at an index, or at the end for -1.
func (c *menuContainer) insertItem(s any, index int) {
	item, name := c.lookupItem(s)
	if item == nil {
		return
	}
	item.init()
	if cc, ok := item.(container); ok {
		cb := cc.cbase()
		cb.parents = append(cb.parents, c.parents...)
		cb.parents = append(cb.parents, c.cself)
		cb.assertRecursiveRelation()
	}
	in := itemName{item, name}
	if index < 0 {
		c.allitems = append(c.allitems, in)
	} else {
		index = min(index, len(c.allitems))
		c.allitems = append(c.allitems[:index], append([]itemName{in}, c.allitems[index:]...)...)
	}
}

func (c *menuContainer) populate() {
	c.allitems = nil
	for _, name := range c.cself.namesAsList() {
		c.insertItem(name, -1)
	}
	// populate successor items
	c.cself.populateMore()
	// run populate callback
	if c.populateCB != nil {
		c.populateCB(c.cself)
	}
	// send populate event
	c.sendEvent("populate", c.cself)
}

func (c *menuContainer) updateItems() {
	var items []element
	var names []string
	for _, in := range c.allitems {
		if in.item.isEnabled() {
			items = append(items, in.item)
			names = append(names, in.name)
		}
	}
	if len(items) == 0 {
		panic(&jinja.Error{Type: "ValueError", Msg: "not enough values to unpack (expected 2, got 0)"})
	}
	c.items, c.names = items, names
}

// select methods

func (c *menuContainer) initSelection() { c.selectAt(0) }

func (c *menuContainer) selectAt(index int) element {
	c.selected = index
	// select element
	item := c.selectedItem()
	if item != nil {
		item.base().selectItem()
	}
	return item
}

func (c *menuContainer) selectedItem() element {
	if c.selected >= 0 && c.selected < len(c.items) {
		return c.items[c.selected]
	}
	return nil
}

func (c *menuContainer) selectNext() element {
	var index int
	switch {
	case c.selected < 0:
		index = -1
		if len(c.items) > 0 {
			index = 0
		}
	case c.selected < len(c.items)-1:
		index = c.selected + 1
	default:
		index = c.selected
	}
	return c.selectAt(index)
}

func (c *menuContainer) selectPrev() element {
	var index int
	switch {
	case c.selected < 0:
		index = -1
		if len(c.items) > 0 {
			index = 0
		}
	case 0 < c.selected && c.selected < len(c.items):
		index = c.selected - 1
	default:
		index = c.selected
	}
	return c.selectAt(index)
}

func (c *menuContainer) drawContainer(nrows int, eventtime float64) {}

type menuDisabled struct{ *menuElement }

func newMenuDisabled(manager *MenuManager, config *configfile.Wrapper, kw kwargs) element {
	d := &menuDisabled{}
	d.menuElement = newMenuElement(d, manager, config, kwargs{"name": ""})
	d.self.init()
	return d
}

func (d *menuDisabled) isEnabled() bool { return false }

type menuCommand struct{ *menuElement }

func newMenuCommand(manager *MenuManager, config *configfile.Wrapper, kw kwargs) element {
	c := &menuCommand{}
	c.menuElement = newMenuElement(c, manager, config, kw)
	c.self.init()
	c.loadScript(config, kw, "gcode")
	return c
}

type menuInput struct {
	*menuCommand
	input                              any
	inputMin, inputMax                 any
	inputStep                          float64
	realtime                           bool
	inputTpl, inputMinTpl, inputMaxTpl *gcode_macro.TemplateWrapper
	isDirty                            bool
	lastChange                         float64
	hasLastChange                      bool
	inputValue                         float64
	hasInputValue                      bool
	evalMinValue, evalMaxValue         float64
}

func kwOr(kw kwargs, name string, def any) any {
	if v, ok := kw[name]; ok {
		return v
	}
	return def
}

func newMenuInput(manager *MenuManager, config *configfile.Wrapper, kw kwargs) element {
	m := &menuInput{menuCommand: &menuCommand{}}
	m.menuElement = newMenuElement(m, manager, config, kw)
	m.self.init()
	m.loadScript(config, kw, "gcode")
	// set class defaults and attributes from arguments
	m.input = kw["input"]
	m.inputMin = kwOr(kw, "input_min", -999999.0)
	m.inputMax = kwOr(kw, "input_max", 999999.0)
	m.inputStep = pyFloat(kwOr(kw, "input_step", 1.0))
	m.realtime = jinja.Truthy(kwOr(kw, "realtime", false))
	if config != nil {
		// overwrite class attributes from config
		m.realtime = config.GetBool("realtime", configfile.Default(m.realtime))
		m.inputTpl = manager.gcodeMacro.LoadTemplate(config, "input", nil)
		minDef, maxDef := jinja.Str(m.inputMin), jinja.Str(m.inputMax)
		m.inputMinTpl = manager.gcodeMacro.LoadTemplate(config, "input_min", &minDef)
		m.inputMaxTpl = manager.gcodeMacro.LoadTemplate(config, "input_max", &maxDef)
		m.inputStep = config.GetFloat("input_step", configfile.Default(m.inputStep), configfile.Above(0.))
	}
	return m
}

func (m *menuInput) init() {
	m.isDirty = false
	m.hasLastChange = false
	m.hasInputValue = false
}

func (m *menuInput) isScrollable() bool { return false }
func (m *menuInput) isEditing() bool    { return m.hasInputValue }

func (m *menuInput) stopEditing() {
	if !m.isEditing() {
		return
	}
	m.resetValue()
}

func (m *menuInput) startEditing() {
	if m.isEditing() {
		return
	}
	m.initValue()
}

func (m *menuInput) heartbeat(eventtime float64) {
	m.menuElement.heartbeat(eventtime)
	if m.isDirty && m.hasLastChange && m.hasInputValue && eventtime-m.lastChange > 0.250 {
		if m.realtime {
			m.runScript("gcode", "change", nil, false)
			m.runScript("change", "", nil, false)
		}
		m.isDirty = false
	}
}

func (m *menuInput) getContext(cxt map[string]any) map[string]any {
	context := m.menuElement.getContext(cxt)
	var value any
	if !m.hasInputValue {
		if v, ok := m.evalValue(context); ok {
			value = v
		}
	} else {
		value = m.inputValue
	}
	context["menu"].(*jinja.Dict).Set("input", value)
	return context
}

func (m *menuInput) isEnabled() bool { return m.evalEnable(m.menuElement.getContext(nil)) }

// evalFloat returns float(ast.literal_eval()) of a rendered template, or of a value. A ValueError
// is logged and returns false.
func evalFloat(tpl *gcode_macro.TemplateWrapper, value any, context map[string]any, what string) (f float64, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			if !isValueError(r) {
				panic(r)
			}
			log.Printf("Input %s evaluation error: %v", what, r)
			f, ok = 0, false
		}
	}()
	if tpl != nil {
		return pyFloat(literalEval(tpl.Render(context))), true
	}
	return pyFloat(value), true
}

func (m *menuInput) evalMin(context map[string]any) (float64, bool) {
	return evalFloat(m.inputMinTpl, m.inputMin, context, "min value")
}

func (m *menuInput) evalMax(context map[string]any) (float64, bool) {
	return evalFloat(m.inputMaxTpl, m.inputMax, context, "max value")
}

func (m *menuInput) evalValue(context map[string]any) (float64, bool) {
	return evalFloat(m.inputTpl, m.input, context, "value")
}

func (m *menuInput) valueChanged() {
	m.lastChange, m.hasLastChange = m.lastHeartbeat, m.hasLastHeartbeat
	m.isDirty = true
}

func (m *menuInput) initValue() {
	context := m.menuElement.getContext(nil)
	m.hasInputValue = false
	lo, okMin := m.evalMin(context)
	hi, okMax := m.evalMax(context)
	m.inputMin, m.inputMax = lo, hi
	if !okMin {
		m.inputMin = nil
	}
	if !okMax {
		m.inputMax = nil
	}
	v, okValue := m.evalValue(context)
	if !okMin || !okMax || !okValue {
		panic(&jinja.Error{Type: "TypeError", Msg: "'>' not supported between instances of 'NoneType' and 'float'"})
	}
	m.evalMinValue, m.evalMaxValue = lo, hi
	m.inputValue, m.hasInputValue = pyMin(hi, pyMax(lo, v)), true
	m.valueChanged()
}

// pyMin and pyMax are Python's min() and max() of two values.
func pyMin(a, b float64) float64 {
	if b < a {
		return b
	}
	return a
}

func pyMax(a, b float64) float64 {
	if b > a {
		return b
	}
	return a
}

func (m *menuInput) resetValue() { m.hasInputValue = false }

func (m *menuInput) getInputStep(fastRate bool) float64 {
	if fastRate && (m.evalMaxValue-m.evalMinValue)/m.inputStep > 100.0 {
		return 10.0 * m.inputStep
	}
	return m.inputStep
}

func (m *menuInput) incValue(fastRate bool) {
	lastValue := m.inputValue
	if !m.hasInputValue {
		return
	}
	inputStep := m.getInputStep(fastRate)
	m.inputValue += math.Abs(inputStep)
	m.inputValue = pyMin(m.evalMaxValue, pyMax(m.evalMinValue, m.inputValue))
	if lastValue != m.inputValue {
		m.valueChanged()
	}
}

func (m *menuInput) decValue(fastRate bool) {
	lastValue := m.inputValue
	if !m.hasInputValue {
		return
	}
	inputStep := m.getInputStep(fastRate)
	m.inputValue -= math.Abs(inputStep)
	m.inputValue = pyMin(m.evalMaxValue, pyMax(m.evalMinValue, m.inputValue))
	if lastValue != m.inputValue {
		m.valueChanged()
	}
}

// default behaviour on click
func (m *menuInput) handleScript(name string) {
	if name != "click" {
		return
	}
	if !m.isEditing() {
		m.startEditing()
	} else {
		m.stopEditing()
	}
}

type menuList struct {
	*menuContainer
	viewportTop int
	itemBack    element
}

func initMenuList(l *menuList, self container, manager *MenuManager, config *configfile.Wrapper, kw kwargs) {
	l.menuContainer = newMenuContainer(self, manager, config, kw)
	l.self.init()
	// create back item
	l.itemBack = manager.menuitemFrom("command", kwargs{"name": "..",
		"gcode": scriptFunc(func(el element, context map[string]any) string {
			el.base().manager.back(false, true)
			return ""
		})})
}

func newMenuList(manager *MenuManager, config *configfile.Wrapper, kw kwargs) element {
	l := &menuList{}
	initMenuList(l, l, manager, config, kw)
	return l
}

func (l *menuList) isList() bool { return true }

func (l *menuList) namesAsList() []string {
	return l.manager.lookupChildren(l.getNS("."))
}

func (l *menuList) populateMore() {
	l.viewportTop = 0
	//  add back as first item
	l.insertItem(l.itemBack, 0)
}

func padRight(s string, width int) string {
	if n := utf8.RuneCountInString(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

type listMarker interface{ isList() bool }

func (l *menuList) drawContainer(nrows int, eventtime float64) {
	display := l.manager.display
	selectedRow := l.selected
	// adjust viewport
	if selectedRow >= 0 {
		if selectedRow >= l.viewportTop+nrows {
			l.viewportTop = (selectedRow - nrows) + 1
		}
		if selectedRow < l.viewportTop {
			l.viewportTop = selectedRow
		}
	} else {
		l.viewportTop = 0
	}
	// clamps viewport
	l.viewportTop = max(0, min(l.viewportTop, len(l.items)-nrows))
	defer func() {
		if r := recover(); r != nil {
			log.Printf("List drawing error: %v", r)
		}
	}()
	y := 0
	// Python keeps current and selected of the last item for the rows after the items.
	var current element
	var selected bool
	for row := l.viewportTop; row < l.viewportTop+nrows; row++ {
		text, prefix, suffix := "", "", ""
		if row < len(l.items) {
			current = l.items[row]
			selected = row == selectedRow
			if selected {
				current.heartbeat(eventtime)
			}
			text = current.base().renderName2(selected)
			// add prefix (selection indicator)
			if selected && !current.isEditing() {
				prefix = current.base().cursorText()
			} else if selected && current.isEditing() {
				prefix = "*"
			} else {
				prefix = " "
			}
			// add suffix (folder indicator)
			if _, ok := current.(listMarker); ok {
				suffix += ">"
			}
		}
		// draw to display
		plen := utf8.RuneCountInString(prefix)
		slen := utf8.RuneCountInString(suffix)
		width := l.manager.cols - plen - slen
		// draw item prefix (cursor)
		ppos := display.DrawText(y, 0, prefix, eventtime)
		// draw item name
		tpos := display.DrawText(y, ppos, padRight(text, width), eventtime)
		if current == nil {
			panic(fmt.Errorf("NameError: name 'current' is not defined"))
		}
		// check scroller
		if selected && tpos > l.manager.cols && current.isScrollable() {
			// scroll next
			current.base().needScroller(1)
		} else {
			// reset scroller
			current.base().needScroller(-1)
		}
		// draw item suffix
		if suffix != "" {
			display.DrawText(y, l.manager.cols-slen, suffix, eventtime)
		}
		// next display row
		y++
	}
}

// fileLister is the virtual_sdcard object.
type fileLister interface {
	// FileNames are the names of the files, as get_file_list().
	FileNames() []string
}

type menuVSDList struct{ menuList }

func newMenuVSDList(manager *MenuManager, config *configfile.Wrapper, kw kwargs) element {
	l := &menuVSDList{}
	initMenuList(&l.menuList, l, manager, config, kw)
	return l
}

func (l *menuVSDList) populateMore() {
	l.menuList.populateMore()
	if sdcard, ok := l.manager.printer.TryLookupObject("virtual_sdcard").(fileLister); ok {
		for _, fname := range sdcard.FileNames() {
			l.insertItem(l.manager.menuitemFrom("command", kwargs{"name": jinja.Repr(fname),
				"gcode": "M23 /" + fname}), -1)
		}
	}
}

// menuItems are the menu item types. init sets them, because a list makes its back item.
var menuItems map[string]func(*MenuManager, *configfile.Wrapper, kwargs) element

func init() {
	menuItems = map[string]func(*MenuManager, *configfile.Wrapper, kwargs) element{
		"disabled": newMenuDisabled,
		"command":  newMenuCommand,
		"input":    newMenuInput,
		"list":     newMenuList,
		"vsdlist":  newMenuVSDList,
	}
}

// menuItemsRepr is Python's repr() of menu_items, for the error message.
const menuItemsRepr = "{'disabled': <class 'extras.display.menu.MenuDisabled'>, " +
	"'command': <class 'extras.display.menu.MenuCommand'>, " +
	"'input': <class 'extras.display.menu.MenuInput'>, " +
	"'list': <class 'extras.display.menu.MenuList'>, " +
	"'vsdlist': <class 'extras.display.menu.MenuVSDList'>}"

const menuTimerDelay = 1.0

// MenuManager is the menu of the primary display.
type MenuManager struct {
	running           bool
	menuitems         map[string]element
	menustack         []container
	children          map[string][]string
	display           *PrinterLCD
	printer           *host.Printer
	pconfig           *configfile.PrinterConfig
	gcode             *gcode.Dispatch
	gcodeQueue        []string
	context           map[string]any
	root              element
	rootName          string
	cols, rows        int
	timeout           int
	timer             int
	reverseNavigation bool
	gcodeMacro        *gcode_macro.PrinterGCodeMacro
}

// NewMenuManager returns the menu of a display.
func NewMenuManager(config *configfile.Wrapper, display *PrinterLCD) *MenuManager {
	printer := host.Of(config)
	m := &MenuManager{menuitems: map[string]element{}, children: map[string][]string{}, display: display,
		printer: printer, context: map[string]any{}}
	m.pconfig = printer.LookupObject("configfile").(*configfile.PrinterConfig)
	m.gcode = gcode.Of(printer)
	m.rootName = config.Get("menu_root", configfile.Default("__main"))
	m.cols, m.rows = display.Dimensions()
	m.timeout = config.GetInt("menu_timeout", configfile.Default(0))
	// reverse container navigation
	m.reverseNavigation = config.GetBool("menu_reverse_navigation", configfile.Default(false))
	// load printer objects
	m.gcodeMacro = gcode_macro.Load(config)
	// register itself for printer callbacks
	printer.AddObject("menu", m)
	printer.RegisterEventHandler("klippy:ready", func(...any) { m.handleReady() })
	// register for key events
	newMenuKeys(config, m.keyEvent)
	// Load local config file in same directory as current module
	m.loadConfig()
	// Load items from main config
	m.loadMenuitems(config)
	// Load menu root
	m.root = m.lookupMenuitem(m.rootName)
	// send init event
	m.sendEvent("init", m)
	return m
}

func (m *MenuManager) handleReady() {
	// start timer
	m.printer.Reactor().RegisterTimer(m.timerEvent, reactor.Now)
}

func (m *MenuManager) timerEvent(eventtime float64) float64 {
	m.timeoutCheck(eventtime)
	return eventtime + menuTimerDelay
}

func (m *MenuManager) rootContainer() bool {
	_, ok := m.root.(container)
	return ok
}

func (m *MenuManager) timeoutCheck(eventtime float64) {
	if m.running && m.timeout > 0 && m.rootContainer() {
		if m.timer >= m.timeout {
			m.exit(false)
		} else {
			m.timer++
		}
	} else {
		m.timer = 0
	}
}

func (m *MenuManager) sendEvent(event string, args ...any) {
	m.printer.SendEvent("menu:"+event, args...)
}

func (m *MenuManager) begin(eventtime float64) {
	m.menustack = nil
	m.timer = 0
	if root, ok := m.root.(container); ok {
		// send begin event
		m.sendEvent("begin", m)
		m.updateContext(eventtime)
		root.cbase().initSelection()
		m.stackPush(root)
		m.running = true
		return
	} else if m.root != nil {
		log.Print("Invalid root, menu stopped!")
	}
	m.running = false
}

// Status is the status of the menu.
func (m *MenuManager) Status(eventtime float64) map[string]any {
	return map[string]any{"timeout": m.timeout, "running": m.running, "rows": m.rows, "cols": m.cols}
}

// isTrue is Python's "value is True".
func isTrue(v any) bool { b, ok := v.(bool); return ok && b }

func (m *MenuManager) getContext(cxt map[string]any) map[string]any {
	context := map[string]any{}
	for k, v := range m.context {
		context[k] = v
	}
	for k, v := range cxt {
		context[k] = v
	}
	return context
}

func (m *MenuManager) updateContext(eventtime float64) {
	// menu default jinja2 context
	m.context = m.gcodeMacro.CreateTemplateContextAt(eventtime)
	menu := jinja.NewDict()
	menu.Set("eventtime", eventtime)
	menu.Set("back", jinja.Func(func(args []any, kw map[string]any) any {
		force, update := any(false), any(true)
		if len(args) > 0 {
			force = args[0]
		} else if v, ok := kw["force"]; ok {
			force = v
		}
		if len(args) > 1 {
			update = args[1]
		} else if v, ok := kw["update"]; ok {
			update = v
		}
		m.back(isTrue(force), isTrue(update))
		return ""
	}))
	menu.Set("exit", jinja.Func(func(args []any, kw map[string]any) any {
		force := any(false)
		if len(args) > 0 {
			force = args[0]
		} else if v, ok := kw["force"]; ok {
			force = v
		}
		m.exit(isTrue(force))
		return ""
	}))
	m.context["menu"] = menu
}

func (m *MenuManager) stackPush(c container) {
	c.cbase().populate()
	top := m.stackPeek(0)
	if _, ok := top.(listMarker); ok {
		top.base().runScript("leave", "", nil, false)
	}
	if _, ok := c.(listMarker); ok {
		c.base().runScript("enter", "", nil, false)
	}
	if !c.isEditing() {
		c.cbase().updateItems()
		c.cbase().initSelection()
	}
	m.menustack = append(m.menustack, c)
}

func (m *MenuManager) stackPop(update bool) container {
	var c container
	if len(m.menustack) > 0 {
		c = m.menustack[len(m.menustack)-1]
		m.menustack = m.menustack[:len(m.menustack)-1]
		top := m.stackPeek(0)
		if top != nil {
			if !top.isEditing() && update {
				top.cbase().updateItems()
				top.cbase().initSelection()
			}
			if _, ok := c.(listMarker); ok {
				c.base().runScript("leave", "", nil, false)
			}
			if _, ok := top.(listMarker); ok {
				top.base().runScript("enter", "", nil, false)
			}
		} else {
			if _, ok := c.(listMarker); ok {
				c.base().runScript("leave", "", nil, false)
			}
		}
	}
	return c
}

func (m *MenuManager) stackPeek(lvl int) container {
	if len(m.menustack) > lvl {
		return m.menustack[len(m.menustack)-lvl-1]
	}
	return nil
}

// ScreenUpdateEvent draws the menu, and tells if the menu is running.
func (m *MenuManager) ScreenUpdateEvent(eventtime float64) bool {
	// screen update
	if !m.running {
		return false
	}
	// draw menu
	m.updateContext(eventtime)
	c := m.stackPeek(0)
	if m.running && c != nil {
		c.heartbeat(eventtime)
		c.drawContainer(m.rows, eventtime)
	}
	return true
}

func (m *MenuManager) up(fastRate bool) {
	c := m.stackPeek(0)
	if m.running && c != nil {
		m.timer = 0
		current := c.cbase().selectedItem()
		if in, ok := current.(*menuInput); ok && in.isEditing() {
			in.decValue(fastRate)
		} else if m.reverseNavigation {
			c.cbase().selectNext() // reverse
		} else {
			c.cbase().selectPrev() // normal
		}
	}
}

func (m *MenuManager) down(fastRate bool) {
	c := m.stackPeek(0)
	if m.running && c != nil {
		m.timer = 0
		current := c.cbase().selectedItem()
		if in, ok := current.(*menuInput); ok && in.isEditing() {
			in.incValue(fastRate)
		} else if m.reverseNavigation {
			c.cbase().selectPrev() // reverse
		} else {
			c.cbase().selectNext() // normal
		}
	}
}

func (m *MenuManager) back(force, update bool) {
	c := m.stackPeek(0)
	if m.running && c != nil {
		m.timer = 0
		current := c.cbase().selectedItem()
		if in, ok := current.(*menuInput); ok && in.isEditing() {
			if force {
				in.stopEditing()
			} else {
				return
			}
		}
		parent := m.stackPeek(1)
		if parent != nil {
			m.stackPop(update)
			index := parent.cbase().indexOf(c, true)
			if index >= 0 {
				parent.cbase().selectAt(index)
			} else if parent.cbase().selectedItem() == nil {
				parent.cbase().initSelection()
			}
		} else {
			m.stackPop(true)
			m.running = false
		}
	}
}

func (m *MenuManager) exit(force bool) {
	c := m.stackPeek(0)
	if m.running && c != nil {
		m.timer = 0
		current := c.cbase().selectedItem()
		if in, ok := current.(*menuInput); !force && ok && in.isEditing() {
			return
		}
		if _, ok := c.(listMarker); ok {
			c.base().runScript("leave", "", nil, false)
		}
		m.sendEvent("exit", m)
		m.running = false
	}
}

func (m *MenuManager) pushContainer(menu element) bool {
	c := m.stackPeek(0)
	if m.running && c != nil {
		if mc, ok := menu.(container); ok && !c.isEditing() && mc != c {
			m.stackPush(mc)
			return true
		}
	}
	return false
}

func (m *MenuManager) press(event string) {
	c := m.stackPeek(0)
	if m.running && c != nil {
		m.timer = 0
		current := c.cbase().selectedItem()
		switch cur := current.(type) {
		case container:
			m.stackPush(cur)
		case *menuInput:
			if cur.isEditing() {
				cur.runScript("gcode", event, nil, false)
			}
			cur.runScript(event, "", nil, false)
		case *menuCommand:
			cur.runScript("gcode", event, nil, false)
			cur.runScript(event, "", nil, false)
		default:
			// current is None, no selection. passthru to container
			c.base().runScript(event, "", nil, false)
		}
	}
}

func (m *MenuManager) queueGcode(script string) {
	if script == "" {
		return
	}
	if len(m.gcodeQueue) == 0 {
		m.printer.Reactor().RegisterCallback(m.dispatchGcode, reactor.Now)
	}
	m.gcodeQueue = append(m.gcodeQueue, script)
}

func (m *MenuManager) dispatchGcode(eventtime float64) any {
	for len(m.gcodeQueue) > 0 {
		script := m.gcodeQueue[0]
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("Script running error: %v", r)
				}
			}()
			m.gcode.RunScript(script)
		}()
		m.gcodeQueue = m.gcodeQueue[1:]
	}
	return nil
}

func (m *MenuManager) menuitemFrom(typ string, kw kwargs) element {
	ctor, ok := menuItems[typ]
	if !ok {
		panic(fmt.Errorf("Choice '%s' for option '%s' is not a valid choice", typ, menuItemsRepr))
	}
	return ctor(m, nil, kw)
}

func (m *MenuManager) addMenuitem(name string, item element) {
	_, existingItem := m.menuitems[name]
	if existingItem {
		log.Printf("Declaration of '%s' hides previous menuitem declaration", name)
	}
	m.menuitems[name] = item
	parent := item.base().getNS("..")
	if parent != "" && !existingItem {
		ns := item.base().getNS(".")
		if idx := item.base().index; idx != nil {
			m.children[parent] = pyInsert(m.children[parent], *idx, ns)
		} else {
			m.children[parent] = append(m.children[parent], ns)
		}
	}
}

// pyInsert is Python's list.insert().
func pyInsert(list []string, index int, v string) []string {
	if index < 0 {
		index = max(0, len(list)+index)
	}
	index = min(index, len(list))
	return append(list[:index], append([]string{v}, list[index:]...)...)
}

func (m *MenuManager) lookupMenuitem(name string) element {
	if item, ok := m.menuitems[name]; ok {
		return item
	}
	configfile.Raise("Unknown menuitem '%s'", name)
	return nil
}

func (m *MenuManager) lookupChildren(ns string) []string {
	return append([]string(nil), m.children[ns]...)
}

func (m *MenuManager) loadConfig() {
	cfg, err := m.pconfig.ReadConfigData(menuCfg, "menu.cfg")
	if err != nil {
		configfile.Raise("Cannot load config '%s'", "menu.cfg")
	}
	m.loadMenuitems(cfg)
}

func (m *MenuManager) loadMenuitems(config *configfile.Wrapper) {
	for _, cfg := range config.PrefixSections("menu ") {
		typ := cfg.Get("type")
		ctor, ok := menuItems[typ]
		if !ok {
			panic(fmt.Errorf("Choice '%s' for option '%s' is not a valid choice", typ, menuItemsRepr))
		}
		item := ctor(m, cfg, nil)
		m.addMenuitem(item.base().getNS("."), item)
	}
}

func (m *MenuManager) clickCallback(eventtime float64, event string) {
	if m.running {
		m.press(event)
	} else {
		// lets start and populate the menu items
		m.begin(eventtime)
	}
}

func (m *MenuManager) keyEvent(key string, eventtime float64) {
	switch key {
	case "click", "long_click":
		m.clickCallback(eventtime, key)
	case "up":
		m.up(false)
	case "fast_up":
		m.up(true)
	case "down":
		m.down(false)
	case "fast_down":
		m.down(true)
	case "back":
		m.back(false, true)
	}
	m.display.RequestRedraw()
}

// Collection of manager class helper methods

// stripliterals removes the double or single quotes at the start and the end.
func stripliterals(s string) string {
	if (strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`)) ||
		(strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'")) {
		if len(s) < 2 {
			return ""
		}
		s = s[1 : len(s)-1]
	}
	return s
}

// lineBreaks are the line boundaries of Python's str.splitlines().
var lineBreaks = strings.NewReplacer("\r\n", "", "\n", "", "\r", "", "\v", "", "\f", "", "\x1c", "", "\x1d", "",
	"\x1e", "", "\u0085", "", " ", "", " ", "")

// asflat removes the line breaks and the quotes of a name.
func asflat(s string) string { return stripliterals(lineBreaks.Replace(s)) }
