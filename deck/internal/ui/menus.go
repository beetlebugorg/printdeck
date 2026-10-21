package ui

import (
	"fmt"
	"image"
	"printdeck/kit"
	"strings"

	"image/color"

	"printdeck/internal/board"
	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// The menu pages of the idle screen: Maintenance and Machine, and the pages that Machine opens.

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
	limit := rowH
	if !wide {
		limit = rowH * 16 / 10
	}
	rowH = min(limit, (area.Dy()+gap)/max(1, len(items))-gap)
	s := kit.NewStack(area, gap)
	for _, it := range items {
		a.menuRow(c, s.Next(rowH), it.title, it.sub, it.col, it.tap)
	}
}

// drawMaintenance lists the calibrations that the printer has (the modules of Klipper).
func (a *App) drawMaintenance(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	f := a.page(c, "Maintenance")
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
	var items []menuItem
	for _, r := range rows {
		col := dim
		if r.warn {
			col = orange
		}
		items = append(items, menuItem{r.title, r.sub, col, r.tap})
	}
	a.menuRows(c, a.listArea(f), f.wide, 86, 12, items)
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
		side, main := scr.Split(sideW)
		l := kit.Rows(kit.Inset(side, wideOut, 16, wideIn, lcdTop), 16, kit.Px(92), kit.Fr(1))
		lcdR, box = l[0], l[1]
		r := kit.Rows(kit.Inset(main, wideIn, lcdTop, wideOut, lcdTop), 10, kit.Fr(1), kit.Px(24), kit.Px(90))
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
	for _, l := range wrapText(face, "Put a sheet of paper under the nozzle. Lower the nozzle until the paper drags a little when you move it, then tap Accept.", box.Dx()-36) {
		c.Text(ls.Next(34), l, face, text, gfx.Left)
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
			a.Toast("Accepted. Save config in Maintenance writes it.")
			return nil
		})
	})
	a.button(c, k[1], "Abort", raised, red, func() { a.do(func() error { return pr.Gcode("ABORT") }) })
}

// drawMachine is the Machine page: the versions and the totals, and the advanced pages.
func (a *App) drawMachine(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	f := a.page(c, "Machine")
	mach := pr.Machine()
	// Tall: the information, the rows, and the restart keys at the bottom. Wide: the information
	// and the restart keys in the left column, and the rows at the right.
	var info, rowsArea, restart image.Rectangle
	if f.wide {
		r := kit.Rows(f.aside(), 10, kit.Fr(1), kit.Px(56))
		info, restart, rowsArea = r[0], r[1], f.body()
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
		c.Text(image.Rect(info.Min.X+130, y, info.Max.X-16, y+lineH), gfx.Fit(gfx.Face(true, 17), l[1], info.Dx()-150), gfx.Face(true, 17), lcdInk, gfx.Right)
	}

	macros := "No macros"
	if n := len(mach.Macros); n > 0 {
		macros = strings.Join(mach.Macros[:min(2, n)], ", ")
		if n > 2 {
			macros += fmt.Sprintf(", %d more", n-2)
		}
	}
	items := []menuItem{
		{"Statistics", fmt.Sprintf("%d prints · %d h · %s of filament", prints, hours, a.weight(filament)), dim, func() { a.open(pageStats) }},
		{"Print history", "Each print, with the result", dim, func() { a.open(pageHistory) }},
		{"Macros", macros, dim, func() { a.open(pageMacros) }},
		{"Console", "Type G-code, and read the replies", dim, func() { a.open(pageConsole) }},
		{"Theme", themeLine(a.Theme, a.NightFrom, a.NightTo), dim, a.cycleTheme},
		{"Clock", clockLine(a.Clock24), dim, a.toggleClock},
	}
	if a.Updater != nil {
		line, warn := updateLine(a.Updater.Status())
		col := dim
		if warn {
			col = orange
		}
		items = append([]menuItem{{"Software update", line, col, func() { a.open(pageUpdate) }}}, items...)
	}
	if a.Setup != nil {
		items = append(items, menuItem{"Wi-Fi and help", "The network, the password, the addresses", dim, a.OpenHelp})
	}
	a.menuRows(c, rowsArea, f.wide, 60, 5, items)
	k := kit.Cols(restart, 12, kit.Fr(1), kit.Fr(1))
	a.button(c, k[0], "Restart Klipper", raised, text, func() {
		a.confirm = &confirmBox{question: "Restart Klipper?", yes: "Restart", onYes: func() {
			a.Toast("Restarting Klipper…")
			a.do(pr.RestartKlipper)
		}}
	})
	a.button(c, k[1], "Firmware restart", raised, red, func() {
		a.confirm = &confirmBox{question: "Restart the printer firmware?", yes: "Restart", danger: true, onYes: func() {
			a.Toast("Restarting the firmware…")
			a.do(pr.FirmwareRestart)
		}}
	})
}

