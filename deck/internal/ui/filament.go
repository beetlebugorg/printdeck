package ui

import (
	"fmt"
	"image"
	"printdeck/kit"
	"strings"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// drawFilament is the Filament page: the loaded material, the guided change, load and unload, a
// short extrude or retract, and the heating for the material.
func (a *App) drawFilament(c *gfx.Canvas, st printer.State) {
	if st.Busy() && !st.Paused {
		a.drawFilamentPrinting(c, st)
		return
	}
	pr := a.Printer
	f := a.navPage(c, "Filament")
	rolls := pr.SpoolStore()
	if rolls != nil {
		a.headKey(c, f.head, "Rolls", func() { a.open(pageSpools) })
	}
	m := a.loaded()
	// Tall: the material, the temperatures and the actions under each other. Wide: the material and
	// the temperatures in the left column, and the actions in rows that fill the right column.
	var win, hint, mats, temps, change, load, ext, heat image.Rectangle
	if f.wide {
		// The display at the left shows the temperatures. Change filament is the main action.
		content, acts := f.split()
		r := kit.Rows(content, 10, kit.Px(44), kit.Px(52), kit.Fr(1), kit.Fr(1), kit.Fr(1))
		win, mats, load, ext, heat = r[0], r[1], r[2], r[3], r[4]
		change = kit.Cols(acts, 12, kit.Fr(1), kit.Fr(1))[1]
	} else {
		r := kit.Rows(kit.Inset(f.body(), 0, 10, 0, 8), 8, kit.Px(54), kit.Px(22), kit.Px(52), kit.Px(80), kit.Fr(1.4), kit.Fr(1), kit.Fr(1), kit.Fr(1))
		win, hint, mats, temps, change, load, ext, heat = r[0], r[1], kit.Inset(r[2], 0, -4, 0, 4), kit.Inset(r[3], 16, 6, 16, -6), r[4], r[5], r[6], r[7]
	}
	window(c, win)
	a.region("Loaded", win)
	a.region("Material", mats)
	loaded := "NO MATERIAL SET"
	if m.Name != "" {
		loaded = fmt.Sprintf("%s LOADED · %d° / %d°", m.Name, m.Tool, m.Bed)
	}
	if rolls != nil {
		if sp, ok := rolls.In(); ok {
			loaded = strings.ToUpper(gfx.Fit(gfx.Face(true, 20), sp.Label(), win.Dx()-200) + " · " + printer.Weight(sp.Left()) + " left")
		}
	}
	c.Text(win, loaded, gfx.Face(true, 20), lcdInk, gfx.Center)
	if !hint.Empty() {
		label(c, hint, "Tap the material that is in the printer", gfx.Left)
	}
	// During a print, the page keeps its keys in their places, and they wait for its end.
	fkey := a.button
	if st.Busy() && !st.Paused {
		fkey = a.printerKey
	}
	for i, r := range kit.Cols(mats, 6, kit.Fr(1), kit.Fr(1), kit.Fr(1), kit.Fr(1)) {
		mat := materials[i]
		fill, fg := keyOff, text
		if mat.Name == m.Name {
			fill, fg = keyOn, keyOnInk
		}
		name := mat.Name
		if st.Busy() && !st.Paused {
			a.lockedKey(c, r, name, fill) // the LED of the material in the printer stays lit
			continue
		}
		// A change of the loaded material asks first: a tap by mistake also takes the roll out.
		fkey(c, r, name, fill, fg, func() {
			if name == m.Name {
				return
			}
			a.confirm = &confirmBox{question: fmt.Sprintf("Is %s in the printer?", name), yes: "Set " + name, onYes: func() {
				a.setMaterial(name)
				// A roll of another material is not in the printer.
				if rolls != nil {
					if sp, ok := rolls.In(); ok && sp.Material != name {
						rolls.Put("")
					}
				}
			}}
		})
	}

	if !temps.Empty() {
		a.temps(c, temps, st.Tool, st.ToolTarget, st.Bed, st.BedTarget, st.Connected)
	}

	fkey(c, change, "Change filament", orange, onAcc, func() { a.openSwap(swapChange) })
	lu := kit.Cols(load, 12, kit.Fr(1), kit.Fr(1))
	fkey(c, lu[0], "Load", raised, text, func() { a.openSwap(swapLoadOnly) })
	fkey(c, lu[1], "Unload", raised, text, func() { a.openSwap(swapUnloadOnly) })

	cold := st.Tool < minExtrudeTemp
	extrude := func(r image.Rectangle, name string, mm float64) {
		if cold && !(st.Busy() && !st.Paused) {
			key(c, r, name, surface, dim, false)
			a.region(name, r)
			a.addHit(kit.Hit{R: r, Tap: func() { a.Toast(fmt.Sprintf("Heat the nozzle to %d° first", minExtrudeTemp)) }})
			return
		}
		fkey(c, r, name, raised, text, func() { a.do(func() error { return pr.Extrude(mm, 300) }) })
	}
	er := kit.Cols(ext, 12, kit.Fr(1), kit.Fr(1))
	extrude(er[0], "Extrude 10 mm", 10)
	extrude(er[1], "Retract 10 mm", -10)

	name := fmt.Sprintf("Heat to %d°", m.Tool)
	if m.Name != "" {
		name = "Heat for " + m.Name
	}
	tool := m.Tool
	hc := kit.Cols(heat, 12, kit.Fr(1), kit.Fr(1))
	// Heat is on while the nozzle holds the temperature of the material: a tap then turns it off.
	if st.ToolTarget == float64(tool) && tool > 0 {
		fkey(c, hc[0], name, keyOn, keyOnInk, func() { a.do(func() error { return pr.SetToolTemp(0) }) })
	} else {
		fkey(c, hc[0], name, keyOff, text, func() { a.do(func() error { return pr.SetToolTemp(tool) }) })
	}
	fkey(c, hc[1], "Cool down", raised, text, func() { pr.Heat(0, 0) })
}

// drawFilamentPrinting is the Filament page during a print: the roll in the printer, and if it
// lasts for the rest of the print. Its keys that heat and move the filament wait for the end of the
// print, so the page has none of them; Change filament pauses the print first.
func (a *App) drawFilamentPrinting(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	f := a.navPage(c, "Filament")
	rolls := pr.SpoolStore()
	if rolls != nil {
		a.headKey(c, f.head, "Rolls", func() { a.open(pageSpools) })
	}
	var card, change image.Rectangle
	if f.wide {
		content, acts := f.split()
		card, change = kit.Rows(content, 0, kit.Px(min(content.Dy(), 236)))[0], kit.Cols(acts, 12, kit.Fr(1), kit.Fr(1))[1]
	} else {
		r := kit.Rows(kit.Inset(f.body(), 0, 10, 0, 8), 12, kit.Px(236), kit.Fr(1), kit.Px(84))
		card, change = r[0], r[2]
	}
	plate(c, card)
	a.region("Roll", card)
	in := kit.Inset(card, 20, 16, 20, 16)

	// The filament that the rest of the print uses, from what the slicer wrote and what printed.
	meta := pr.FileMeta(st.FilePath)
	rest := -1.0
	switch {
	case meta.FilamentMM > 0:
		rest = a.grams(max(0, meta.FilamentMM-st.FilamentUsed), "")
	case meta.FilamentG > 0:
		rest = max(0, meta.FilamentG-a.grams(st.FilamentUsed, ""))
	}

	sp, ok := printer.Spool{}, false
	if rolls != nil {
		sp, ok = rolls.In()
	}
	if !ok {
		// No roll: the material, and the filament of the rest of the print.
		m := a.loaded()
		name := "No roll in the printer"
		if m.Name != "" {
			name = m.Name + " · no roll"
		}
		c.Text(image.Rect(in.Min.X, in.Min.Y, in.Max.X, in.Min.Y+36), name, gfx.Face(true, 24), text, gfx.Left)
		if rest >= 0 {
			c.Text(image.Rect(in.Min.X, in.Min.Y+44, in.Max.X, in.Min.Y+70), "The rest of the print uses "+printer.Weight(rest)+".", gfx.Face(false, 17), dim, gfx.Left)
		}
		label(c, image.Rect(in.Min.X, in.Max.Y-24, in.Max.X, in.Max.Y), "Add the roll in Rolls to follow its filament", gfx.Left)
	} else {
		left := max(0, sp.Left()-a.grams(st.FilamentUsed, ""))
		full := sp.Capacity()
		// The dot in the color of the roll, its number and name, and its material.
		d := 64
		dot := image.Rect(in.Min.X, in.Min.Y, in.Min.X+d, in.Min.Y+d)
		c.RoundRect(dot, d/2, edge)
		if col, ok := parseHex(sp.Color); ok {
			c.RoundRect(dot.Inset(3), d/2-3, col)
		} else {
			c.RoundRect(dot.Inset(3), d/2-3, lcd)
		}
		tx := dot.Max.X + 18
		c.Text(image.Rect(tx, in.Min.Y+2, in.Max.X, in.Min.Y+34), sp.Number()+" "+sp.Label(), gfx.Face(true, 24), text, gfx.Left)
		c.Text(image.Rect(tx, in.Min.Y+36, in.Max.X, in.Min.Y+62), sp.Material+" · "+printer.Weight(left)+" left", gfx.Face(false, 17), dim, gfx.Left)
		segments(c, image.Rect(in.Min.X, in.Min.Y+d+16, in.Max.X, in.Min.Y+d+36), min(1, left/full))
		// If the roll lasts: the filament of the rest of the print, and what is left after it.
		y := in.Min.Y + d + 50
		switch {
		case rest < 0:
			c.Text(image.Rect(in.Min.X, y, in.Max.X, y+26), "The file does not say how much filament it uses.", gfx.Face(false, 17), dim, gfx.Left)
		case rest > left:
			c.Text(image.Rect(in.Min.X, y, in.Max.X, y+28), fmt.Sprintf("Too little: the rest of the print uses %s.", printer.Weight(rest)), gfx.Face(true, 18), red, gfx.Left)
			c.Text(image.Rect(in.Min.X, y+30, in.Max.X, y+56), "Change the roll before it runs out: Change filament pauses the print.", gfx.Face(false, 16), dim, gfx.Left)
		default:
			c.Text(image.Rect(in.Min.X, y, in.Max.X, y+28), fmt.Sprintf("The rest of the print uses %s.", printer.Weight(rest)), gfx.Face(true, 18), text, gfx.Left)
			c.Text(image.Rect(in.Min.X, y+30, in.Max.X, y+56), printer.Weight(left-rest)+" left after the print.", gfx.Face(false, 16), dim, gfx.Left)
		}
		a.region("Lasts", image.Rect(in.Min.X, y, in.Max.X, y+56))
	}

	// Change filament in the print: a pause first, and then the filament swap, which goes back to
	// the temperature of the print at its end (swapAfterPause).
	a.button(c, change, "Change filament", raised, text, func() {
		a.confirm = &confirmBox{question: "Pause the print, and change the filament?", yes: "Pause", onYes: func() {
			a.swapAfterPause = true
			pr.Pause()
		}}
	})
}
