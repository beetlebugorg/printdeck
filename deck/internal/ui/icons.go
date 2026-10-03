package ui

import (
	"image"
	"image/color"
	"math"

	"speakerdeck/internal/gfx"
)

// Icons are drawn with shapes, so they scale and take any colour. cx, cy is the centre, r half the size.

func pt(cx, cy int, r, angle float64) image.Point {
	return image.Pt(cx+int(math.Round(r*math.Cos(angle))), cy+int(math.Round(r*math.Sin(angle))))
}

// drawFan draws a fan with three blades. turn is the rotation in degrees.
func drawFan(c *gfx.Canvas, cx, cy, r int, turn float64, col color.RGBA) {
	for k := 0; k < 3; k++ {
		a := (float64(k)*120 + turn) * math.Pi / 180
		R := float64(r)
		c.Polygon([]image.Point{
			pt(cx, cy, R*0.18, a-1.2),
			pt(cx, cy, R*0.95, a-0.55),
			pt(cx, cy, R*1.0, a+0.15),
			pt(cx, cy, R*0.55, a+0.75),
			pt(cx, cy, R*0.18, a+0.9),
		}, col)
	}
	c.RoundRect(image.Rect(cx-r/5, cy-r/5, cx+r/5, cy+r/5), r/5, col)
}

// drawNozzle draws a heater block with a cone tip.
func drawNozzle(c *gfx.Canvas, cx, cy, r int, col color.RGBA) {
	u := float64(r) / 10
	f := func(v float64) int { return int(math.Round(v * u)) }
	c.RoundRect(image.Rect(cx-f(7), cy-f(10), cx+f(7), cy-f(2)), f(1.5), col) // block
	c.Polygon([]image.Point{{cx - f(4), cy - f(2)}, {cx + f(4), cy - f(2)}, {cx + f(1), cy + f(8)}, {cx - f(1), cy + f(8)}}, col)
	c.Fill(image.Rect(cx-f(1.5), cy-f(10), cx+f(1.5), cy-f(10)+f(1)), col)
}

// drawBed draws a bed plate with heat waves above it.
func drawBed(c *gfx.Canvas, cx, cy, r int, col color.RGBA) {
	u := float64(r) / 10
	f := func(v float64) int { return int(math.Round(v * u)) }
	c.RoundRect(image.Rect(cx-f(10), cy+f(4), cx+f(10), cy+f(8)), f(1.5), col)
	for _, dx := range []float64{-5, 0, 5} {
		x := cx + f(dx)
		for i := 0; i < 2; i++ { // a wave from two slanted strokes
			y := cy - f(8) + i*f(6)
			s := f(1.6)
			if i == 1 {
				s = -s
			}
			c.Polygon([]image.Point{{x, y}, {x + f(1.4), y}, {x + f(1.4) + s, y + f(6)}, {x + s, y + f(6)}}, col)
		}
	}
}

// drawHeight draws a double arrow with a base line, for the height of the head.
func drawHeight(c *gfx.Canvas, cx, cy, r int, col color.RGBA) {
	u := float64(r) / 10
	f := func(v float64) int { return int(math.Round(v * u)) }
	c.Fill(image.Rect(cx-f(0.8), cy-f(6), cx+f(0.8), cy+f(6)), col)
	c.Polygon([]image.Point{{cx, cy - f(10)}, {cx - f(4), cy - f(4)}, {cx + f(4), cy - f(4)}}, col)
	c.Polygon([]image.Point{{cx, cy + f(10)}, {cx - f(4), cy + f(4)}, {cx + f(4), cy + f(4)}}, col)
}
