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

// PartCrop is the crop of a time lapse of the part in the box b (the places of the printer), for
// frames of size sz of the camera with the turn rotate. It is the projection of the box with a
// margin of 15 % of its size at each side, with its edges on a grid of 16 pixels, inside the
// picture. It is false when a corner of the box is behind the camera, or for an empty crop.
func (c *Calibration) PartCrop(b Box, sz image.Point, rotate int) (image.Rectangle, bool) {
	if c == nil || sz.X <= 0 || sz.Y <= 0 {
		return image.Rectangle{}, false
	}
	pts, ok := projectAll(func(p [3]float64) (float64, float64, bool) { return c.Project(p, sz.X, sz.Y, rotate) }, boxCorners(b))
	if !ok {
		return image.Rectangle{}, false
	}
	return gridCrop(bounds(pts), sz)
}

// gridCrop is the crop of the projected bounds r (x0, y0, x1, y1) with the margin, on the grid,
// inside a picture of size sz.
func gridCrop(r [4]float64, sz image.Point) (image.Rectangle, bool) {
	mx, my := partMargin*(r[2]-r[0]), partMargin*(r[3]-r[1])
	x0 := int(math.Floor((r[0]-mx)/cropGrid)) * cropGrid
	y0 := int(math.Floor((r[1]-my)/cropGrid)) * cropGrid
	x1 := int(math.Ceil((r[2]+mx)/cropGrid)) * cropGrid
	y1 := int(math.Ceil((r[3]+my)/cropGrid)) * cropGrid
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
