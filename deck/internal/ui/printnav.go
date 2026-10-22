package ui

import (
	"fmt"
	"image"
	"image/color"
	"printdeck/kit"
	"strings"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// The screens: a screen for the state of the printer (idle, preparing, first layer, printing,
// paused, done, error), and the pages that the idle screen opens: Files, Move, Filament,
// Maintenance and Machine. The pages are a stack: Back goes one page up.

type page int

const (
	pageFiles page = iota + 1
	pageFile       // the detail of one file (a.file)
	pageMove
	pageFilament
	pageMaintenance
	pageMachine
	pageMacros
	pageHistory
	pageConsole
	pageStats
	pageConfigs // the example configs of Klipper, for the setup of a new printer
	pageUpdate  // the software update
	pageRecord  // one finished print of the history (a.record)
	pageSpools  // the rolls of filament
	pageSpool   // one roll (a.spoolID)
)

// open shows a page over the current one.
func (a *App) open(p page) {
	a.pages = append(a.pages, p)
	a.Redraw()
}

// back closes the top page.
func (a *App) back() {
	if len(a.pages) > 0 {
		a.pages = a.pages[:len(a.pages)-1]
	}
	a.Redraw()
}

func (a *App) topPage() page {
	if len(a.pages) == 0 {
		return 0
	}
	return a.pages[len(a.pages)-1]
}

// backButton is the Back key at the top right of a page.
func (a *App) backButton(c *gfx.Canvas) {
	a.button(c, image.Rect(W-130, 20, W-24, 66), "Back", raised, text, a.back)
}

// material is a filament with its temperatures.
type material struct {
	Name      string
	Tool, Bed int
}

var materials = []material{{"PLA", 210, 60}, {"PETG", 240, 80}, {"ABS", 250, 100}, {"TPU", 225, 50}}

// loaded is the loaded material, or the preheat setting when no material is set.
func (a *App) loaded() material {
	for _, m := range materials {
		if m.Name == a.Material {
			return m
		}
	}
	return material{"", a.PreheatTool, a.PreheatBed}
}

// setMaterial sets the loaded material, and saves it.
func (a *App) setMaterial(name string) {
	a.Material = name
	if a.OnMaterial != nil {
		a.OnMaterial(name)
	}
}

// lcd2 makes the two lines of the character display: l at the left and r at the right of each line.
func lcd2(l1, r1, l2, r2 string) []string {
	lr := func(l, r string) string {
		n := 20 - len([]rune(r))
		ls := []rune(strings.ToUpper(l))
		if len(ls) > n-1 {
			ls = ls[:max(0, n-1)]
		}
		return string(ls) + strings.Repeat(" ", max(0, n-len(ls))) + strings.ToUpper(r)
	}
	return []string{lr(l1, r1), lr(l2, r2)}
}

// printLCD is the character display of the other screens.
var printLCD = image.Rect(16, 16, BaseW-16, 108)

// drawHome is the idle screen: the status screen of the printer display, Print, preheat, the last
// print, and the menus. A wide screen has Print and preheat under the display, and the rest at the
// right. While a print runs (Menu on the screen of the print), the keys stay in their places, and
// those that change the printer wait.
func (a *App) drawHome(c *gfx.Canvas, st printer.State) {
	if W > H {
		a.drawHomeWide(c, st)
		return
	}
	f := a.stateFrame()
	a.statusPanelAt(c, st, f.lcd)
	var keys, rest image.Rectangle
	if f.wide {
		keys, rest = f.side, f.main
	} else {
		s := kit.NewStack(kit.Inset(f.main, 40, 2, 40, 0), 12)
		keys, rest = s.Next(96+12+68), s.Rest()
	}

	k := kit.Rows(keys, 12, kit.Px(96), kit.Px(68))
	a.homeKeys(c, st, k[0], k[1])

	// The last print, and the pages in the space under it, down to the bottom.
	if !f.wide {
		rest = kit.Inset(rest, 0, 0, 0, 16)
	}
	r := kit.Rows(rest, 12, kit.Px(90), kit.Fr(1))
	a.drawLastPrint(c, st, r[0])
	a.region("Pages", r[1])
	type homePage struct {
		name string
		p    page
	}
	// The pages stay in their places during a print. Move waits for its end; the others open,
	// and their keys that change the printer wait.
	pages := []homePage{{"Filament", pageFilament}, {"Move", pageMove}, {"Maintenance", pageMaintenance}, {"Machine", pageMachine}}
	for i, cell := range kit.Grid(r[1], 2, 2, 12) {
		p := pages[i].p
		if p == pageMove {
			a.printerKey(c, cell, pages[i].name, raised, text, func() { a.open(p) })
			continue
		}
		a.button(c, cell, pages[i].name, raised, text, func() { a.open(p) })
	}
}

// drawHomeWide is the home screen of a wide screen, as a main page: the display and the key panel
// at the left, with Home lit; Print a file, preheat and the last print at the right.
func (a *App) drawHomeWide(c *gfx.Canvas, st printer.State) {
	f := a.navFrame()
	a.statusPanelAt(c, st, f.lcd)
	a.drawNavKeys(c, f.side)
	headingAt(c, f.head, "Home")
	r := kit.Rows(f.main, 12, kit.Px(96), kit.Px(68), kit.Px(90))
	a.homeKeys(c, st, r[0], r[1])
	a.drawLastPrint(c, st, r[2])
}

// homeKeys draws the main key of the home screen in main (Print a file, or Files during a print),
// and Preheat and Cool down in heat.
func (a *App) homeKeys(c *gfx.Canvas, st printer.State, main, heat image.Rectangle) {
	pr := a.Printer
	m := a.loaded()
	pre := fmt.Sprintf("Preheat %d/%d", m.Tool, m.Bed)
	if m.Name != "" {
		pre = "Preheat " + m.Name
	}
	files := func() {
		go func() { pr.LoadFiles(); a.Redraw() }()
		a.open(pageFiles)
	}
	if st.Busy() {
		// Away from the print: the files to look at, and the heat keys wait for the end of the
		// print. The print bar at the bottom goes back to the print.
		a.button(c, main, "Files", raised, text, files)
	} else {
		a.button(c, main, "Print a file", orange, onAcc, files)
	}
	h := kit.Cols(heat, 12, kit.Fr(1), kit.Fr(1))
	// Preheat is on while the heaters hold its temperatures: a tap then turns them off.
	if st.ToolTarget == float64(m.Tool) && st.BedTarget == float64(m.Bed) && m.Tool > 0 {
		a.printerKey(c, h[0], pre, keyOn, keyOnInk, func() { pr.Heat(0, 0) })
	} else {
		a.printerKey(c, h[0], pre, keyOff, text, func() { pr.Heat(m.Tool, m.Bed) })
	}
	a.printerKey(c, h[1], "Cool down", raised, text, func() { pr.Heat(0, 0) })
}

// drawLastPrint draws the last print in r, with Reprint.
func (a *App) drawLastPrint(c *gfx.Canvas, st printer.State, last image.Rectangle) {
	plate(c, last)
	a.region("Last print", last)
	l, ok := a.lastPrint(st)
	if !ok {
		c.Text(last, "No print yet", gfx.Face(false, 18), dim, gfx.Center)
		return
	}
	label(c, image.Rect(last.Min.X+16, last.Min.Y+8, last.Max.X, last.Min.Y+28), "Last print", gfx.Left)
	c.Text(image.Rect(last.Min.X+16, last.Min.Y+28, last.Max.X-150, last.Min.Y+58), gfx.Fit(gfx.Face(true, 20), l.File, last.Dx()-170), gfx.Face(true, 20), text, gfx.Left)
	c.Text(image.Rect(last.Min.X+16, last.Min.Y+56, last.Max.X-150, last.Max.Y-8), l.Summary(func(mm float64) float64 { return a.grams(mm, "") }), gfx.Face(false, 16), dim, gfx.Left)
	path := l.Path
	a.printerKey(c, image.Rect(last.Max.X-140, last.Min.Y+20, last.Max.X-14, last.Max.Y-16), "Reprint", raised, text, func() {
		a.confirmPrint(path)
	})
}

// confirmPrint asks before it starts a file. The question checks the material of the file
// (materialCheck).
func (a *App) confirmPrint(path string) {
	name := path[strings.LastIndex(path, "/")+1:]
	q, yes, danger, set := a.materialCheck(path)
	if q == "" {
		q, yes = "Print "+gfx.Fit(gfx.Face(true, 28), name, 300)+"?", "Print"
	}
	a.confirm = &confirmBox{question: q, yes: yes, danger: danger, onYes: func() {
		if set != "" {
			a.setMaterial(set)
		}
		a.Printer.PrintFile(path)
		a.pages = nil
	}}
}

// ConfirmFilePrint starts a print that a slicer sent. When the material of the file is not known to
// be in the printer, the touch screen asks first. It is safe to call from any goroutine.
func (a *App) ConfirmFilePrint(path string) {
	a.post(func() {
		q, yes, danger, set := a.materialCheck(path)
		if q == "" {
			a.Printer.PrintFile(path)
			return
		}
		a.confirm = &confirmBox{question: q, yes: yes, danger: danger, onYes: func() {
			if set != "" {
				a.setMaterial(set)
			}
			a.Printer.PrintFile(path)
			a.pages = nil
		}}
	})
}

// materialCheck compares the material of a file with the material in the printer: the roll, or the
// loaded material. It returns "" when the file names no material, or when the material is in the
// printer. Else it returns the question and its yes key. danger is true when another material is
// in the printer. set is the material to set as loaded after yes, when none is set.
func (a *App) materialCheck(path string) (question, yes string, danger bool, set string) {
	m := a.Printer.FileMeta(path)
	want := m.Material
	if want == "" {
		return "", "", false, ""
	}
	wantName := want
	if m.MaterialName != "" {
		wantName = m.MaterialName
	}
	have, haveName := a.Material, a.Material
	if s := a.Printer.SpoolStore(); s != nil {
		if sp, ok := s.In(); ok {
			have, haveName = sp.Material, sp.Label()
			if !strings.EqualFold(sp.Label(), sp.Material) {
				haveName += " (" + sp.Material + ")"
			}
		}
	}
	fit := func(s string) string { return gfx.Fit(gfx.Face(true, 28), s, 400) }
	switch {
	case have == "":
		return "Is " + fit(wantName) + " in the printer?", "Yes, print", false, strings.ToUpper(want)
	case !strings.EqualFold(have, want):
		return "The file is for " + fit(wantName) + ". " + fit(haveName) + " is in the printer.", "Print anyway", true, ""
	}
	return "", "", false, ""
}

// rowText draws the title and the text of a menu row: in two lines, or in one line when the row is
// low (many rows on a wide screen), with the text at the right of the title.
func rowText(c *gfx.Canvas, r image.Rectangle, title, sub string, titleCol, subCol color.RGBA) {
	title = strings.ToUpper(title)
	if r.Dy() >= 58 {
		c.Text(image.Rect(r.Min.X+18, r.Min.Y+8, r.Max.X-50, r.Min.Y+40), title, gfx.Face(true, 20), titleCol, gfx.Left)
		c.Text(image.Rect(r.Min.X+18, r.Min.Y+38, r.Max.X-50, r.Max.Y-8), gfx.Fit(gfx.Face(false, 16), sub, r.Dx()-70), gfx.Face(false, 16), subCol, gfx.Left)
		return
	}
	tf, sf := gfx.Face(true, 17), gfx.Face(false, 14)
	tw := gfx.Width(tf, title)
	c.Text(image.Rect(r.Min.X+16, r.Min.Y, r.Min.X+16+tw, r.Max.Y), title, tf, titleCol, gfx.Left)
	c.Text(image.Rect(r.Min.X+28+tw, r.Min.Y, r.Max.X-46, r.Max.Y), gfx.Fit(sf, sub, r.Dx()-74-tw), sf, subCol, gfx.Right)
}

// menuRow is a row of a menu page: a title, a line under it, and an arrow. tap runs when the row
// is tapped; nil draws a row with no action.
func (a *App) menuRow(c *gfx.Canvas, r image.Rectangle, title, sub string, subCol color.RGBA, tap func()) {
	a.drawMenuRow(c, r, title, sub, subCol, tap != nil, false)
	if tap == nil {
		return
	}
	a.addHit(kit.Hit{R: r, Tap: tap})
	a.addFocus(title, r, tap)
}

// drawMenuRow draws a row of a menu page, with no touch area: the title and the text, and at the
// right an arrow (arrow), or a lock for an action that waits for the end of the print (locked).
func (a *App) drawMenuRow(c *gfx.Canvas, r image.Rectangle, title, sub string, subCol color.RGBA, arrow, locked bool) {
	plate(c, r)
	a.region(title, r)
	if locked {
		rowText(c, r, title, sub, dim, dim)
		drawLock(c, r.Max.X-28, r.Min.Y+r.Dy()/2-2, dim)
		return
	}
	if arrow && a.isPressed(r) {
		c.RoundRect(r.Inset(1), 4, raised)
	}
	rowText(c, r, title, sub, text, subCol)
	if arrow {
		cx, cy := r.Max.X-28, r.Min.Y+r.Dy()/2
		c.Polygon([]image.Point{{cx - 5, cy - 10}, {cx + 5, cy}, {cx - 5, cy + 10}, {cx - 9, cy + 6}, {cx - 3, cy}, {cx - 9, cy - 6}}, dim)
	}
}
