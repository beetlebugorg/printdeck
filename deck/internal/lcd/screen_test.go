package lcd

import (
	"testing"

	"printdeck/internal/klipper/extras/display"
)

// lit counts the lit pixels of a rectangle.
func lit(c *canvas, x0, y0, x1, y1 int) int {
	n := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if c.get(x, y) {
				n++
			}
		}
	}
	return n
}

func TestRowText(t *testing.T) {
	var c canvas
	(&Screen{Rows: []Row{{Left: "A", Right: "B"}}}).draw(&c, 0)
	// The pixels of a glyph at x in the area of the screens.
	glyphAt := func(x, row int) byte {
		var b byte
		for i := 0; i < 8; i++ {
			if c.get(x+i, row) {
				b |= 0x80 >> i
			}
		}
		return b
	}
	for row, bits := range display.VGAFont['A'][:canvasH/4] {
		if got := glyphAt(0, row); got != bits {
			t.Fatalf("row %d of A: %08b, want %08b", row, got, bits)
		}
		if got := glyphAt(canvasW-8, row); got != display.VGAFont['B'][row] {
			t.Fatalf("row %d of B at the right: %08b", row, got)
		}
	}
	// The margin of the panel stays dark.
	for y := 0; y < panelH; y++ {
		if c[y][0]&0xc0 != 0 || c[y][15]&0x03 != 0 {
			t.Fatalf("a lit pixel in the margin at row %d", y)
		}
	}
}

func TestSelectedRow(t *testing.T) {
	var c canvas
	(&Screen{Rows: []Row{{}, {Left: "Move", Selected: true}}}).draw(&c, 0)
	if lit(&c, 0, 0, canvasW, 16) != 0 {
		t.Error("the empty first row has lit pixels")
	}
	// The frame of the second row: its top and bottom lines, and the ">".
	if lit(&c, 0, 16, canvasW, 17) != canvasW || lit(&c, 0, 31, canvasW, 32) != canvasW {
		t.Error("no frame around the selected row")
	}
	if lit(&c, 0, 17, 8, 31) == 0 {
		t.Error("no cursor")
	}
	// Few pixels: the selection does not fill the row.
	if n := lit(&c, 0, 16, canvasW, 32); n > canvasW*16/3 {
		t.Errorf("%d lit pixels in the selected row", n)
	}
}

func TestBar(t *testing.T) {
	var half, full canvas
	(&Screen{Rows: []Row{{HasBar: true, Bar: 0.5}}}).draw(&half, 0)
	(&Screen{Rows: []Row{{HasBar: true, Bar: 1}}}).draw(&full, 0)
	h, f := lit(&half, 2, 6, canvasW-2, 10), lit(&full, 2, 6, canvasW-2, 10)
	if f == 0 || h*2 < f-8 || h*2 > f+8 {
		t.Errorf("half bar %d pixels, full bar %d", h, f)
	}
}

func TestAnimations(t *testing.T) {
	for _, a := range []string{"work", "heat", "in", "out"} {
		var c0, c1 canvas
		s := &Screen{Anim: a, Title: "TITLE", Caption: "WAIT"}
		s.draw(&c0, 0)
		s.draw(&c1, 1)
		if lit(&c0, 0, 16, canvasW, 48) == 0 {
			t.Errorf("%s: no picture", a)
		}
		if c0 == c1 {
			t.Errorf("%s: frame 1 is the same as frame 0", a)
		}
		// Thin lines only: at most a quarter of the pixels of the picture are lit.
		if n := lit(&c0, 0, 16, canvasW, 48); n > canvasW*32/4 {
			t.Errorf("%s: %d lit pixels", a, n)
		}
	}
}

// A heater at its temperature moves while it has power, and stops while it has none.
func TestHeaterMovesWithPower(t *testing.T) {
	s := Status{Tool: 210, ToolT: 210, ToolPow: 0.3}
	if !s.Moves() {
		t.Error("a heater with power does not move")
	}
	s.ToolPow = 0
	if s.Moves() {
		t.Error("a heater with no power moves")
	}
}

// During the layers of a print, the screen shows the progress and the time left in the large
// font, and a heater at its target does not blink.
func TestRunningScreen(t *testing.T) {
	s := &Status{Printing: true, Running: true, Progress: 0.42, Left: "1:23", Tool: 210, ToolT: 210, ToolPow: 0.4,
		Bed: 60, BedT: 60, Layer: "17/120", Next: "1:32", Z: "3.20", File: "pot", Clock: "2:24PM"}
	var c0, c3 canvas
	s.draw(&c0, 0)
	s.draw(&c3, 3)
	if c0 != c3 {
		t.Error("the screen of a print at its temperatures changes between frames")
	}
	if lit(&c0, 0, 0, 40, 16) == 0 || lit(&c0, canvasW-40, 0, canvasW, 16) == 0 {
		t.Error("no progress or time left in the large font")
	}
	s.Tool = 180 // under the target: the hotend blinks
	var h0, h3 canvas
	s.draw(&h0, 0)
	s.draw(&h3, 3)
	if h0 == h3 {
		t.Error("a hotend under its target does not blink")
	}
}
