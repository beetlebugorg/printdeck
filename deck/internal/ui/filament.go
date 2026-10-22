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
	pr := a.Printer
	f := a.page(c, "Filament")
	rolls := pr.SpoolStore()
	if rolls != nil {
		a.headKey(c, f.head, "Rolls", func() { a.open(pageSpools) })
	}
	m := a.loaded()
	// Tall: the material, the temperatures and the actions under each other. Wide: the material and
	// the temperatures in the left column, and the actions in rows that fill the right column.
	var win, hint, mats, temps, change, load, ext, heat image.Rectangle
	if f.wide {
		l := kit.Rows(f.aside(), 6, kit.Px(44), kit.Px(52), kit.Fr(1))
		win, mats, temps = l[0], l[1], l[2]
		r := kit.Rows(f.body(), 12, kit.Fr(1.4), kit.Fr(1), kit.Fr(1), kit.Fr(1))
		change, load, ext, heat = r[0], r[1], r[2], r[3]
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
	for i, r := range kit.Cols(mats, 6, kit.Fr(1), kit.Fr(1), kit.Fr(1), kit.Fr(1)) {
		mat := materials[i]
		fill, fg := raised, text
		if mat.Name == m.Name {
			fill, fg = orange, onAcc
		}
		name := mat.Name
		a.button(c, r, name, fill, fg, func() {
			a.setMaterial(name)
			// A roll of another material is not in the printer.
			if rolls != nil {
				if sp, ok := rolls.In(); ok && sp.Material != name {
					rolls.Put("")
				}
			}
		})
	}

	a.temps(c, temps, st.Tool, st.ToolTarget, st.Bed, st.BedTarget, st.Connected)

	if st.Busy() && !st.Paused {
		key(c, change, "A print runs", surface, dim, false)
		return
	}
	a.button(c, change, "Change filament", orange, onAcc, func() { a.openSwap(swapChange) })
	lu := kit.Cols(load, 12, kit.Fr(1), kit.Fr(1))
	a.button(c, lu[0], "Load", raised, text, func() { a.openSwap(swapLoadOnly) })
	a.button(c, lu[1], "Unload", raised, text, func() { a.openSwap(swapUnloadOnly) })

	cold := st.Tool < minExtrudeTemp
	extrude := func(r image.Rectangle, name string, mm float64) {
		if cold {
			key(c, r, name, surface, dim, false)
			a.region(name, r)
			a.addHit(kit.Hit{R: r, Tap: func() { a.Toast(fmt.Sprintf("Heat the nozzle to %d° first", minExtrudeTemp)) }})
			return
		}
		a.button(c, r, name, raised, text, func() { a.do(func() error { return pr.Extrude(mm, 300) }) })
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
	a.button(c, hc[0], name, raised, text, func() { a.do(func() error { return pr.SetToolTemp(tool) }) })
	a.button(c, hc[1], "Cool down", raised, text, func() { pr.Heat(0, 0) })
}
