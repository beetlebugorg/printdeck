package ui

import (
	"fmt"
	"image"
	"strings"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/kit"
)

// The Filament page of a printer with an AMS (a Bambu Lab printer): a row for each tray, with the
// color, the material, the roll of the deck in the tray and the filament left. A tap on a tray
// adds a new roll for it, puts a roll of the deck in it, or takes its roll out. The deck subtracts
// the filament of each print from the roll of each tray that the print used.

// amsPrinter is a printer with an AMS (printer.Bambu).
type amsPrinter interface {
	AMS() printer.AMS
	LinkTray(slot int, roll string) error
	AddTrayRoll(slot int, grams float64, color string) (printer.Spool, error)
}

// amsOf returns the AMS of the printer of the app, or false.
func (a *App) amsOf() (amsPrinter, bool) {
	p, ok := a.Printer.(amsPrinter)
	return p, ok
}

// trayName is the name of a tray for the screens: "Tray 2", or "AMS 2 · Tray 1" with more than one
// AMS.
func trayName(t printer.AMSTray, units int) string {
	if t.External {
		return "External spool"
	}
	if units > 1 {
		return fmt.Sprintf("AMS %d · Tray %d", t.Unit+1, t.Index+1)
	}
	return fmt.Sprintf("Tray %d", t.Index+1)
}

// unitLine is the line of an AMS: its temperature and its humidity.
func unitLine(u printer.AMSUnit, units int) string {
	name := "AMS"
	if units > 1 {
		name = fmt.Sprintf("AMS %d", u.Index+1)
	}
	parts := []string{name, fmt.Sprintf("%.0f°", u.Temp)}
	if u.HumidityPct > 0 {
		parts = append(parts, fmt.Sprintf("%.0f%% humidity", u.HumidityPct))
	}
	return strings.Join(parts, " · ")
}

// drawAMS is the Filament page of a printer with an AMS.
func (a *App) drawAMS(c *gfx.Canvas, ams amsPrinter) {
	f := a.navPage(c, "Filament")
	a.headKey(c, f.head, "Rolls", func() { a.open(pageSpools) })
	st := ams.AMS()
	area := a.listArea(f)
	if len(st.Trays) == 0 {
		noItems(c, area, "No AMS", "The printer reports no AMS.")
		return
	}
	units := len(st.Units)
	two := a.Printer.State().HasTool2
	r := kit.Rows(area, 10, kit.Px(28), kit.Fr(1))
	var lines []string
	for _, u := range st.Units {
		lines = append(lines, unitLine(u, units))
	}
	c.Text(r[0], strings.Join(lines, "   "), gfx.Face(false, 16), dim, gfx.Left)
	spools := a.Printer.SpoolStore()
	l := kit.List{Area: r[1], N: len(st.Trays), RowH: 84, Gap: 8, Scroll: &a.spoolScroll}
	a.list(c, l, func(i int) string { return trayName(st.Trays[i], units) }, func(c *gfx.Canvas, i int, rr image.Rectangle) {
		t := st.Trays[i]
		title, sub := trayName(t, units), "Empty"
		color := t.Color
		if t.Loaded {
			title += " · " + t.Material
			// A printer with two nozzles: the nozzle that the spool is in, as the status panel names
			// it, and a mark under the swatch for the spool that feeds now.
			switch {
			case two && t.Feeds == 0:
				title += " · nozzle"
			case two && t.Feeds == 1:
				title += " · aux"
			case t.Active:
				title += " · feeding"
			}
			sub = "No roll of the deck · tap to add one"
			if t.Remain >= 0 {
				sub = fmt.Sprintf("%d%% left (from the printer)", t.Remain)
			}
		}
		if t.Roll != "" && spools != nil {
			if sp, ok := spools.Get(t.Roll); ok {
				sub = fmt.Sprintf("%s %s · %s", sp.Number(), sp.Label(), rollLine(sp))
				if sp.Color != "" {
					color = sp.Color
				}
			}
		}
		sw := image.Rect(rr.Min.X, rr.Min.Y, rr.Min.X+24, rr.Max.Y)
		swatch := kit.Center(sw, 22, 22)
		if col, ok := printer.ParseHex(color); ok && t.Loaded {
			c.RoundRect(swatch.Inset(-1), 4, dim)
			c.RoundRect(swatch, 3, col)
		} else {
			c.RoundRect(swatch, 3, keyOff)
		}
		if two && t.Active {
			c.Fill(image.Rect(swatch.Min.X, swatch.Max.Y+3, swatch.Max.X, swatch.Max.Y+6), orange)
		}
		a.menuRow(c, image.Rect(sw.Max.X+10, rr.Min.Y, rr.Max.X, rr.Max.Y), title, sub, dim, nil)
	}, func(i int) { a.askTray(ams, st.Trays[i], units) })
}

