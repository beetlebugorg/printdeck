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

// The colours of the current palette. setNight switches between the day and the night palette.
var (
	bg, surface, raised, edge, shine color.RGBA // chassis, module plate, key cap, seams, top highlight
	text, dim                        color.RGBA // ink, secondary ink
	orange, green, red, onAcc        color.RGBA // safety orange (the one accent), red for stop, ink on orange
	lcd, lcdInk, lcdDim              color.RGBA // display window and its amber digits
	ledOff, ledGlow                  color.RGBA
	keyOn, keyOnInk                  color.RGBA // the cap of a key that is on, like the active mode key
)

type palette struct {
	bg, surface, raised, edge, shine, text, dim, orange, red, onAcc, lcd, lcdInk, lcdDim, ledOff, ledGlow, keyOn, keyOnInk color.RGBA
}

func rgb(v uint32) color.RGBA { return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255} }

var (
	// Day: a light grey chassis, like a Braun or Teenage Engineering device.
	dayPalette = palette{
		bg: rgb(0xd3d0c8), surface: rgb(0xe6e3dc), raised: rgb(0xf3f1ec), edge: rgb(0xa9a59c), shine: rgb(0xfbfaf7),
		text: rgb(0x171717), dim: rgb(0x6e6a63), orange: rgb(0xff5a1f), red: rgb(0xd12b1d), onAcc: rgb(0x121212),
		lcd: rgb(0x1b1c1a), lcdInk: rgb(0xff7a30), lcdDim: rgb(0x7a4d30), ledOff: rgb(0x8f8a80), ledGlow: rgb(0xffc8a8),
		keyOn: rgb(0x171717), keyOnInk: rgb(0xf3f1ec),
	}
	// Night: an anthracite chassis. Less light from the panel, the same orange, softer ink.
	nightPalette = palette{
		bg: rgb(0x121211), surface: rgb(0x1c1c1a), raised: rgb(0x292825), edge: rgb(0x060606), shine: rgb(0x34332f),
		text: rgb(0xcfcac0), dim: rgb(0x77726a), orange: rgb(0xe8541c), red: rgb(0xb83020), onAcc: rgb(0x0e0e0d),
		lcd: rgb(0x080808), lcdInk: rgb(0xe8692a), lcdDim: rgb(0x5e3820), ledOff: rgb(0x3a3834), ledGlow: rgb(0x4a2412),
		keyOn: rgb(0x080808), keyOnInk: rgb(0xe8692a),
	}
	night bool // the night palette is in use
)

func init() { setNight(false) }

// setNight switches the palette. The next frame is drawn in it.
func setNight(on bool) {
	p := dayPalette
	if on {
		p = nightPalette
	}
	night = on
	bg, surface, raised, edge, shine = p.bg, p.surface, p.raised, p.edge, p.shine
	text, dim = p.text, p.dim
	orange, green, red, onAcc = p.orange, p.orange, p.red, p.onAcc
	lcd, lcdInk, lcdDim = p.lcd, p.lcdInk, p.lcdDim
	ledOff, ledGlow = p.ledOff, p.ledGlow
	keyOn, keyOnInk = p.keyOn, p.keyOnInk
}

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
		c.RoundRect(image.Rect(x-7, y-7, x+7, y+7), 7, ledGlow)
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
