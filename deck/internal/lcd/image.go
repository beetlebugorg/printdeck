package lcd

import (
	"image"
	"image/color"

	"printdeck/internal/klipper/extras/display"
)

// The colors of a pixel of a blue ST7920 display: off, and lit.
var (
	pixelOff = color.RGBA{0x1f, 0x4b, 0xd1, 0xff}
	pixelOn  = color.RGBA{0xf2, 0xf6, 0xff, 0xff}
)

// Image draws a frame of the screen as a picture of a blue display, scale pixels for each pixel
// of the display, for the manual and for the tests.
func (s *Screen) Image(frame, scale int) *image.RGBA {
	return BitsImage(s.Bits(frame), scale, pixelOff, pixelOn)
}

// BitsImage draws a frame as a picture, scale pixels for each pixel, in the colors off and on.
func BitsImage(c [64][16]byte, scale int, off, on color.RGBA) *image.RGBA {
	pixelOff, pixelOn := off, on
	img := image.NewRGBA(image.Rect(0, 0, canvasW*scale, canvasH*scale))
	for y := 0; y < canvasH; y++ {
		for x := 0; x < canvasW; x++ {
			col := pixelOff
			if c[y][x>>3]&(0x80>>(x&7)) != 0 {
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

// DotImage draws a frame as a picture of a dot-matrix display: each pixel is a square dot of
// scale-1 pixels in the color on or off, with a gap of one pixel in the color gap.
func DotImage(c [64][16]byte, scale int, gap, off, on color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, canvasW*scale, canvasH*scale))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = gap.R, gap.G, gap.B, gap.A
	}
	for y := 0; y < canvasH; y++ {
		for x := 0; x < canvasW; x++ {
			col := off
			if c[y][x>>3]&(0x80>>(x&7)) != 0 {
				col = on
			}
			for dy := 0; dy < scale-1; dy++ {
				for dx := 0; dx < scale-1; dx++ {
					img.SetRGBA(x*scale+dx, y*scale+dy, col)
				}
			}
		}
	}
	return img
}

// DotLines draws lines of text as the dots of a dot-matrix display, cols characters wide, in the
// font "5x8", "6x10" or "8x16" (the font of Klipper), scale pixels for each dot, with a gap of
// one pixel. A longer line is cut.
func DotLines(lines []string, cols int, font string, scale int, gap, off, on color.RGBA) *image.RGBA {
	fw, fh := 8, 16
	f := fonts[font]
	if f != nil {
		fw, fh = f.w, f.h
	}
	w, h := cols*fw, len(lines)*fh
	bits := make([][]bool, h)
	for i := range bits {
		bits[i] = make([]bool, w)
	}
	for row, l := range lines {
		l = fit(l, cols)
		for i := 0; i < len(l); i++ {
			var g []byte
			if f != nil {
				g = f.glyph(l[i])
			} else {
				vg := display.VGAFont[l[i]]
				g = vg[:]
			}
			for y, b := range g {
				for x := 0; x < fw && x < 8; x++ {
					if b&(0x80>>x) != 0 && y < fh {
						bits[row*fh+y][i*fw+x] = true
					}
				}
			}
		}
	}
	img := image.NewRGBA(image.Rect(0, 0, w*scale, h*scale))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = gap.R, gap.G, gap.B, gap.A
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			col := off
			if bits[y][x] {
				col = on
			}
			for dy := 0; dy < scale-1; dy++ {
				for dx := 0; dx < scale-1; dx++ {
					img.SetRGBA(x*scale+dx, y*scale+dy, col)
				}
			}
		}
	}
	return img
}
