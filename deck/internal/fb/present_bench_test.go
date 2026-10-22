//go:build linux

package fb

import (
	"testing"

	"printdeck/internal/gfx"
)

// BenchmarkPresent measures the copy of a portrait frame to a 800x480 panel at 32 bits.
func BenchmarkPresent(b *testing.B) {
	d := &FB{mem: make([]byte, 800*480*4), w: 800, h: 480, stride: 800 * 4, bpp: 32, rot: 270, LogicalW: 480, LogicalH: 800}
	c := gfx.New(480, 800)
	for i := 0; i < b.N; i++ {
		d.Present(c)
	}
}

// A frame writes the rows that changed since the last frame, and no others.
func TestPresentWritesChangedRows(t *testing.T) {
	d := &FB{mem: make([]byte, 800*480*4), w: 800, h: 480, stride: 800 * 4, bpp: 32, rot: 270, LogicalW: 480, LogicalH: 800}
	c := gfx.New(480, 800)
	d.Present(c)
	// Mark the framebuffer pixel of canvas row 10, then change only canvas row 20.
	px := func(x, y int) []byte { o := (d.LogicalW-1-x)*d.stride + y*4; return d.mem[o : o+4] }
	px(0, 10)[0] = 0x77
	c.Pix[20*c.Stride] = 0x55 // the red byte of pixel (0, 20)
	d.Present(c)
	if px(0, 10)[0] != 0x77 {
		t.Error("an unchanged row was written again")
	}
	if px(0, 20)[2] != 0x55 {
		t.Errorf("the changed row is %v, want red 0x55", px(0, 20))
	}
}