// drawMacros lists the macros of the printer. A tap runs one, after a question.
func (a *App) drawMacros(c *gfx.Canvas, st printer.State) {
	f := a.page(c, "Macros")
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
		c.Text(kit.Inset(r, 18, 0, 18, 0), macros[i], gfx.Face(true, 19), text, gfx.Left)
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

// drawHistory lists the finished prints, newest first. A tap prints a file again.
func (a *App) drawHistory(c *gfx.Canvas) {
	pr := a.Printer
	f := a.page(c, "Print history")
	recs := pr.History()
	area := a.listArea(f)
	if len(recs) == 0 {
		noItems(c, area, "", "No finished prints yet")
		return
	}
	reprint := func(i int) {
		if !pr.State().Busy() {
			a.confirmPrint(recs[i].Path)
		}
	}
	if f.wide {
		st := printer.ComputeStats(recs, a.Now())
		tiles := [][2]string{{"PRINTS", fmt.Sprint(st.Prints)}, {"SUCCESS", fmt.Sprintf("%d%%", 100*st.Completed/max(1, st.Prints))}}
		statTiles(c, kit.Rows(f.aside(), 0, kit.Px(92))[0], 2, tiles)
	}
	l := kit.List{Area: area, N: len(recs), RowH: 90, Gap: 10, Scroll: &a.historyScroll}
	a.list(c, l, func(i int) string { return recs[i].File }, func(c *gfx.Canvas, i int, box image.Rectangle) {
		r := recs[i]
		plate(c, box)
		col := dim
		if r.Result != "complete" {
			col = red
		}
		c.Text(image.Rect(box.Min.X+18, box.Min.Y+8, box.Max.X-18, box.Min.Y+38), gfx.Fit(gfx.Face(true, 19), r.File, box.Dx()-36), gfx.Face(true, 19), text, gfx.Left)
		// The date at the right, and the summary in the space that it leaves.
		date := fileDate(r.End, a.Now(), a.Clock24)
		line := image.Rect(box.Min.X+18, box.Min.Y+38, box.Max.X-18, box.Max.Y-8)
		c.Text(line, date, gfx.Face(false, 14), dim, gfx.Right)
		sum := line
		sum.Max.X -= gfx.Width(gfx.Face(false, 14), date) + 12
		c.Text(sum, gfx.Fit(gfx.Face(false, 16), r.Summary(func(mm float64) float64 { return a.grams(mm, "") }), sum.Dx()), gfx.Face(false, 16), col, gfx.Left)
	}, reprint)
}

// drawStats is the Statistics page: the totals of the print history. A wide screen has the four
// totals in the left column, and the results and the details at the right.
func (a *App) drawStats(c *gfx.Canvas) {
	f := a.page(c, "Statistics")
	s := printer.ComputeStats(a.Printer.History(), a.Now())
	body := f.body()
	if s.Prints == 0 {
		noItems(c, body, "", "No finished prints yet")
		return
	}
	success := 100 * s.Completed / s.Prints
	tiles := [][2]string{
		{"PRINTS", fmt.Sprint(s.Prints)},
		{"SUCCESS", fmt.Sprintf("%d%%", success)},
		{"PRINT TIME", fmt.Sprintf("%.0f h", s.Time.Hours())},
		{"FILAMENT", strings.ToUpper(a.weight(s.Filament))},
	}
	var rest image.Rectangle
	if f.wide {
		statTiles(c, f.aside(), 2, tiles)
		a.region("Totals", f.aside())
		rest = body
	} else {
		r := kit.Rows(kit.Inset(body, 0, 10, 0, 0), 18, kit.Px(196), kit.Fr(1))
		statTiles(c, r[0], 2, tiles)
		a.region("Totals", r[0])
		rest = r[1]
	}

	// The results as a bar: completed, cancelled, failed. Then the details.
	r := kit.Rows(kit.Inset(rest, 0, 0, 0, 8), 4, kit.Px(24), kit.Px(24), kit.Fr(1), kit.Px(22))
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
		top = fmt.Sprintf("%s (%d×)", s.TopFile, s.TopCount)
	}
	rows := [][2]string{
		{"Filament", fmt.Sprintf("%s · %.0f m", a.weight(s.Filament), s.Filament/1000)},
		{"Average print", hm(int(s.Average.Minutes()))},
		{"Longest print", hm(int(s.Longest.Minutes()))},
		{"Last 7 days", fmt.Sprintf("%d prints", s.Last7)},
		{"Last 30 days", fmt.Sprintf("%d prints", s.Last30)},
		{"Most printed", top},
	}
	for i, kv := range kit.Rows(kit.Inset(box, 18, 8, 18, 8), 0, kit.Fr(1), kit.Fr(1), kit.Fr(1), kit.Fr(1), kit.Fr(1), kit.Fr(1)) {
		label(c, kv, rows[i][0], gfx.Left)
		v := kit.Inset(kv, 152, 0, 0, 0)
		c.Text(v, gfx.Fit(gfx.Face(true, 17), rows[i][1], v.Dx()), gfx.Face(true, 17), text, gfx.Right)
	}
	if !s.Since.IsZero() {
		label(c, kit.Inset(r[3], 0, 4, 0, 0), "Since "+s.Since.Format("Jan 2, 2006")+" · grams for 1.75 mm PLA", gfx.Center)
	}
}

// statTiles draws big numbers in display windows, cols in a row: a name and a value in each.
func statTiles(c *gfx.Canvas, area image.Rectangle, cols int, tiles [][2]string) {
	rows := (len(tiles) + cols - 1) / cols
	for i, r := range kit.Grid(area, cols, rows, 12)[:len(tiles)] {
		window(c, r)
		c.Text(image.Rect(r.Min.X+14, r.Min.Y+8, r.Max.X-14, r.Min.Y+30), tiles[i][0], gfx.Face(true, 14), lcdDim, gfx.Left)
		c.Text(image.Rect(r.Min.X+14, r.Min.Y+30, r.Max.X-14, r.Max.Y-6), tiles[i][1], gfx.Face(true, 40), lcdInk, gfx.Left)
	}
}
