package ui

import (
	"bytes"
	_ "embed"
	"image"
	"image/draw"
	"image/png"
	"math"
	"sync"

	"printdeck/internal/gfx"
)

// The printer of the start animation: the LulzBot TAZ Workhorse, seen from the front, from the
// outlines of its parts in intro-workhorse.png (lulzbot/intro_art.py makes it from the LulzBot
// files). The outlines are drawn in small dots, as the rest of the display.

//go:embed intro-workhorse.png
var workhorsePNG []byte

// whArt is the picture: the outline of each part, one bool for each pixel of 2 mm.
type whArt struct {
	w, h                   int
	frame, xaxis, bed, box [][]bool
}

var workhorse = sync.OnceValue(func() whArt {
	img, err := png.Decode(bytes.NewReader(workhorsePNG))
	if err != nil {
		return whArt{}
	}
	b := img.Bounds()
	a := whArt{w: b.Dx(), h: b.Dy()}
	plane := func() [][]bool {
		p := make([][]bool, a.h)
		for y := range p {
			p[y] = make([]bool, a.w)
		}
		return p
	}
	a.frame, a.xaxis, a.bed, a.box = plane(), plane(), plane(), plane()
	// The four channels are four pictures, so they are read as they are, not premultiplied.
	n := image.NewNRGBA(b)
	draw.Draw(n, b, img, b.Min, draw.Src)
	for y := range a.h {
		for x := range a.w {
			p := n.Pix[y*n.Stride+x*4:]
			a.frame[y][x], a.xaxis[y][x], a.bed[y][x], a.box[y][x] = p[0] > 127, p[1] > 127, p[2] > 127, p[3] > 127
		}
	}
	return a
})

// whPlace is the place of the printer on the screen: s screen pixels for each pixel of the
// picture, and the screen place of its top left pixel.
type whPlace struct {
	s, ox, oy float64
	dot       float64 // the size of the small dots
}

// workhorsePlace fits the printer on the screen b, with its bed under a word of cols dots.
func workhorsePlace(b image.Rectangle, cols int) whPlace {
	a := workhorse()
	if a.w == 0 {
		return whPlace{s: 1, dot: 3}
	}
	s := min((float64(b.Dx())-32)/float64(a.w), float64(b.Dy())*0.85/float64(a.h))
	p := (whBedRight - whBedLeft) * s / float64(cols+2) // the pitch of the word on the bed
	return whPlace{s: s, ox: float64(b.Min.X) + (float64(b.Dx())-float64(a.w)*s)/2,
		oy: float64(b.Min.Y) + (float64(b.Dy())-float64(a.h)*s)/2, dot: max(2, math.Round(p*0.4))}
}

// at is the screen place of a place in the picture.
func (pl whPlace) at(x, y float64) (float64, float64) { return pl.ox + x*pl.s, pl.oy + y*pl.s }

// The dots of each part, in the coordinates of the picture, for one size of the dots.
var whDots struct {
	sync.Mutex
	s, dot                 float64
	frame, xaxis, bed, box [][2]float64
}

// dots returns the dots of the parts of the picture for a place: the middle of each small dot
// that has a pixel of the outline in it, on a grid of the picture.
func (pl whPlace) dots() (frame, xaxis, bed, box [][2]float64) {
	whDots.Lock()
	defer whDots.Unlock()
	if whDots.s == pl.s && whDots.dot == pl.dot && whDots.frame != nil {
		return whDots.frame, whDots.xaxis, whDots.bed, whDots.box
	}
	a := workhorse()
	step := pl.dot / pl.s // the size of a small dot in pixels of the picture
	sample := func(plane [][]bool) [][2]float64 {
		var out [][2]float64
		for gy := 0.0; gy < float64(a.h); gy += step {
			for gx := 0.0; gx < float64(a.w); gx += step {
				lit := false
				for y := int(gy); y < min(a.h, int(gy+step)+1) && !lit; y++ {
					for x := int(gx); x < min(a.w, int(gx+step)+1); x++ {
						if plane[y][x] {
							lit = true
							break
						}
					}
				}
				if lit {
					out = append(out, [2]float64{gx + step/2, gy + step/2})
				}
			}
		}
		return out
	}
	whDots.s, whDots.dot = pl.s, pl.dot
	whDots.frame, whDots.xaxis, whDots.bed, whDots.box = sample(a.frame), sample(a.xaxis), sample(a.bed), sample(a.box)
	return whDots.frame, whDots.xaxis, whDots.bed, whDots.box
}

