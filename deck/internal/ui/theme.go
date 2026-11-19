package ui

import (
	"image"
	"image/color"
	"strings"

	"golang.org/x/image/font"

	"printdeck/internal/gfx"
	"printdeck/kit"
)

// The look of a piece of equipment: a light grey chassis with module plates, physical keys that travel when
// pressed, dark LCD windows with amber digits, faders, and LEDs. Monospace type, and safety orange as the one
// accent. Red is only for what stops or deletes.

// The colours of the current palette. setNight switches between the day and the night palette.
var (
	bg, surface, raised, edge, shine color.RGBA // chassis, module plate, key cap, seams, top highlight
	text, dim                        color.RGBA // ink, secondary ink
	orange, green, red, onAcc        color.RGBA // safety orange (the one accent), red text, ink on orange
	// accentText is orange as text on the chassis: the safety orange has too little contrast on
	// the light chassis (2.0 to 2.4:1), so the day palette has a darker orange (about 4.9:1).
	accentText                   color.RGBA
	redCap, onRed                color.RGBA // the cap of a key that stops, and its lettering
	lcd, lcdInk, lcdDim, lcdCell color.RGBA // display window, its amber digits, and an unlit character cell
	ledOff, ledGlow              color.RGBA
	keyOn, keyOnInk              color.RGBA // the cap of a key that is on, like the active mode key
	keyOff                       color.RGBA // the cap of a key with an on/off state that is off
	// The character display, in the colors of the graphic LCD of a LulzBot: a blue backlight with
	// white pixels.
	glcd, glcdCell, glcdInk color.RGBA
)

type palette struct {
	bg, surface, raised, edge, shine, text, dim, orange, red, onAcc, lcd, lcdInk, lcdDim, ledOff, ledGlow, keyOn, keyOnInk color.RGBA
	accentText                                                                                                             color.RGBA
	redCap, onRed, lcdCell                                                                                                 color.RGBA
	glcd, glcdCell, glcdInk                                                                                                color.RGBA
}

func rgb(v uint32) color.RGBA { return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255} }

var (
	// Day: a light grey chassis, like a Braun or Teenage Engineering device.
	dayPalette = palette{
		bg: rgb(0xd3d0c8), surface: rgb(0xe6e3dc), raised: rgb(0xf3f1ec), edge: rgb(0xa9a59c), shine: rgb(0xfbfaf7),
		text: rgb(0x171717), dim: rgb(0x35332f), orange: rgb(0xff5a1f), red: rgb(0xd12b1d), onAcc: rgb(0x121212), accentText: rgb(0xb83c0c),
		lcd: rgb(0x1b1c1a), lcdInk: rgb(0xff7a30), lcdDim: rgb(0xe0965f), ledOff: rgb(0x8f8a80), ledGlow: rgb(0xffc8a8),
		keyOn: rgb(0x171717), keyOnInk: rgb(0xf3f1ec), redCap: rgb(0xc0291b), onRed: rgb(0xfbfaf7), lcdCell: rgb(0x2a211a),
		glcd: rgb(0x1f4bd1), glcdCell: rgb(0x2a58e0), glcdInk: rgb(0xf2f6ff),
	}
	// Night: an anthracite chassis. The panel has a narrow viewing angle, so the text keeps at least 7:1 contrast
	// (contrast_test.go); mid-grey on dark grey washes out when the panel is seen from the side.
	nightPalette = palette{
		bg: rgb(0x121211), surface: rgb(0x1c1c1a), raised: rgb(0x292825), edge: rgb(0x060606), shine: rgb(0x34332f),
		text: rgb(0xeee9df), dim: rgb(0xd6d0c4), orange: rgb(0xff7a3a), red: rgb(0xff7a6a), onAcc: rgb(0x0e0e0d), accentText: rgb(0xff7a3a),
		lcd: rgb(0x080808), lcdInk: rgb(0xff8c4a), lcdDim: rgb(0xe8a070), ledOff: rgb(0x3a3834), ledGlow: rgb(0x4a2412),
		keyOn: rgb(0x080808), keyOnInk: rgb(0xff8c4a), redCap: rgb(0x9c2216), onRed: rgb(0xf3efe8), lcdCell: rgb(0x1a120c),
		glcd: rgb(0x0f2766), glcdCell: rgb(0x143079), glcdInk: rgb(0xd6e2ff),
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
	accentText = p.accentText
	lcd, lcdInk, lcdDim, lcdCell = p.lcd, p.lcdInk, p.lcdDim, p.lcdCell
	ledOff, ledGlow = p.ledOff, p.ledGlow
	keyOn, keyOnInk = p.keyOn, p.keyOnInk
	keyOff = color.RGBA{p.raised.R, p.raised.G, p.raised.B, 254} // a mark, not a color: key draws raised
	redCap, onRed = p.redCap, p.onRed
	glcd, glcdCell, glcdInk = p.glcd, p.glcdCell, p.glcdInk
}

// mix is the color between a (f 0) and b (f 1).
func mix(a, b color.RGBA, f float64) color.RGBA {
	f = max(0, min(1, f))
	m := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*f + 0.5) }
	return color.RGBA{m(a.R, b.R), m(a.G, b.G), m(a.B, b.B), 255}
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
		c.Text(image.Rect(r.Min.X+22, r.Min.Y+2, r.Max.X-22, r.Min.Y+panelLabelH()), label, gfx.Face(true, 13), dim, gfx.Left)
	}
}