// askTray asks what to do with a tray: add a new full roll, put a roll of the deck in it, open its
// roll, or take its roll out.
func (a *App) askTray(ams amsPrinter, t printer.AMSTray, units int) {
	name := trayName(t, units)
	choices := []menuItem{
		{"New 1 kg roll", "A full roll of " + nonEmpty(t.Material, "filament") + " in " + name, dim, func() {
			a.addTrayRoll(ams, t, 1000)
		}},
		{"New roll of another size", "Type the grams of filament on the spool", dim, func() {
			a.editNumber("Filament on the spool", 1000, 9999, " g", func(v int) { a.addTrayRoll(ams, t, float64(v)) })
		}},
	}
	if s := a.Printer.SpoolStore(); s != nil {
		if len(s.List()) > 0 {
			choices = append(choices, menuItem{"A roll of the deck", "Put a roll that the deck has in " + name, dim, func() {
				a.pickTrayRoll(ams, t, name)
			}})
		}
	}
	if t.Roll != "" {
		choices = append(choices,
			menuItem{"Open the roll", "Its color, its weight and its prints", dim, func() {
				a.spoolID = t.Roll
				a.open(pageSpool)
			}},
			menuItem{"Take the roll out", "The deck stops counting the filament of " + name, dim, func() {
				a.trayChange(func() error { return ams.LinkTray(t.Slot, "") })
			}})
	}
	a.confirm = &confirmBox{question: name, choices: choices}
}

// addTrayRoll adds a roll with grams of filament for the tray t, and opens it so the user can check
// its color.
func (a *App) addTrayRoll(ams amsPrinter, t printer.AMSTray, grams float64) {
	sp, err := ams.AddTrayRoll(t.Slot, grams, "")
	if err != nil {
		a.Toast("The roll was not added: " + err.Error())
		return
	}
	a.spoolID = sp.ID
	a.open(pageSpool)
}

// pickTrayRoll asks which roll of the deck goes in the tray t.
func (a *App) pickTrayRoll(ams amsPrinter, t printer.AMSTray, name string) {
	s := a.Printer.SpoolStore()
	var choices []menuItem
	for _, sp := range s.List() {
		sp := sp
		choices = append(choices, menuItem{sp.Number() + " " + sp.Label(), rollLine(sp), dim, func() {
			a.trayChange(func() error { return ams.LinkTray(t.Slot, sp.ID) })
		}})
	}
	a.confirm = &confirmBox{question: "Roll in " + name, choices: choices}
}

// trayChange runs a change of a tray, and shows its error.
func (a *App) trayChange(f func() error) {
	if err := f(); err != nil {
		a.Toast("The change was not saved: " + err.Error())
	}
	a.Redraw()
}

func nonEmpty(s, alt string) string {
	if s == "" {
		return alt
	}
	return s
}

// trayAsker is a printer that asks which roll a new spool of a tray is (printer.Bambu).
type trayAsker interface {
	TrayQuestion() (printer.AMSTray, bool)
	AnswerTray(slot int)
}

