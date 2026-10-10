package ui

import (
	"image"
	"image/color"
	"strings"

	"speakerdeck/internal/gfx"
)

// The look of a piece of equipment: a light grey chassis with module plates, physical keys that travel when
// pressed, dark LCD windows with amber digits, faders, and LEDs. Monospace type, and safety orange as the one
// accent. Red is only for what stops or deletes.

var (
	bg      = color.RGBA{0xd3, 0xd0, 0xc8, 255} // chassis
	surface = color.RGBA{0xe6, 0xe3, 0xdc, 255} // module plate
	raised  = color.RGBA{0xf3, 0xf1, 0xec, 255} // light key cap
	edge    = color.RGBA{0xa9, 0xa5, 0x9c, 255} // seams and plate edges
	shine   = color.RGBA{0xfb, 0xfa, 0xf7, 255} // top highlight of plates and caps
	text    = color.RGBA{0x17, 0x17, 0x17, 255} // ink
	dim     = color.RGBA{0x6e, 0x6a, 0x63, 255} // secondary ink
	orange  = color.RGBA{0xff, 0x5a, 0x1f, 255} // safety orange
	green   = orange                            // the one accent: active, playing, selected
	red     = color.RGBA{0xd1, 0x2b, 0x1d, 255}
	onAcc   = color.RGBA{0x12, 0x12, 0x12, 255} // ink on an orange cap
	lcd     = color.RGBA{0x1b, 0x1c, 0x1a, 255} // display window
	lcdInk  = color.RGBA{0xff, 0x7a, 0x30, 255} // amber digits
	lcdDim  = color.RGBA{0x7a, 0x4d, 0x30, 255}
	ledOff  = color.RGBA{0x8f, 0x8a, 0x80, 255}
)

func darken(c color.RGBA, f float64) color.RGBA {
	m := func(v uint8) uint8 { return uint8(float64(v) * (1 - f)) }
	return color.RGBA{m(c.R), m(c.G), m(c.B), 255}
}

// plate is a module plate: an edge, a face, and a light line along the top.
func plate(c *gfx.Canvas, r image.Rectangle) {
	c.RoundRect(r, 5, edge)
	c.RoundRect(r.Inset(1), 4, surface)
	c.Fill(image.Rect(r.Min.X+5, r.Min.Y+1, r.Max.X-5, r.Min.Y+2), shine)
}

// panel is a larger plate with screw heads in the corners and an optional label, like "01 TRANSPORT".
func panel(c *gfx.Canvas, r image.Rectangle, label string) {
	plate(c, r)
	for _, p := range []image.Point{{r.Min.X + 9, r.Min.Y + 9}, {r.Max.X - 9, r.Min.Y + 9}, {r.Min.X + 9, r.Max.Y - 9}, {r.Max.X - 9, r.Max.Y - 9}} {
		screw(c, p.X, p.Y)
	}
	if label != "" {
		c.Text(image.Rect(r.Min.X+22, r.Min.Y+2, r.Max.X-22, r.Min.Y+18), label, gfx.Face(true, 11), dim, gfx.Left)
	}
}

// screw draws a small slotted screw head.
func screw(c *gfx.Canvas, x, y int) {
	c.RoundRect(image.Rect(x-4, y-4, x+4, y+4), 4, edge)
	c.RoundRect(image.Rect(x-3, y-3, x+3, y+3), 3, raised)
	c.Fill(image.Rect(x-3, y, x+3, y+1), edge)
}

// window is a dark display window, sunk into the plate.
func window(c *gfx.Canvas, r image.Rectangle) {
	c.RoundRect(r.Inset(-2), 6, edge)
	c.RoundRect(r, 4, lcd)
	c.Fill(image.Rect(r.Min.X+4, r.Min.Y, r.Max.X-4, r.Min.Y+2), color.RGBA{0x08, 0x08, 0x08, 255}) // inner shadow
}

// led is an indicator light.
func led(c *gfx.Canvas, x, y int, on bool) {
	if on {
		c.RoundRect(image.Rect(x-7, y-7, x+7, y+7), 7, color.RGBA{0xff, 0xc8, 0xa8, 255}) // glow
		c.RoundRect(image.Rect(x-4, y-4, x+4, y+4), 4, orange)
		return
	}
	c.RoundRect(image.Rect(x-4, y-4, x+4, y+4), 4, ledOff)
}

