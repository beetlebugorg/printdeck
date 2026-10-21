package lcd

import "math"

// The large icons of the carousel: 24 x 24 pixels, drawn with lines, circles and filled shapes,
// so that they have more detail than the 8 x 8 icons at 3 times their size.
var icons24 = map[string]func(c *canvas, x, y int){
	"status": func(c *canvas, x, y int) { // a gauge with ticks and a needle
		cx, cy := x+12, y+16
		c.arc(cx, cy, 11, math.Pi, 2*math.Pi)
		c.arc(cx, cy, 10, math.Pi, 2*math.Pi)
		for i := 0; i <= 4; i++ {
			a := math.Pi + float64(i)*math.Pi/4
			c.line(cx+round(8*math.Cos(a)), cy+round(8*math.Sin(a)), cx+round(6*math.Cos(a)), cy+round(6*math.Sin(a)))
		}
		c.line(cx, cy, cx+6, cy-6)
		c.line(cx+1, cy, cx+6, cy-5)
		c.disc(cx, cy, 2)
		c.line(x+1, cy+3, x+22, cy+3)
	},
	"print": func(c *canvas, x, y int) { // a printer: the frame, the gantry, the nozzle, a part
		c.frame(x+1, y+1, x+23, y+23)
		c.line(x+2, y+7, x+21, y+7) // the gantry
		c.fill(x+9, y+5, x+15, y+10, true)
		c.line(x+11, y+10, x+12, y+12)
		c.line(x+12, y+10, x+12, y+12)
		c.fill(x+8, y+16, x+16, y+19, true) // the part
		c.fill(x+10, y+14, x+14, y+16, true)
		c.line(x+3, y+19, x+20, y+19) // the bed
		c.line(x+3, y+20, x+20, y+20)
	},
	"preheat": func(c *canvas, x, y int) { // a flame with an inner flame
		c.polygon([][2]int{{12, 1}, {16, 7}, {19, 12}, {19, 17}, {16, 21}, {12, 23}, {8, 21}, {5, 17}, {5, 12}, {8, 8}, {10, 11}}, x, y, true)
		c.polygon([][2]int{{12, 11}, {15, 16}, {14, 20}, {12, 21}, {10, 20}, {9, 16}}, x, y, false)
	},
	"cool": func(c *canvas, x, y int) { // a snowflake
		cx, cy := x+12, y+12
		for k := 0; k < 3; k++ {
			a := float64(k) * math.Pi / 3
			dx, dy := round(11*math.Cos(a)), round(11*math.Sin(a))
			c.line(cx-dx, cy-dy, cx+dx, cy+dy)
			for _, s := range []float64{-1, 1} {
				for _, r := range []float64{6, 9} {
					bx, by := cx+round(s*r*math.Cos(a)), cy+round(s*r*math.Sin(a))
					for _, d := range []float64{-0.7, 0.7} {
						c.line(bx, by, bx+round(s*3*math.Cos(a+d*s*1.4)), by+round(s*3*math.Sin(a+d*s*1.4)))
					}
				}
			}
		}
	},
	"filament": func(c *canvas, x, y int) { // a spool from the side: two flanges, the wound filament, a strand
		for py := 5; py < 19; py++ { // the filament on the spool: diagonal turns
			for px := 5; px < 17; px++ {
				c.set(x+px, y+py, (px+py)%3 != 0)
			}
		}
		for _, fx := range []int{1, 17} { // the flanges, round at the ends
			c.fill(x+fx, y+2, x+fx+4, y+22, true)
			c.set(x+fx, y+2, false)
			c.set(x+fx+3, y+2, false)
			c.set(x+fx, y+21, false)
			c.set(x+fx+3, y+21, false)
		}
		c.line(x+15, y+5, x+20, y+0) // the strand off the top of the spool
		c.line(x+20, y+0, x+23, y+0)
	},
	"move": func(c *canvas, x, y int) { // four arrows around a center
		cx, cy := x+12, y+12
		c.fill(cx-2, cy-2, cx+3, cy+3, true)
		for _, d := range [][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}} {
			ex, ey := cx+d[0]*11, cy+d[1]*11
			c.line(cx+d[0]*4, cy+d[1]*4, ex, ey)
			for i := 1; i <= 4; i++ {
				px, py := ex-d[0]*i, ey-d[1]*i
				c.line(px-d[1]*i, py-d[0]*i, px+d[1]*i, py+d[0]*i)
			}
		}
	},
	"maint": func(c *canvas, x, y int) { // a wrench: an open jaw at the top right, a ring at the bottom left
		for i := -1; i <= 1; i++ { // the handle
			c.line(x+6+i, y+17+i, x+15+i, y+8+i)
			c.line(x+6+i+1, y+17+i, x+15+i+1, y+8+i)
		}
		c.disc(x+18, y+6, 5) // the jaw, with the opening to the top right
		c.polygon([][2]int{{17, 6}, {21, 0}, {24, 0}, {24, 3}, {19, 8}}, x, y, false)
		c.disc(x+5, y+18, 4) // the ring
		c.disc(x+5, y+18, 1)
		for _, p := range [][2]int{{5, 18}, {4, 18}, {5, 17}, {6, 18}, {5, 19}} {
			c.set(x+p[0], y+p[1], false)
		}
	},
	"machine": func(c *canvas, x, y int) { // a gear
		gear(c, x+12, y+12, 11, 0.2, 8)
		c.arc(x+12, y+12, 4, 0, 2*math.Pi)
		c.arc(x+12, y+12, 5, 0, 2*math.Pi)
	},
	"pause": func(c *canvas, x, y int) {
		c.fill(x+6, y+3, x+10, y+21, true)
		c.fill(x+14, y+3, x+18, y+21, true)
	},
	"play": func(c *canvas, x, y int) {
		c.polygon([][2]int{{6, 3}, {20, 12}, {6, 21}}, x, y, true)
	},
	"tune": func(c *canvas, x, y int) { // three sliders
		for i, k := range []int{14, 6, 11} {
			lx := x + 5 + i*7
			c.line(lx, y+2, lx, y+22)
			c.fill(lx-2, y+k, lx+3, y+k+4, true)
		}
	},
	"no": func(c *canvas, x, y int) { // a cross in a circle
		c.arc(x+12, y+12, 11, 0, 2*math.Pi)
		c.arc(x+12, y+12, 10, 0, 2*math.Pi)
		for i := 0; i < 2; i++ {
			c.line(x+7+i, y+7, x+17+i, y+17)
			c.line(x+17+i, y+7, x+7+i, y+17)
		}
	},
}