// drawPrinter draws the parts that do not move: the frame, the control box with its display and
// its knob, and the outline of the bed. dx is the slide of the printer, fade its fade.
func (pl whPlace) drawPrinter(c *gfx.Canvas, dx, fade float64) {
	frame, _, bed, box := pl.dots()
	frameCol, inkCol := mixRGB(introFrame, introBG, fade), mixRGB(introInk, introBG, fade)
	for _, set := range [][][2]float64{frame, bed, box} {
		for _, p := range set {
			x, y := pl.at(p[0], p[1])
			c.Fill(image.Rect(int(x+dx-pl.dot/2), int(y-pl.dot/2), int(x+dx+pl.dot/2), int(y+pl.dot/2)), frameCol)
		}
	}
	// The display and the knob on the front of the control box, near its top.
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range box {
		minX, minY, maxX, maxY = min(minX, p[0]), min(minY, p[1]), max(maxX, p[0]), max(maxY, p[1])
	}
	if len(box) == 0 {
		return
	}
	bw, bh := maxX-minX, maxY-minY
	l, t := pl.at(minX+bw*0.15, minY+bh*0.06)
	r, bt := pl.at(maxX-bw*0.15, minY+bh*0.16)
	lcd := mixRGB(introBG, introInk, 0.4*(1-fade))
	for y := t; y < bt; y += pl.dot {
		for x := l; x < r; x += pl.dot {
			c.Fill(image.Rect(int(x+dx), int(y), int(x+dx+pl.dot-1), int(y+pl.dot-1)), lcd)
		}
	}
	kx, ky := pl.at(maxX-bw*0.22, minY+bh*0.20)
	c.Fill(image.Rect(int(kx+dx-pl.dot), int(ky-pl.dot), int(kx+dx+pl.dot), int(ky+pl.dot)), inkCol)
}

// rods are the heights of the two smooth rods of the X axis in the picture.
func (pl whPlace) rods() (top, bottom float64) {
	h := whAxisBottom - whAxisTop
	return whAxisTop + 0.35*h, whAxisTop + 0.62*h
}

// drawAxis draws the X axis at the height of the tip of the nozzle at y: its two end blocks, the
// two smooth rods between them, and the green carriage at x. dx is the slide of the printer.
func (pl whPlace) drawAxis(c *gfx.Canvas, x, y, dx, fade float64) {
	_, xaxis, _, _ := pl.dots()
	_, tipY := pl.at(0, whTipY)
	dy := y - tipY // the axis goes up and down with the toolhead
	ink, frame := mixRGB(introInk, introBG, fade), mixRGB(introFrame, introBG, fade)
	for _, p := range xaxis {
		px, py := pl.at(p[0], p[1])
		c.Fill(image.Rect(int(px+dx-pl.dot/2), int(py+dy-pl.dot/2), int(px+dx+pl.dot/2), int(py+dy+pl.dot/2)), frame)
	}
	top, bottom := pl.rods()
	l, _ := pl.at(whAxisLeft, 0)
	r, _ := pl.at(whAxisRight, 0)
	for _, ry := range []float64{top, bottom} {
		_, sy := pl.at(0, ry)
		for rx := l; rx <= r; rx += pl.dot {
			c.Fill(image.Rect(int(rx+dx), int(sy+dy), int(rx+dx+pl.dot), int(sy+dy+pl.dot-1)), ink)
		}
	}
	// The carriage: a green block on the rods, over the hotend.
	_, ct := pl.at(0, top)
	_, cb := pl.at(0, bottom)
	green := mixRGB(introGreen, introBG, fade)
	c.Fill(image.Rect(int(x-6*pl.dot), int(ct+dy-2*pl.dot), int(x+6*pl.dot), int(cb+dy+3*pl.dot)), green)
}

// inletAbove is the height of the top of the carriage over the tip of the nozzle: the filament
// goes in there.
func (pl whPlace) inletAbove() float64 {
	top, _ := pl.rods()
	return (whTipY-top)*pl.s + 2*pl.dot
}