// key draws a physical key: a dark wall below a cap that moves down when pressed.
func key(c *gfx.Canvas, r image.Rectangle, label string, cap, ink color.RGBA, pressed bool) {
	const depth = 5
	wall := darken(cap, 0.35)
	if cap == raised {
		wall = edge
	}
	c.RoundRect(r, 6, wall)
	face := image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Max.Y-depth)
	if pressed {
		face = face.Add(image.Pt(0, depth-1))
	}
	c.RoundRect(face, 6, cap)
	c.Fill(image.Rect(face.Min.X+6, face.Min.Y+1, face.Max.X-6, face.Min.Y+2), lighten(lighten(cap)))
	size := 20
	if face.Dy() < 44 {
		size = 16
	}
	c.Text(face, strings.ToUpper(label), gfx.Face(true, size), ink, gfx.Center)
}

// fader draws a horizontal slider: a slot, the part up to the value in orange, and a grip cap.
func fader(c *gfx.Canvas, r image.Rectangle, f float64, accent color.RGBA) {
	cy := r.Min.Y + r.Dy()/2
	slot := image.Rect(r.Min.X, cy-4, r.Max.X, cy+4)
	c.RoundRect(slot, 4, lcd)
	x := slot.Min.X + int(f*float64(slot.Dx()))
	if x > slot.Min.X+4 {
		c.RoundRect(image.Rect(slot.Min.X+2, cy-2, x, cy+2), 2, accent)
	}
	faderCap(c, x, cy, false)
}

// faderCap is the grip of a fader: a block with ridges and an orange index line.
func faderCap(c *gfx.Canvas, x, y int, vertical bool) {
	w, h := 14, 22 // half sizes
	if vertical {
		w, h = 22, 14
	}
	c.RoundRect(image.Rect(x-w, y-h+3, x+w, y+h+3), 4, edge) // shadow
	c.RoundRect(image.Rect(x-w, y-h, x+w, y+h), 4, text)
	for i := -2; i <= 2; i++ {
		if i == 0 {
			continue
		}
		if vertical {
			c.Fill(image.Rect(x-w+4, y+i*5, x+w-4, y+i*5+1), dim)
		} else {
			c.Fill(image.Rect(x+i*5, y-h+4, x+i*5+1, y+h-4), dim)
		}
	}
	if vertical {
		c.Fill(image.Rect(x-w+2, y-1, x+w-2, y+1), orange)
	} else {
		c.Fill(image.Rect(x-1, y-h+2, x+1, y+h-2), orange)
	}
}

// label draws a small uppercase label, as printed on a panel.
func label(c *gfx.Canvas, r image.Rectangle, s string, a gfx.Align) {
	c.Text(r, strings.ToUpper(s), gfx.Face(true, 12), dim, a)
}

// segments is a meter of lit segments in a dark window, for progress.
func segments(c *gfx.Canvas, r image.Rectangle, f float64) {
	window(c, r)
	const w, gap = 6, 3
	n := (r.Dx() - 8 + gap) / (w + gap)
	lit := int(f*float64(n) + 0.5)
	for i := 0; i < n; i++ {
		x := r.Min.X + 4 + i*(w+gap)
		col := color.RGBA{0x3a, 0x2a, 0x20, 255}
		if i < lit {
			col = lcdInk
		}
		c.Fill(image.Rect(x, r.Min.Y+3, x+w, r.Max.Y-3), col)
	}
}

// playhead is a thin slot with the played part in orange and a cursor, for the position in a track.
func playhead(c *gfx.Canvas, r image.Rectangle, f float64) {
	cy := r.Min.Y + r.Dy()/2
	slot := image.Rect(r.Min.X, cy-3, r.Max.X, cy+3)
	c.RoundRect(slot, 3, lcd)
	x := slot.Min.X + int(max(0, min(1, f))*float64(slot.Dx()))
	if x > slot.Min.X+2 {
		c.Fill(image.Rect(slot.Min.X+1, cy-1, x, cy+1), orange)
	}
	for i := 0; i <= 10; i++ { // tick marks every tenth
		tx := slot.Min.X + i*slot.Dx()/10
		c.Fill(image.Rect(tx, slot.Max.Y+3, tx+1, slot.Max.Y+7), edge)
	}
	c.Fill(image.Rect(x-2, cy-11, x+2, cy+11), orange)
	c.Fill(image.Rect(x-6, cy-13, x+6, cy-9), orange)
}

// heading is the title of a screen: an orange mark and uppercase type.
func heading(c *gfx.Canvas, title string) {
	c.Fill(image.Rect(24, 76, 34, 96), orange)
	c.Text(image.Rect(44, 64, W-150, 108), strings.ToUpper(title), gfx.Face(true, 24), text, gfx.Left)
}
