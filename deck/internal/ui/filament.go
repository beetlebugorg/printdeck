package ui

import (
	"fmt"
	"image"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// drawFilament is the Filament page: the loaded material, the guided change, load and unload, a
// short extrude or retract, and the heating for the material.
func (a *App) drawFilament(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	heading(c, "Filament")
	a.backButton(c)
	m := a.loaded()

	win := image.Rect(24, 122, W-24, 176)
	window(c, win)
	loaded := "NO MATERIAL SET"
	if m.Name != "" {
		loaded = fmt.Sprintf("%s LOADED · %d° / %d°", m.Name, m.Tool, m.Bed)
	}
	c.Text(win, loaded, gfx.Face(true, 20), lcdInk, gfx.Center)
	label(c, image.Rect(24, 184, W-24, 206), "Tap the material that is in the printer", gfx.Left)
	q := (W - 48 - 18) / 4
	for i, mat := range materials {
		r := image.Rect(24+i*(q+6), 210, 24+i*(q+6)+q, 262)
		fill, fg := raised, text
		if mat.Name == m.Name {
			fill, fg = orange, onAcc
		}
		name := mat.Name
		a.button(c, r, name, fill, fg, func() { a.setMaterial(name) })
	}

	a.temps(c, 276, 80, st.Tool, st.ToolTarget, st.Bed, st.BedTarget, st.Connected)

	busy := st.Busy() && !st.Paused
	half := (W - 48 - 12) / 2
	if busy {
		key(c, image.Rect(24, 372, W-24, 452), "A print runs", surface, dim, false)
		return
	}
	a.button(c, image.Rect(24, 372, W-24, 452), "Change filament", orange, onAcc, func() { a.swap = &swapState{} })
	a.button(c, image.Rect(24, 464, 24+half, 534), "Load", raised, text, func() { a.swap = &swapState{mode: swapLoadOnly} })
	a.button(c, image.Rect(W-24-half, 464, W-24, 534), "Unload", raised, text, func() { a.swap = &swapState{mode: swapUnloadOnly} })

	cold := st.Tool < minExtrudeTemp
	ext := func(r image.Rectangle, name string, mm float64) {
		if cold {
			key(c, r, name, surface, dim, false)
			a.addHit(hit{r: r, tap: func() { a.Toast(fmt.Sprintf("Heat the nozzle to %d° first", minExtrudeTemp)) }})
			return
		}
		a.button(c, r, name, raised, text, func() { a.do(func() error { return pr.Extrude(mm, 300) }) })
	}
	ext(image.Rect(24, 546, 24+half, 606), "Extrude 10 mm", 10)
	ext(image.Rect(W-24-half, 546, W-24, 606), "Retract 10 mm", -10)

	heat := fmt.Sprintf("Heat to %d°", m.Tool)
	if m.Name != "" {
		heat = "Heat for " + m.Name
	}
	tool := m.Tool
	a.button(c, image.Rect(24, 618, 24+half, 678), heat, raised, text, func() { a.do(func() error { return pr.SetToolTemp(tool) }) })
	a.button(c, image.Rect(W-24-half, 618, W-24, 678), "Cool down", raised, text, func() { pr.Heat(0, 0) })
}
