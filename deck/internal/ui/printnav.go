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
// paused, done, error), and the pages that the idle screen opens: Files, Toolhead, Filament
// and Machine. Machine opens Maintenance. The pages are a stack: Back goes one page up.

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
	pageConfigs     // the example configs of Klipper, for the setup of a new printer
	pageUpdate      // the software update
	pageRecord      // one finished print of the history (a.record)
	pageSpools      // the rolls of filament
	pageSpool       // one roll (a.spoolID)
	pageSpoolPrints // the prints that used one roll (a.spoolID)
	pageSettings    // the settings of the deck
	pagePhones      // the phones of the Printdeck app that get push messages
	pageMonitor     // the graphs of the CPU, the memory and the disk writes of the deck
	pageSimulator   // the simulated printers
	pageCamera      // the picture of a camera, its turn and its calibration
	pageCalibrate   // the calibration of the camera (a.calib)
	pageTimeLapse   // the time lapse and the picture of the first layer
	pageFirstLayer  // the last picture of a first layer
	pageCameras     // the list of the cameras
	pageCameraEdit  // the settings of one camera (a.camEdit): the name, on or off, and the uses
	pagePlayer      // the player of a time lapse video (a.player)
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
		keys, rest = s.Next(96+12+68+12+homeRowH), s.Rest()
	}

	k := kit.Rows(keys, 12, kit.Px(96), kit.Px(68), kit.Px(homeRowH))
	a.homeKeys(c, st, k[0], k[1])
	a.homeRow(c, st, k[2])

	// The last print, and the pages in the space under it, down to the bottom.
	if !f.wide {
		rest = kit.Inset(rest, 0, 0, 0, 16)
	}
	r := kit.Rows(rest, 12, kit.Px(lastPrintH()), kit.Fr(1))
	a.drawLastPrint(c, st, r[0])
	a.region("Pages", r[1])
	type homePage struct {
		name string
		p    page
	}
	// The pages stay in their places during a print. Toolhead waits for its end; the others open,
	// and their keys that change the printer wait. A deck with a camera has Camera in the last place.
	pages := []homePage{{"Filament", pageFilament}, {"Toolhead", pageMove}, {"Machine", pageMachine}}
	if a.Camera != nil {
		pages = append(pages, homePage{"Camera", pageCamera})
	}
	for i, cell := range kit.Grid(r[1], 2, 2, 12) {
		if i >= len(pages) {
			break
		}
		p := pages[i].p
		if p == pageMove {
			a.printerKey(c, cell, pages[i].name, raised, text, func() { a.open(p) })
			continue
		}
		a.button(c, cell, pages[i].name, raised, text, func() { a.open(p) })
	}
}

// drawHomeWide is the home screen of a wide screen, as a main page: the display and the key panel
// at the left, with Main lit; Print a file, preheat and the last print at the right.
func (a *App) drawHomeWide(c *gfx.Canvas, st printer.State) {
	f := a.navFrame()
	a.statusPanelAt(c, st, f.lcd)
	a.drawNavKeys(c, f.side)
	headingAt(c, f.head, "Main")
	// Print a file, and the last print as on its page of the history. Preheat and Cool down are in
	// Filament.
	// Print a file is lower than on a tall screen, so the table of the last print keeps its rows.
	r := kit.Rows(f.main, 12, kit.Px(72), kit.Px(homeRowH), kit.Fr(1))
	a.mainKey(c, st, r[0])
	a.homeRow(c, st, r[1])
	a.lastPrintPanel(c, st, r[2])
}

// homeRowH is the height of the row of the home keys on the home screen.
const homeRowH = 60

// homeRow draws Home all, X, Y and Z, as on the Toolhead page. The LED of a key is lit when its
// axes are homed. During a print the keys wait for its end.
func (a *App) homeRow(c *gfx.Canvas, st printer.State, r image.Rectangle) {
	pr := a.Printer
	homed := func(axes string) color.RGBA {
		for _, ax := range axes {
			if !strings.ContainsRune(st.Homed, ax) {
				return keyOff
			}
		}
		return keyOn
	}
	h := kit.Cols(r, 8, kit.Fr(1.8), kit.Fr(1), kit.Fr(1), kit.Fr(1)) // "Home all" needs the room
	a.printerKey(c, h[0], "Home all", homed("xyz"), text, func() { a.do(func() error { return pr.Home() }) })
	for i, ax := range []string{"x", "y", "z"} {
		a.printerKey(c, h[i+1], strings.ToUpper(ax), homed(ax), text, func() { a.do(func() error { return pr.Home(ax) }) })
	}
}

