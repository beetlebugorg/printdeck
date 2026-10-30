package camera

import (
	"image"
	"math"
)

// The crop of a time lapse: the part of the picture with the part of the print, the same in each
// frame of the video.

// partMargin is the space around the projected box of the part in a time lapse crop, as a share
// of its width and its height at each side.
const partMargin = 0.15

// cropGrid is the grid of the edges of a time lapse crop, in pixels.
const cropGrid = 16

// minViewShare is the smallest width of a time lapse crop and of the window of the zoom to the
// part, as a share of the width of the wide picture of the camera (no zoom).
const minViewShare = 0.5

// viewRect grows the rectangle r (x0, y0, x1, y1) in a picture of w x h to the shape of the
// picture, and to the share minW of its width or more, around the middle of r. The plate of the
// bed is a low and wide shape in the picture, so the crop then shows the bed in front of the part
// and behind it. The rectangle stays inside the picture. It is false when r is outside the
// picture.
func viewRect(r [4]float64, w, h, minW float64) ([4]float64, bool) {
	x0, y0, x1, y1 := max(0, r[0]), max(0, r[1]), min(w, r[2]), min(h, r[3])
	if x1 <= x0 || y1 <= y0 {
		return r, false
	}
	cw, ch := max(x1-x0, minW*w), y1-y0
	if cw/ch > w/h {
		ch = cw * h / w
	} else {
		cw = ch * w / h
	}
	cw, ch = min(cw, w), min(ch, h)
	cx := min(max((x0+x1-cw)/2, 0), w-cw)
	cy := min(max((y0+y1-ch)/2, 0), h-ch)
	return [4]float64{cx, cy, cx + cw, cy + ch}, true
}

// PartCrop is the crop of a time lapse of the part in the box b (the toolhead coordinates), with the
// bed at the toolhead Y bedY, for frames of size sz of the camera with the turn rotate. A time
// lapse with a park has the bed at the Y of the park. It is the projection of the box with a
// margin of 15 % of its size at each side, grown to the shape of the picture (viewRect), with its
// edges on a grid of 16 pixels, inside the picture. It is false when a corner of the box is behind
// the camera, or for an empty crop. The frames are the wide frames of the camera (no zoom).
func (c *Calibration) PartCrop(b Box, bedY float64, sz image.Point, rotate int) (image.Rectangle, bool) {
	return c.PartCropAt(b, bedY, sz, rotate, 1)
}

// PartCropAt is PartCrop for frames with the zoom factor zoom of the camera (PTZMap.Window). The
// smallest crop is minViewShare of the wide picture, so it is a larger share of a zoomed frame.
func (c *Calibration) PartCropAt(b Box, bedY float64, sz image.Point, rotate int, zoom float64) (image.Rectangle, bool) {
	if c == nil || sz.X <= 0 || sz.Y <= 0 {
		return image.Rectangle{}, false
	}
	pts, ok := projectAll(func(p [3]float64) (float64, float64, bool) { return c.ProjectAt(p, bedY, sz.X, sz.Y, rotate) }, boxCorners(b))
	if !ok {
		return image.Rectangle{}, false
	}
	return gridCrop(bounds(pts), sz, min(1, minViewShare*max(1, zoom)))
}

// gridCrop is the crop of the projected bounds r (x0, y0, x1, y1) with the margin, in the shape of
// the picture and the share minW of its width or more, on the grid, inside a picture of size sz.
func gridCrop(r [4]float64, sz image.Point, minW float64) (image.Rectangle, bool) {
	mx, my := partMargin*(r[2]-r[0]), partMargin*(r[3]-r[1])
	v, ok := viewRect([4]float64{r[0] - mx, r[1] - my, r[2] + mx, r[3] + my}, float64(sz.X), float64(sz.Y), minW)
	if !ok {
		return image.Rectangle{}, false
	}
	x0 := int(math.Floor(v[0]/cropGrid)) * cropGrid
	y0 := int(math.Floor(v[1]/cropGrid)) * cropGrid
	x1 := int(math.Ceil(v[2]/cropGrid)) * cropGrid
	y1 := int(math.Ceil(v[3]/cropGrid)) * cropGrid
	out := image.Rect(x0, y0, x1, y1).Intersect(image.Rect(0, 0, sz.X, sz.Y))
	return out, !out.Empty()
}

// CropTo returns the part r of the frame, scaled to w x h (even numbers). The frame decodes its
// JPEG at 1/2, 1/4 or 1/8 of its size when that has the pixels of the output. The new frame
// encodes its JPEG when it is asked for.
func (f *Frame) CropTo(r image.Rectangle, w, h int) *Frame {
	r = r.Intersect(image.Rect(0, 0, f.w, f.h))
	if r.Empty() || w < 2 || h < 2 {
		return f
	}
	// The width of the picture that gives the crop w pixels or more.
	pic := f.pictureFor((w*f.w + r.Dx() - 1) / r.Dx())
	if pic == nil {
		return f
	}
	k := float64(pic.Rect.Dx()) / float64(f.w)
	c := [4]float64{float64(r.Min.X) * k, float64(r.Min.Y) * k, float64(r.Max.X) * k, float64(r.Max.Y) * k}
	out := picFrame(cropScale(pic, c, w&^1, h&^1))
	out.Seq, out.Time, out.blank, out.cam = f.Seq, f.Time, f.blank, f.cam
	return out
}
