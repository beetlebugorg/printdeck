package ui

import (
	"image"
	"strings"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/kit"
)

// The color of a roll: a choice from the colors of filament, over everything until a choice or
// Back. The roll gauge of the print screen and the Cura plugin show it.

// colorName is the name of a color of the palette, "Other" for a color from Cura that is not in
// the palette, or "None".
func colorName(hex string) string {
	if hex == "" {
		return "None"
	}
	for _, rc := range printer.RollColors {
		if strings.EqualFold(rc.Hex, hex) {
			return rc.Name
		}
	}
	return "Other"
}

// colorPicker is the palette of the colors of a roll.
type colorPicker struct {
	current string // the color now, or ""
	done    func(hex string)
}

// pickColor opens the palette. done gets the choice; "" is no color.
func (a *App) pickColor(current string, done func(hex string)) {
	a.colors = &colorPicker{current: current, done: done}
}

func (a *App) drawColorPicker(c *gfx.Canvas) {
	p := a.colors
	f := a.pageBack(c, "Roll color", "Back", func() { a.colors = nil })
	// The colors in a grid, and No color under them. Wide: No color at the left of the row of
	// actions.
	var grid, none image.Rectangle
	if f.wide {
		content, acts := f.split()
		grid, none = content, kit.Cols(acts, 12, kit.Fr(1), kit.Fr(1))[0]
	} else {
		r := kit.Rows(kit.Inset(f.body(), 0, 10, 0, 8), 12, kit.Fr(1), kit.Px(72))
		grid, none = r[0], r[1]
	}
	cols := 4
	a.region("Colors", grid)
	for i, cell := range kit.Grid(grid, cols, (len(printer.RollColors)+cols-1)/cols, 10) {
		if i >= len(printer.RollColors) {
			break
		}
		rc := printer.RollColors[i]
		fill := keyOff
		if strings.EqualFold(rc.Hex, p.current) {
			fill = keyOn
		}
		a.button(c, cell, "", fill, text, func() {
			a.colors = nil
			p.done(rc.Hex)
		})
		// The chip, and the name under it.
		d := min(cell.Dy()/2, 40)
		top := cell.Min.Y + (cell.Dy()-d-26)/2
		chip := image.Rect(cell.Min.X+(cell.Dx()-d)/2, top, cell.Min.X+(cell.Dx()+d)/2, top+d)
		swatch(c, chip, rc.Hex)
		c.Text(image.Rect(cell.Min.X+4, chip.Max.Y+4, cell.Max.X-4, chip.Max.Y+26), strings.ToUpper(rc.Name), gfx.Face(true, 15), text, gfx.Center)
	}
	a.button(c, none, "No color", raised, text, func() {
		a.colors = nil
		p.done("")
	})
}
