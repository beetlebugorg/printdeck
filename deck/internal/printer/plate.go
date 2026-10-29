package printer

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"

	"printdeck/internal/gcode"
	"printdeck/internal/stl"
)

// The picture of the share page: the print on the build plate of the printer.

// Bed is the build plate of a printer, in mm, in the coordinates of the G-code.
type Bed struct {
	Min, Max [2]float64   // the print surface; an empty area when the size is not known
	Washers  [][2]float64 // the centers of the washers of the probe, or nil
	PEI      bool         // the PEI sheet of a TAZ
}

// Known reports that the bed has a size.
func (b Bed) Known() bool { return b.Max[0] > b.Min[0] && b.Max[1] > b.Min[1] }

// The bed of the TAZ Workhorse: 280 x 280 mm. The washers are 4 mm out from the probe points of
// the example config (-10 and 290), as in the simulator (simmcu).
var (
	workhorseBed     = [2]float64{280, 280}
	workhorseWashers = [][2]float64{{-14, -14}, {294, -14}, {294, 294}, {-14, 294}}
)

const (
	washerR     = 11.0 // the radius of a washer of the probe, in mm
	probeMargin = 10.0 // how far the probe points of a TAZ are out from the print surface, in mm
	// meshEdge is the margin around the bed mesh for a printer with no travel of X and Y in
	// printer.cfg: a guess of the size of the bed.
	meshEdge = 10.0
)

// BedOf returns the bed of the printer of m, with model the name of the printer (ConfigModel).
//   - 4 points of [bed_tilt] are the washers of a TAZ: the print surface is a PEI sheet 10 mm
//     inside the points, and each washer is 4 mm out from its point.
//   - A TAZ Workhorse with no [bed_tilt] has the bed of the example config.
//   - Else the print surface is the travel of X and Y from 0, or the area of the bed mesh with a
//     margin.
func BedOf(m Machine, model string) Bed {
	if pts := m.TiltPoints; len(pts) == 4 {
		b := Bed{Min: pts[0], Max: pts[0], PEI: true}
		var c [2]float64
		for _, p := range pts {
			for i := range 2 {
				b.Min[i], b.Max[i] = min(b.Min[i], p[i]), max(b.Max[i], p[i])
				c[i] += p[i] / 4
			}
		}
		for _, p := range pts {
			w := p
			for i := range 2 {
				if p[i] < c[i] {
					w[i] -= 4
				} else if p[i] > c[i] {
					w[i] += 4
				}
			}
			b.Washers = append(b.Washers, w)
		}
		for i := range 2 {
			b.Min[i], b.Max[i] = b.Min[i]+probeMargin, b.Max[i]-probeMargin
		}
		return b
	}
	if strings.Contains(strings.ToLower(model), "taz workhorse") {
		return Bed{Max: workhorseBed, Washers: workhorseWashers, PEI: true}
	}
	b := Bed{Min: [2]float64{max(0, m.MinX), max(0, m.MinY)}, Max: [2]float64{m.MaxX, m.MaxY}}
	if !b.Known() && m.HasMesh {
		b = Bed{Min: [2]float64{m.MeshMin[0] - meshEdge, m.MeshMin[1] - meshEdge}, Max: [2]float64{m.MeshMax[0] + meshEdge, m.MeshMax[1] + meshEdge}}
	}
	if !b.Known() {
		return Bed{}
	}
	return b
}

var (
	peiColor   = color.RGBA{0xe0, 0xb0, 0x68, 255} // the warm gold-tan of a PEI sheet
	metalColor = color.RGBA{0x55, 0x58, 0x5c, 255} // the plate of a TAZ around the PEI sheet
	plainColor = color.RGBA{0x4a, 0x4c, 0x50, 255} // the plate of another printer
	// The background of the share page around the plate: the color of the model box of the page,
	// darker at the bottom.
	shareTop    = color.RGBA{0x24, 0x25, 0x23, 255}
	shareBottom = color.RGBA{0x12, 0x12, 0x11, 255}
)

const (
	frameMargin = 0.35 // the margin around the part, a part of its size
	frameMinMM  = 25.0 // the smallest margin around the part, in mm
	// bigPart is the size of the framed part over the size of the bed from which the picture
	// shows the whole bed.
	bigPart = 0.7
)

// plateOf returns the plate of bed b, and the area of the plate to show in the picture of a part
// in the box part (partOK false for no part).
func plateOf(b Bed, part Box, partOK bool) (p gcode.Plate, lo, hi [2]float64) {
	// The area around the part.
	if partOK {
		m := max(frameMinMM, frameMargin*max(part.Max[0]-part.Min[0], part.Max[1]-part.Min[1]))
		lo, hi = [2]float64{part.Min[0] - m, part.Min[1] - m}, [2]float64{part.Max[0] + m, part.Max[1] + m}
	}
	switch {
	case !b.Known():
		// No size: a plate with its edges far out of the picture.
		if !partOK {
			lo, hi = [2]float64{0, 0}, [2]float64{200, 200}
		}
		p = gcode.Plate{Color: plainColor}
		for i := range 2 {
			p.Min[i], p.Max[i] = lo[i]-1000, hi[i]+1000
		}
		return p, lo, hi
	case b.PEI:
		p = gcode.Plate{Min: b.Min, Max: b.Max, Color: metalColor, SheetMin: b.Min, SheetMax: b.Max, Sheet: peiColor,
			Washers: b.Washers, WasherR: washerR}
		for _, w := range b.Washers {
			for i := range 2 {
				p.Min[i], p.Max[i] = min(p.Min[i], w[i]-washerR-6), max(p.Max[i], w[i]+washerR+6)
			}
		}
	default:
		p = gcode.Plate{Min: b.Min, Max: b.Max, Color: plainColor}
	}
	big := !partOK
	for i := range 2 {
		if hi[i]-lo[i] >= bigPart*(b.Max[i]-b.Min[i]) {
			big = true
		}
	}
	if big {
		// The whole plate, and the part where it is out of the bed.
		for i := range 2 {
			lo[i], hi[i] = p.Min[i], p.Max[i]
			if partOK {
				lo[i], hi[i] = min(lo[i], part.Min[i]), max(hi[i], part.Max[i])
			}
		}
	}
	return p, lo, hi
}

