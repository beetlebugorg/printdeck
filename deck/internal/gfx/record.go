package gfx

import (
	"encoding/json"
	"image"
	"image/color"
	"sync"
)

// A Recording is the drawing commands of one frame, for a display in a browser. The browser draws
// them on a canvas at the pixel density of its screen, so that the text and the shapes are sharp.
// The coordinates are in the units of the canvas (480 x 800 for the deck).
//
// Each command is a JSON array that starts with its name:
//
//	["f", x0, y0, x1, y1, rgb]               fill a rectangle
//	["r", x0, y0, x1, y1, radius, rgb]       fill a rounded rectangle
//	["p", [x, y, x, y, ...], rgb]            fill a polygon
//	["t", x, baseline, text, bold, px, rgb]  draw text (bold is 0 or 1)
//	["c", x0, y0, x1, y1, image]             draw an image that fills the rectangle (the middle)
//	["i", x0, y0, x1, y1, image]             draw an image at its size at x0, y0, in the rectangle
//	["d", divisor]                           make the frame darker (each color / divisor)
//	["clip", x0, y0, x1, y1]                 draw only in the rectangle, until "unclip"
//	["unclip"]
//
// rgb is a number 0xRRGGBB. image is the number of an image (ImageByID).
type Recording struct {
	Ops  []any
	imgs map[int]image.Image // the images of the commands, for DrawScaled
}

func (r *Recording) add(op ...any) { r.Ops = append(r.Ops, op) }

// image registers an image of the recording and returns its number.
func (r *Recording) image(img image.Image) int {
	id := imageID(img)
	if r.imgs == nil {
		r.imgs = map[int]image.Image{}
	}
	r.imgs[id] = img
	return id
}

// JSON returns the commands as a JSON array.
func (r *Recording) JSON() []byte {
	b, _ := json.Marshal(r.Ops)
	return b
}

// NewRecording returns a canvas that records its drawing commands and draws no pixels.
func NewRecording(w, h int) (*Canvas, *Recording) {
	rec := &Recording{}
	return &Canvas{RGBA: &image.RGBA{Rect: image.Rect(0, 0, w, h)}, rec: rec}, rec
}

// Recording returns the recording of a canvas from NewRecording, or nil.
func (c *Canvas) Recording() *Recording { return c.rec }

// raster reports that the canvas has pixels.
func (c *Canvas) raster() bool { return len(c.Pix) > 0 }

func rgb(col color.Color) int {
	r, g, b, _ := col.RGBA()
	return int(r>>8)<<16 | int(g>>8)<<8 | int(b>>8)
}

func rect(r image.Rectangle) []any { return []any{r.Min.X, r.Min.Y, r.Max.X, r.Max.Y} }

// Dim makes the whole frame darker: each color component is divided by div. A dialog draws over
// it.
func (c *Canvas) Dim(div uint8) {
	if c.rec != nil {
		c.rec.add("d", div)
	}
	if !c.raster() {
		return
	}
	for i := 0; i < len(c.Pix); i += 4 {
		c.Pix[i], c.Pix[i+1], c.Pix[i+2] = c.Pix[i]/div, c.Pix[i+1]/div, c.Pix[i+2]/div
	}
}

// Clipped calls draw with a canvas that draws only in r.
func (c *Canvas) Clipped(r image.Rectangle, draw func(*Canvas)) {
	inner := &Canvas{rec: c.rec}
	if c.raster() {
		inner.RGBA = c.RGBA.SubImage(r).(*image.RGBA)
	} else {
		inner.RGBA = &image.RGBA{Rect: r.Intersect(c.Rect)}
	}
	if c.rec != nil {
		c.rec.add(append([]any{"clip"}, rect(r)...)...)
		defer c.rec.add("unclip")
	}
	draw(inner)
}

// The images of the recordings. A browser loads an image by its number. The registry keeps the
// last images only.
var images struct {
	sync.Mutex
	ids   map[image.Image]int
	byID  map[int]image.Image
	order []int
	next  int
}

const keepImages = 32

// imageID returns the number of an image, and registers it.
func imageID(img image.Image) int {
	images.Lock()
	defer images.Unlock()
	if images.ids == nil {
		images.ids, images.byID = map[image.Image]int{}, map[int]image.Image{}
	}
	if id, ok := images.ids[img]; ok {
		return id
	}
	images.next++
	id := images.next
	images.ids[img], images.byID[id] = id, img
	images.order = append(images.order, id)
	if len(images.order) > keepImages {
		old := images.order[0]
		images.order = images.order[1:]
		delete(images.ids, images.byID[old])
		delete(images.byID, old)
	}
	return id
}

// ImageByID returns the image with a number from a recording, or nil.
func ImageByID(id int) image.Image {
	images.Lock()
	defer images.Unlock()
	return images.byID[id]
}

// faceInfo is the weight and the size of each face of Face, for the text commands.
var faceInfo = map[any][2]int{}

