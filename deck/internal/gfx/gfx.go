// Package gfx has the small set of 2D drawing helpers that the UI needs.
package gfx

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"sync"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type Align int

const (
	Left Align = iota
	Center
	Right
)

// Canvas is the logical drawing surface.
type Canvas struct {
	*image.RGBA
}

func New(w, h int) *Canvas { return &Canvas{image.NewRGBA(image.Rect(0, 0, w, h))} }

func (c *Canvas) Fill(r image.Rectangle, col color.Color) {
	draw.Draw(c.RGBA, r.Intersect(c.Bounds()), image.NewUniform(col), image.Point{}, draw.Src)
}

// RoundRect fills a rectangle with anti-aliased corners. A radius of half the height makes a pill.
func (c *Canvas) RoundRect(r image.Rectangle, rad int, col color.RGBA) {
	if rad*2 > r.Dy() {
		rad = r.Dy() / 2
	}
	if rad*2 > r.Dx() {
		rad = r.Dx() / 2
	}
	if rad <= 0 {
		c.Fill(r, col)
		return
	}
	c.Fill(image.Rect(r.Min.X+rad, r.Min.Y, r.Max.X-rad, r.Max.Y), col)
	c.Fill(image.Rect(r.Min.X, r.Min.Y+rad, r.Min.X+rad, r.Max.Y-rad), col)
	c.Fill(image.Rect(r.Max.X-rad, r.Min.Y+rad, r.Max.X, r.Max.Y-rad), col)
	corners := []struct{ cx, cy, x0, y0 int }{
		{r.Min.X + rad, r.Min.Y + rad, r.Min.X, r.Min.Y},
		{r.Max.X - rad, r.Min.Y + rad, r.Max.X - rad, r.Min.Y},
		{r.Min.X + rad, r.Max.Y - rad, r.Min.X, r.Max.Y - rad},
		{r.Max.X - rad, r.Max.Y - rad, r.Max.X - rad, r.Max.Y - rad},
	}
	for _, k := range corners {
		for y := k.y0; y < k.y0+rad; y++ {
			for x := k.x0; x < k.x0+rad; x++ {
				d := math.Hypot(float64(x)+0.5-float64(k.cx), float64(y)+0.5-float64(k.cy))
				a := math.Max(0, math.Min(1, float64(rad)-d+0.5))
				if a > 0 {
					c.blend(x, y, col, a)
				}
			}
		}
	}
}

func (c *Canvas) blend(x, y int, col color.RGBA, a float64) {
	if !(image.Point{x, y}).In(c.Bounds()) {
		return
	}
	o := c.PixOffset(x, y)
	p := c.Pix[o : o+4]
	mix := func(dst, src uint8) uint8 { return uint8(float64(dst)*(1-a) + float64(src)*a + 0.5) }
	p[0], p[1], p[2], p[3] = mix(p[0], col.R), mix(p[1], col.G), mix(p[2], col.B), 255
}

// Triangle fills a triangle without anti-aliasing at the interior and with a soft edge.
func (c *Canvas) Polygon(pts []image.Point, col color.RGBA) {
	minX, minY, maxX, maxY := pts[0].X, pts[0].Y, pts[0].X, pts[0].Y
	for _, p := range pts {
		minX, maxX = min(minX, p.X), max(maxX, p.X)
		minY, maxY = min(minY, p.Y), max(maxY, p.Y)
	}
	const ss = 3
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			n := 0
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					if inside(pts, float64(x)+(float64(sx)+0.5)/ss, float64(y)+(float64(sy)+0.5)/ss) {
						n++
					}
				}
			}
			if n > 0 {
				c.blend(x, y, col, float64(n)/(ss*ss))
			}
		}
	}
}

func inside(pts []image.Point, x, y float64) bool {
	in := false
	for i, j := 0, len(pts)-1; i < len(pts); j, i = i, i+1 {
		xi, yi, xj, yj := float64(pts[i].X), float64(pts[i].Y), float64(pts[j].X), float64(pts[j].Y)
		if (yi > y) != (yj > y) && x < (xj-xi)*(y-yi)/(yj-yi)+xi {
			in = !in
		}
	}
	return in
}

// Cover scales src to fill r, cropping the middle.
func (c *Canvas) Cover(r image.Rectangle, src image.Image) {
	sb := src.Bounds()
	sw, sh := float64(sb.Dx()), float64(sb.Dy())
	dw, dh := float64(r.Dx()), float64(r.Dy())
	var crop image.Rectangle
	if sw/sh > dw/dh {
		w := int(sh * dw / dh)
		crop = image.Rect(sb.Min.X+(sb.Dx()-w)/2, sb.Min.Y, sb.Min.X+(sb.Dx()-w)/2+w, sb.Max.Y)
	} else {
		h := int(sw * dh / dw)
		crop = image.Rect(sb.Min.X, sb.Min.Y+(sb.Dy()-h)/2, sb.Max.X, sb.Min.Y+(sb.Dy()-h)/2+h)
	}
	xdraw.CatmullRom.Scale(c.RGBA, r, src, crop, xdraw.Src, nil)
}

var (
	fontMu sync.Mutex
	fonts  = map[[2]int]font.Face{}
	regF   *opentype.Font
	boldF  *opentype.Font
)

// Face returns a cached Go font face with the given pixel height.
func Face(bold bool, px int) font.Face {
	fontMu.Lock()
	defer fontMu.Unlock()
	if regF == nil {
		regF, _ = opentype.Parse(goregular.TTF)
		boldF, _ = opentype.Parse(gobold.TTF)
	}
	key := [2]int{px, 0}
	f := regF
	if bold {
		key[1], f = 1, boldF
	}
	if face, ok := fonts[key]; ok {
		return face
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: float64(px), DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		panic(err)
	}
	fonts[key] = face
	return face
}

func Width(face font.Face, s string) int { return font.MeasureString(face, s).Ceil() }

// Fit shortens s with an ellipsis until it fits in width.
func Fit(face font.Face, s string, width int) string {
	if Width(face, s) <= width {
		return s
	}
	rs := []rune(s)
	for len(rs) > 0 && Width(face, string(rs)+"…") > width {
		rs = rs[:len(rs)-1]
	}
	return string(rs) + "…"
}

// Text draws s vertically centred in r.
func (c *Canvas) Text(r image.Rectangle, s string, face font.Face, col color.Color, a Align) {
	s = Fit(face, s, r.Dx())
	m := face.Metrics()
	base := r.Min.Y + (r.Dy()+m.Ascent.Ceil()-m.Descent.Ceil())/2
	x := r.Min.X
	switch a {
	case Center:
		x = r.Min.X + (r.Dx()-Width(face, s))/2
	case Right:
		x = r.Max.X - Width(face, s)
	}
	d := font.Drawer{Dst: c.RGBA, Src: image.NewUniform(col), Face: face, Dot: fixed.P(x, base)}
	d.DrawString(s)
}
