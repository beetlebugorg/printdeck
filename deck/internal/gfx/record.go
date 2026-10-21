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
	Ops []any
}

func (r *Recording) add(op ...any) { r.Ops = append(r.Ops, op) }

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
	var list [][]any
	if err := json.Unmarshal(ops, &list); err != nil {
		return err
	}
	n := func(op []any, i int) int {
		if i < len(op) {
			if f, ok := op[i].(float64); ok {
				return int(f)
			}
		}
		return 0
	}
	col := func(v int) color.RGBA { return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255} }
	r := func(op []any) image.Rectangle { return image.Rect(n(op, 1), n(op, 2), n(op, 3), n(op, 4)) }
	var clips []*Canvas
	cur := c
	for _, op := range list {
		if len(op) == 0 {
			continue
		}
		name, _ := op[0].(string)
		switch name {
		case "f":
			cur.Fill(r(op), col(n(op, 5)))
		case "r":
			cur.RoundRect(r(op), n(op, 5), col(n(op, 6)))
		case "p":
			xy, _ := op[1].([]any)
			var pts []image.Point
			for i := 0; i+1 < len(xy); i += 2 {
				x, _ := xy[i].(float64)
				y, _ := xy[i+1].(float64)
				pts = append(pts, image.Pt(int(x), int(y)))
			}
			if len(pts) > 0 {
				cur.Polygon(pts, col(n(op, 2)))
			}
		case "t":
			s, _ := op[3].(string)
			cur.textAt(n(op, 1), n(op, 2), s, Face(n(op, 4) == 1, n(op, 5)), col(n(op, 6)))
		case "c", "i":
			if im := img(n(op, 5)); im != nil {
				if name == "c" {
					cur.Cover(r(op), im)
				} else {
					cur.Image(r(op), im)
				}
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
	return nil
}