// plateCamera returns the camera of a w x h picture of the area from lo to hi at z 0, and of the
// part in the box part up to the height top.
func plateCamera(w, h int, lo, hi [2]float64, part Box, partOK bool, top float64) gcode.Camera {
	minU, minV, maxU, maxV := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	add := func(x, y, z float64) {
		u, v := gcode.Project(x, y, z)
		minU, maxU = min(minU, u), max(maxU, u)
		minV, maxV = min(minV, v), max(maxV, v)
	}
	for i := range 4 {
		add([2]float64{lo[0], hi[0]}[i%2], [2]float64{lo[1], hi[1]}[i/2], 0)
		if partOK {
			add([2]float64{part.Min[0], part.Max[0]}[i%2], [2]float64{part.Min[1], part.Max[1]}[i/2], top)
		}
	}
	return gcode.Fit(w, h, 4, minU, minV, maxU, maxV)
}

// platePicture draws the print on the plate of bed b, at the size of the preview: the G-code gc,
// or the model mesh when it is not nil. body is the box of the part from its second layer on
// (View.PartBox), with ok false for none. The part is printed up to the file offset pos, and the
// layer that starts at curStart prints now, in the colors of palette. A mesh has the place of
// body on the plate.
func platePicture(gc *gcode.Model, mesh *stl.Mesh, body Box, ok bool, pos, curStart int64, colors []color.RGBA, b Bed) *image.RGBA {
	w, h := PreviewW, PreviewH
	part, top := body, float64(gc.MaxZ)
	var dx, dy, dz float64 // the move of the mesh to its place on the plate
	if mesh != nil && ok {
		lo, hi := [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}, [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
		for _, t := range mesh.Tris {
			for _, v := range t.V {
				for i := range 3 {
					lo[i], hi[i] = min(lo[i], float64(v[i])), max(hi[i], float64(v[i]))
				}
			}
		}
		dx = (body.Min[0]+body.Max[0])/2 - (lo[0]+hi[0])/2
		dy = (body.Min[1]+body.Max[1])/2 - (lo[1]+hi[1])/2
		dz = -lo[2]
		part = Box{Min: [3]float64{lo[0] + dx, lo[1] + dy, 0}, Max: [3]float64{hi[0] + dx, hi[1] + dy, hi[2] + dz}}
		top = part.Max[2]
	}
	p, lo, hi := plateOf(b, part, ok)
	cam := plateCamera(w, h, lo, hi, part, ok, top)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	gcode.DrawPlate(img, cam, p, shareTop, shareBottom)
	if !ok {
		return img
	}
	var model *image.RGBA
	if mesh != nil {
		zTop := float64(gc.ZAt(pos)) / float64(max(gc.MaxZ, 0.001))
		zLow := 0.0
		if curStart > 0 {
			zLow = float64(gc.ZAt(curStart-1)) / float64(max(gc.MaxZ, 0.001))
		}
		if pos < 0 {
			zTop = -1
		}
		u, v := gcode.Project(dx, dy, dz)
		model = mesh.RenderView(w, h, cam.Scale, cam.X+u*cam.Scale, cam.Y+v*cam.Scale, zLow, zTop, colors[0], hotColor, ghostColor)
	} else {
		// The rest of the toolpath has the ghost color of the mesh: pathGhost is for a black
		// background.
		model = gc.RenderView(w, h, cam, pos, curStart, colors, hotColor, ghostColor)
	}
	draw.Draw(img, img.Bounds(), model, image.Point{}, draw.Over)
	return img
}

// SharePicture returns the picture of the print of st on the build plate of bed b, for the share
// page: the part as it is at the file position of st, with a margin of the plate around it. It
// returns nil when the G-code of the print is not loaded, and when no print runs.
func (v *View) SharePicture(st State, b Bed) image.Image {
	v.mu.Lock()
	gc, mesh, hex := v.gc, v.mesh, v.startColor
	if gc == nil || !st.Busy() || v.file != fileKey(st) {
		v.mu.Unlock()
		return nil
	}
	var body Box
	ok := false
	if len(gc.Segs) > 0 {
		v.loadBoxes(gc)
		body, ok = v.boxBody, v.bodyOK
	}
	v.mu.Unlock()
	pos := st.FilePos
	return platePicture(gc, mesh, body, ok, pos, gc.LayerStart(gc.LayerAt(pos)), v.palette(gc, hex), b)
}