// FontRegular and FontBold are the font files of Face, for a browser.
func FontRegular() []byte { return plexRegular }
func FontBold() []byte    { return plexBold }

// Replay draws the commands of a recording (the JSON of Recording.JSON) on a canvas. img returns
// an image by its number, or nil.
func Replay(c *Canvas, ops []byte, img func(id int) image.Image) error {
	return ReplayScaled(c, ops, img, 1)
}

// ReplayScaled is Replay at k times the size: the shapes and the text are drawn at the larger size,
// for sharp pictures, for example in a printed manual. The canvas is k times the size of the
// recording.
func ReplayScaled(c *Canvas, ops []byte, img func(id int) image.Image, k int) error {
	var list []any
	if err := json.Unmarshal(ops, &list); err != nil {
		return err
	}
	replay(c, list, img, k, 1)
	return nil
}

// DrawScaled draws a recording on a canvas at num/den times its size, for example 3/2 for the
// 720 x 1280 Touch Display 2 and a UI of 480 x 853. The text and the shapes are drawn at the
// larger size, so they are sharp.
func (r *Recording) DrawScaled(c *Canvas, num, den int) {
	replay(c, r.Ops, func(id int) image.Image {
		if im, ok := r.imgs[id]; ok {
			return im
		}
		return ImageByID(id)
	}, num, den)
}

// replay draws the commands at num/den times their size. A command is a []any: from JSON its
// numbers are float64, from a Recording in memory they are int or uint8.
func replay(c *Canvas, list []any, img func(id int) image.Image, num, den int) {
	num0 := func(v any) int {
		switch v := v.(type) {
		case float64:
			return int(v)
		case int:
			return v
		case uint8:
			return int(v)
		}
		return 0
	}
	n := func(op []any, i int) int {
		if i < len(op) {
			return num0(op[i])
		}
		return 0
	}
	// sc scales a coordinate. The floor keeps the edges of two shapes that touch together.
	sc := func(v int) int {
		p := v * num
		if p < 0 && p%den != 0 {
			return p/den - 1
		}
		return p / den
	}
	col := func(v int) color.RGBA { return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255} }
	r := func(op []any) image.Rectangle {
		return image.Rect(sc(n(op, 1)), sc(n(op, 2)), sc(n(op, 3)), sc(n(op, 4)))
	}
	var clips []*Canvas
	cur := c
	for _, o := range list {
		op, _ := o.([]any)
		if len(op) == 0 {
			continue
		}
		name, _ := op[0].(string)
		switch name {
		case "f":
			cur.Fill(r(op), col(n(op, 5)))
		case "r":
			cur.RoundRect(r(op), sc(n(op, 5)), col(n(op, 6)))
		case "p":
			var xy []int
			switch v := op[1].(type) {
			case []int:
				xy = v
			case []any:
				for _, e := range v {
					xy = append(xy, num0(e))
				}
			}
			var pts []image.Point
			for i := 0; i+1 < len(xy); i += 2 {
				pts = append(pts, image.Pt(sc(xy[i]), sc(xy[i+1])))
			}
			if len(pts) > 0 {
				cur.Polygon(pts, col(n(op, 2)))
			}
		case "t":
			s, _ := op[3].(string)
			cur.textAt(sc(n(op, 1)), sc(n(op, 2)), s, Face(n(op, 4) == 1, sc(n(op, 5))), col(n(op, 6)))
		case "c", "i":
			im := img(n(op, 5))
			if im == nil {
				continue
			}
			rr := r(op)
			switch {
			case name == "c":
				cur.Cover(rr, im)
			case num == den:
				cur.Image(rr, im)
			case len(op) > 6 && n(op, 6) == 1:
				// A picture of pixels: scale it with the rest, and keep its pixels sharp.
				b := im.Bounds()
				cur.Clipped(rr, func(in *Canvas) { in.Image(rr, scaleNearest(im, sc(b.Dx()), sc(b.Dy()))) })
			default:
				// An image at its size: scale it with the rest.
				b := im.Bounds()
				cur.Clipped(rr, func(in *Canvas) {
					in.Cover(image.Rect(rr.Min.X, rr.Min.Y, rr.Min.X+sc(b.Dx()), rr.Min.Y+sc(b.Dy())), im)
				})
			}
		case "d":
			cur.Dim(uint8(n(op, 1)))
		case "clip":
			clips = append(clips, cur)
			cur = &Canvas{RGBA: cur.RGBA.SubImage(r(op)).(*image.RGBA)}
		case "unclip":
			if len(clips) > 0 {
				cur, clips = clips[len(clips)-1], clips[:len(clips)-1]
			}
		}
	}
}

// scaleNearest scales an image to w x h, each pixel a block of pixels.
func scaleNearest(im image.Image, w, h int) *image.RGBA {
	b := im.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			out.Set(x, y, im.At(b.Min.X+x*b.Dx()/w, b.Min.Y+y*b.Dy()/h))
		}
	}
	return out
}
