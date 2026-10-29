package ui

import (
	"cmp"
	"fmt"
	"image"
	"printdeck/kit"
	"slices"
	"strings"

	"image/color"

	"golang.org/x/image/font"

	"printdeck/internal/board"
	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/internal/wifi"
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

// menuGroup is a group of rows of a menu page, under a heading. A group with no heading has no
// heading line.
type menuGroup struct {
	heading string
	items   []menuItem
	// checks makes each row a check row (checkRow), checked when checks[i] is true. nil is rows
	// with an arrow.
	checks []bool
	// note is a line of small text under the rows, or "".
	note string
}

// noteFace is the font of the note of a group, and noteLineH the height of each line.
const noteLineH = 20

func noteFace() font.Face { return gfx.Face(false, 14) }

// groupHeadH is the height of the heading of a group of rows. groupSpace is the space over the
// heading of each group after the first.
const groupHeadH, groupSpace = 24, 10

// menuGroups draws the groups of rows of a menu page from the top of area, each group under its
// heading. The rows share the height of the area, at most rowH high on a wide screen and 1.6 times
// rowH on a tall screen, as in menuRows. When the rows do not fit, the headings and the rows are
// in a list that scrolls, with rows of a size to touch.
func (a *App) menuGroups(c *gfx.Canvas, area image.Rectangle, wide bool, rowH, gap int, groups []menuGroup) {
	// The parts from the top of the list: the headings, the rows and the notes.
	type part struct {
		y, h    int
		top     int // the top of a row, or of the heading over the first row of a group
		heading string
		note    string
		it      menuItem
		row     bool
		check   int // 0 for a row with an arrow, 1 for a check row, 2 for a checked row
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
	fixed := (len(shown) - 1) * groupSpace
	noteLines := func(g menuGroup) int {
		if g.note == "" {
			return 0
		}
		return len(wrapText(noteFace(), g.note, area.Dx()-8))
	}
	for _, g := range shown {
		if g.heading != "" {
			fixed += groupHeadH + gap
		}
		if n := noteLines(g); n > 0 {
			fixed += n*noteLineH + gap
		}
	}
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
		if g.heading != "" {
			parts = append(parts, part{y: y, h: groupHeadH, heading: g.heading})
			y += groupHeadH + gap
		}
		for i, it := range g.items {
			h := max(rowH, minRowH)
			if gfx.FinePrint() {
				// The large text: a row is high enough for the title and the lines under it.
				h = max(h, largeRowH(len(subLines(it.sub, area.Dx()-largeRowInset))))
			}
			p := part{y: y, h: h, top: top, it: it, row: true}
			if g.checks != nil {
				p.check = 1
				if g.checks[i] {
					p.check = 2
				}
			}
			parts = append(parts, p)
			y += h + gap
			top = y
		}
		if n := noteLines(g); n > 0 {
			parts = append(parts, part{y: y - gap + 6, h: n * noteLineH, note: g.note})
			y += n*noteLineH + gap
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
	draw := func(c *gfx.Canvas, p part, r image.Rectangle) {
		switch {
		case p.row && p.check > 0:
			a.drawCheckRow(c, r, p.it.title, p.it.sub, p.check == 2, p.it.tap != nil)
		case p.row:
			a.drawMenuRow(c, r, p.it.title, p.it.sub, p.it.col, p.it.tap != nil, false)
		case p.note != "":
			textLines(c, kit.Inset(r, 4, 0, 4, 0), p.note, noteFace(), dim, noteLineH)
		default:
			label(c, r, p.heading, gfx.Left)
		}
	}
	if maxScroll == 0 {
		// All the rows fit: rows that stay in their place.
		for _, p := range parts {
			r := rect(p)
			draw(c, p, r)
			if p.row && p.it.tap != nil {
				a.addHit(kit.Hit{R: r, Tap: p.it.tap})
				a.addFocus(p.it.title, r, p.it.tap)
			}
		}
		return
	}
	c.Clipped(area, func(c *gfx.Canvas) {
		for _, p := range parts {
			if r := rect(p); r.Overlaps(area) {
				draw(c, p, r)
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
	simSub, simCol := simLine(st)
	rows = append(rows, row{"Simulated printer", simSub, simCol == orange, func() { a.open(pageSimulator) }})
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

// drawMachine is the Machine page: two lines of information, and a grid of keys for the pages
// of the printer and the deck, with the most used at the top. Restart… in the head asks which
// restart to do.
func (a *App) drawMachine(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	f := a.navPage(c, "Machine")
	mach := pr.Machine()
	b := backRect(f.head)
	a.printerKey(c, b.Sub(image.Pt(b.Dx()+12, 0)), "Restart…", raised, text, a.askRestart)

	var info, grid image.Rectangle
	if f.wide {
		r := kit.Rows(f.body(), 12, kit.Px(64), kit.Fr(1))
		info, grid = r[0], r[1]
	} else {
		r := kit.Rows(kit.Inset(f.body(), 0, 10, 0, 6), 16, kit.Px(76), kit.Fr(1))
		info, grid = r[0], r[1]
	}

	var wifiStatus wifi.Status
	if a.Setup != nil {
		wifiStatus, _ = a.Setup.Status()
	}
	var update UpdateStatus
	if a.Updater != nil {
		update = a.Updater.Status()
	}
	a.machineInfo(c, info, st, mach, update, wifiStatus.IP)

	var prints int
	var filament float64
	for _, r := range pr.History() {
		prints++
		filament += r.Filament
	}
	printsLine := "No prints yet"
	if prints > 0 {
		printsLine = fmt.Sprintf("%d %s · %s", prints, plural(prints, "print", "prints"), a.weight(filament))
	}
	maint, maintCol := maintenanceLine(mach, st)
	keys := []menuItem{
		{"Settings", "Theme, clock, cloud", dim, func() { a.open(pageSettings) }},
		{"Prints", printsLine, dim, func() { a.open(pageStats) }},
		{"Maintenance", maint, maintCol, func() { a.open(pageMaintenance) }},
		{"Macros", macrosLine(len(mach.Macros)), dim, func() { a.open(pageMacros) }},
		{"Console", "G-code", dim, func() { a.open(pageConsole) }},
	}
	if a.Updater != nil {
		line, warn := updateLine(update, st.Busy())
		col := dim
		if warn {
			col = orange
		}
		keys = append(keys, menuItem{"Software update", line, col, func() { a.open(pageUpdate) }})
	}
	if a.Setup != nil {
		keys = append(keys, menuItem{"Wi-Fi and help", wifiLine(wifiStatus), dim, a.OpenHelp})
	}
	if a.Metrics != nil {
		keys = append(keys, menuItem{"System monitor", monitorLine, dim, func() { a.open(pageMonitor) }})
	}
	// Two columns. The keys share the height of the grid, up to a height that keeps the two lines
	// of a key together.
	const gap = 10
	rows := (len(keys) + 1) / 2
	most := 84
	if !f.wide {
		most = 150
	}
	keyH := min(most, (grid.Dy()-(rows-1)*gap)/rows)
	grid.Max.Y = grid.Min.Y + rows*keyH + (rows-1)*gap
	for i, r := range kit.Grid(grid, 2, rows, gap)[:len(keys)] {
		k := keys[i]
		a.statusKey(c, r, k.title, k.sub, k.col, k.tap)
	}
}

// machineInfo draws the information of the Machine page in a display window: the versions in
// the first line; the address and the state of the firmware in the second line. Old firmware
// makes the second line bright.
func (a *App) machineInfo(c *gfx.Canvas, r image.Rectangle, st printer.State, mach printer.Machine, up UpdateStatus, ip string) {
	window(c, r)
	a.region("Information", r)
	var top, bottom []string
	switch {
	case up.System != "":
		// The deck program of the installed update runs on the old system.
		top = append(top, "SCREEN "+up.Current, "SYSTEM "+up.System)
	case up.Current != "" && up.HasStaged && up.Staged != "":
		top = append(top, "PRINTDECK "+up.Current+" · NEXT "+up.Staged)
	case up.Current != "":
		top = append(top, "PRINTDECK "+up.Current)
	}
	if v := shortVersion(mach.KlipperVersion); v != "" {
		top = append(top, "KLIPPER "+v)
	}
	if ip != "" {
		bottom = append(bottom, ip)
	}
	ink := lcdDim
	switch {
	case st.Firmware != "" && st.FirmwareOld:
		bottom = append(bottom, "FIRMWARE: FLASH IT")
		ink = lcdInk
	case st.Firmware != "":
		bottom = append(bottom, "FIRMWARE OK")
	}
	in := kit.Inset(r, 16, 6, 16, 6)
	l := kit.Rows(in, 0, kit.Fr(1), kit.Fr(1))
	for i, line := range []struct {
		parts []string
		face  font.Face
		ink   color.RGBA
	}{{top, gfx.Face(true, 18), lcdInk}, {bottom, gfx.Face(true, 16), ink}} {
		s := strings.Join(line.parts, " · ")
		if s == "" {
			s = "—"
		}
		c.Text(l[i], gfx.Fit(line.face, s, in.Dx()), line.face, line.ink, gfx.Left)
	}
}

// shortVersion is the release of a Klipper version: "v0.13" for "v0.13.0-777-g7bc4d0946". It is
// the version itself when it has no such form.
func shortVersion(v string) string {
	v, _, _ = strings.Cut(v, "-")
	if parts := strings.Split(v, "."); len(parts) > 2 {
		v = strings.Join(parts[:2], ".")
	}
	return v
}

// macrosLine is the line of the Macros key: the number of macros.
func macrosLine(n int) string {
	if n == 0 {
		return "No macros"
	}
	return fmt.Sprintf("%d %s", n, plural(n, "macro", "macros"))
}

// wifiLine is the line of the Wi-Fi and help key: the network, or the address.
func wifiLine(s wifi.Status) string {
	switch {
	case s.SSID != "":
		return s.SSID
	case s.IP != "":
		return s.IP
	}
	return "Not connected"
}

// statusKey is a key of the Machine page: the name of a page, and a short line under it in
// subCol. A tap opens the page. A name that is too long gets a smaller font. A line that is too
// long goes in more lines when the key is high enough, and else loses its end.
func (a *App) statusKey(c *gfx.Canvas, r image.Rectangle, title, sub string, subCol color.RGBA, tap func()) {
	a.drawAck(c, r)
	pressed := a.isPressed(r)
	key(c, r, "", raised, text, pressed)
	in := kit.Inset(r, 16, 8, 16, 8)
	if pressed {
		in = in.Add(image.Pt(0, 1))
	}
	w := in.Dx()
	size, subSize := 18, 15
	if r.Dy() >= 90 {
		size, subSize = 20, 16
	}
	upper := strings.ToUpper(title)
	one := size
	for one > 14 && gfx.Width(gfx.Face(true, one), upper) > w {
		one--
	}
	sf := gfx.Face(false, subSize)
	sh := sf.Metrics().Height.Ceil() + 2
	// A name that does not fit in one line goes in two lines, when the key is high enough for
	// them and a line of the text under them.
	names := []string{upper}
	tf := gfx.Face(true, one)
	if i := splitAt(upper); i > 0 && gfx.Width(tf, upper) > w {
		f2 := gfx.Face(true, size)
		for s := size; s >= 14; s-- {
			if f2 = gfx.Face(true, s); gfx.Width(f2, upper[:i]) <= w && gfx.Width(f2, upper[i+1:]) <= w {
				break
			}
		}
		if lh := f2.Metrics().Height.Ceil(); in.Dy() >= 2*lh+6+sh {
			names, tf = []string{upper[:i], upper[i+1:]}, f2
		}
	}
	lh := tf.Metrics().Height.Ceil()
	th := len(names)*lh + 6
	lines := wrapText(sf, sub, w)
	n := min(2, max(1, (in.Dy()-th)/sh)) // the lines that fit, at most 2
	if len(lines) > n {
		lines = append(lines[:n-1], strings.Join(lines[n-1:], " "))
	}
	// The block of n lines in the middle of the key, so the names of a row line up.
	y := in.Min.Y + (in.Dy()-th-n*sh)/2
	for i, name := range names {
		ny := y + 3 + i*lh
		c.Text(image.Rect(in.Min.X, ny, in.Max.X, ny+lh), gfx.Fit(tf, name, w), tf, text, gfx.Left)
	}
	for i, l := range lines {
		ly := y + th + i*sh
		c.Text(image.Rect(in.Min.X, ly, in.Max.X, ly+sh), gfx.Fit(sf, l, w), sf, subCol, gfx.Left)
	}
	acked := func() {
		a.ackR, a.ackAt = r, a.Now()
		tap()
	}
	a.addHit(kit.Hit{R: r, Tap: acked})
	a.addFocus(title, r, acked)
	a.region(title, r)
}

// askRestart asks which restart to do: Restart Klipper reads printer.cfg again and keeps the
// firmware; Firmware restart restarts the firmware of the board. Each asks again before it acts.
func (a *App) askRestart() {
	pr := a.Printer
	a.confirm = &confirmBox{question: "Which restart?", choices: []menuItem{
		{"Restart Klipper", "Reads printer.cfg again", dim, func() {
			a.confirm = &confirmBox{question: "Restart Klipper?", yes: "Restart", onYes: func() {
				a.Toast("Restarting Klipper…")
				a.do(pr.RestartKlipper)
			}}
		}},
		{"Firmware restart", "Restarts the board firmware", red, func() {
			a.confirm = &confirmBox{question: "Restart the printer firmware?", yes: "Restart", danger: true, onYes: func() {
				a.Toast("Restarting the firmware…")
				a.do(pr.FirmwareRestart)
			}}
		}},
	}}
}

// maintenanceLine is the line of the Maintenance key, and its color: a task that waits in orange
// (old firmware, results to save), or else the calibrations that the printer has.
func maintenanceLine(mach printer.Machine, st printer.State) (string, color.RGBA) {
	switch {
	case board.CanFlash("") && st.Firmware != "" && st.FirmwareOld:
		return "Old firmware", orange
	case st.SaveConfigPending:
		return "Results to save", orange
	}
	var names []string
	if mach.Has("bed_mesh") || mach.Has("bed_tilt") || mach.Has("screws_tilt_adjust") {
		names = append(names, "Bed")
	}
	if mach.Has("probe") {
		names = append(names, "Z offset")
	}
	if mach.Has("extruder") {
		names = append(names, "PID")
	}
	if len(names) == 0 {
		return "Nozzle change", dim
	}
	return strings.Join(names, ", "), dim
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
	a.list(c, l, func(i int) string { return recs[i].Name() }, func(c *gfx.Canvas, i int, box image.Rectangle) {
		a.recordRow(c, box, recs[i], recs[i].Summary(func(mm float64) float64 { return a.grams(mm, "") }), fileDate(recs[i].End, a.Now(), a.Clock24), files)
	}, open)
	if len(printer.Projects(recs)) > 0 {
		a.headKey(c, f.head, "Projects", func() { a.open(pageProjects) })
	}
}

// recordRowH is the height of a row of recordRow, with the gap under it.
func recordRowH() int {
	if gfx.FinePrint() {
		return 114
	}
	return fileRowH
}

// recordRow is the row of a finished print in a list: the thumbnail of the file at the left, and
// at its right the name, the summary and the date line in three lines, as a row of the Files page.
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
	c.Text(image.Rect(x, box.Min.Y+y1, box.Max.X-12, box.Min.Y+y1+30), r.Name(), gfx.Face(true, 19), text, gfx.Left)
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
	if a.Camera != nil {
		list := a.Camera.Cameras()
		items = append(items, menuItem{"Camera", pictureLine(list), dim, func() { a.open(pageCamera) }},
			menuItem{"Cameras", cameraLine(list), dim, func() { a.open(pageCameras) }})
	}
	if a.TimeLapse != nil {
		items = append(items, menuItem{"Time lapse", timeLapseLine(a.TimeLapse.Settings()), dim, func() { a.open(pageTimeLapse) }})
	}
	if a.Phones != nil {
		items = append(items, menuItem{"Phones", phonesLine(len(a.Phones.List())), dim, func() { a.open(pagePhones) }})
	}
	if a.OnNotify != nil {
		items = append(items, menuItem{"Notifications", notifyLine(a.NotifyTopic), dim, a.editNotify})
	}
	if a.Cloud != nil {
		items = append(items, menuItem{"Cloud", cloudLine(a.Cloud.State()), dim, func() { a.open(pageCloud) }})
	}
	if nm, ok := a.Setup.(Namer); ok {
		items = append(items, menuItem{"Name", nameLine(nm.Name()), dim, func() { a.editName(nm) }})
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