// askTrays asks on each screen which roll the new spool of a tray is, when a tray waits for an
// answer and no screen shows a question. Draw calls it.
func (a *App) askTrays() {
	q, ok := a.Printer.(trayAsker)
	ams, ok2 := a.amsOf()
	if !ok || !ok2 || !a.fleetQuiet() || a.hub.question() != nil {
		return
	}
	t, ok := q.TrayQuestion()
	if !ok {
		return
	}
	units := len(ams.AMS().Units)
	name := trayName(t, units)
	slot := t.Slot
	answer := func(f func()) func() {
		return func() {
			q.AnswerTray(slot)
			if f != nil {
				f()
			}
			a.hub.redraw()
		}
	}
	add := func(grams float64) {
		if _, err := ams.AddTrayRoll(slot, grams, ""); err != nil {
			a.Toast("The roll was not added: " + err.Error())
		}
	}
	what := nonEmpty(strings.TrimSpace(t.Brand+" "+t.Material), "filament")
	choices := []menuItem{
		{"New 1 kg roll", "A full roll of " + what + colorSuffix(t.Color), dim, answer(func() { add(1000) })},
		{"New roll of another size", "Type the grams of filament on the spool", dim, answer(func() {
			a.editNumber("Filament on the spool", 1000, 9999, " g", func(v int) { add(float64(v)) })
		})},
	}
	// The rolls of the deck that are in no tray, with the material of the spool first.
	if s := a.Printer.SpoolStore(); s != nil {
		inTray := map[string]bool{}
		for _, tr := range ams.AMS().Trays {
			inTray[tr.Roll] = true
		}
		var same, other []printer.Spool
		for _, sp := range s.List() {
			switch {
			case inTray[sp.ID] || sp.Left() <= 0:
			case strings.EqualFold(sp.Material, t.Material):
				same = append(same, sp)
			default:
				other = append(other, sp)
			}
		}
		for i, sp := range append(same, other...) {
			if i == 3 {
				break
			}
			sp := sp
			choices = append(choices, menuItem{sp.Number() + " " + sp.Label(), rollLine(sp), dim, answer(func() {
				if err := ams.LinkTray(slot, sp.ID); err != nil {
					a.Toast("The change was not saved: " + err.Error())
				}
			})})
		}
	}
	choices = append(choices, menuItem{"Do not count it", "The deck counts no filament of " + name, dim, answer(nil)})
	a.hub.ask(&confirmBox{question: "New spool in " + name, choices: choices, onNo: answer(nil)})
}

// colorSuffix is ", gray" for a tray with a color, or "".
func colorSuffix(hex string) string {
	if hex == "" {
		return ""
	}
	return ", " + strings.ToLower(colorName(hex))
}

// trayMatcher is a printer that feeds each filament of a file from a tray of its AMS
// (printer.Bambu).
type trayMatcher interface {
	FileTrays(path string) ([]printer.TrayMatch, error)
}

// trayChoices asks to start a file on a printer with an AMS. The question shows the tray of each
// filament. A filament that no tray has stops the start: the user puts a spool of it in the AMS.
func (a *App) trayChoices(tm trayMatcher, path, name string, start func()) *confirmBox {
	matches, err := tm.FileTrays(path)
	if err != nil {
		return infoBox("NOT A SLICED 3MF", name+"\n\nSlice it for this printer in Bambu Studio or OrcaSlicer, and send the 3MF.")
	}
	units := 1
	if ams, ok := a.amsOf(); ok {
		units = max(1, len(ams.AMS().Units))
	}
	var parts []string
	for _, m := range matches {
		if m.Slot < 0 {
			return infoBox("NO TRAY FOR "+strings.ToUpper(m.Filament.Type),
				fmt.Sprintf("%s\n\nFilament %d is %s %s. No tray of the AMS has %s. Put a spool of it in the AMS, then start again.",
					name, m.Filament.ID, strings.ToLower(colorName(m.Filament.Color)), m.Filament.Type, m.Filament.Type))
		}
		parts = append(parts, trayName(printer.AMSTray{Unit: m.Slot / 4, Index: m.Slot % 4}, units))
	}
	choices := []menuItem{{"Print", "The bed is clear · " + strings.Join(parts, ", "), dim, start}}
	return &confirmBox{question: "Print " + gfx.Fit(gfx.Face(true, 26), name, 340) + "?", choices: choices, camera: true}
}

