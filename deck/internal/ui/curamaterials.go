package ui

import (
	"image"
	"image/color"
	"strconv"
	"strings"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/kit"
)

// The materials of Cura for the rolls. The Cura plugin sends the materials that fit the printer
// (printer.SlicerMaterial). A roll can have one: Cura then selects that material when the roll is
// in the printer.

// curaPicker is the list of the materials of Cura, over everything until a choice or Back.
type curaPicker struct {
	material string // the type of the roll: its materials come first
	done     func(m printer.SlicerMaterial)
	scroll   int
}

// pickCuraMaterial opens the list of the materials of Cura. done gets the choice; an empty
// material is "No Cura material".
func (a *App) pickCuraMaterial(material string, done func(m printer.SlicerMaterial)) {
	a.picker = &curaPicker{material: material, done: done}
}

// curaMaterials are the materials of Cura that the deck knows, or nil.
func (a *App) curaMaterials() []printer.SlicerMaterial {
	if s := a.Printer.SpoolStore(); s != nil {
		return s.SlicerMaterials()
	}
	return nil
}

// sortedFor puts the materials of the type first, in their order, then the others.
func sortedFor(list []printer.SlicerMaterial, material string) []printer.SlicerMaterial {
	var same, other []printer.SlicerMaterial
	for _, m := range list {
		if strings.EqualFold(m.Material, material) {
			same = append(same, m)
		} else {
			other = append(other, m)
		}
	}
	return append(same, other...)
}

func (a *App) drawCuraPicker(c *gfx.Canvas) {
	p := a.picker
	f := a.pageBack(c, "Cura material", "Back", func() { a.picker = nil })
	list := append([]printer.SlicerMaterial{{}}, sortedFor(a.curaMaterials(), p.material)...)
	area := a.listArea(f)
	l := kit.List{Area: area, N: len(list), RowH: 86, Gap: 10, Scroll: &p.scroll}
	name := func(i int) string {
		if list[i].ID == "" {
			return "No Cura material"
		}
		return list[i].Name
	}
	a.list(c, l, name, func(c *gfx.Canvas, i int, r image.Rectangle) {
		m := list[i]
		if m.ID == "" {
			a.menuRow(c, r, "No Cura material", "Cura keeps its own choice", dim, nil)
			return
		}
		a.menuRow(c, r, m.Name, strings.TrimSpace(m.Brand+" · "+m.Material), dim, nil)
		swatch(c, image.Rect(r.Max.X-60, r.Min.Y+r.Dy()/2-14, r.Max.X-32, r.Min.Y+r.Dy()/2+14), m.Color)
	}, func(i int) {
		a.picker = nil
		p.done(list[i])
	})
}

// swatch draws the color of a material, "#rrggbb", as a round chip with a dark edge. An unknown
// color draws nothing.
func swatch(c *gfx.Canvas, r image.Rectangle, hex string) {
	col, ok := parseHex(hex)
	if !ok {
		return
	}
	c.RoundRect(r, r.Dx()/2, lcd)
	c.RoundRect(r.Inset(2), r.Dx()/2-2, col)
}

// parseHex reads a color "#rrggbb".
func parseHex(s string) (color.RGBA, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return color.RGBA{}, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.RGBA{}, false
	}
	return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}, true
}