// panelLabelH is the height of the label at the top of a panel. The large text draws the label
// larger, so it needs more height.
func panelLabelH() int {
	if gfx.FinePrint() {
		return 28
	}
	return 18
}

// screw draws a small slotted screw head.
func screw(c *gfx.Canvas, x, y int) {
	c.RoundRect(image.Rect(x-4, y-4, x+4, y+4), 4, edge)
	c.RoundRect(image.Rect(x-3, y-3, x+3, y+3), 3, raised)
	c.Fill(image.Rect(x-3, y, x+3, y+1), edge)
}

// window is a dark display window, sunk into the plate.
func window(c *gfx.Canvas, r image.Rectangle) {
	// The edge is inside r, as the edge of a plate or a key, so a window lines up with them.
	c.RoundRect(r, 6, edge)
	in := r.Inset(2)
	c.RoundRect(in, 4, lcd)
	c.Fill(image.Rect(in.Min.X+4, in.Min.Y, in.Max.X-4, in.Min.Y+2), color.RGBA{0x08, 0x08, 0x08, 255}) // inner shadow
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

// key draws a key of the panel: a membrane key with a printed frame, as on the panel of a machine.
// A key with an on/off state that stays in view has an LED at its top left: lit for cap keyOn (the
// open page, the material in the printer), dark for cap keyOff. A key that only acts has no LED.
// A pressed key gets darker.
func key(c *gfx.Canvas, r image.Rectangle, label string, cap, ink color.RGBA, pressed bool) {
	keyAt(c, r, label, cap, ink, pressed, 0, 0)
}

// keyBeside is key with room at the right of the label: right pixels of the cap stay free, for a
// sign such as the lock of printerKey.
func keyBeside(c *gfx.Canvas, r image.Rectangle, label string, cap, ink color.RGBA, pressed bool, right int) {
	keyAt(c, r, label, cap, ink, pressed, right, 0)
}

// keyAt is keyBeside with a label of at most px pixels (0 for no limit). The keys of one row use
// the same px from rowLabelPx, so that their labels have one size.
func keyAt(c *gfx.Canvas, r image.Rectangle, label string, cap, ink color.RGBA, pressed bool, right, px int) {
	on, hasLED := cap == keyOn, cap == keyOn || cap == keyOff
	if hasLED {
		cap = raised
		if ink == keyOnInk {
			ink = text
		}
	}
	face := cap
	if pressed {
		face = darken(cap, 0.12)
	}
	border := darken(cap, 0.35)
	if cap == raised {
		border = edge
	}
	c.RoundRect(r, 4, border)
	c.RoundRect(r.Inset(2), 3, face)
	// The printed frame: closer to the edge on a small key.
	in := keyFrame(r)
	c.RoundRect(r.Inset(in), 2, darken(face, 0.10))
	c.RoundRect(r.Inset(in+1), 2, face)
	// The LED: lit orange when the key is on, dark when it is off.
	if hasLED {
		l := image.Rect(r.Min.X+in+5, r.Min.Y+in+4, r.Min.X+in+17, r.Min.Y+in+9)
		if r.Dy() < 50 {
			l = image.Rect(r.Min.X+in+3, r.Min.Y+in+3, r.Min.X+in+11, r.Min.Y+in+7)
		}
		c.RoundRect(l.Inset(-1), 2, darken(face, 0.4))
		if on {
			c.RoundRect(l, 2, orange)
		} else {
			c.RoundRect(l, 2, darken(face, 0.25))
		}
	}
	area := r.Inset(in)
	if pressed {
		area = area.Add(image.Pt(0, 1))
	}
	area.Max.X -= right
	f := fitKeyLabel(area, strings.ToUpper(label), keyLabelSize(area), px)
	f.draw(c, area, ink)
}

// keyFrame is the inset of the printed frame of key r: closer to the edge on a small key.
func keyFrame(r image.Rectangle) int {
	if min(r.Dx(), r.Dy()) < 50 {
		return 4
	}
	return 6
}

// keyLabelSize is the size of the label of a key with the label area area, before the fit.
func keyLabelSize(area image.Rectangle) int {
	if area.Dy() < 40 {
		return 16
	}
	return 20
}

// rowLabelPx is the label size for one row of keys: the largest size at which each label fits its
// key rs[i]. Give it to keyAt or buttonAt for each key of the row.
func rowLabelPx(rs []image.Rectangle, labels []string) int {
	px := 0
	for i, r := range rs {
		area := r.Inset(keyFrame(r))
		f := fitKeyLabel(area, strings.ToUpper(labels[i]), keyLabelSize(area), 0)
		if px == 0 || f.px < px {
			px = f.px
		}
	}
	return px
}

// labelFit is the place of the label of a key: one or two lines of px pixels in the room in.
type labelFit struct {
	in    image.Rectangle
	label string
	px    int
	split int // the index of the space between the two lines, or -1 for one line
}

// draw draws the label in area, the label area of the key.
func (f labelFit) draw(c *gfx.Canvas, area image.Rectangle, ink color.RGBA) {
	face := gfx.ExactFace(true, f.px)
	if f.split < 0 {
		c.Text(f.in, f.label, face, ink, gfx.Center)
		return
	}
	lineH := face.Metrics().Height.Ceil()
	y := area.Min.Y + area.Dy()/2 - lineH
	c.Text(image.Rect(f.in.Min.X, y, f.in.Max.X, y+lineH), f.label[:f.split], face, ink, gfx.Center)
	c.Text(image.Rect(f.in.Min.X, y+lineH, f.in.Max.X, y+2*lineH), f.label[f.split+1:], face, ink, gfx.Center)
}

// fitKeyLabel finds the place of the label of a key in area, at size pixels or smaller, and at most
// px pixels when px is more than 0. A label is never cut: one line down to 13 pixels; then two
// lines, when the key is high enough; then the size that fits the key, also under the floor of
// the large text.
func fitKeyLabel(area image.Rectangle, label string, size, px int) labelFit {
	// The size of the label with the large text, and the smallest size before a label goes to
	// two lines: 2/3 of it, and not below keyLabelMin.
	top := gfx.FaceSize(size)
	if px > 0 {
		top = min(top, px)
	}
	floor := min(top, max(keyLabelMin, top*2/3))
	pad := max(2, min(keyLabelPad, area.Dx()/12)) // less on a small key, such as a step of a move
	oneLine := func(in image.Rectangle, lo int) (labelFit, bool) {
		for px := top; px >= lo; px-- {
			if gfx.Width(gfx.ExactFace(true, px), label) <= in.Dx() {
				return labelFit{in, label, px, -1}, true
			}
		}
		return labelFit{}, false
	}
	twoLines := func(in image.Rectangle, lo int) (labelFit, bool) {
		i := splitAt(label)
		if i <= 0 {
			return labelFit{}, false
		}
		for px := top; px >= lo; px-- {
			f := gfx.ExactFace(true, px)
			lineH := f.Metrics().Height.Ceil()
			if area.Dy() >= 2*lineH+2 && gfx.Width(f, label[:i]) <= in.Dx() && gfx.Width(f, label[i+1:]) <= in.Dx() {
				return labelFit{in, label, px, i}, true
			}
		}
		return labelFit{}, false
	}
	in := kit.Inset(area, pad, 0, pad, 0)
	wide := kit.Inset(area, 2, 0, 2, 0)
	// A little smaller on one line; then two lines at a larger size before one smaller line.
	// Then the whole width of the key, then smaller text, to keyLabelLast at the least.
	for _, try := range []struct {
		lines func(image.Rectangle, int) (labelFit, bool)
		in    image.Rectangle
		lo    int
	}{
		{oneLine, in, max(floor, top*5/6)}, {twoLines, in, floor}, {oneLine, in, floor},
		{oneLine, wide, floor}, {twoLines, wide, floor}, {oneLine, wide, keyLabelLast}, {twoLines, wide, keyLabelLast},
	} {
		if f, ok := try.lines(try.in, try.lo); ok {
			return f
		}
	}
	return labelFit{wide, label, keyLabelLast, -1}
}

// shrinkFace is the face of size pixels, or smaller down to least, at which s fits in width.
func shrinkFace(bold bool, size, least int, s string, width int) font.Face {
	for size > least && gfx.Width(gfx.Face(bold, size), s) > width {
		size--
	}
	return gfx.Face(bold, size)
}

// labelCols splits r into a column for each label, with gap between them: each column as wide as
// its label needs at 16 pixels, and the rest of r shared out evenly. When r is too narrow for that,
// the widths keep the same ratio, and the labels get a smaller font (keyLabel).
func labelCols(r image.Rectangle, gap int, labels ...string) []image.Rectangle {
	need := make([]int, len(labels))
	sum := 0
	for i, l := range labels {
		need[i] = gfx.Width(gfx.Face(true, 16), strings.ToUpper(l)) + 2*(keyLabelPad+6)
		sum += need[i]
	}
	free := r.Dx() - gap*(len(labels)-1)
	extra := max(0, float64(free-sum)/float64(len(labels)))
	sizes := make([]kit.Size, len(labels))
	for i, n := range need {
		sizes[i] = kit.Fr(float64(n) + extra)
	}
	return kit.Cols(r, gap, sizes...)
}

// keyLabelPad is the most room between the label of a key and its printed frame, on each side.
const keyLabelPad = 8

// keyMin is the smallest width and height of a key to touch.
const keyMin = 56

// The sizes of a key label: below keyLabelMin, a label goes to two lines or uses the whole width
// of its key first; keyLabelLast is the smallest size.
const (
	keyLabelMin  = 15
	keyLabelLast = 12
)

// splitAt returns the index of the space that splits s into two lines of about the same length,
// or -1 when s has no space.
func splitAt(s string) int {
	best := -1
	for i, r := range s {
		if r == ' ' && (best < 0 || abs(len(s)-2*i) < abs(len(s)-2*best)) {
			best = i
		}
	}
	return best
}

// fader draws a horizontal slider: a slot, the part up to the value in orange, and a grip cap.
func fader(c *gfx.Canvas, r image.Rectangle, f float64, accent color.RGBA) {
	faderWith(c, r, f, accent, text)
}

// faderWith is a fader with a cap of the colour cap. Inside a dark window the cap is light in both themes.
func faderWith(c *gfx.Canvas, r image.Rectangle, f float64, accent, cap color.RGBA) {
	cy := r.Min.Y + r.Dy()/2
	slot := image.Rect(r.Min.X, cy-4, r.Max.X, cy+4)
	c.RoundRect(slot, 4, lcd)
	x := slot.Min.X + int(f*float64(slot.Dx()))
	if x > slot.Min.X+4 {
		c.RoundRect(image.Rect(slot.Min.X+2, cy-2, x, cy+2), 2, accent)
	}
	faderCapWith(c, x, cy, false, cap)
}

// faderCap is the grip of a fader: a block with ridges and an orange index line.
func faderCap(c *gfx.Canvas, x, y int, vertical bool) { faderCapWith(c, x, y, vertical, text) }

func faderCapWith(c *gfx.Canvas, x, y int, vertical bool, cap color.RGBA) {
	w, h := 14, 22 // half sizes
	if vertical {
		w, h = 22, 14
	}
	c.RoundRect(image.Rect(x-w, y-h+3, x+w, y+h+3), 4, edge) // shadow
	c.RoundRect(image.Rect(x-w, y-h, x+w, y+h), 4, cap)
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
// hintText draws a sentence under or over the keys: a hint, a count, or a step. It is in sentence
// case at 16 pixels: a sentence in small capitals is the hardest text to read on the screen.
func hintText(c *gfx.Canvas, r image.Rectangle, s string, a gfx.Align) {
	f := gfx.Face(false, 16)
	c.Text(r, s, f, dim, a) // Text makes it smaller until it fits
}

func label(c *gfx.Canvas, r image.Rectangle, s string, a gfx.Align) {
	c.Text(r, strings.ToUpper(s), gfx.Face(true, 14), dim, a)
}

// textLines draws s in lines that fit the width of r, lineH high each, from the top of r, as many
// as r has room for. The last line ends in an ellipsis when the text is longer. It returns the
// number of lines.
func textLines(c *gfx.Canvas, r image.Rectangle, s string, face font.Face, col color.RGBA, lineH int) int {
	lines := wrapText(face, s, r.Dx())
	n := max(1, min(len(lines), r.Dy()/lineH))
	if len(lines) > n {
		lines = append(lines[:n-1], strings.Join(lines[n-1:], " "))
	}
	for i, l := range lines {
		y := r.Min.Y + i*lineH
		c.Text(image.Rect(r.Min.X, y, r.Max.X, y+lineH), l, face, col, gfx.Left)
	}
	return len(lines)
}

// detailRows draws rows of a label at the left and a value at the right in box, rowH high each,
// from the top of box. The value gets the width that its label leaves.
func detailRows(c *gfx.Canvas, box image.Rectangle, rows [][2]string, rowH int) {
	vf := gfx.Face(true, 17)
	for i, kv := range rows {
		r := image.Rect(box.Min.X, box.Min.Y+i*rowH, box.Max.X, box.Min.Y+(i+1)*rowH)
		label(c, r, kv[0], gfx.Left)
		v := image.Rect(r.Min.X+gfx.Width(gfx.Face(true, 14), strings.ToUpper(kv[0]))+16, r.Min.Y, r.Max.X, r.Max.Y)
		c.Text(v, kv[1], vf, text, gfx.Right)
	}
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
	setHeading(title)
	c.Fill(image.Rect(24, 30, 34, 50), accentMark())
	c.Text(image.Rect(44, 18, W-150, 62), strings.ToUpper(fleetTitle(title)), gfx.Face(true, 24), text, gfx.Left)
}

// charLCD is a character display of the printer: lines of text as the dots of a dot-matrix display,
// in the 6x10 font, at the largest whole scale that fits r. Lines longer than cols are cut.
func charLCD(c *gfx.Canvas, r image.Rectangle, lines []string, cols int) {
	c.RoundRect(r.Inset(-2), 6, edge)
	c.RoundRect(r, 4, glcd)
	scale := max(1, min((r.Dx()-16)/(cols*6), (r.Dy()-12)/(len(lines)*10)))
	img := textImage(lines, cols, scale)
	b := img.Bounds()
	x, y := r.Min.X+(r.Dx()-b.Dx())/2, r.Min.Y+(r.Dy()-b.Dy())/2
	c.PixelImage(image.Rect(x, y, x+b.Dx(), y+b.Dy()), img)
}

// textInk is the colour of text in c on the chassis: accentText in place of orange.
func textInk(c color.RGBA) color.RGBA {
	if c == orange {
		return accentText
	}
	return c
}