// lastPrintPanel draws the last print in a plate: its file and Reprint, and under them its model
// and the table of its page in the history. A tap on the model opens that page.
func (a *App) lastPrintPanel(c *gfx.Canvas, st printer.State, area image.Rectangle) {
	plate(c, area)
	a.region("Last print", area)
	l, ok := a.lastPrint(st)
	if !ok {
		c.Text(area, "No print yet", gfx.Face(false, 18), dim, gfx.Center)
		return
	}
	in := kit.Inset(area, 16, 10, 14, 14)
	r := kit.Rows(in, 10, kit.Px(52), kit.Fr(1))
	head, body := r[0], r[1]
	hk := kit.Cols(head, 12, kit.Fr(1), kit.Px(140))
	label(c, image.Rect(hk[0].Min.X, hk[0].Min.Y, hk[0].Max.X, hk[0].Min.Y+20), "Last print", gfx.Left)
	c.Text(image.Rect(hk[0].Min.X, hk[0].Min.Y+20, hk[0].Max.X, hk[0].Max.Y), l.File, gfx.Face(true, 20), text, gfx.Left)
	rows := a.recordRows(l)
	side := min(body.Dy(), body.Dx()*2/5)
	cols := kit.Cols(body, 16, kit.Px(side), kit.Fr(1))
	pic := kit.Rows(cols[0], 0, kit.Px(side))[0]
	gone := a.recordModel(c, pic, l)
	if l.Path != "" {
		rec := l
		a.addHit(kit.Hit{R: pic, Tap: func() {
			a.record = rec
			a.pages = []page{pageMachine, pageStats, pageHistory, pageRecord}
			a.Redraw()
		}})
	}
	rowH := max(20, min(28, (body.Dy()-8)/max(1, len(rows))))
	detailRows(c, image.Rect(cols[1].Min.X, cols[1].Min.Y+4, cols[1].Max.X, cols[1].Max.Y), rows, rowH)
	a.reprintKey(c, hk[1], l, gone, raised, text)
}

// mainKey draws the main key of the home screen: Print a file, or Files during a print. The print
// bar goes back to the print.
func (a *App) mainKey(c *gfx.Canvas, st printer.State, r image.Rectangle) {
	pr := a.Printer
	files := func() {
		go func() { pr.LoadFiles(); a.Redraw() }()
		a.open(pageFiles)
	}
	if st.Busy() {
		a.button(c, r, "Files", raised, text, files)
	} else {
		a.button(c, r, "Print a file", orange, onAcc, files)
	}
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
	a.mainKey(c, st, main)
	h := kit.Cols(heat, 12, kit.Fr(1), kit.Fr(1))
	// Preheat is on while the heaters hold its temperatures: a tap then turns them off.
	if st.ToolTarget == float64(m.Tool) && st.BedTarget == float64(m.Bed) && m.Tool > 0 {
		a.printerKey(c, h[0], pre, keyOn, keyOnInk, func() { pr.Heat(0, 0) })
	} else {
		a.printerKey(c, h[0], pre, keyOff, text, func() { pr.Heat(m.Tool, m.Bed) })
	}
	a.printerKey(c, h[1], "Cool down", raised, text, func() { pr.Heat(0, 0) })
}

// lastPrintH is the height of the last print on the tall home screen (drawLastPrint).
func lastPrintH() int {
	if gfx.FinePrint() {
		return 166
	}
	return 90
}

