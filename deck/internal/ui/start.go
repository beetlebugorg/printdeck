package ui

import (
	"image"
	"math"
	"sync"

	"printdeck/internal/gfx"
	lcdscreen "printdeck/internal/lcd"
)

// The start screen: one blue dot-matrix LCD over the whole screen, with the title of the cover of
// the manual: PRINTDECK in white dots of the 8 x 16 font of the printer display, a white rule under
// it, and the version under the rule in a small font.

const (
	startFont   = 16 // the rows of the 8 x 16 font
	startRule   = 2  // the empty rows between the word and the rule
	startVerGap = 2  // the empty rows between the rule and the version
	startRows   = startRule + 1 + startVerGap + tinyRows
)

// The colours: the display of the day palette, the white of the word, and the pale blue of the
// version.
var (
	startBG   = rgb(0x1f4bd1)
	startCell = rgb(0x2a58e0)
	startInk  = rgb(0xf2f6ff)
	startDim  = rgb(0x8fa8ef)
)

// startWord is the word of the start screen.
const startWord = "PRINTDECK"

// startGeom is the grid of the display: the size of its dots, and the screen place of the dot
// 0, 0. The dot 0, 0 is the top left dot of the word. The word has cols x rows dots. The rule and
// the version are under it.
type startGeom struct {
	b          image.Rectangle
	cols, rows int
	pitch      int
	x0, y0     int
}

// startLayout puts the word, the rule and the version in the middle of the screen, with the
// largest dots that fit.
func startLayout(b image.Rectangle) startGeom {
	cols, rows := len(startWord)*8, startFont
	pitch := max(4, min(14, (b.Dx()-48)/cols, (b.Dy()-48)/(rows+startRows)))
	return startGeom{b: b, cols: cols, rows: rows, pitch: pitch,
		x0: b.Min.X + (b.Dx()-cols*pitch)/2, y0: b.Min.Y + (b.Dy()-(rows+startRows)*pitch)/2}
}

// cell is the screen rectangle of the dot at column x and row y of the grid.
func (g startGeom) cell(x, y int) image.Rectangle {
	px, py := g.x0+x*g.pitch, g.y0+y*g.pitch
	return image.Rect(px+1, py+1, px+g.pitch-1, py+g.pitch-1)
}

// lit reports the dots of the word.
func (g startGeom) lit(x, y int) bool {
	if x < 0 || y < 0 || x >= g.cols || y >= g.rows {
		return false
	}
	return lcdscreen.Glyph8x16(startWord[x/8])[y]&(0x80>>(x%8)) != 0
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
	c.PixelImage(b, startBackground(startLayout(b)))
}

// drawStart draws the start screen on the whole canvas. version is printed under the bed; it can
// be empty. A version that is too wide for the bed loses its last characters.
func drawStart(c *gfx.Canvas, version string) {
	b := c.Bounds()
	g := startLayout(b)
	// An image command, so a recorded frame of a remote screen has the grid too.
	c.PixelImage(b, startBackground(g))
	for y := range g.rows {
		for x := range g.cols {
			if g.lit(x, y) {
				c.Fill(g.cell(x, y), startInk)
			}
		}
	}
	// The rule: a line in the middle of its row of dots, as wide as the word.
	ry := g.y0 + (g.rows+startRule)*g.pitch + g.pitch/2
	lw := max(2, g.pitch/3)
	c.Fill(image.Rect(g.x0+1, ry-lw/2, g.x0+g.cols*g.pitch-1, ry-lw/2+lw), startInk)
	// The version, in the small font, in the middle under the rule.
	version = tinyText(version, g.cols)
	cols := max(0, len(version)*tinyAdvance-1)
	vx, vy := (g.cols-cols)/2, g.rows+startRule+1+startVerGap
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
