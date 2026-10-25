package ui

import (
	"cmp"
	"fmt"
	"image"
	"printdeck/kit"
	"slices"
	"strings"

	"image/color"

	"printdeck/internal/board"
	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// The menu page Machine of the idle screen, and the pages that Machine opens: Maintenance and the others.

const menuTop, menuRowH = 78, 98

// menuItem is a row of a menu page.
type menuItem struct {
	title, sub string
	col        color.RGBA // the color of sub
	tap        func()     // nil: a row with no action
}

// menuRows draws the rows of a menu page, from the top of area. The rows share the height of the
// area. On a wide screen a row is at most rowH high, on a tall screen at most 1.6 times rowH, so
// that the rows fill the screen.
func (a *App) menuRows(c *gfx.Canvas, area image.Rectangle, wide bool, rowH, gap int, items []menuItem) {
	a.menuRowsWait(c, area, wide, rowH, gap, items, false)
}

// minRowH is the least height of a menu row: a row that a finger hits with no error.
const minRowH = 60 // also the height of the two lines of a row (rowText)

// pageScroll is the scroll position of the list of the page that is open.
func (a *App) pageScroll() *int {
	if a.menuScroll == nil {
		a.menuScroll = map[page]*int{}
	}
	p := a.topPage()
	if a.menuScroll[p] == nil {
		a.menuScroll[p] = new(int)
	}
	return a.menuScroll[p]
}

// menuRowsWait is menuRows. When wait is true, the rows with an action wait for the end of the
// print (lockedRow).
func (a *App) menuRowsWait(c *gfx.Canvas, area image.Rectangle, wide bool, rowH, gap int, items []menuItem, wait bool) {
	limit := rowH
	if !wide {
		limit = rowH * 16 / 10
	}
	rowH = min(limit, (area.Dy()+gap)/max(1, len(items))-gap)
	least := minRowH
	if gfx.FinePrint() {
		// The large text: a row is high enough for the title and the lines under it.
		for _, it := range items {
			least = max(least, largeRowH(len(subLines(it.sub, area.Dx()-largeRowInset))))
		}
	}
	if rowH < least {
		// Too many rows for the area: rows of a size to touch, in a list that scrolls.
		l := kit.List{Area: area, N: len(items), RowH: least + gap, Gap: gap, Scroll: a.pageScroll()}
		a.list(c, l, func(i int) string { return items[i].title }, func(c *gfx.Canvas, i int, r image.Rectangle) {
			it := items[i]
			a.drawMenuRow(c, r, it.title, it.sub, it.col, it.tap != nil, wait && it.tap != nil)
		}, func(i int) {
			switch it := items[i]; {
			case it.tap == nil:
			case wait:
				a.Toast(waitText)
			default:
				it.tap()
			}
		})
		return
	}
	s := kit.NewStack(area, gap)
	for _, it := range items {
		if wait && it.tap != nil {
			a.lockedRow(c, s.Next(rowH), it.title, it.sub)
			continue
		}
		a.menuRow(c, s.Next(rowH), it.title, it.sub, it.col, it.tap)
	}
}

// menuGroup is a group of rows of a menu page, under a heading.
type menuGroup struct {
	heading string
	items   []menuItem
}

// groupHeadH is the height of the heading of a group of rows. groupSpace is the space over the
// heading of each group after the first.
const groupHeadH, groupSpace = 24, 10

// menuGroups draws the groups of rows of a menu page from the top of area, each group under its
// heading. The rows share the height of the area, at most rowH high on a wide screen and 1.6 times
// rowH on a tall screen, as in menuRows. When the rows do not fit, the headings and the rows are
// in a list that scrolls, with rows of a size to touch.
func (a *App) menuGroups(c *gfx.Canvas, area image.Rectangle, wide bool, rowH, gap int, groups []menuGroup) {
	// The parts from the top of the list: the headings, and the rows.
	type part struct {
		y, h    int
		top     int // the top of a row, or of the heading over the first row of a group
		heading string
		it      menuItem
		row     bool
	}
	var shown []menuGroup
	n := 0
	for _, g := range groups {
		if len(g.items) > 0 {
			shown = append(shown, g)
			n += len(g.items)
		}
	}
	if n == 0 {
		return
	}
	fixed := len(shown)*(groupHeadH+gap) + (len(shown)-1)*groupSpace
	limit := rowH
	if !wide {
		limit = rowH * 16 / 10
	}
	rowH = min(limit, (area.Dy()-fixed+gap)/n-gap)
	var parts []part
	y := 0
	for gi, g := range shown {
		if gi > 0 {
			y += groupSpace
		}
		top := y
		parts = append(parts, part{y: y, h: groupHeadH, heading: g.heading})
		y += groupHeadH + gap
		for _, it := range g.items {
			h := max(rowH, minRowH)
			if gfx.FinePrint() {
				// The large text: a row is high enough for the title and the lines under it.
				h = max(h, largeRowH(len(subLines(it.sub, area.Dx()-largeRowInset))))
			}
			parts = append(parts, part{y: y, h: h, top: top, it: it, row: true})
			y += h + gap
			top = y
		}
	}
	total := y - gap
	scroll := a.pageScroll()
	maxScroll := max(0, total-area.Dy())
	*scroll = max(0, min(*scroll, maxScroll))
	rect := func(p part) image.Rectangle {
		y := area.Min.Y + p.y - *scroll
		return image.Rect(area.Min.X, y, area.Max.X, y+p.h)
	}
	if maxScroll == 0 {
		// All the rows fit: rows that stay in their place.
		for _, p := range parts {
			if p.row {
				a.menuRow(c, rect(p), p.it.title, p.it.sub, p.it.col, p.it.tap)
			} else {
				label(c, rect(p), p.heading, gfx.Left)
			}
		}
		return
	}
	c.Clipped(area, func(c *gfx.Canvas) {
		for _, p := range parts {
			switch r := rect(p); {
			case !r.Overlaps(area):
			case p.row:
				a.drawMenuRow(c, r, p.it.title, p.it.sub, p.it.col, p.it.tap != nil, false)
			default:
				label(c, r, p.heading, gfx.Left)
			}
		}
	})
	a.addHit(kit.Hit{R: area,
		Scroll: func(dy int) { *scroll = max(0, min(*scroll+dy, maxScroll)) },
		TapAt: func(pt image.Point) {
			for _, p := range parts {
				if p.row && p.it.tap != nil && pt.In(rect(p)) {
					p.it.tap()
					return
				}
			}
		}})
	if !a.collect {
		return
	}
	for _, p := range parts {
		if !p.row || p.it.tap == nil {
			continue
		}
		// The knob scrolls the row into view, with the heading over the first row of a group.
		reveal := func() {
			if p.top < *scroll {
				*scroll = p.top
			} else if p.y+p.h > *scroll+area.Dy() {
				*scroll = p.y + p.h - area.Dy()
			}
		}
		a.focus = append(a.focus, focusItem{label: lcdText(p.it.title), r: rect(p), tap: p.it.tap, reveal: reveal})
	}
}

// drawMaintenance lists the calibrations that the printer has (the modules of Klipper).
func (a *App) drawMaintenance(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	f := a.navPage(c, "Maintenance")
	mach := pr.Machine()
	idle := st.Connected && !st.Busy()

	type row struct {
		title, sub string
		warn       bool
		tap        func()
	}
	var rows []row
	run := func(name, question, yes string, lines ...string) func() {
		return func() {
			if !idle {
				a.Toast("Wait until the print ends")
				return
			}
			a.confirm = &confirmBox{question: question, yes: yes, onYes: func() { a.runAction(name, lines...) }}
		}
	}
	home := func() []string {
		if st.Homed == "xyz" {
			return nil
		}
		return []string{"G28"}
	}
	if mach.Has("bed_mesh") {
		rows = append(rows, row{"Bed mesh", "Probe the bed and map its height", false,
			run("Bed mesh", "Probe the bed now?", "Probe", append(home(), "BED_MESH_CALIBRATE")...)})
	}
	if mach.Has("bed_tilt") {
		rows = append(rows, row{"Bed tilt", "Probe the bed and correct its tilt", false,
			run("Bed tilt", "Probe the bed now?", "Probe", append(home(), "BED_TILT_CALIBRATE")...)})
	}
	if mach.Has("screws_tilt_adjust") {
		rows = append(rows, row{"Screw adjust", "How far to turn each bed screw", false,
			run("Screw adjust", "Probe the bed screws now?", "Probe", append(home(), "SCREWS_TILT_CALCULATE")...)})
	}
	if mach.Has("probe") {
		rows = append(rows, row{"Probe Z offset", "Find the nozzle height with a sheet of paper", false,
			run("Probe Z offset", "Start the paper test?", "Start", append(home(), "PROBE_CALIBRATE")...)})
	}
	if mach.Has("extruder") {
		lines := []string{"PID_CALIBRATE HEATER=extruder TARGET=210"}
		if mach.Has("heater_bed") {
			lines = append(lines, "PID_CALIBRATE HEATER=heater_bed TARGET=60")
		}
		rows = append(rows, row{"PID tune", "Tune the heaters: about 10 minutes", false,
			run("PID tune", "Heat and tune the heaters now?", "Tune", lines...)})
	}
	rows = append(rows, row{"Nozzle change", "Heat to 250°, and raise the head", false, func() {
		if !idle {
			a.Toast("Wait until the print ends")
			return
		}
		lines := append(home(), "G90", fmt.Sprintf("G1 Z%.0f F600", min(st.Axis[1][2], max(st.Z, 0)+50)), "M104 S250")
		if st.Axis[1][2] == 0 {
			lines = append(home(), "G91", "G1 Z50 F600", "G90", "M104 S250")
		}
		a.confirm = &confirmBox{question: "Heat the nozzle to 250°?", yes: "Heat", onYes: func() {
			a.do(func() error { return pr.Gcode(lines...) })
		}}
	}})
	if board.CanFlash("") && st.Firmware != "" {
		sub, warn := "Klipper firmware is current", false
		if st.FirmwareOld {
			sub, warn = "Old: write the firmware of this Klipper", true
		}
		rows = append(rows, row{"Firmware", sub, warn, func() {
			if !idle {
				a.Toast("Wait until the print ends")
				return
			}
			a.askFlash("")
		}})
	}
	if st.SaveConfigPending {
		rows = append(rows, row{"Save config", "Calibration results are not saved yet", true, func() {
			if !idle {
				a.Toast("Wait until the print ends")
				return
			}
			a.confirm = &confirmBox{question: "Save the config? Klipper restarts.", yes: "Save", onYes: func() {
				a.do(func() error { return pr.Gcode("SAVE_CONFIG") })
			}}
		}})
	} else {
		rows = append(rows, row{"Save config", "No unsaved changes", false, nil})
	}
	if mach.Has("extruder") {
		// The pressure advance test: print the tower, then type the height of its best corners.
		m := a.loaded()
		rows = append(rows, row{"Pressure advance test", "Print a 30 mm tower: about 15 minutes", false, func() {
			if !idle {
				a.Toast("Wait until the print ends")
				return
			}
			q := fmt.Sprintf("Print the pressure advance tower at %d°?", m.Tool)
			a.confirm = &confirmBox{question: q, yes: "Print", onYes: func() {
				a.do(func() error { return pr.PrintPATower(m.Tool, m.Bed, m.Name) })
			}}
		}})
		rows = append(rows, row{"Pressure advance", fmt.Sprintf("Now %.3f · set it from the tower", mach.PressureAdvance), false, func() {
			if !idle {
				a.Toast("Wait until the print ends")
				return
			}
			a.editNumber("Height of the best corners", 0, int(printer.PATowerHeight), " mm", func(h int) {
				v := printer.PATowerValue(float64(h))
				a.confirm = &confirmBox{question: fmt.Sprintf("Set the pressure advance to %.3f?", v), yes: "Set", onYes: func() {
					a.do(func() error {
						if err := pr.SetPressureAdvance(v); err != nil {
							return err
						}
						a.Toast(fmt.Sprintf("Pressure advance %.3f, in printer.cfg", v))
						return nil
					})
				}}
			})
		}})
	}
	var items []menuItem
	for _, r := range rows {
		col := dim
		if r.warn {
			col = orange
		}
		items = append(items, menuItem{r.title, r.sub, col, r.tap})
	}
	a.menuRowsWait(c, a.listArea(f), f.wide, 86, 12, items, st.Busy())
}

// drawManualProbe is the paper test of a manual Z probe: move the nozzle until a sheet of paper
// drags a little, then accept. It shows by itself while Klipper runs a manual probe.
func (a *App) drawManualProbe(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	// Tall: the display, the text, the steps, and Accept and Abort under each other. Wide: the
	// display and the text in the left column, the keys at the right.
	scr := kit.Screen{W: W, H: contentY1}
	var lcdR, box, steps, hint, end image.Rectangle
	if scr.Wide() {
		left, right := wideZones(wideScale)
		l := kit.Rows(left, 16, kit.Px(92), kit.Fr(1))
		lcdR, box = l[0], l[1]
		r := kit.Rows(right, 10, kit.Fr(1), kit.Px(24), kit.Px(90))
		steps, hint, end = r[0], r[1], r[2]
	} else {
		lcdR = printLCD
		r := kit.Rows(kit.Inset(scr.Bounds(), margin, 124, margin, 16), 16, kit.Px(160), kit.Fr(1), kit.Px(24), kit.Px(90))
		box, steps, hint, end = r[0], r[1], r[2], r[3]
	}
	charLCD(c, lcdR, lcd2("Paper test", "", "Nozzle at Z", fmt.Sprintf("%.3f", st.ManualZ)), 20)
	a.region("Height", lcdR)
	a.region("Steps", steps)
	plate(c, box)
	face := gfx.Face(false, 18)
	ls := kit.NewStack(kit.Inset(box, 18, 14, 18, 0), 0)
	lineH := 34
	if gfx.FinePrint() {
		lineH = 28 // the larger text has more lines
	}
	for _, l := range wrapText(face, "Put a sheet of paper under the nozzle. Lower the nozzle until the paper drags a little when you move it, then tap Accept.", box.Dx()-36) {
		c.Text(ls.Next(lineH), l, face, text, gfx.Left)
	}
	for i, r := range kit.Grid(steps, 3, 2, 10) {
		v := []float64{-1, -0.1, -0.025, 0.025, 0.1, 1}[i]
		a.button(c, r, fmt.Sprintf("%+g", v), raised, text, func() { a.do(func() error { return pr.Gcode(fmt.Sprintf("TESTZ Z=%g", v)) }) })
	}
	label(c, hint, "− lowers the nozzle, + raises it (mm)", gfx.Center)
	k := kit.Cols(end, 12, kit.Fr(1), kit.Fr(1))
	a.button(c, k[0], "Accept", orange, onAcc, func() {
		a.do(func() error {
			if err := pr.Gcode("ACCEPT"); err != nil {
				return err
			}
			if st.PauseCheck {
				a.Toast("Accepted. The print goes on from this height.")
			} else {
				a.Toast("Accepted. Save config in Maintenance writes it.")
			}
			return nil
		})
	})
	a.button(c, k[1], "Abort", raised, red, func() { a.do(func() error { return pr.Gcode("ABORT") }) })
}

// drawMachine is the Machine page: the versions and the totals, and the advanced pages.
func (a *App) drawMachine(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	f := a.navPage(c, "Machine")
	mach := pr.Machine()
	// The information, the rows, and the restart keys in the row of actions at the bottom.
	var info, rowsArea, restart image.Rectangle
	if f.wide {
		content, acts := f.split()
		r := kit.Rows(content, 8, kit.Px(96), kit.Fr(1))
		info, rowsArea, restart = r[0], r[1], acts
	} else {
		r := kit.Rows(kit.Inset(f.body(), 0, 10, 0, 14), 6, kit.Px(128), kit.Fr(1), kit.Px(66))
		info, rowsArea, restart = r[0], r[1], r[2]
	}
	window(c, info)
	a.region("Information", info)
	lineH := min(28, (info.Dy()-12)/4)
	fw := "—"
	switch {
	case st.Firmware != "" && st.FirmwareOld:
		fw = "OLD: FLASH IT"
	case st.Firmware != "":
		fw = "CURRENT"
	}
	ip := "—"
	if a.Setup != nil {
		if s, err := a.Setup.Status(); err == nil && s.IP != "" {
			ip = s.IP
		}
	}
	var prints, hours int
	var filament float64
	for _, r := range pr.History() {
		prints++
		hours += int(r.Duration.Hours())
		filament += r.Filament
	}
	lines := [][2]string{
		{"KLIPPER", strings.TrimSuffix(mach.KlipperVersion, "-printdeck-go")},
		{"FIRMWARE", fw},
		{"NETWORK", ip},
		{"PRINTS", fmt.Sprintf("%d · %d H · %s", prints, hours, strings.ToUpper(a.weight(filament)))},
	}
	for i, l := range lines {
		y := info.Min.Y + 10 + i*lineH
		c.Text(image.Rect(info.Min.X+16, y, info.Max.X, y+lineH), l[0], gfx.Face(true, 14), lcdDim, gfx.Left)
		c.Text(image.Rect(info.Min.X+130, y, info.Max.X-16, y+lineH), l[1], gfx.Face(true, 17), lcdInk, gfx.Right)
	}

	macros := "No macros"
	if n := len(mach.Macros); n > 0 {
		macros = strings.Join(mach.Macros[:min(2, n)], ", ")
		if n > 2 {
			macros += fmt.Sprintf(", %d more", n-2)
		}
	}
	// The groups, with the rows that are used most at the top.
	printerRows := []menuItem{
		{"Maintenance", maintenanceLine(mach, st), dim, func() { a.open(pageMaintenance) }},
		{"Macros", macros, dim, func() { a.open(pageMacros) }},
		{"Console", "Type G-code, and read the replies", dim, func() { a.open(pageConsole) }},
	}
	printRows := []menuItem{
		{"Prints", fmt.Sprintf("%d prints · %d h · %s", prints, hours, a.weight(filament)), dim, func() { a.open(pageStats) }},
	}
	deckRows := []menuItem{{"Settings", "Theme, clock, notifications", dim, func() { a.open(pageSettings) }}}
	if a.Updater != nil {
		line, warn := updateLine(a.Updater.Status())
		col := dim
		if warn {
			col = orange
		}
		deckRows = append(deckRows, menuItem{"Software update", line, col, func() { a.open(pageUpdate) }})
	}
	if a.Setup != nil {
		deckRows = append(deckRows, menuItem{"Wi-Fi and help", "The network, the password, the addresses", dim, a.OpenHelp})
	}
	if a.Metrics != nil {
		deckRows = append(deckRows, menuItem{"System monitor", monitorLine, dim, func() { a.open(pageMonitor) }})
	}
	a.menuGroups(c, rowsArea, f.wide, 60, 5, []menuGroup{{"Printer", printerRows}, {"History", printRows}, {"Deck", deckRows}})
	k := kit.Cols(restart, 12, kit.Fr(1), kit.Fr(1))
	// The destructive action at the left.
	a.printerKey(c, k[1], "Restart Klipper", raised, text, func() {
		a.confirm = &confirmBox{question: "Restart Klipper?", yes: "Restart", onYes: func() {
			a.Toast("Restarting Klipper…")
			a.do(pr.RestartKlipper)
		}}
	})
	a.printerKey(c, k[0], "Firmware restart", raised, red, func() {
		a.confirm = &confirmBox{question: "Restart the printer firmware?", yes: "Restart", danger: true, onYes: func() {
			a.Toast("Restarting the firmware…")
			a.do(pr.FirmwareRestart)
		}}
	})
}

// maintenanceLine is the line of the Maintenance row: the calibrations that the printer has, and
// the firmware.
func maintenanceLine(mach printer.Machine, st printer.State) string {
	var names []string
	for _, m := range []struct{ module, name string }{
		{"bed_mesh", "bed mesh"}, {"bed_tilt", "bed tilt"}, {"screws_tilt_adjust", "screws"},
		{"probe", "Z offset"}, {"extruder", "PID"},
	} {
		if mach.Has(m.module) {
			names = append(names, m.name)
		}
	}
	if board.CanFlash("") && st.Firmware != "" {
		names = append(names, "firmware")
	}
	if len(names) == 0 {
		return "Nozzle change, save config"
	}
	if len(names) > 4 {
		names = append(names[:3], fmt.Sprintf("%d more", len(names)-3))
	}
	s := strings.Join(names, ", ")
	return strings.ToUpper(s[:1]) + s[1:]
}

// drawMacros lists the macros of the printer. A tap runs one, after a question.
func (a *App) drawMacros(c *gfx.Canvas, st printer.State) {
	f := a.navPage(c, "Macros")
	macros := a.Printer.Machine().Macros
	area := a.listArea(f)
	if len(macros) == 0 {
		noItems(c, area, "", "The printer has no macros")
		return
	}
	run := func(i int) {
		if st.Busy() {
			a.Toast("Wait until the print ends")
			return
		}
		m := macros[i]
		a.confirm = &confirmBox{question: "Run " + gfx.Fit(gfx.Face(true, 28), m, 300) + "?", yes: "Run", onYes: func() {
			a.runAction(m, m)
		}}
	}
	l := kit.List{Area: area, N: len(macros), RowH: 70, Gap: 10, Scroll: &a.macroScroll}
	a.list(c, l, func(i int) string { return macros[i] }, func(c *gfx.Canvas, i int, r image.Rectangle) {
		plate(c, r)
		ink := text
		if st.Busy() {
			ink = dim
			drawLock(c, r.Max.X-28, r.Min.Y+r.Dy()/2-2, dim)
		}
		c.Text(kit.Inset(r, 18, 0, 48, 0), macros[i], gfx.Face(true, 19), ink, gfx.Left)
	}, run)
}

// listArea is the area of a list on a page: under the head on a tall screen, the main pane on a
// wide screen.
func (a *App) listArea(f frame) image.Rectangle {
	if f.wide {
		return f.body()
	}
	return kit.Inset(f.body(), 0, menuTop-headH, 0, 0)
}

// drawHistory lists the finished prints, newest first. A tap opens the page of the print.
func (a *App) drawHistory(c *gfx.Canvas) {
	pr := a.Printer
	f := a.navPage(c, "Print history")
	recs := pr.History()
	area := a.listArea(f)
	if len(recs) == 0 {
		noItems(c, area, "", "No finished prints yet")
		return
	}
	open := func(i int) {
		a.record = recs[i]
		a.open(pageRecord)
	}
	files := a.fileIndex()
	l := kit.List{Area: area, N: len(recs), RowH: recordRowH(), Gap: 10, Scroll: &a.historyScroll}
	a.list(c, l, func(i int) string { return recs[i].File }, func(c *gfx.Canvas, i int, box image.Rectangle) {
		a.recordRow(c, box, recs[i], recs[i].Summary(func(mm float64) float64 { return a.grams(mm, "") }), fileDate(recs[i].End, a.Now(), a.Clock24), files)
	}, open)
}

// recordRowH is the height of a row of recordRow, with the gap under it.
func recordRowH() int {
	if gfx.FinePrint() {
		return 114
	}
	return fileRowH
}

// recordRow is the row of a finished print in a list: the thumbnail of the file at the left, and
// at its right the file, the summary and the date line in three lines, as a row of the Files page.
// The summary is red for a print that did not complete.
func (a *App) recordRow(c *gfx.Canvas, box image.Rectangle, r printer.Record, summary, date string, files map[string]printer.File) {
	plate(c, box)
	y := box.Min.Y + (box.Dy()-thumbSmall)/2
	a.recordThumb(c, image.Rect(box.Min.X+10, y, box.Min.X+10+thumbSmall, y+thumbSmall), r, files)
	col := dim
	if r.Result != "complete" {
		col = red
	}
	y1, y2, y3 := 8, 36, 58
	if gfx.FinePrint() {
		y1, y2, y3 = 8, 38, 66
	}
	x := box.Min.X + 92
	c.Text(image.Rect(x, box.Min.Y+y1, box.Max.X-12, box.Min.Y+y1+30), r.File, gfx.Face(true, 19), text, gfx.Left)
	c.Text(image.Rect(x, box.Min.Y+y2, box.Max.X-12, box.Min.Y+y2+24), summary, gfx.Face(false, 16), col, gfx.Left)
	c.Text(image.Rect(x, box.Min.Y+y3, box.Max.X-12, min(box.Max.Y-6, box.Min.Y+y3+28)), date, gfx.Face(false, 14), dim, gfx.Left)
}

// drawSettings is the Settings page: a row for each setting of the deck. A tap changes it.
func (a *App) drawSettings(c *gfx.Canvas) {
	f := a.navPage(c, "Settings")
	items := []menuItem{
		{"Theme", themeLine(a.Theme, a.NightFrom, a.NightTo), dim, a.cycleTheme},
		{"Clock", clockLine(a.Clock24), dim, a.toggleClock},
		{"Home in a filament change", swapHomeLine(a.SwapHome), dim, a.toggleSwapHome},
	}
	if !a.Wide {
		items = slices.Insert(items, 2, menuItem{"Text", a.textSizeLine(), dim, a.cycleTextSize})
	}
	if a.OnRotate != nil {
		items = append(items, menuItem{"Screen", rotateLine(a.Rotate), dim, a.askRotate})
	}
	if a.Phones != nil {
		items = append(items, menuItem{"Phones", phonesLine(len(a.Phones.List())), dim, func() { a.open(pagePhones) }})
	}
	if a.OnNotify != nil {
		items = append(items, menuItem{"Notifications", notifyLine(a.NotifyTopic), dim, a.editNotify})
	}
	a.menuRows(c, f.body(), f.wide, 60, 5, items)
}

// phonesLine is the line of the Phones row: the number of paired phones.
func phonesLine(n int) string {
	switch n {
	case 0:
		return "None · pair a phone in the Printdeck app"
	case 1:
		return "1 phone gets the messages of the prints"
	}
	return fmt.Sprintf("%d phones get the messages of the prints", n)
}

// drawPhones is the list of the paired phones. A tap on a phone asks to remove it.
func (a *App) drawPhones(c *gfx.Canvas) {
	f := a.navPage(c, "Phones")
	list := a.Phones.List()
	area := a.listArea(f)
	if len(list) == 0 {
		noItems(c, area, "No phones", "Pair a phone in the Printdeck app,", "with the address and the API key.")
		return
	}
	var items []menuItem
	for _, ph := range list {
		ph := ph
		sub := "Paired " + fileDate(ph.Added, a.Now(), a.Clock24)
		if ph.Env == "sandbox" {
			sub += " · from Xcode"
		}
		items = append(items, menuItem{ph.Name, sub, dim, func() {
			a.confirm = &confirmBox{question: "Remove " + gfx.Fit(gfx.Face(true, 28), ph.Name, 300) + "?", yes: "Remove", danger: true,
				onYes: func() { a.Phones.Remove(ph.ID) }}
		}})
	}
	a.menuRows(c, area, f.wide, 60, 5, items)
}

// notifyLine is the line of the Notifications row: the ntfy topic, or Off.
func notifyLine(topic string) string {
	if topic == "" {
		return "Off · tap to type an ntfy topic"
	}
	return "ntfy · " + topic
}

// editNotify opens the keyboard for the ntfy topic of the push messages. An empty topic turns
// them off. A new topic gets a test message.
func (a *App) editNotify() {
	a.textKB = &keyboard{title: "ntfy topic, or the address of a topic", value: a.NotifyTopic, max: 120, ok: "Save", cancel: "Back",
		back: func() { a.textKB = nil },
		done: func(v string) {
			a.textKB = nil
			a.NotifyTopic = strings.TrimSpace(v)
			if a.NotifyTopic == "" {
				a.Toast("Notifications are off")
			} else {
				a.Toast("A test message goes to " + a.NotifyTopic)
			}
			a.OnNotify(a.NotifyTopic)
		}}
}

// rotateLine is the line of the Screen row: the turn of the UI, and its shape.
func rotateLine(deg int) string {
	shape := "wide"
	if deg == 90 || deg == 270 {
		shape = "tall"
	}
	return fmt.Sprintf("Turned %d°, %s", deg, shape)
}

// askRotate asks to turn the UI a quarter turn more, and then restarts the deck in that turn. Four
// taps go round once. The touch turns with the UI, so each turn can be used.
func (a *App) askRotate() {
	next := (a.Rotate + 90) % 360
	a.confirm = &confirmBox{question: fmt.Sprintf("Turn the screen to %d°? The deck restarts.", next), yes: "Turn",
		onYes: func() { a.OnRotate(next) }}
}

// swapHomeLine is the line of the Home in a filament change row.
func swapHomeLine(on bool) string {
	if on {
		return "On: homes while the nozzle heats"
	}
	return "Off"
}

// toggleSwapHome turns the homing in a filament change on or off.
func (a *App) toggleSwapHome() {
	a.SwapHome = !a.SwapHome
	a.Toast(swapHomeLine(a.SwapHome))
	if a.OnSwapHome != nil {
		a.OnSwapHome(a.SwapHome)
	}
}

// drawStats is the Statistics page: the totals of the print history. A wide screen has the four
// totals in the left column, and the results and the details at the right. History in the head
// opens the list of the prints.
func (a *App) drawStats(c *gfx.Canvas) {
	f := a.navPage(c, "Statistics")
	s := printer.ComputeStats(a.Printer.History(), a.Now())
	body := f.body()
	if s.Prints == 0 {
		noItems(c, body, "", "No finished prints yet")
		return
	}
	a.headKey(c, f.head, "History", func() { a.open(pageHistory) })
	success := 100 * s.Completed / s.Prints
	tiles := [][2]string{
		{"PRINTS", fmt.Sprint(s.Prints)},
		{"SUCCESS", fmt.Sprintf("%d%%", success)},
		{"PRINT TIME", fmt.Sprintf("%.0f h", s.Time.Hours())},
		{"FILAMENT", strings.ToUpper(a.weight(s.Filament))},
	}
	var rest image.Rectangle
	if f.wide {
		// The four totals in one row over the results and the details.
		r := kit.Rows(body, 14, kit.Px(80), kit.Fr(1))
		statTiles(c, r[0], 4, tiles)
		a.region("Totals", r[0])
		rest = r[1]
	} else {
		r := kit.Rows(kit.Inset(body, 0, 10, 0, 0), 18, kit.Px(196), kit.Fr(1))
		statTiles(c, r[0], 2, tiles)
		a.region("Totals", r[0])
		rest = r[1]
	}

	// The results as a bar: completed, cancelled, failed. Then the details.
	footH := 22
	if gfx.FinePrint() {
		footH = 48 // two lines
	}
	r := kit.Rows(kit.Inset(rest, 0, 0, 0, 8), 4, kit.Px(24), kit.Px(24), kit.Fr(1), kit.Px(footH))
	bar := r[0]
	window(c, bar)
	a.region("Results", bar)
	in := bar.Inset(4)
	x := in.Min.X
	for _, part := range []struct {
		n   int
		col color.RGBA
	}{{s.Completed, lcdInk}, {s.Cancelled, lcdDim}, {s.Failed, red}} {
		w := in.Dx() * part.n / s.Prints
		c.Fill(image.Rect(x, in.Min.Y, x+w, in.Max.Y), part.col)
		x += w
	}
	label(c, r[1], fmt.Sprintf("%d done · %d cancelled · %d failed", s.Completed, s.Cancelled, s.Failed), gfx.Center)

	box := kit.Inset(r[2], 0, 6, 0, 0)
	plate(c, box)
	a.region("Details", box)
	top := "—"
	if s.TopFile != "" {
		top = fmt.Sprintf("%d× %s", s.TopCount, strings.TrimSuffix(s.TopFile, ".gcode")) // the count first: a long name loses its end
	}
	rows := [][2]string{
		{"Filament", fmt.Sprintf("%s · %.0f m", a.weight(s.Filament), s.Filament/1000)},
		{"Average print", hm(int(s.Average.Minutes()))},
		{"Longest print", hm(int(s.Longest.Minutes()))},
		{"Last 7 days", fmt.Sprintf("%d prints", s.Last7)},
		{"Last 30 days", fmt.Sprintf("%d prints", s.Last30)},
		{"Most printed", top},
	}
	// The timings of this printer, which the time left of a print uses.
	rt := a.Printer.Rates()
	heat := func(kind string) string {
		if rt.Samples[kind] == 0 {
			return "Not known yet"
		}
		return fmt.Sprintf("%.1f s/°", rt.Heat[kind])
	}
	mmss := func(sec float64) string { return fmt.Sprintf("%d:%02d", int(sec)/60, int(sec)%60) }
	probe, cool, plan := "Not known yet", "Not known yet", "Not known yet"
	if rt.Samples["probe"] > 0 {
		probe = mmss(rt.Step["probe"])
	}
	if rt.CoolBed > 0 {
		cool = mmss(rt.CoolBed)
	}
	if rt.PlanSamples > 0 {
		plan = fmt.Sprintf("%+.0f %%", 100*rt.PlanError)
	}
	rows = append(rows, [][2]string{
		{"Bed heats", heat("heat-bed")},
		{"Nozzle heats", heat("heat-nozzle")},
		{"Probe", probe},
		{fmt.Sprintf("Bed cools to %.0f°", printer.ReleaseBed), cool},
		{"Moves vs plan", plan},
	}...)
	// The rows share the height. A wide screen has them in two columns.
	area := kit.Inset(box, 18, 8, 18, 8)
	if f.wide {
		half := (len(rows) + 1) / 2
		cols := kit.Cols(area, 28, kit.Fr(1), kit.Fr(1))
		detailRows(c, cols[0], rows[:half], cols[0].Dy()/half)
		detailRows(c, cols[1], rows[half:], cols[1].Dy()/half)
	} else {
		detailRows(c, area, rows, area.Dy()/len(rows))
	}
	if !s.Since.IsZero() {
		since := "Since " + s.Since.Format("Jan 2, 2006")
		grams := fmt.Sprintf("grams for %.2f mm %s", a.Printer.Machine().Diameter(), cmp.Or(a.Material, "PLA"))
		if gfx.FinePrint() {
			l := kit.Rows(kit.Inset(r[3], 0, 4, 0, 0), 0, kit.Fr(1), kit.Fr(1))
			label(c, l[0], since, gfx.Center)
			label(c, l[1], grams, gfx.Center)
		} else {
			label(c, kit.Inset(r[3], 0, 4, 0, 0), since+" · "+grams, gfx.Center)
		}
	}
}

// statTiles draws big numbers in display windows, cols in a row: a name and a value in each.
func statTiles(c *gfx.Canvas, area image.Rectangle, cols int, tiles [][2]string) {
	rows := (len(tiles) + cols - 1) / cols
	for i, r := range kit.Grid(area, cols, rows, 12)[:len(tiles)] {
		window(c, r)
		c.Text(image.Rect(r.Min.X+14, r.Min.Y+8, r.Max.X-14, r.Min.Y+30), tiles[i][0], gfx.Face(true, 14), lcdDim, gfx.Left)
		// The number as large as the tile allows, from 40 pixels down to 24.
		size := 40
		for size > 24 && gfx.Width(gfx.Face(true, size), tiles[i][1]) > r.Dx()-28 {
			size -= 2
		}
		c.Text(image.Rect(r.Min.X+14, r.Min.Y+30, r.Max.X-14, r.Max.Y-6), tiles[i][1], gfx.Face(true, size), lcdInk, gfx.Left)
	}
}