// drawLastPrint draws the last print in r: the thumbnail, the file and its summary, and Reprint at
// the right. With the large text, Reprint is under them, so that the text has the width of r.
func (a *App) drawLastPrint(c *gfx.Canvas, st printer.State, last image.Rectangle) {
	plate(c, last)
	a.region("Last print", last)
	l, ok := a.lastPrint(st)
	if !ok {
		c.Text(last, "No print yet", gfx.Face(false, 18), dim, gfx.Center)
		return
	}
	textR := last.Max.X - 150
	reprint := image.Rect(last.Max.X-140, last.Min.Y+20, last.Max.X-14, last.Max.Y-16)
	if gfx.FinePrint() {
		textR = last.Max.X - 14
		reprint = image.Rect(last.Min.X+14, last.Min.Y+92, last.Max.X-14, last.Max.Y-14)
	}
	// The thumbnail at the left, and the label, the file and the summary at its right.
	y := last.Min.Y + 11
	a.recordThumb(c, image.Rect(last.Min.X+11, y, last.Min.X+11+thumbSmall, y+thumbSmall), l, a.fileIndex())
	x := last.Min.X + 92
	label(c, image.Rect(x, last.Min.Y+8, textR, last.Min.Y+28), "Last print", gfx.Left)
	c.Text(image.Rect(x, last.Min.Y+28, textR, last.Min.Y+58), l.File, gfx.Face(true, 20), text, gfx.Left)
	c.Text(image.Rect(x, last.Min.Y+56, textR, last.Min.Y+82), l.Summary(func(mm float64) float64 { return a.grams(mm, "") }), gfx.Face(false, 16), dim, gfx.Left)
	path := l.Path
	a.printerKey(c, reprint, "Reprint", raised, text, func() {
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
		// A print from a slicer: the question is on each screen of the deck.
		a.hub.ask(&confirmBox{question: q, yes: yes, danger: danger, onYes: func() {
			if set != "" {
				a.setMaterial(set)
			}
			a.Printer.PrintFile(path)
		}})
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
	if gfx.FinePrint() && r.Dy() >= largeRowH(1) {
		// The large text: the text under the title goes in two lines when it is too long for one
		// line and the row is high enough. The lines are in the middle of the row.
		subW := r.Dx() - largeRowInset
		lines := subLines(sub, subW)
		if r.Dy() < largeRowH(len(lines)) {
			lines = []string{sub}
		}
		y := r.Min.Y + (r.Dy()-largeRowH(len(lines)))/2 + largeRowPad
		c.Text(image.Rect(r.Min.X+18, y, r.Min.X+18+subW, y+largeTitleH), title, gfx.Face(true, 20), titleCol, gfx.Left)
		for i, l := range lines {
			ly := y + largeTitleH + i*largeSubH
			c.Text(image.Rect(r.Min.X+18, ly, r.Min.X+18+subW, ly+largeSubH), l, gfx.Face(false, 16), subCol, gfx.Left)
		}
		return
	}
	if r.Dy() >= 58 {
		c.Text(image.Rect(r.Min.X+18, r.Min.Y+8, r.Max.X-50, r.Min.Y+40), title, gfx.Face(true, 20), titleCol, gfx.Left)
		c.Text(image.Rect(r.Min.X+18, r.Min.Y+38, r.Max.X-50, r.Max.Y-8), sub, gfx.Face(false, 16), subCol, gfx.Left)
		return
	}
	tf, sf := gfx.Face(true, 17), gfx.Face(false, 14)
	tw := gfx.Width(tf, title)
	c.Text(image.Rect(r.Min.X+16, r.Min.Y, r.Min.X+16+tw, r.Max.Y), title, tf, titleCol, gfx.Left)
	c.Text(image.Rect(r.Min.X+28+tw, r.Min.Y, r.Max.X-46, r.Max.Y), sub, sf, subCol, gfx.Right)
}

// The parts of a menu row with the large text: the space over and under the text, the title, and
// each line of the text under it. largeRowInset is the width of the row that has no text: the
// margin at the left, and the arrow with space around it at the right.
const largeRowPad, largeTitleH, largeSubH, largeRowInset = 8, 30, 27, 18 + 58

// largeRowH is the height of a menu row with the large text and n lines under the title.
func largeRowH(n int) int { return 2*largeRowPad + largeTitleH + n*largeSubH }

// subLines is the text under the title of a menu row with the large text: one line, or two lines
// when it is too wide for one line in width. The two lines keep the full size.
func subLines(sub string, width int) []string {
	f := gfx.Face(false, 16)
	if gfx.Width(f, sub) <= width {
		return []string{sub}
	}
	lines := wrapText(f, sub, width)
	if len(lines) > 2 {
		lines = append(lines[:1], strings.Join(lines[1:], " "))
	}
	return lines
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
		rowArrow(c, r)
	}
}

// rowArrow draws the arrow at the right of a menu row r.
func rowArrow(c *gfx.Canvas, r image.Rectangle) {
	cx, cy := r.Max.X-28, r.Min.Y+r.Dy()/2
	c.Polygon([]image.Point{{cx - 5, cy - 10}, {cx + 5, cy}, {cx - 5, cy + 10}, {cx - 9, cy + 6}, {cx - 3, cy}, {cx - 9, cy - 6}}, dim)
}
