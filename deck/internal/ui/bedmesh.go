package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/kit"
)

// The Bed mesh page (Maintenance > Bed mesh): the map of the last bed mesh of Klipper, seen from
// above with the front of the bed at the bottom. The colors go from blue (low) through grey to
// orange (high), around the middle of the range. Each probed point has its height, from the
// middle. Probe runs BED_MESH_CALIBRATE on the full bed.

// meshImage is the picture of the map of one mesh at one size, so that the deck makes it again
// only for a new mesh, a new size or a new theme.
type meshImage struct {
	mesh *printer.BedMesh
	size image.Point
	bg   color.RGBA
	img  *image.RGBA
}

// meshLow, meshMid and meshHigh are the colors of the lowest point, the middle and the highest
// point of the map.
var (
	meshLow  = color.RGBA{0x2f, 0x7f, 0xe0, 255}
	meshHigh = color.RGBA{0xff, 0x7a, 0x30, 255}
)

func meshMid() color.RGBA { return color.RGBA{0x5a, 0x5a, 0x56, 255} }

// meshColor is the color of z on the map: t is -1 at mid-half, 0 at mid and 1 at mid+half.
func meshColor(z, mid, half float64) color.RGBA {
	t := 0.
	if half > 0 {
		t = math.Max(-1, math.Min(1, (z-mid)/half))
	}
	m := meshMid()
	to := meshHigh
	if t < 0 {
		to, t = meshLow, -t
	}
	mix := func(a, b uint8) uint8 { return uint8(float64(a) + (float64(b)-float64(a))*t + 0.5) }
	return color.RGBA{mix(m.R, to.R), mix(m.G, to.G), mix(m.B, to.B), 255}
}

// meshScale is the middle of the range of m, and half its height. A flat mesh gets a half of
// 0.05 mm, so that it shows grey.
func meshScale(m printer.BedMesh) (mid, half float64) {
	lo, hi := m.Range()
	return (lo + hi) / 2, math.Max((hi-lo)/2, 0.05)
}

// meshArea is the bed area of the map: the probed area of the full bed ([bed_mesh] of
// printer.cfg) when it has the mesh in it, or else the area of the mesh. A mesh of a smaller area
// then shows in its place on the full bed.
func meshArea(mach printer.Machine, m printer.BedMesh) (lo, hi [2]float64) {
	lo, hi = m.Min, m.Max
	if mach.HasMesh && mach.MeshMin[0] <= lo[0] && mach.MeshMin[1] <= lo[1] && mach.MeshMax[0] >= hi[0] && mach.MeshMax[1] >= hi[1] {
		lo, hi = mach.MeshMin, mach.MeshMax
	}
	return lo, hi
}

// mapRect is the largest rectangle in r with the shape of the bed area lo to hi.
func mapRect(r image.Rectangle, lo, hi [2]float64) image.Rectangle {
	w, h := hi[0]-lo[0], hi[1]-lo[1]
	if w <= 0 || h <= 0 {
		return r
	}
	sw, sh := r.Dx(), int(float64(r.Dx())*h/w)
	if sh > r.Dy() {
		sw, sh = int(float64(r.Dy())*w/h), r.Dy()
	}
	return kit.Center(r, sw, sh)
}

// drawMeshImage makes the picture of the map: the mesh in its part of the area lo to hi, and the
// rest of the area in the background color.
func drawMeshImage(m printer.BedMesh, size image.Point, lo, hi [2]float64, back color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rectangle{Max: size})
	mid, half := meshScale(m)
	for py := 0; py < size.Y; py++ {
		y := hi[1] - (float64(py)+0.5)/float64(size.Y)*(hi[1]-lo[1])
		for px := 0; px < size.X; px++ {
			x := lo[0] + (float64(px)+0.5)/float64(size.X)*(hi[0]-lo[0])
			col := back
			if x >= m.Min[0] && x <= m.Max[0] && y >= m.Min[1] && y <= m.Max[1] {
				col = meshColor(m.At(x, y), mid, half)
			}
			img.SetRGBA(px, py, col)
		}
	}
	return img
}

// heightText is a height from the middle of the map, in mm with two decimals and a sign. A height
// that rounds to zero has no minus sign.
func heightText(v float64) string {
	v = math.Round(v*100) / 100
	if v == 0 {
		return "+0.00"
	}
	return fmt.Sprintf("%+.2f", v)
}

// meshSub is the line of the Bed mesh key of the Maintenance page.
func (a *App) meshSub(m *printer.BedMesh) string {
	if m == nil {
		return "Probe the bed and map its height"
	}
	lo, hi := m.Range()
	return fmt.Sprintf("Range %.2f mm · %s", hi-lo, fileDate(m.Time.Local(), a.Now(), a.Clock24))
}

