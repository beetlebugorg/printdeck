//go:build linux

package fb

import (
	"image"
	"image/color"
	"testing"

	"printdeck/internal/gfx"
)

// A framebuffer that is half the size of the canvas shows the whole canvas, scaled down.
func TestPresentScales(t *testing.T) {
	c := gfx.New(4, 8)
	c.Fill(image.Rect(0, 4, 4, 8), color.RGBA{255, 0, 0, 255}) // the bottom half is red
	d := &FB{mem: make([]byte, 2*4*4), w: 2, h: 4, stride: 2 * 4, bpp: 32, LogicalW: 2, LogicalH: 4}
	d.Present(c)
	for y := 0; y < 4; y++ {
		red := d.mem[y*d.stride+2] == 255 // the bytes are b, g, r, a
		if want := y >= 2; red != want {
			t.Errorf("row %d: red is %v, want %v", y, red, want)
		}
	}
}