func round(v float64) int { return int(math.Round(v)) }

// arc draws a part of a circle from angle a0 to a1.
func (c *canvas) arc(cx, cy, r int, a0, a1 float64) {
	px, py := -1, -1
	steps := 4 * r * 6
	for i := 0; i <= steps; i++ {
		a := a0 + (a1-a0)*float64(i)/float64(steps)
		x, y := cx+round(float64(r)*math.Cos(a)), cy+round(float64(r)*math.Sin(a))
		if px >= 0 {
			c.line(px, py, x, y)
		}
		px, py = x, y
	}
}

// disc fills a circle.
func (c *canvas) disc(cx, cy, r int) {
	for y := -r; y <= r; y++ {
		for x := -r; x <= r; x++ {
			if x*x+y*y <= r*r+r/2 {
				c.set(cx+x, cy+y, true)
			}
		}
	}
}

// polygon fills a polygon (on) or clears it (off), its points at x, y.
func (c *canvas) polygon(pts [][2]int, x, y int, on bool) {
	minY, maxY := 1<<30, -1
	for _, p := range pts {
		minY, maxY = min(minY, p[1]), max(maxY, p[1])
	}
	for py := minY; py <= maxY; py++ {
		fy := float64(py) + 0.5
		var xs []float64
		for i := range pts {
			a, b := pts[i], pts[(i+1)%len(pts)]
			if (float64(a[1]) <= fy) != (float64(b[1]) <= fy) {
				xs = append(xs, float64(a[0])+(fy-float64(a[1]))*float64(b[0]-a[0])/float64(b[1]-a[1]))
			}
		}
		for i := 0; i+1 < len(xs); i += 2 {
			x0, x1 := min(xs[i], xs[i+1]), max(xs[i], xs[i+1])
			for px := round(x0); px <= round(x1); px++ {
				c.set(x+px, y+py, on)
			}
		}
	}
	if !on {
		return
	}
	for i := range pts { // the edge, for a clean outline
		a, b := pts[i], pts[(i+1)%len(pts)]
		c.line(x+a[0], y+a[1], x+b[0], y+b[1])
	}
}

// bigIcon draws the large icon of a name at x, y, or the small icon at 3 times its size.
func (c *canvas) bigIcon(name string, x, y int) {
	if f, ok := icons24[name]; ok {
		f(c, x, y)
		return
	}
	c.icon(name, x, y, 3)
}
