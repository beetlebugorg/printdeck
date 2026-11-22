package ui

import (
	"image"
	"image/color"
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

// nearColorName is the name of the color of the palette nearest to hex, for a color that the
// printer reports: "#f72323" is "Red". It is "" for no color.
func nearColorName(hex string) string {
	col, ok := printer.ParseHex(hex)
	if !ok {
		return ""
	}
	name, best := "", -1
	for _, rc := range printer.RollColors {
		if d := colorDist(col, rc.Hex); best < 0 || d < best {
			name, best = rc.Name, d
		}
	}
	return name
}

// colorDist is the squared distance of col to the color hex, or a large number for no color.
func colorDist(col color.RGBA, hex string) int {
	o, ok := printer.ParseHex(hex)
	if !ok {
		return 1 << 30
	}
	dr, dg, db := int(col.R)-int(o.R), int(col.G)-int(o.G), int(col.B)-int(o.B)
	return dr*dr + dg*dg + db*db
}

// drawSwatch draws the color hex at the left of r, and returns the rest of r for the row. No
// color is an empty square.
func drawSwatch(c *gfx.Canvas, r image.Rectangle, hex string) image.Rectangle {
	const size, gap = 44, 10
	sw := kit.Center(image.Rect(r.Min.X, r.Min.Y, r.Min.X+size, r.Max.Y), size, size)
	c.RoundRect(sw, 6, dim)
	if col, ok := printer.ParseHex(hex); ok {
		c.RoundRect(sw.Inset(2), 5, col)
	} else {
		c.RoundRect(sw.Inset(2), 5, bg)
	}
	return image.Rect(r.Min.X+size+gap, r.Min.Y, r.Max.X, r.Max.Y)
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
