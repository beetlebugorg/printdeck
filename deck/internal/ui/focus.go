package ui

import (
	"fmt"
	"image"
	"strings"
	"sync"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// The knob of the printer display moves a focus through the keys of the Print tab, and a click
// taps the focused key. The keys of each frame are the menu of the display, under the heading of
// the page. The touch screen shows the focus with a ring for a while after each turn.

// focusItem is a key of the frame: its label, its place, and what a tap does.
type focusItem struct {
	label  string
	r      image.Rectangle
	tap    func()
	reveal func() // scrolls the row of a list into view, or nil
	out    bool   // the way out of the menu: Back, or Status on the home menu
}

// knobRing is how long the touch screen shows the focus after a turn of the knob.
const knobRing = 20 * time.Second

// lcdMu protects App.lcd, which LCD reads from another goroutine.
var lcdMu sync.Mutex

// drawing is the App that draws now (under drawMu), for heading.
var drawing *App

// listFocus adds the rows of a list that scrolls to the menu of the display: n rows of height
// rowH from y top to bottom, at the scroll position *scroll. A row that the knob focuses scrolls
// into view.
func (a *App) listFocus(n, top, bottom, rowH int, scroll *int, label func(i int) string, tap func(i int)) {
	if !a.collect {
		return
	}
	for i := range n {
		y := top + i*rowH - *scroll
		a.focus = append(a.focus, focusItem{label: lcdText(label(i)), r: image.Rect(24, y, W-24, y+rowH-10),
			tap: func() { tap(i) }, reveal: func() {
				if y := top + i*rowH; y-*scroll < top {
					*scroll = y - top
				} else if y+rowH-*scroll > bottom {
					*scroll = y + rowH - bottom
				}
			}})
	}
}

// addFocus adds a key to the menu of the display, while the frame collects keys.
func (a *App) addFocus(label string, r image.Rectangle, tap func()) {
	if a.collect && tap != nil {
		a.focus = append(a.focus, focusItem{label: lcdText(label), r: r, tap: tap})
	}
}

// lcdNote adds a row under the heading of the page on the display, for example the step of a
// filament change.
func (a *App) lcdNote(r printer.LCDRow) {
	if a.collect {
		a.lcdNotes = append(a.lcdNotes, r)
	}
}

// setHeading keeps the heading of the page for the display.
func setHeading(title string) {
	if drawing != nil && drawing.collect {
		drawing.lcdTitle = title
	}
}

// LCDKey takes a key of the knob of the printer display. It is safe to call from any goroutine.
func (a *App) LCDKey(key string) {
	a.post(func() {
		// On the status screen, the first key only opens the menu.
		wake := !a.lcdMenu()
		a.knobAt = a.Now()
		if wake {
			a.Redraw()
			return
		}
		switch key {
		case "up", "fast_up":
			a.lcdSel--
		case "down", "fast_down":
			a.lcdSel++
		}
		if a.lcdSel >= 0 && a.lcdSel < len(a.focus) && a.focus[a.lcdSel].reveal != nil {
			a.focus[a.lcdSel].reveal()
		}
		switch key {
		case "click":
			if a.lcdSel >= 0 && a.lcdSel < len(a.focus) {
				a.focus[a.lcdSel].tap()
			} else if a.tab != TabPrint {
				a.tab = TabPrint
			}
		case "back", "long_click":
			if a.confirm != nil {
				a.confirm = nil
			} else {
				a.back()
			}
		}
		a.Redraw()
	})
}

// endFocus ends the frame: it keeps the focus in the menu, resets it on a new page, draws the ring
// on the touch screen, and makes the screen of the display.
func (a *App) endFocus(c *gfx.Canvas, st printer.State) {
	for i := range a.focus {
		a.focus[i].out = a.focus[i].label == "Back" || a.focus[i].label == "Close"
	}
	page := a.lcdTitle
	switch {
	case a.confirm != nil:
		page = "?" + a.confirm.question
	case a.lcdTitle == "" && a.keypad == nil && len(a.focus) > 0:
		// The home menu: Status at the top goes back to the status screen.
		a.focus = append([]focusItem{{label: "Status", out: true, tap: func() { a.knobAt = time.Time{} }}}, a.focus...)
		page = "home"
		if !a.lcdMenu() {
			page = "status"
		}
	}
	if page != a.lcdPage {
		// A new menu: the cursor at the first key under the way out.
		a.lcdPage, a.lcdSel = page, 0
		if len(a.focus) > 1 && a.focus[0].out {
			a.lcdSel = 1
		}
	}
	a.lcdSel = max(0, min(a.lcdSel, len(a.focus)-1))
	if len(a.focus) > 0 && !a.knobAt.IsZero() && a.Now().Sub(a.knobAt) < knobRing && a.focus[a.lcdSel].reveal == nil &&
		!a.focus[a.lcdSel].r.Empty() {
		r := a.focus[a.lcdSel].r.Inset(-5)
		const t = 3
		c.Fill(image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+t), orange)
		c.Fill(image.Rect(r.Min.X, r.Max.Y-t, r.Max.X, r.Max.Y), orange)
		c.Fill(image.Rect(r.Min.X, r.Min.Y, r.Min.X+t, r.Max.Y), orange)
		c.Fill(image.Rect(r.Max.X-t, r.Min.Y, r.Max.X, r.Max.Y), orange)
	}
	s := a.lcdScreen(st)
	lcdMu.Lock()
	a.lcd = s
	lcdMu.Unlock()
}

