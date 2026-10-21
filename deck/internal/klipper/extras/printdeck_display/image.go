package printdeck_display

import (
	"image"
	"image/color"
)

// The colors of a pixel of a blue ST7920 display: off, and lit.
var (
	pixelOff = color.RGBA{0x1f, 0x4b, 0xd1, 0xff}
	pixelOn  = color.RGBA{0xf2, 0xf6, 0xff, 0xff}
)

// Image draws a frame of the screen as a picture of the display, scale pixels for each pixel of
// the display, for the manual and for the tests.
func (s *Screen) Image(frame, scale int) *image.RGBA {
	var c canvas
	s.draw(&c, frame)
	img := image.NewRGBA(image.Rect(0, 0, canvasW*scale, canvasH*scale))
	for y := 0; y < canvasH; y++ {
		for x := 0; x < canvasW; x++ {
			col := pixelOff
			if c.get(x, y) {
				col = pixelOn
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					img.SetRGBA(x*scale+dx, y*scale+dy, col)
				}
			}
		}
	}
	return img
}
