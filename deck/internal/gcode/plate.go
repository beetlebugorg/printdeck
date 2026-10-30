package gcode

import (
	"image"
	"image/color"
	"math"
)

// Plate is the build plate under a print: a slab with its top at z 0, in mm, in the coordinates
// of the G-code.
type Plate struct {
	Min, Max [2]float64 // the top of the slab
	Color    color.RGBA // the color of the slab
	// SheetMin and SheetMax are the print surface on the slab, for example a PEI sheet, in the
	// color Sheet. An empty area has no sheet.
	SheetMin, SheetMax [2]float64
	Sheet              color.RGBA
	// Washers are the centers of the washers of the probe on the slab, with the radius WasherR.
	Washers [][2]float64
	WasherR float64
}

// plateThick is the height of the sides of the slab, in mm.
const plateThick = 6.0

// plateFade is how much darker the back of the plate is than the front.
const plateFade = 0.18

var (
	washerColor = color.RGBA{0xc4, 0xc6, 0xc8, 255}
	screwColor  = color.RGBA{0x30, 0x31, 0x33, 255} // the screw in the hole of a washer
	rimColor    = color.RGBA{0x1c, 0x1d, 0x1e, 255} // the edge of a washer
)

// DrawPlate draws the plate p into img in the view view with the camera cam, on a background that
// goes from the color top at the top of the picture to bottom at the bottom. The top of the plate
// and the two sides that face the viewer have the lights of the model. Each pixel is the mean of
// the colors at its 4 corners, so the edges are smooth.
func DrawPlate(img *image.RGBA, view View, cam Camera, p Plate, top, bottom color.RGBA) {
	b := img.Bounds()
	sinA, cosA, sinE, cosE := view.sinA, view.cosA, view.sinE, view.cosE
	// The shade of each face: the top, the side x = sideX and the side y = sideY. These sides face
	// the viewer.
	shade := func(n [3]float64) float64 {
		dot := func(a, b [3]float64) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
		return 0.36 + 0.6*max(0, dot(n, view.keyLight)) + 0.22*max(0, dot(n, view.fillLight))
	}
	sideX, nx := p.Min[0], -1.0
	if view.viewer[0] > 0 {
		sideX, nx = p.Max[0], 1
	}
	sideY, ny := p.Min[1], -1.0
	if view.viewer[1] > 0 {
		sideY, ny = p.Max[1], 1
	}
	topShade := shade([3]float64{0, 0, 1})
	plate, sheet := lit(p.Color, topShade), lit(p.Sheet, topShade)
	washer, screw, rim := lit(washerColor, topShade), lit(screwColor, topShade), lit(rimColor, topShade)
	left, front := lit(p.Color, shade([3]float64{nx, 0, 0})*0.8), lit(p.Color, shade([3]float64{0, ny, 0})*0.8)
	// The depth of the top of the plate, from the nearest corner to the farthest corner, for the
	// fade.
	d0, d1 := math.Inf(1), math.Inf(-1)
	for _, x := range [2]float64{p.Min[0], p.Max[0]} {
		for _, y := range [2]float64{p.Min[1], p.Max[1]} {
			d := x*sinA + y*cosA
			d0, d1 = min(d0, d), max(d1, d)
		}
	}
	fade := 0.0
	if d1 > d0 {
		fade = plateFade / (d1 - d0)
	}
	hasSheet := p.SheetMax[0] > p.SheetMin[0] && p.SheetMax[1] > p.SheetMin[1]
	r, r2 := p.WasherR, p.WasherR*p.WasherR

	// sample returns the color of the scene at the screen point (u, v), or ok false for the
	// background.
	sample := func(u, v float64) (c [3]float64, ok bool) {
		// The point of the top of the plate (z 0) at (u, v).
		depth := -v / sinE
		x, y := u*cosA+depth*sinA, -u*sinA+depth*cosA
		if x >= p.Min[0] && x <= p.Max[0] && y >= p.Min[1] && y <= p.Max[1] {
			c = plate
			if hasSheet && x >= p.SheetMin[0] && x <= p.SheetMax[0] && y >= p.SheetMin[1] && y <= p.SheetMax[1] {
				c = sheet
			}
			for _, w := range p.Washers {
				dx, dy := x-w[0], y-w[1]
				if dx > r || dx < -r || dy > r || dy < -r {
					continue
				}
				switch d := dx*dx + dy*dy; {
				case d > r2:
					continue
				case d < r2*0.09:
					c = screw
				case d > r2*0.88:
					c = rim
				default:
					c = washer
				}
				break
			}
			f := 1 - fade*(depth-d0)
			return [3]float64{c[0] * f, c[1] * f, c[2] * f}, true
		}
		// The side x = sideX, from z 0 down to -plateThick. A view along the side does not show it.
		if math.Abs(sinA) > 1e-6 {
			sy := (sideX*cosA - u) / sinA
			if sy >= p.Min[1] && sy <= p.Max[1] {
				if z := (-v - (sideX*sinA+sy*cosA)*sinE) / cosE; z <= 0 && z >= -plateThick {
					return left, true
				}
			}
		}
		// The side y = sideY.
		if math.Abs(cosA) > 1e-6 {
			sx := (u + sideY*sinA) / cosA
			if sx >= p.Min[0] && sx <= p.Max[0] {
				if z := (-v - (sx*sinA+sideY*cosA)*sinE) / cosE; z <= 0 && z >= -plateThick {
					return front, true
				}
			}
		}
		return c, false
	}

	// The colors at the corners of the pixels, a row at a time. A pixel is the mean of its 4
	// corners.
	w := b.Dx()
	prev, cur := make([][3]float64, w+1), make([][3]float64, w+1)
	corners := func(row [][3]float64, cy int) {
		t := float64(cy-b.Min.Y) / float64(max(1, b.Dy()))
		bg := lit(mix(top, bottom, min(t, 1)), 1)
		v := (float64(cy) - cam.Y) / cam.Scale
		for i := range row {
			c, ok := sample((float64(b.Min.X+i)-cam.X)/cam.Scale, v)
			if !ok {
				c = bg
			}
			row[i] = c
		}
	}
	corners(prev, b.Min.Y)
	for py := b.Min.Y; py < b.Max.Y; py++ {
		corners(cur, py+1)
		o := img.PixOffset(b.Min.X, py)
		for x := 0; x < w; x++ {
			a, c, d, e := prev[x], prev[x+1], cur[x], cur[x+1]
			i := o + x*4
			img.Pix[i] = uint8((a[0] + c[0] + d[0] + e[0]) / 4)
			img.Pix[i+1] = uint8((a[1] + c[1] + d[1] + e[1]) / 4)
			img.Pix[i+2] = uint8((a[2] + c[2] + d[2] + e[2]) / 4)
			img.Pix[i+3] = 255
		}
		prev, cur = cur, prev
	}
}

// lit is the color c with the brightness f, as floats.
func lit(c color.RGBA, f float64) [3]float64 {
	return [3]float64{math.Min(255, float64(c.R)*f), math.Min(255, float64(c.G)*f), math.Min(255, float64(c.B)*f)}
}
