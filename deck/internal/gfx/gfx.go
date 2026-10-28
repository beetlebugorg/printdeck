// Package gfx has the small set of 2D drawing helpers that the UI needs.
package gfx

import (
	_ "embed"
	"image"
	"image/color"
	"image/draw"
	"math"
	"sync"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type Align int

const (
	Left Align = iota
	Center
	Right
)

// Canvas is the logical drawing surface. It draws pixels, or records the drawing commands for a
// browser (NewRecording), or both.
type Canvas struct {
	*image.RGBA
	rec *Recording
}

func New(w, h int) *Canvas { return &Canvas{RGBA: image.NewRGBA(image.Rect(0, 0, w, h))} }

func (c *Canvas) Fill(r image.Rectangle, col color.Color) {
	if c.rec != nil {
		c.rec.add(append([]any{"f"}, append(rect(r), rgb(col))...)...)
	}
	if c.raster() {
		draw.Draw(c.RGBA, r.Intersect(c.Bounds()), image.NewUniform(col), image.Point{}, draw.Src)
	}
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
	if c.rec != nil {
		c.rec.add(append([]any{"r"}, append(rect(r), rad, rgb(col))...)...)
		if !c.raster() {
			return
		}
		rec := c.rec
		c.rec = nil // the fills below are part of this command
		defer func() { c.rec = rec }()
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
	if c.rec != nil {
		xy := make([]int, 0, 2*len(pts))
		for _, p := range pts {
			xy = append(xy, p.X, p.Y)
		}
		c.rec.add("p", xy, rgb(col))
	}
	if !c.raster() {
		return
	}
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

// coverCache is the last scaled cover. The cover changes with the track, and the screen draws it
// several times a second.
var coverCache struct {
	sync.Mutex
	src    image.Image
	w, h   int
	scaled *image.RGBA
}

// Cover scales src to fill r, cropping the middle. The scaled image of the last src and size is
// kept, so the next frames only copy it.
func (c *Canvas) Cover(r image.Rectangle, src image.Image) {
	if c.rec != nil {
		c.rec.add(append([]any{"c"}, append(rect(r), c.rec.image(src))...)...)
	}
	if !c.raster() {
		return
	}
	if p, ok := src.(Photo); ok {
		// A picture of a camera at about the size of r: a copy of its middle with no scale. It
		// can be 2 pixels smaller than r, or 1/32 larger.
		pic := p.Picture()
		near := func(n, want int) bool { return n >= want-2 && n <= want+want/32 }
		if pb := pic.Bounds(); near(pb.Dx(), r.Dx()) && near(pb.Dy(), r.Dy()) {
			at := pb.Min.Add(image.Pt(max(0, pb.Dx()-r.Dx())/2, max(0, pb.Dy()-r.Dy())/2))
			draw.Draw(c.RGBA, r, pic, at, draw.Src)
			return
		}
	}
	coverCache.Lock()
	defer coverCache.Unlock()
	if coverCache.src != src || coverCache.w != r.Dx() || coverCache.h != r.Dy() {
		scaled := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
		if p, ok := src.(Photo); ok {
			// A picture of a camera changes often: a fast scale.
			scaleCoverWith(scaled, scaled.Rect, p.Picture(), xdraw.ApproxBiLinear)
		} else {
			scaleCover(scaled, scaled.Rect, src)
		}
		coverCache.src, coverCache.w, coverCache.h, coverCache.scaled = src, r.Dx(), r.Dy(), scaled
	}
	draw.Draw(c.RGBA, r, coverCache.scaled, image.Point{}, draw.Over) // a picture with a clear background shows what is under it
}

// Image draws img at its size at r.Min, only in r.
func (c *Canvas) Image(r image.Rectangle, img image.Image) {
	c.image(r, img, false)
}

// PixelImage draws an image of pixels at its size, as Image. A browser or a larger replay that
// scales it keeps its pixels sharp: the printer display, a QR code.
func (c *Canvas) PixelImage(r image.Rectangle, img image.Image) {
	c.image(r, img, true)
}

func (c *Canvas) image(r image.Rectangle, img image.Image, pixels bool) {
	if c.rec != nil {
		op := append([]any{"i"}, append(rect(r), c.rec.image(img))...)
		if pixels {
			op = append(op, 1)
		}
		c.rec.add(op...)
	}
	if c.raster() {
		draw.Draw(c.RGBA, image.Rectangle{r.Min, r.Min.Add(img.Bounds().Size())}.Intersect(r), img, img.Bounds().Min, draw.Over)
	}
}

// Photo is a picture of a camera, as a JPEG. A browser loads the JPEG, and Cover draws the
// Picture with a fast scale.
type Photo interface {
	image.Image
	JPEG() []byte
	Picture() image.Image
}

// scaleCover scales src to fill r of dst, cropping the middle.
func scaleCover(dst *image.RGBA, r image.Rectangle, src image.Image) {
	scaleCoverWith(dst, r, src, xdraw.CatmullRom)
}

// scaleCoverWith is scaleCover with the scaler s.
func scaleCoverWith(dst *image.RGBA, r image.Rectangle, src image.Image, s xdraw.Scaler) {
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
	s.Scale(dst, r, src, crop, xdraw.Src, nil)
}

// IBM Plex Mono (SIL Open Font License, fonts/OFL.txt). Go Mono fills in the few symbols that Plex lacks (♪ ▲ ▼).
var (
	//go:embed fonts/IBMPlexMono-Regular.ttf
	plexRegular []byte
	//go:embed fonts/IBMPlexMono-SemiBold.ttf
	plexBold []byte
)

var (
	fontMu       sync.Mutex
	fonts        = map[[2]int]font.Face{}
	regF, boldF  *opentype.Font
	fallR, fallB *opentype.Font
)

// finePrint is the setting of SetFinePrint. fontMu protects it.
var finePrint struct{ min, top int }

// SetFinePrint makes Face draw the small sizes larger, for a screen with small pixels. Size 12
// becomes min, the sizes from 12 to top go up in proportion, and top and larger stay the same.
// A min of 0 turns it off.
func SetFinePrint(min, top int) {
	fontMu.Lock()
	finePrint.min, finePrint.top = min, top
	fontMu.Unlock()
}

// FinePrint reports that SetFinePrint makes the small sizes larger.
func FinePrint() bool {
	fontMu.Lock()
	defer fontMu.Unlock()
	return finePrint.min > 12
}

// fineSize returns the size of Face for the size px. The caller holds fontMu.
func fineSize(px int) int {
	lo, top := 12, finePrint.top
	if finePrint.min <= lo || px >= top || top <= lo {
		return px
	}
	return max(px, finePrint.min+(px-lo)*(top-finePrint.min)/(top-lo))
}

// Face returns a cached monospace face with the given pixel height. SetFinePrint can make the
// face larger.
func Face(bold bool, px int) font.Face {
	fontMu.Lock()
	defer fontMu.Unlock()
	return face(bold, fineSize(px))
}

// face returns a cached face of exactly px. The caller holds fontMu.
func face(bold bool, px int) font.Face {
	if regF == nil {
		regF, _ = opentype.Parse(plexRegular)
		boldF, _ = opentype.Parse(plexBold)
		fallR, _ = opentype.Parse(gomono.TTF)
		fallB, _ = opentype.Parse(gomonobold.TTF)
	}
	key := [2]int{px, 0}
	f, fb := regF, fallR
	if bold {
		key[1], f, fb = 1, boldF, fallB
	}
	if face, ok := fonts[key]; ok {
		return face
	}
	opts := &opentype.FaceOptions{Size: float64(px), DPI: 72, Hinting: font.HintingFull}
	primary, err := opentype.NewFace(f, opts)
	if err != nil {
		panic(err)
	}
	fallback, _ := opentype.NewFace(fb, opts)
	face := fallbackFace{primary, fallback}
	fonts[key] = face
	faceInfo[face] = key
	return face
}

// fallbackFace draws each character with the primary face, or with the fallback face if the primary
// does not have it.
type fallbackFace struct{ primary, fallback font.Face }

func (f fallbackFace) pick(r rune) font.Face {
	if _, ok := f.primary.GlyphAdvance(r); ok {
		return f.primary
	}
	return f.fallback
}

func (f fallbackFace) Close() error { return nil }
func (f fallbackFace) Glyph(dot fixed.Point26_6, r rune) (image.Rectangle, image.Image, image.Point, fixed.Int26_6, bool) {
	return f.pick(r).Glyph(dot, r)
}
func (f fallbackFace) GlyphBounds(r rune) (fixed.Rectangle26_6, fixed.Int26_6, bool) {
	return f.pick(r).GlyphBounds(r)
}
func (f fallbackFace) GlyphAdvance(r rune) (fixed.Int26_6, bool) { return f.pick(r).GlyphAdvance(r) }
func (f fallbackFace) Kern(r0, r1 rune) fixed.Int26_6 {
	if f.pick(r0) == f.primary && f.pick(r1) == f.primary {
		return f.primary.Kern(r0, r1)
	}
	return 0
}
func (f fallbackFace) Metrics() font.Metrics { return f.primary.Metrics() }

func Width(face font.Face, s string) int { return font.MeasureString(face, s).Ceil() }

// Shrink returns the largest size of face, down to three quarters of its size, in which s fits in
// width. A face that SetFinePrint made larger goes down to six sevenths of its size only, so that
// the small text stays large. It returns face when s fits, or when no size fits, or for a face
// that Face did not make.
func Shrink(f0 font.Face, s string, width int) font.Face {
	if Width(f0, s) <= width {
		return f0
	}
	fontMu.Lock()
	info, ok := faceInfo[f0]
	fontMu.Unlock()
	if !ok {
		return f0
	}
	px, bold := info[0], info[1] == 1
	floor := max(10, px*3/4)
	fontMu.Lock()
	if finePrint.min > 12 && px >= finePrint.min && px < finePrint.top {
		floor = px * 6 / 7
	}
	fontMu.Unlock()
	for size := px - 1; size >= floor; size-- {
		fontMu.Lock()
		f := face(bold, size)
		fontMu.Unlock()
		if Width(f, s) <= width {
			return f
		}
	}
	return f0
}

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

// textAt draws s with its baseline at y, from x, for the replay of a recording.
func (c *Canvas) textAt(x, y int, s string, face font.Face, col color.Color) {
	if !c.raster() {
		return
	}
	d := font.Drawer{Dst: c.RGBA, Src: image.NewUniform(col), Face: face, Dot: fixed.P(x, y)}
	d.DrawString(s)
}

// Text draws s vertically centred in r. A text that is wider than r gets a smaller size of its
// face (Shrink); a text that is still too wide ends in an ellipsis (Fit).
func (c *Canvas) Text(r image.Rectangle, s string, face font.Face, col color.Color, a Align) {
	face = Shrink(face, s, r.Dx())
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
	if c.rec != nil {
		fontMu.Lock()
		info, ok := faceInfo[face]
		fontMu.Unlock()
		if ok {
			c.rec.add("t", x, base, s, info[1], info[0], rgb(col))
		}
	}
	if !c.raster() {
		return
	}
	d := font.Drawer{Dst: c.RGBA, Src: image.NewUniform(col), Face: face, Dot: fixed.P(x, base)}
	d.DrawString(s)
}
