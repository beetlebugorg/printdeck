package ui

import (
	"image"
	"image/draw"
	"math"
	"sync"

	"printdeck/internal/gfx"
	lcdscreen "printdeck/internal/lcd"
)

// The start screen: one blue dot-matrix LCD over the whole screen. PRINTDECK is in orange dots on
// the white bed icon, and the version is under the bed in a small font. A tall screen has the
// word on two lines, PRINT over DECK, for larger dots.

const (
	startFont   = 8 // the rows of the 5 x 8 font
	startGap    = 2 // the empty rows between two lines
	startBed    = 3 // the rows of the bed: the plate and its legs
	startVerGap = 2 // the empty rows between the bed and the version
	startRows   = startBed + startVerGap + tinyRows
)

// The colours: the display of the day palette, the print (PLA orange), the white of the bed, and
// the pale blue of the version.
var (
	startBG   = rgb(0x1f4bd1)
	startCell = rgb(0x2a58e0)
	startInk  = rgb(0xf2f6ff)
	startPLA  = rgb(0xff7a1a)
	startDim  = rgb(0x8fa8ef)
)

// startGeom is the grid of the display: the size of its dots, and the screen place of the dot
// 0, 0. The dot 0, 0 is the top left dot of the word. The word has cols x rows dots. The bed is
// under it, and the version is under the bed.
type startGeom struct {
	b          image.Rectangle
	lines      []string
	cols, rows int
	pitch      int
	x0, y0     int
}

// startLayout puts the word, the bed and the version in the middle of the screen, with the
// largest dots that fit.
func startLayout(b image.Rectangle) startGeom {
	lines := []string{"PRINTDECK"}
	if b.Dy() > b.Dx() {
		lines = []string{"PRINT", "DECK"}
	}
	cols := 0 // the 5 x 8 font, with one empty column between the letters
	for _, l := range lines {
		cols = max(cols, len(l)*6-1)
	}
	rows := len(lines)*startFont + (len(lines)-1)*startGap
	// The bed is one dot wider than the word at each end.
	pitch := max(4, min(14, (b.Dx()-48)/(cols+2), (b.Dy()-48)/(rows+startRows)))
	return startGeom{b: b, lines: lines, cols: cols, rows: rows, pitch: pitch,
		x0: b.Min.X + (b.Dx()-cols*pitch)/2, y0: b.Min.Y + (b.Dy()-(rows+startRows)*pitch)/2}
}

// cell is the screen rectangle of the dot at column x and row y of the grid.
func (g startGeom) cell(x, y int) image.Rectangle {
	px, py := g.x0+x*g.pitch, g.y0+y*g.pitch
	return image.Rect(px+1, py+1, px+g.pitch-1, py+g.pitch-1)
}

// lit reports the dots of the word. Each line is in the middle of the word.
func (g startGeom) lit(x, y int) bool {
	line, row := y/(startFont+startGap), y%(startFont+startGap)
	if row >= startFont || line >= len(g.lines) {
		return false
	}
	return glyphDot(g.lines[line], x-(g.cols-(len(g.lines[line])*6-1))/2, row)
}

// glyphDot reports the dot at column x and row y of a text in the 5 x 8 font.
func glyphDot(s string, x, y int) bool {
	ch, col := x/6, x%6
	if x < 0 || col == 5 || ch >= len(s) {
		return false
	}
	return lcdscreen.Glyph5x8(s[ch])[y]&(0x80>>col) != 0
}

// The empty display of a screen size, drawn once.
var startGrid struct {
	sync.Mutex
	size image.Point
	img  *image.RGBA
}

// startBackground returns the empty display: the dots of the grid over the whole screen.
func startBackground(g startGeom) *image.RGBA {
	startGrid.Lock()
	defer startGrid.Unlock()
	b := g.b
	if startGrid.img != nil && startGrid.size == b.Size() {
		return startGrid.img
	}
	c := gfx.New(b.Dx(), b.Dy())
	c.Fill(c.Bounds(), startBG)
	fx := -((g.x0 - b.Min.X + g.pitch - 1) / g.pitch)
	fy := -((g.y0 - b.Min.Y + g.pitch - 1) / g.pitch)
	lx, ly := (b.Max.X-g.x0)/g.pitch, (b.Max.Y-g.y0)/g.pitch
	for y := fy; y <= ly; y++ {
		for x := fx; x <= lx; x++ {
			c.Fill(g.cell(x, y).Sub(b.Min), startCell)
		}
	}
	startGrid.size, startGrid.img = b.Size(), c.RGBA
	return c.RGBA
}

// drawEmptyDisplay draws the empty display on the whole canvas. It is the kernel logo.
func drawEmptyDisplay(c *gfx.Canvas) {
	b := c.Bounds()
	draw.Draw(c.RGBA, b, startBackground(startLayout(b)), image.Point{}, draw.Src)
}

// drawStart draws the start screen on the whole canvas. version is printed under the bed; it can
// be empty. A version that is too wide for the bed loses its last characters.
func drawStart(c *gfx.Canvas, version string) {
	b := c.Bounds()
	g := startLayout(b)
	draw.Draw(c.RGBA, b, startBackground(g), image.Point{}, draw.Src)
	for y := range g.rows {
		for x := range g.cols {
			if g.lit(x, y) {
				c.Fill(g.cell(x, y), startPLA)
			}
		}
	}
	// The bed icon: a plate of two rows, one dot wider than the word at each end, and two legs.
	top := g.rows
	for x := -1; x <= g.cols; x++ {
		c.Fill(g.cell(x, top), startInk)
		c.Fill(g.cell(x, top+1), startInk)
	}
	c.Fill(g.cell(0, top+2), startInk)
	c.Fill(g.cell(g.cols-1, top+2), startInk)
	// The version, in the small font, in the middle under the bed.
	version = tinyText(version, g.cols+2)
	cols := max(0, len(version)*tinyAdvance-1)
	vx, vy := (g.cols-cols)/2, top+startBed+startVerGap
	for y := range tinyRows {
		for x := range cols {
			if tinyDot(version, x, y) {
				c.Fill(g.cell(vx+x, vy+y), startDim)
			}
		}
	}
}

// Crossfade draws the mix of two frames of the same size in dst: from at f 0, to at f 1. The deck
// goes from the start screen into its first screen with it.
func Crossfade(dst, from, to *gfx.Canvas, f float64) {
	a := uint32(math.Round(max(0, min(1, f)) * 256))
	for i := 0; i+3 < len(dst.Pix) && i+3 < len(from.Pix) && i+3 < len(to.Pix); i += 4 {
		for k := range 3 {
			dst.Pix[i+k] = uint8((uint32(from.Pix[i+k])*(256-a) + uint32(to.Pix[i+k])*a) >> 8)
		}
		dst.Pix[i+3] = 255
	}
}
