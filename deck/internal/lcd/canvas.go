package lcd

import (
	"math"

	"printdeck/internal/klipper/extras/display"
)

// canvas is the 128 x 64 pixels of the display, one bit each, 8 pixels in a byte with the left
// pixel in the top bit, as the graphics of the ST7920.
type canvas [64][16]byte

const (
	canvasW = 128
	canvasH = 64
)

func (c *canvas) set(x, y int, on bool) {
	if x < 0 || x >= canvasW || y < 0 || y >= canvasH {
		return
	}
	bit := byte(0x80) >> (x & 7)
	if on {
		c[y][x>>3] |= bit
	} else {
		c[y][x>>3] &^= bit
	}
}

func (c *canvas) get(x, y int) bool {
	if x < 0 || x >= canvasW || y < 0 || y >= canvasH {
		return false
	}
	return c[y][x>>3]&(byte(0x80)>>(x&7)) != 0
}

// fill sets a rectangle; x1 and y1 are not part of it.
func (c *canvas) fill(x0, y0, x1, y1 int, on bool) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			c.set(x, y, on)
		}
	}
}

// frame draws the edge of a rectangle.
func (c *canvas) frame(x0, y0, x1, y1 int) {
	for x := x0; x < x1; x++ {
		c.set(x, y0, true)
		c.set(x, y1-1, true)
	}
	for y := y0; y < y1; y++ {
		c.set(x0, y, true)
		c.set(x1-1, y, true)
	}
}

// bitmapFont is a font of fixed width.
type bitmapFont struct {
	w, h   int
	glyphs [][]byte
}

// glyph returns the rows of a character: ASCII, or the degree sign (0xf8 of the 8 x 16 font).
func (f *bitmapFont) glyph(ch byte) []byte {
	switch {
	case ch == 0xf8:
		return f.glyphs[len(f.glyphs)-1]
	case ch >= 32 && ch < 127:
		return f.glyphs[ch-32]
	}
	return f.glyphs[0]
}

// smallText draws text in a small font with its top left corner at x, y. bold draws each pixel
// two times, one pixel apart. It returns the x after the text.
func (c *canvas) smallText(f *bitmapFont, x, y int, s string, bold bool) int {
	for _, ch := range []byte(s) {
		for row, bits := range f.glyph(ch) {
			for b := 0; b < 8; b++ {
				if bits&(0x80>>b) != 0 {
					c.set(x+b, y+row, true)
					if bold {
						c.set(x+b+1, y+row, true)
					}
				}
			}
		}
		x += f.w
	}
	return x
}

// text draws text in the 8 x 16 font of the display, with its top left corner at x, y. It returns
// the x after the text.
func (c *canvas) text(x, y int, s string) int {
	for _, ch := range []byte(s) {
		g := display.VGAFont[ch]
		for row, bits := range g {
			for b := 0; b < 8; b++ {
				if bits&(0x80>>b) != 0 {
					c.set(x+b, y+row, true)
				}
			}
		}
		x += 8
	}
	return x
}

// line draws a line from x0, y0 to x1, y1.
func (c *canvas) line(x0, y0, x1, y1 int) {
	n := max(abs(x1-x0), abs(y1-y0), 1)
	for i := 0; i <= n; i++ {
		c.set(x0+(x1-x0)*i/n, y0+(y1-y0)*i/n, true)
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// wave is a value of a sine curve, rounded.
func wave(amp, phase float64) int { return int(math.Round(amp * math.Sin(phase))) }
