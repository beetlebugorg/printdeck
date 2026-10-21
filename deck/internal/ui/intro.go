package ui

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"
	"sync"
	"time"

	"printdeck/internal/gfx"
	lcdscreen "printdeck/internal/lcd"
)

// The start animation: the whole screen is a blue LCD. A small toolhead comes down and prints
// PRINTDECK in the dots of the display, one row of dots at a time from the bottom up, as a printer
// prints its layers. A tall screen has the word on two lines, PRINT over DECK, for larger dots. Each dot is hot when it lands, and cools to the white of the display. Then
// the toolhead goes up and away, and the version shows under the name.

const (
	introBlank = 400 * time.Millisecond // the empty display
	introDrop  = 400 * time.Millisecond // the toolhead comes down
	introRow   = 325 * time.Millisecond // one row of dots
	introCool  = 400 * time.Millisecond // a dot cools from orange to white
	introLift  = 500 * time.Millisecond // the toolhead goes up and away
	introShow  = 400 * time.Millisecond // the version fades in
	introFont  = 8                      // the rows of the 5 x 8 font
	introGap   = 2                      // the empty rows between two lines
)

// IntroLength is the time of the whole animation, on the screen with the most rows: the tall one.
const IntroLength = introBlank + introDrop + (2*introFont+introGap)*introRow + introLift + introShow

// The colours of the animation: the display of the day palette, and the hot plastic.
var (
	introBG   = rgb(0x1f4bd1)
	introCell = rgb(0x2a58e0)
	introInk  = rgb(0xf2f6ff)
	introHot  = rgb(0xff9f43)
)

// introGeom is the place of the word on a screen: its lines, the pitch of the dots, the top left
// dot, and the size in dots.
type introGeom struct {
	lines         []string
	pitch, x0, y0 int
	cols, rows    int
}

func introLayout(b image.Rectangle) introGeom {
	lines := []string{"PRINTDECK"}
	if b.Dy() > b.Dx() {
		lines = []string{"PRINT", "DECK"}
	}
	cols := 0 // the 5 x 8 font, with one empty column between the letters
	for _, l := range lines {
		cols = max(cols, len(l)*6-1)
	}
	rows := len(lines)*introFont + (len(lines)-1)*introGap
	pitch := max(4, min(14, (b.Dx()-48)/cols))
	return introGeom{lines: lines, pitch: pitch, cols: cols, rows: rows,
		x0: b.Min.X + (b.Dx()-cols*pitch)/2, y0: b.Min.Y + (b.Dy()-rows*pitch)/2 - pitch}
}

// end is the time when the toolhead puts down the last dot.
func (g introGeom) end() time.Duration {
	return introBlank + introDrop + time.Duration(g.rows)*introRow
}

// cell is the rectangle of the dot at column x and row y of the word.
func (g introGeom) cell(x, y int) image.Rectangle {
	px, py := g.x0+x*g.pitch, g.y0+y*g.pitch
	return image.Rect(px+1, py+1, px+g.pitch-1, py+g.pitch-1)
}

// lit reports the dots of the word. Each line is in the middle, on the grid of the dots.
func (g introGeom) lit(x, y int) bool {
	line, row := y/(introFont+introGap), y%(introFont+introGap)
	if row >= introFont || line >= len(g.lines) {
		return false
	}
	word := g.lines[line]
	x -= (g.cols - (len(word)*6 - 1)) / 2
	ch, col := x/6, x%6
	if x < 0 || col == 5 || ch >= len(word) {
		return false
	}
	return lcdscreen.Glyph5x8(word[ch])[row]&(0x80>>col) != 0
}

// printedAt is the time when the toolhead puts down the dot at column x and row y. The bottom
// row comes first, and the toolhead goes back and forth.
func (g introGeom) printedAt(x, y int) time.Duration {
	layer := g.rows - 1 - y
	f := float64(x) / float64(g.cols-1)
	if layer%2 == 1 {
		f = 1 - f
	}
	return introBlank + introDrop + time.Duration(layer)*introRow + time.Duration(f*float64(introRow))
}

// The empty display of a screen size, drawn once.
var introGrid struct {
	sync.Mutex
	size image.Point
	img  *image.RGBA
}

func introBackground(b image.Rectangle, g introGeom) *image.RGBA {
	introGrid.Lock()
	defer introGrid.Unlock()
	if introGrid.img != nil && introGrid.size == b.Size() {
		return introGrid.img
	}
	c := gfx.New(b.Dx(), b.Dy())
	c.Fill(c.Bounds(), introBG)
	// The dots of the whole screen, on the grid of the word.
	ox, oy := (g.x0-b.Min.X)%g.pitch, (g.y0-b.Min.Y)%g.pitch
	for y := oy - g.pitch; y < b.Dy(); y += g.pitch {
		for x := ox - g.pitch; x < b.Dx(); x += g.pitch {
			c.Fill(image.Rect(x+1, y+1, x+g.pitch-1, y+g.pitch-1), introCell)
		}
	}
	introGrid.size, introGrid.img = b.Size(), c.RGBA
	return c.RGBA
}