// drawBedMesh is the Bed mesh page.
func (a *App) drawBedMesh(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	f := a.navPage(c, "Bed mesh")
	mach := pr.Machine()
	idle := st.Connected && !st.Busy()
	if mach.Has("bed_mesh") {
		a.headKey(c, f.head, "Probe", func() {
			if !idle {
				a.Toast("Wait until the print ends")
				return
			}
			lines := []string{"BED_MESH_CALIBRATE"}
			if st.Homed != "xyz" {
				lines = append([]string{"G28"}, lines...)
			}
			a.confirm = &confirmBox{question: "Probe the full bed now?", yes: "Probe", onYes: func() { a.runAction("Bed mesh", lines...) }}
		})
	}
	body := f.body()
	m := st.Mesh
	if m == nil {
		noItems(c, body, "No map yet", "Tap Probe to map the bed")
		return
	}
	lo, hi := m.Range()
	nx, ny := m.Size()
	tiles := [][2]string{
		{"RANGE", fmt.Sprintf("%.2f", hi-lo)},
		{"LOWEST", heightText(lo - (lo+hi)/2)},
		{"HIGHEST", heightText(hi - (lo+hi)/2)},
	}
	var mapR, tileR, foot image.Rectangle
	footH := 22
	if gfx.FinePrint() {
		footH = 28
	}
	if f.wide {
		cols := kit.Cols(body, 14, kit.Fr(1), kit.Px(132))
		r := kit.Rows(cols[0], 6, kit.Fr(1), kit.Px(footH))
		mapR, foot = r[0], r[1]
		tileR = kit.Rows(cols[1], 0, kit.Px(3*80+2*12), kit.Fr(1))[0]
	} else {
		r := kit.Rows(kit.Inset(body, 0, 10, 0, 0), 12, kit.Fr(1), kit.Px(footH), kit.Px(84))
		mapR, foot, tileR = r[0], r[1], r[2]
	}
	if f.wide {
		statTiles(c, tileR, 1, tiles)
	} else {
		statTiles(c, tileR, 3, tiles)
	}
	a.region("Range", tileR)
	label(c, foot, fmt.Sprintf("%d × %d points · mm · %s", nx, ny, fileDate(m.Time.Local(), a.Now(), a.Clock24)), gfx.Center)
	a.drawMeshMap(c, mapR, mach, *m, m)
}

// drawMeshMap draws the map of the mesh in a display window in r: the colors, the probed points
// with their heights from the middle of the range, and Back and Front.
func (a *App) drawMeshMap(c *gfx.Canvas, r image.Rectangle, mach printer.Machine, m printer.BedMesh, key *printer.BedMesh) {
	window(c, r)
	a.region("Map", r)
	// The margin at each side has room for half a label of the points at the edge.
	labelH, side := 18, 30
	in := kit.Inset(r, side, labelH+10, side, labelH+10)
	alo, ahi := meshArea(mach, m)
	mr := mapRect(in, alo, ahi)
	size := mr.Size()
	if size.X <= 0 || size.Y <= 0 {
		return
	}
	if a.meshImg.mesh != key || a.meshImg.size != size || a.meshImg.bg != lcd {
		a.meshImg = meshImage{mesh: key, size: size, bg: lcd, img: drawMeshImage(m, size, alo, ahi, lcd)}
	}
	c.Image(mr, a.meshImg.img)
	c.Text(image.Rect(r.Min.X, r.Min.Y+4, r.Max.X, r.Min.Y+4+labelH), "BACK", gfx.Face(true, 12), lcdDim, gfx.Center)
	c.Text(image.Rect(r.Min.X, r.Max.Y-4-labelH, r.Max.X, r.Max.Y-4), "FRONT", gfx.Face(true, 12), lcdDim, gfx.Center)
	// The probed points: a dot and the height from the middle, in hundredths of a mm when the
	// points are close together.
	nx, ny := m.Size()
	mid, _ := meshScale(m)
	toPx := func(x, y float64) image.Point {
		return image.Pt(mr.Min.X+int((x-alo[0])/(ahi[0]-alo[0])*float64(mr.Dx())),
			mr.Min.Y+int((ahi[1]-y)/(ahi[1]-alo[1])*float64(mr.Dy())))
	}
	step := func(i, n int, lo, hi float64) float64 {
		if n < 2 {
			return lo
		}
		return lo + float64(i)*(hi-lo)/float64(n-1)
	}
	cellW := mr.Dx() * int(m.Max[0]-m.Min[0]) / max(1, int(ahi[0]-alo[0])) / max(1, nx-1)
	face := gfx.Face(true, 13)
	if cellW < 52 {
		face = gfx.Face(true, 11)
	}
	for j := 0; j < ny; j++ {
		for i := 0; i < nx; i++ {
			p := toPx(step(i, nx, m.Min[0], m.Max[0]), step(j, ny, m.Min[1], m.Max[1]))
			c.RoundRect(image.Rect(p.X-3, p.Y-3, p.X+3, p.Y+3), 3, color.RGBA{0x10, 0x10, 0x10, 255})
			c.RoundRect(image.Rect(p.X-2, p.Y-2, p.X+2, p.Y+2), 2, color.RGBA{0xf2, 0xf2, 0xee, 255})
			if cellW >= 34 {
				s := heightText(m.Z[j][i] - mid)
				tr := image.Rect(p.X-30, p.Y+3, p.X+30, p.Y+19)
				if j == 0 {
					tr = tr.Sub(image.Pt(0, 22))
				}
				// A dark copy under the text keeps it clear on the orange and the blue.
				c.Text(tr.Add(image.Pt(1, 1)), s, face, color.RGBA{0x10, 0x10, 0x10, 255}, gfx.Center)
				c.Text(tr, s, face, color.RGBA{0xf2, 0xf2, 0xee, 255}, gfx.Center)
			}
		}
	}
}
