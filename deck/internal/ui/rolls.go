package ui

import (
	"fmt"
	"image"
	"printdeck/kit"
	"strconv"
	"strings"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// The rolls of filament: the form of a new roll, the Rolls page, the page of one roll, and the
// prints that used a roll. The filament on a roll is the weight on a scale less the weight of the
// empty reel. Each finished print uses filament of the roll in the printer (printer.Spools).

// rollForm is the form of a new roll, over everything until Save or Back.
type rollForm struct {
	material string
	cura     printer.SlicerMaterial // the material of Cura, or none
	color    string                 // the color of the roll, or "": the color of the material of Cura
	name     string
	scale    int // grams on the scale, with the reel
	reel     int // grams of the empty reel
	price    int // dollars
	kb       *keyboard
	done     func(sp printer.Spool) // after Save; nil only closes the form
}

// openRollForm opens the form of a new roll. done is called with the new roll, which is in the
// printer after Save.
func (a *App) openRollForm(done func(sp printer.Spool)) {
	m := a.Material
	if m == "" {
		m = "PLA"
	}
	reel := 0
	if s := a.Printer.SpoolStore(); s != nil {
		reel = int(s.LastReel() + 0.5)
	}
	a.form = &rollForm{material: m, reel: reel, done: done}
}

// editNumber opens the keypad for a whole number with a unit, for example grams.
func (a *App) editNumber(title string, current, maxV int, unit string, set func(v int)) {
	a.keypad = &keypad{title: title, digits: strconv.Itoa(current), max: maxV, unit: unit, set: set}
}

// drawRollForm draws the form of a new roll: the material, the name, the weights and the price.
func (a *App) drawRollForm(c *gfx.Canvas) {
	fm := a.form
	if fm.kb != nil {
		a.drawKeyboard(c, fm.kb)
		return
	}
	f := a.pageBack(c, "New roll", "Back", func() { a.form = nil })
	// The material, the rows, the filament and Save under each other. Wide: the rows in two
	// columns, and Save at the right of the row of actions.
	var mats, rows, net, save image.Rectangle
	if f.wide {
		content, acts := f.split()
		l := kit.Rows(content, 10, kit.Px(52), kit.Fr(1), kit.Px(24))
		mats, rows, net = l[0], l[1], l[2]
		save = kit.Cols(acts, 12, kit.Fr(1), kit.Fr(1))[1]
	} else {
		r := kit.Rows(kit.Inset(f.body(), 0, 10, 0, 8), 12, kit.Px(52), kit.Fr(1), kit.Px(28), kit.Px(80))
		mats, rows, net, save = r[0], r[1], r[2], r[3]
	}
	a.region("Material", mats)
	for i, r := range kit.Cols(mats, 6, kit.Fr(1), kit.Fr(1), kit.Fr(1), kit.Fr(1)) {
		name := materials[i].Name
		fill, fg := keyOff, text
		if fm.material == name {
			fill, fg = keyOn, keyOnInk
		}
		a.button(c, r, name, fill, fg, func() {
			fm.material = name
			if !strings.EqualFold(fm.cura.Material, name) {
				fm.cura = printer.SlicerMaterial{} // the material of Cura is of another type
			}
		})
	}
	grams := func(v int) string {
		if v <= 0 {
			return "Tap to type it"
		}
		return fmt.Sprintf("%d g", v)
	}
	name := fm.name
	if name == "" {
		name = "Tap to type it, for example Black eSun"
	}
	price := "Tap to type it"
	if fm.price > 0 {
		price = fmt.Sprintf("$%d", fm.price)
	}
	items := []menuItem{
		{"Name", name, dim, func() {
			fm.kb = &keyboard{title: "Name of the roll", value: fm.name, max: 32, ok: "OK", cancel: "Back",
				back: func() { fm.kb = nil }, done: func(v string) { fm.name, fm.kb = strings.TrimSpace(v), nil }}
		}},
		{"Color", colorName(fm.color), dim, func() {
			a.pickColor(fm.color, func(hex string) { fm.color = hex })
		}},
		{"On the scale", grams(fm.scale), dim, func() {
			a.editNumber("Roll on the scale", fm.scale, 9999, " g", func(v int) { fm.scale = v })
		}},
		{"Empty reel", grams(fm.reel), dim, func() {
			a.editNumber("Weight of an empty reel", fm.reel, 2000, " g", func(v int) { fm.reel = v })
		}},
		{"Price", price, dim, func() {
			a.editNumber("Price of the roll in dollars", fm.price, 999, "", func(v int) { fm.price = v })
		}},
	}
	// The material of Cura, when the Cura plugin sent the materials. Its type and its name go into
	// the form.
	if len(a.curaMaterials()) > 0 {
		sub := "Tap to choose it, for example Generic PLA"
		if fm.cura.ID != "" {
			sub = fm.cura.Name
		}
		items = append([]menuItem{{"Cura material", sub, dim, func() {
			a.pickCuraMaterial(fm.material, func(m printer.SlicerMaterial) {
				fm.cura = m
				if m.Material != "" {
					fm.material = m.Material
				}
				if fm.name == "" && m.Name != "" {
					fm.name = m.Name
					if r := []rune(fm.name); len(r) > 32 { // the most that the name takes
						fm.name = string(r[:32])
					}
				}
			})
		}}}, items...)
	}
	if f.wide {
		n := (len(items) + 1) / 2
		for i, cell := range kit.Grid(rows, 2, n, 10)[:len(items)] {
			it := items[i]
			a.menuRow(c, cell, it.title, it.sub, it.col, it.tap)
		}
	} else {
		a.menuRows(c, rows, f.wide, 86, 12, items)
	}

	left := fm.scale - fm.reel
	msg := "Weigh the roll with its reel"
	if fm.scale > 0 && left > 0 {
		msg = fmt.Sprintf("Filament on the roll: %d g", left)
	}
	label(c, net, msg, gfx.Center)
	if fm.scale <= 0 || left <= 0 {
		key(c, save, "Save", surface, dim, false)
		return
	}
	a.button(c, save, "Save", orange, onAcc, func() {
		s := a.Printer.SpoolStore()
		if s == nil {
			a.form = nil
			return
		}
		sp := s.Add(fm.name, fm.material, float64(fm.scale), float64(fm.reel), float64(fm.price), a.Now())
		if fm.cura.ID != "" {
			s.SetCuraMaterial(sp.ID, fm.cura)
		}
		if fm.color != "" {
			s.SetColor(sp.ID, fm.color)
		}
		sp, _ = s.Get(sp.ID)
		a.setMaterial(sp.Material)
		a.form = nil
		if fm.done != nil {
			fm.done(sp)
		}
	})
}

// rollLine is the line of a roll under its name: the material and the filament on it.
func rollLine(sp printer.Spool) string {
	line := sp.Material + " · " + printer.Weight(sp.Left()) + " left"
	if sp.Price > 0 {
		line += fmt.Sprintf(" · $%.0f", sp.Price)
	}
	return line
}

// drawSpools is the Rolls page: the rolls, the newest first, with the roll in the printer marked.
// A tap opens the page of a roll.
func (a *App) drawSpools(c *gfx.Canvas) {
	f := a.navPage(c, "Rolls")
	// Opened with the Rolls key of Filament: the key stays in its place, lit, and goes back.
	if n := len(a.pages); n >= 2 && a.pages[n-2] == pageFilament {
		a.headKeyOn(c, f.head, "Rolls", true, a.back)
	}
	s := a.Printer.SpoolStore()
	var list []printer.Spool
	in := ""
	if s != nil {
		list = s.List()
		if sp, ok := s.In(); ok {
			in = sp.ID
		}
	}
	area := a.listArea(f)
	var add image.Rectangle
	if f.wide {
		// The main action at the right of the row of actions.
		var acts image.Rectangle
		area, acts = f.split()
		add = kit.Cols(acts, 12, kit.Fr(1), kit.Fr(1))[1]
	} else {
		r := kit.Rows(area, 12, kit.Fr(1), kit.Px(72))
		area, add = r[0], r[1]
	}
	a.button(c, add, "New roll", orange, onAcc, func() { a.openRollForm(nil) })
	if len(list) == 0 {
		noItems(c, area, "No rolls yet", "Add a roll here, or when", "you change the filament.")
		return
	}
	l := kit.List{Area: area, N: len(list), RowH: 90, Gap: 10, Scroll: &a.spoolScroll}
	a.list(c, l, func(i int) string { return list[i].Label() }, func(c *gfx.Canvas, i int, r image.Rectangle) {
		sp := list[i]
		title := sp.Label()
		if sp.ID == in {
			title += " · loaded"
		}
		a.menuRow(c, r, title, rollLine(sp), dim, nil)
	}, func(i int) {
		a.spoolID = list[i].ID
		a.open(pageSpool)
	})
}

// drawSpool is the page of one roll: what is on it, and Weigh again and Remove.
func (a *App) drawSpool(c *gfx.Canvas) {
	s := a.Printer.SpoolStore()
	var sp printer.Spool
	ok := false
	if s != nil {
		sp, ok = s.Get(a.spoolID)
	}
	if !ok {
		a.back()
		return
	}
	pf := a.navFrame()
	f := a.navPage(c, gfx.Fit(gfx.Face(true, 24), sp.Label(), pf.head.Dx()-200))
	cura := "None"
	if m, ok := s.SlicerMaterial(sp.CuraMaterial); ok {
		cura = m.Name
	} else if sp.CuraMaterial != "" {
		cura = sp.CuraMaterial
	}
	rows := [][2]string{
		{"Material", sp.Material},
		{"Color", colorName(sp.Color)},
		{"Cura material", cura},
		{"Filament left", printer.Weight(sp.Left())},
		{"Used since weighed", printer.Weight(sp.Used)},
		{"Empty reel", printer.Weight(sp.Reel)},
	}
	if sp.Price > 0 {
		rows = append(rows, [2]string{"Price", fmt.Sprintf("$%.0f · $%.2f/100 g", sp.Price, sp.Cost(100))})
	}
	rows = append(rows, [2]string{"Added", fileDate(sp.Added, a.Now(), a.Clock24)})
	prints := printer.OfSpool(a.Printer.History(), sp)
	if len(prints) > 0 {
		grams := 0.0
		for _, r := range prints {
			grams += a.recordGrams(r, sp)
		}
		rows = append(rows, [2]string{"Prints", fmt.Sprintf("%d · %s", len(prints), printer.Weight(grams))})
	}
	// The rows are 30 high, or less on a wide screen with the print bar, so that the table and
	// the meter fit over the actions.
	rowH := 30
	var box, bar, keys image.Rectangle
	if f.wide {
		content, acts := f.split()
		rowH = max(22, min(30, (content.Dy()-12-20-20)/len(rows)))
		r := kit.Rows(content, 12, kit.Px(len(rows)*rowH+20), kit.Px(20))
		box, bar, keys = r[0], r[1], acts
	} else {
		r := kit.Rows(kit.Inset(f.body(), 0, 12, 0, 8), 16, kit.Px(len(rows)*rowH+20), kit.Px(20), kit.Fr(1), kit.Px(72))
		box, bar, keys = r[0], r[1], r[3]
	}
	plate(c, box)
	a.region("Details", box)
	table := kit.Inset(box, 18, 10, 18, 10)
	id := sp.ID
	// The second row, Color, opens the palette. The chip of the color is left of its name.
	detailRows(c, table, rows[:1], rowH)
	{
		y := table.Min.Y + rowH
		r := image.Rect(box.Min.X+4, y, box.Max.X-4, y+rowH)
		a.detailLink(c, r, rows[1], func() { a.pickColor(sp.Color, func(hex string) { s.SetColor(id, hex) }) })
		if sp.Color != "" {
			x := r.Max.X - 40 - gfx.Width(gfx.Face(true, 17), rows[1][1]) - 10
			d := min(20, rowH-8)
			swatch(c, image.Rect(x-d, r.Min.Y+(rowH-d)/2, x, r.Min.Y+(rowH+d)/2), sp.Color)
		}
		table.Min.Y += 2 * rowH
		rows = rows[2:]
	}
	if len(prints) > 0 {
		// The last row, Prints, opens the list of the prints of the roll.
		detailRows(c, table, rows[:len(rows)-1], rowH)
		y := table.Min.Y + (len(rows)-1)*rowH
		a.detailLink(c, image.Rect(box.Min.X+4, y, box.Max.X-4, y+rowH), rows[len(rows)-1], func() {
			a.printsScroll = 0
			a.open(pageSpoolPrints)
		})
	} else {
		detailRows(c, table, rows, rowH)
	}
	if sp.Full > 0 {
		segments(c, bar, sp.Left()/sp.Full)
		a.region("Left", bar)
	}
	// Cura material, over the other keys, when the Cura plugin sent the materials.
	if len(a.curaMaterials()) > 0 {
		r := kit.Rows(image.Rect(keys.Min.X, keys.Min.Y-84, keys.Max.X, keys.Max.Y), 12, kit.Px(72), kit.Px(72))
		a.button(c, r[0], "Cura material", raised, text, func() {
			a.pickCuraMaterial(sp.Material, func(m printer.SlicerMaterial) { s.SetCuraMaterial(id, m) })
		})
	}
	// Remove, the destructive action, at the left.
	k := kit.Cols(keys, 12, kit.Px(144), kit.Fr(1))
	rollKey := a.button
	if in, ok := s.In(); ok && in.ID == id {
		rollKey = a.printerKey // the roll in the printer: a print takes its filament
	}
	rollKey(c, k[1], "Weigh again", raised, text, func() {
		a.editNumber("Roll on the scale", int(sp.Left()+sp.Reel+0.5), 9999, " g", func(v int) { s.Weigh(id, float64(v)) })
	})
	rollKey(c, k[0], "Remove", raised, red, func() {
		a.confirm = &confirmBox{question: "Remove " + gfx.Fit(gfx.Face(true, 28), sp.Label(), 300) + "?", yes: "Remove", danger: true,
			onYes: func() { s.Remove(id); a.back() }}
	})
}

// recordGrams is the grams of filament that a print used from the roll: the grams of the record,
// or for a record from before them, its filament at the density of the roll.
func (a *App) recordGrams(r printer.Record, sp printer.Spool) float64 {
	if r.Grams > 0 {
		return r.Grams
	}
	return a.grams(r.Filament, sp.Material)
}

// drawSpoolPrints is the page of the prints that used one roll, newest first. A tap opens the
// page of the print.
func (a *App) drawSpoolPrints(c *gfx.Canvas) {
	s := a.Printer.SpoolStore()
	var sp printer.Spool
	ok := false
	if s != nil {
		sp, ok = s.Get(a.spoolID)
	}
	if !ok {
		a.back()
		return
	}
	pf := a.navFrame()
	f := a.navPage(c, gfx.Fit(gfx.Face(true, 24), "Prints · "+sp.Label(), pf.head.Dx()-200))
	recs := printer.OfSpool(a.Printer.History(), sp)
	area := a.listArea(f)
	if len(recs) == 0 {
		noItems(c, area, "", "No prints used this roll")
		return
	}
	l := kit.List{Area: area, N: len(recs), RowH: 90, Gap: 10, Scroll: &a.printsScroll}
	a.list(c, l, func(i int) string { return recs[i].File }, func(c *gfx.Canvas, i int, box image.Rectangle) {
		r := recs[i]
		sum := r.Summary(func(float64) float64 { return a.recordGrams(r, sp) })
		if r.Cost > 0 {
			sum += fmt.Sprintf(" · $%.2f", r.Cost)
		}
		a.recordRow(c, box, r, sum)
	}, func(i int) {
		a.record = recs[i]
		a.open(pageRecord)
	})
}

// detailLink is a row of a table of details that opens a page: the label and the value, with an
// arrow at the right. r is the full width of the plate of the table.
func (a *App) detailLink(c *gfx.Canvas, r image.Rectangle, kv [2]string, tap func()) {
	if a.isPressed(r) {
		c.RoundRect(r, 4, raised)
	}
	t := kit.Inset(r, 14, 0, 40, 0)
	label(c, t, kv[0], gfx.Left)
	vf := gfx.Face(true, 17)
	v := image.Rect(t.Min.X+gfx.Width(gfx.Face(true, 14), strings.ToUpper(kv[0]))+16, t.Min.Y, t.Max.X, t.Max.Y)
	c.Text(v, kv[1], vf, text, gfx.Right)
	cx, cy := r.Max.X-24, r.Min.Y+r.Dy()/2
	c.Polygon([]image.Point{{cx - 4, cy - 8}, {cx + 4, cy}, {cx - 4, cy + 8}, {cx - 7, cy + 5}, {cx - 2, cy}, {cx - 7, cy - 5}}, dim)
	a.region(kv[0], r)
	a.addHit(kit.Hit{R: r, Tap: tap})
	a.addFocus(kv[0], r, tap)
}