// lcdFonts are the fonts of the display, with their rows.
var lcdFonts = map[string]int{"5x8": 8, "6x10": 6, "": 4}

// lcdMenu reports that the display shows a menu: the knob turned a short time ago, or a page or a
// dialog is open. Else it shows the status of the printer.
func (a *App) lcdMenu() bool {
	if !a.knobAt.IsZero() && a.Now().Sub(a.knobAt) < knobRing {
		return true
	}
	return a.lcdTitle != "" || a.confirm != nil || a.keypad != nil
}

// lcdScreen makes the screen of the display: an animation, the status of the printer, or the
// heading and the keys of the page.
func (a *App) lcdScreen(st printer.State) printer.LCDScreen {
	l1, l2, anim := a.lcdStatus(st)
	if anim != "" {
		return printer.LCDScreen{Anim: anim, Title: strings.TrimSpace(l1), Caption: strings.TrimSpace(l2)}
	}
	font := a.LCDFont
	nrows, ok := lcdFonts[font]
	if !ok {
		font, nrows = "5x8", 8
	}
	if !a.lcdMenu() || len(a.focus) == 0 {
		return printer.LCDScreen{Status: a.lcdStatusScreen(st, l1, l2)}
	}
	_ = nrows
	m := &printer.LCDMenu{Selected: a.lcdSel}
	switch {
	case a.confirm != nil:
		m.Title = lcdText(a.confirm.question)
	case a.lcdTitle != "":
		m.Title = lcdText(a.lcdTitle)
		for _, r := range a.lcdNotes {
			m.Notes = append(m.Notes, r.Left)
		}
	default:
		m.Title, m.Carousel = "Printdeck", true
	}
	for _, f := range a.focus {
		m.Items = append(m.Items, printer.LCDMenuItem{Label: f.label, Icon: lcdIcon(f.label)})
	}
	return printer.LCDScreen{Menu: m, Font: font}
}

// lcdIcons are the icons of the keys on the display, by a word of the label.
var lcdIcons = []struct{ word, icon string }{
	{"back", "back"}, {"close", "back"}, {"status", "status"}, {"print a file", "print"}, {"reprint", "print"},
	{"print", "print"}, {"preheat", "preheat"}, {"heat", "preheat"}, {"cool", "cool"}, {"filament", "filament"},
	{"unload", "unload"}, {"load", "load"}, {"purge", "load"}, {"move", "move"}, {"park", "move"},
	{"center", "move"}, {"home", "home"}, {"motors", "move"}, {"maintenance", "maint"}, {"nozzle", "maint"},
	{"machine", "machine"}, {"console", "console"}, {"macro", "macro"}, {"history", "history"},
	{"statistics", "stats"}, {"wi-fi", "wifi"}, {"bed", "bed"}, {"probe", "probe"}, {"pid", "pid"},
	{"firmware", "chip"}, {"save", "save"}, {"stop", "stop"}, {"pause", "pause"}, {"resume", "play"},
	{"cancel", "no"}, {"restart", "machine"}, {"tune", "tune"}, {"run", "play"}, {"use", "yes"},
	{"yes", "yes"}, {"ok", "yes"}, {"done", "yes"}, {"no", "no"},
}

// lcdIcon returns the icon of a key: by the first word that its label has, else a file for a
// name with a dot, else a dot.
func lcdIcon(label string) string {
	l := strings.ToLower(label)
	for _, ic := range lcdIcons {
		if strings.HasPrefix(l, ic.word) || strings.Contains(l, " "+ic.word) {
			return ic.icon
		}
	}
	if strings.Contains(l, ".") {
		return "file"
	}
	return "item"
}

// lcdStatusScreen is the status screen of the display: the state, the heaters and the fan, and
// the progress or the position. The display draws it with icons.
func (a *App) lcdStatusScreen(st printer.State, l1, l2 string) *printer.LCDStatus {
	// One space between the words: the status lines have the padding of 16 characters.
	trim := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	s := &printer.LCDStatus{State: trim(l1), Message: trim(l2), Clock: a.Now().Format("15:04"),
		Tool: st.Tool, ToolT: st.ToolTarget, Bed: st.Bed, BedT: st.BedTarget, Fan: st.Fan / 100}
	if st.Busy() {
		s.Printing, s.Progress = true, st.Completion/100
		e := int(st.Elapsed.Minutes())
		s.Elapsed = fmt.Sprintf("%d:%02d", e/60, e%60)
		if st.HasTimeLeft {
			m := int(st.TimeLeft.Minutes())
			s.Left = fmt.Sprintf("%d:%02d", m/60, m%60)
		}
		return s
	}
	if st.Reachable && st.HasZ && st.Homed != "" {
		s.Position = fmt.Sprintf("X%.0f Y%.0f Z%.2f", st.X, st.Y, st.Z)
	}
	s.Hint = "Click for menu"
	return s
}

// LCD returns the screen of the display of the printer, from the last frame. It is safe to call
// from any goroutine.
func (a *App) LCD() printer.LCDScreen {
	lcdMu.Lock()
	defer lcdMu.Unlock()
	return a.lcd
}