// bambuControls are the controls of a Bambu Lab printer that a Klipper printer does not have
// (printer.Bambu).
type bambuControls interface {
	SpeedLevels() (names []string, now int)
	SetSpeedLevel(level int) error
	Lights() (chamber, work, hasChamber, hasWork bool)
	SetLight(work, on bool) error
	Fans() (part, aux, chamber float64)
	SetFan(fan, percent int) error
}

// askPrinterControls asks which control of the printer to change: the speed level, a light, or a
// fan.
func (a *App) askPrinterControls(bc bambuControls) {
	names, now := bc.SpeedLevels()
	speed := "Not known"
	if now >= 1 && now <= len(names) {
		speed = names[now-1]
	}
	chamber, work, hasChamber, hasWork := bc.Lights()
	part, aux, cham := bc.Fans()
	run := func(f func() error) {
		go func() {
			err := f()
			a.post(func() {
				if err != nil {
					a.Toast("The printer did not get it: " + err.Error())
				}
			})
		}()
	}
	choices := []menuItem{{"Speed", speed, dim, func() {
		var levels []menuItem
		for i, n := range names {
			level := i + 1
			sub := ""
			if level == now {
				sub = "Now"
			}
			levels = append(levels, menuItem{n, sub, dim, func() { run(func() error { return bc.SetSpeedLevel(level) }) }})
		}
		a.confirm = &confirmBox{question: "Speed of the print", choices: levels}
	}}}
	if hasChamber {
		choices = append(choices, menuItem{"Chamber light", onOff(chamber), dim, func() {
			run(func() error { return bc.SetLight(false, !chamber) })
		}})
	}
	if hasWork {
		choices = append(choices, menuItem{"Work light", onOff(work), dim, func() {
			run(func() error { return bc.SetLight(true, !work) })
		}})
	}
	fan := func(title string, id int, v float64) menuItem {
		return menuItem{title, fmt.Sprintf("%.0f%%", v), dim, func() {
			a.editNumber(title, int(v+0.5), 100, " %", func(p int) { run(func() error { return bc.SetFan(id, p) }) })
		}}
	}
	choices = append(choices, fan("Part fan", printer.FanPart, part), fan("Aux fan", printer.FanAux, aux),
		fan("Chamber fan", printer.FanChamber, cham))
	if ts, ok := bc.(tool2Setter); ok {
		if st := a.Printer.State(); st.HasTool2 {
			choices = append(choices, menuItem{"Aux nozzle", fmt.Sprintf("%.0f° · tap to set", st.Tool2), dim, func() {
				a.editTemp("Aux nozzle temperature", int(st.Tool2Target), 300, toolPresets, ts.SetTool2Temp)
			}})
			now := "The nozzle"
			if st.ToolNow == 1 {
				now = "The aux nozzle"
			}
			choices = append(choices, menuItem{"Nozzle in use", now, dim, func() {
				if st.Busy() {
					a.Toast("The printer changes the nozzle by itself during a print")
					return
				}
				pick := func(n int) func() {
					return func() { run(func() error { return ts.SelectTool(n) }) }
				}
				a.confirm = &confirmBox{question: "Which nozzle prints? The toolhead moves to change it.", choices: []menuItem{
					{"The nozzle", "With the AMS", dim, pick(0)},
					{"The aux nozzle", "With the external spool", dim, pick(1)},
				}}
			}})
		}
	}
	a.confirm = &confirmBox{question: "Printer controls", choices: choices}
}