// DrawIntro draws the start animation at the time t from its start, on the whole canvas. At 0 it
// is the empty display, the picture of the kernel logo and of the splash screen. version shows
// at the end; it can be empty.
func DrawIntro(c *gfx.Canvas, t time.Duration, version string) {
	b := c.Bounds()
	g := introLayout(b)
	if c.Recording() == nil {
		draw.Draw(c.RGBA, b, introBackground(b, g), image.Point{}, draw.Src)
	} else {
		c.Image(b, introBackground(b, g))
	}

	// The dots that are printed, hot at first.
	for y := range g.rows {
		for x := range g.cols {
			if !g.lit(x, y) {
				continue
			}
			at := g.printedAt(x, y)
			if t < at {
				continue
			}
			heat := 1 - min(1, float64(t-at)/float64(introCool))
			c.Fill(g.cell(x, y), mixRGB(introInk, introHot, heat))
		}
	}

	// The toolhead: it comes down to the first dot, prints, and goes up and away.
	start := introBlank + introDrop
	end := g.end()
	r := 2 * g.pitch
	var tipX, tipY float64
	hot := false
	switch {
	case t < introBlank:
		// No toolhead yet.
	case t < start:
		f := easeOut(float64(t-introBlank) / float64(introDrop))
		tipX = float64(g.x0) + float64(g.pitch)/2
		tipY = float64(b.Min.Y-3*r) + f*float64(g.y0+(g.rows-1)*g.pitch+3*r-b.Min.Y)
	case t < end:
		layer := int((t - start) / introRow)
		f := float64((t-start)%introRow) / float64(introRow)
		if layer%2 == 1 {
			f = 1 - f
		}
		tipX = float64(g.x0) + float64(g.pitch)/2 + f*float64((g.cols-1)*g.pitch)
		tipY = float64(g.y0 + (g.rows-1-layer)*g.pitch)
		hot = true
	case t < end+introLift:
		f := easeIn(float64(t-end) / float64(introLift))
		x0, y0 := float64(g.x0+g.pitch/2), float64(g.y0) // the end of the top row, at the left
		tipX = x0 + f*float64(b.Dx()/3)
		tipY = y0 - f*float64(y0-float64(b.Min.Y)+3*float64(r))
	}
	if tipX != 0 || tipY != 0 {
		cx, cy := int(tipX), int(tipY-0.8*float64(r))
		drawNozzle(c, cx, cy, r, introInk)
		if hot {
			c.RoundRect(image.Rect(cx-g.pitch/3, int(tipY)-g.pitch/3, cx+g.pitch/3, int(tipY)+g.pitch/3), g.pitch/3, introHot)
		}
	}

	// The version under the name, at the end.
	if version != "" && t > end+introLift {
		f := min(1, float64(t-end-introLift)/float64(introShow))
		y := g.y0 + (g.rows+2)*g.pitch
		c.Text(image.Rect(b.Min.X, y, b.Max.X, y+28), strings.ToUpper(version), gfx.Face(true, 16),
			mixRGB(introCell, introInk, f), gfx.Center)
	}
}

// mixRGB mixes two colours: a at f 0, c at f 1.
func mixRGB(a, c color.RGBA, f float64) color.RGBA {
	m := func(x, y uint8) uint8 { return uint8(math.Round(float64(x) + (float64(y)-float64(x))*f)) }
	return color.RGBA{m(a.R, c.R), m(a.G, c.G), m(a.B, c.B), 255}
}

func easeOut(f float64) float64 { return 1 - (1-f)*(1-f) }
func easeIn(f float64) float64  { return f * f }

// Crossfade draws the mix of two frames of the same size in dst: from at f 0, to at f 1. The start
// animation goes into the first screen of the deck with it.
func Crossfade(dst, from, to *gfx.Canvas, f float64) {
	a := uint32(math.Round(max(0, min(1, f)) * 256))
	for i := 0; i+3 < len(dst.Pix) && i+3 < len(from.Pix) && i+3 < len(to.Pix); i += 4 {
		for k := range 3 {
			dst.Pix[i+k] = uint8((uint32(from.Pix[i+k])*(256-a) + uint32(to.Pix[i+k])*a) >> 8)
		}
		dst.Pix[i+3] = 255
	}
}
