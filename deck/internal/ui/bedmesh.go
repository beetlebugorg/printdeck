package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"sort"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/kit"
)

// The Bed mesh page (Maintenance > Bed mesh): the last bed mesh of Klipper as a 3D picture of the
// bed, seen from the front left and above. The heights are larger than real, so that the curve of
// the bed shows. The colors go from blue (low) through grey to orange (high), around the middle of
// the range. Probe runs BED_MESH_CALIBRATE on the full bed.

// meshPic is the 3D picture of one mesh at one size, so that the deck makes it again only for a
// new mesh, a new size or a new theme.
type meshPic struct {
	mesh  *printer.BedMesh
	size  image.Point
	bg    color.RGBA
	img   *image.RGBA
	at    image.Rectangle // where the picture goes on the screen
	front image.Point     // the middle of the front edge of the bed, on the screen
	exag  float64         // how many times higher than real the heights are
}

// meshLow and meshHigh are the colors of the lowest and the highest point.
var (
	meshLow  = color.RGBA{0x2f, 0x7f, 0xe0, 255}
	meshHigh = color.RGBA{0xff, 0x7a, 0x30, 255}
	meshMid  = color.RGBA{0x8a, 0x8a, 0x84, 255}
)

// meshColor is the color of z: meshLow at mid-half, meshMid at mid and meshHigh at mid+half.
func meshColor(z, mid, half float64) color.RGBA {
	t := 0.
	if half > 0 {
		t = math.Max(-1, math.Min(1, (z-mid)/half))
	}
	to := meshHigh
	if t < 0 {
		to, t = meshLow, -t
	}
	mix := func(a, b uint8) uint8 { return uint8(float64(a) + (float64(b)-float64(a))*t + 0.5) }
	return color.RGBA{mix(meshMid.R, to.R), mix(meshMid.G, to.G), mix(meshMid.B, to.B), 255}
}

// meshScale is the middle of the range of m, and half its height. A flat mesh gets a half of
// 0.05 mm, so that it shows grey.
func meshScale(m printer.BedMesh) (mid, half float64) {
	lo, hi := m.Range()
	return (lo + hi) / 2, math.Max((hi-lo)/2, 0.05)
}

// meshArea is the bed area of the picture: the probed area of the full bed ([bed_mesh] of
// printer.cfg) when it has the mesh in it, or else the area of the mesh. A mesh of a smaller area
// then shows in its place on the full bed.
func meshArea(mach printer.Machine, m printer.BedMesh) (lo, hi [2]float64) {
	lo, hi = m.Min, m.Max
	if mach.HasMesh && mach.MeshMin[0] <= lo[0] && mach.MeshMin[1] <= lo[1] && mach.MeshMax[0] >= hi[0] && mach.MeshMax[1] >= hi[1] {
		lo, hi = mach.MeshMin, mach.MeshMax
	}
	return lo, hi
}

// meshPicture is the 3D picture of m for the window r, from the cache when it has it.
func (a *App) meshPicture(r image.Rectangle, mach printer.Machine, m *printer.BedMesh) meshPic {
	in := kit.Inset(r, 10, 10, 10, 24)
	if a.meshPic.mesh == m && a.meshPic.size == in.Size() && a.meshPic.bg == lcd && a.meshPic.at.Min == in.Min {
		return a.meshPic
	}
	lo, hi := meshArea(mach, *m)
	img, front, exag := renderMesh(*m, in.Size(), lo, hi, lcd)
	a.meshPic = meshPic{mesh: m, size: in.Size(), bg: lcd, img: img, at: in, front: front.Add(in.Min), exag: exag}
	return a.meshPic
}

// meshView is the angle of the picture: the bed turns by yaw, and the eye is elev above it.
const (
	meshYaw  = -32 * math.Pi / 180
	meshElev = 34 * math.Pi / 180
)

// meshTri is a triangle of the picture, with its color and its distance from the eye.
type meshTri struct {
	p     [3][2]float64
	col   color.RGBA
	depth float64
}

// renderMesh draws the bed as a slab with the mesh as its top surface, in a picture of the given
// size. The bed area lo to hi is the plate under the slab. front is the middle of the front edge
// of the plate in the picture, and exag is how many times higher than real the heights are.
func renderMesh(m printer.BedMesh, size image.Point, lo, hi [2]float64, back color.RGBA) (*image.RGBA, image.Point, float64) {
	const ss = 2 // the picture is drawn at double size, then scaled down for smooth edges
	w, h := size.X*ss, size.Y*ss
	if w <= 0 || h <= 0 {
		return nil, image.Point{}, 0
	}
	zlo, zhi := m.Range()
	mid, half := meshScale(m)
	span := math.Max(hi[0]-lo[0], hi[1]-lo[1])
	cx, cy := (lo[0]+hi[0])/2, (lo[1]+hi[1])/2
	// The range of the mesh is 22 % of the size of the bed in the picture, and at most 0.2 mm
	// shows as that.
	zs := 0.22 / math.Max(zhi-zlo, 0.2)
	base := zlo - 0.35*math.Max(zhi-zlo, 0.2)
	cosY, sinY, cosE, sinE := math.Cos(meshYaw), math.Sin(meshYaw), math.Cos(meshElev), math.Sin(meshElev)
	// project is the place of a bed point in the picture before the fit, and its distance.
	project := func(x, y, z float64) (float64, float64, float64) {
		X, Y, Z := (x-cx)/span, (y-cy)/span, (z-mid)*zs
		xr, yr := X*cosY-Y*sinY, X*sinY+Y*cosY
		return xr, -(yr*sinE + Z*cosE), yr*cosE - Z*sinE
	}
	// The grid of the surface, over the area of the mesh.
	const n = 28
	gx := func(i int) float64 { return m.Min[0] + (m.Max[0]-m.Min[0])*float64(i)/n }
	gy := func(j int) float64 { return m.Min[1] + (m.Max[1]-m.Min[1])*float64(j)/n }
	var z [n + 1][n + 1]float64
	for j := 0; j <= n; j++ {
		for i := 0; i <= n; i++ {
			z[j][i] = m.At(gx(i), gy(j))
		}
	}
	var tris []meshTri
	add := func(col color.RGBA, pts ...[3]float64) {
		for k := 1; k+1 < len(pts); k++ {
			var t meshTri
			t.col = col
			for q, v := range [][3]float64{pts[0], pts[k], pts[k+1]} {
				px, py, d := project(v[0], v[1], v[2])
				t.p[q] = [2]float64{px, py}
				t.depth += d / 3
			}
			tris = append(tris, t)
		}
	}
	shade := func(col color.RGBA, f float64) color.RGBA {
		k := func(v uint8) uint8 { return uint8(math.Min(255, float64(v)*f)) }
		return color.RGBA{k(col.R), k(col.G), k(col.B), 255}
	}
	// The plate of the bed, under the slab.
	plate := color.RGBA{0x34, 0x34, 0x31, 255}
	pb := base - 0.08*math.Max(zhi-zlo, 0.2)
	add(plate, [3]float64{lo[0], lo[1], pb}, [3]float64{hi[0], lo[1], pb}, [3]float64{hi[0], hi[1], pb}, [3]float64{lo[0], hi[1], pb})
	// The plate is under all the rest: it goes first in the sort.
	for i := range tris {
		tris[i].depth = math.Inf(1)
	}
	// The light comes from the front left, above the bed.
	lx, ly, lz := -0.45, -0.55, 0.70
	for j := 0; j < n; j++ {
		for i := 0; i < n; i++ {
			zc := (z[j][i] + z[j][i+1] + z[j+1][i] + z[j+1][i+1]) / 4
			// The normal of the cell, with the heights as they are in the picture.
			dx := ((z[j][i+1] + z[j+1][i+1]) - (z[j][i] + z[j+1][i])) / 2 * zs / ((gx(i+1) - gx(i)) / span)
			dy := ((z[j+1][i] + z[j+1][i+1]) - (z[j][i] + z[j][i+1])) / 2 * zs / ((gy(j+1) - gy(j)) / span)
			l := math.Sqrt(dx*dx + dy*dy + 1)
			light := math.Max(0, (-dx*lx-dy*ly+lz)/l)
			col := shade(meshColor(zc, mid, half), 0.55+0.6*light)
			add(col, [3]float64{gx(i), gy(j), z[j][i]}, [3]float64{gx(i + 1), gy(j), z[j][i+1]},
				[3]float64{gx(i + 1), gy(j + 1), z[j+1][i+1]}, [3]float64{gx(i), gy(j + 1), z[j+1][i]})
		}
	}
	// The sides of the slab, from the edge of the surface down to the base. From the front left
	// at meshYaw, only the front and the right side show.
	side := func(x0, y0, z0, x1, y1, z1 float64, f float64) {
		col := shade(meshColor((z0+z1)/2, mid, half), f)
		add(col, [3]float64{x0, y0, z0}, [3]float64{x1, y1, z1}, [3]float64{x1, y1, base}, [3]float64{x0, y0, base})
	}
	for k := 0; k < n; k++ {
		side(gx(k), gy(0), z[0][k], gx(k+1), gy(0), z[0][k+1], 0.62)
		side(gx(n), gy(k), z[k][n], gx(n), gy(k+1), z[k+1][n], 0.40)
	}
	// The fit: all the points of the plate and of the slab in the picture, with a margin.
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, t := range tris {
		for _, p := range t.p {
			minX, maxX = math.Min(minX, p[0]), math.Max(maxX, p[0])
			minY, maxY = math.Min(minY, p[1]), math.Max(maxY, p[1])
		}
	}
	scale := math.Min(float64(w)*0.94/(maxX-minX), float64(h)*0.94/(maxY-minY))
	ox := (float64(w) - (maxX-minX)*scale) / 2
	oy := (float64(h) - (maxY-minY)*scale) / 2
	fit := func(p [2]float64) [2]float64 { return [2]float64{ox + (p[0]-minX)*scale, oy + (p[1]-minY)*scale} }
	sort.Slice(tris, func(i, j int) bool { return tris[i].depth > tris[j].depth })
	big := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range big.Pix {
		big.Pix[i] = [4]uint8{back.R, back.G, back.B, 255}[i%4]
	}
	for _, t := range tris {
		fillTri(big, fit(t.p[0]), fit(t.p[1]), fit(t.p[2]), t.col)
	}
	// The probed rows and columns as thin lines on the surface, and a dot on each probed point.
	nx, ny := m.Size()
	pt := func(x, y float64) [2]float64 {
		px, py, _ := project(x, y, m.At(x, y))
		return fit([2]float64{px, py})
	}
	lineCol := color.RGBA{0x18, 0x18, 0x18, 255}
	step := func(i, cnt int, a, b float64) float64 { return a + (b-a)*float64(i)/float64(max(1, cnt-1)) }
	for j := 0; j < ny; j++ {
		y := step(j, ny, m.Min[1], m.Max[1])
		for k := 0; k < n; k++ {
			fillLine(big, pt(gx(k), y), pt(gx(k+1), y), 1.2, lineCol)
		}
	}
	for i := 0; i < nx; i++ {
		x := step(i, nx, m.Min[0], m.Max[0])
		for k := 0; k < n; k++ {
			fillLine(big, pt(x, gy(k)), pt(x, gy(k+1)), 1.2, lineCol)
		}
	}
	for j := 0; j < ny; j++ {
		for i := 0; i < nx; i++ {
			p := pt(step(i, nx, m.Min[0], m.Max[0]), step(j, ny, m.Min[1], m.Max[1]))
			fillDot(big, p, 3.5, color.RGBA{0xf2, 0xf2, 0xee, 255})
		}
	}
	fx, fy, _ := project((lo[0]+hi[0])/2, lo[1], pb)
	f := fit([2]float64{fx, fy})
	return halve(big), image.Pt(int(f[0])/ss, int(f[1])/ss), zs * span
}

// fillTri fills a triangle with no soft edge: the halve of the picture smooths the edges.
func fillTri(img *image.RGBA, a, b, c [2]float64, col color.RGBA) {
	minX := int(math.Floor(math.Min(a[0], math.Min(b[0], c[0]))))
	maxX := int(math.Ceil(math.Max(a[0], math.Max(b[0], c[0]))))
	minY := int(math.Floor(math.Min(a[1], math.Min(b[1], c[1]))))
	maxY := int(math.Ceil(math.Max(a[1], math.Max(b[1], c[1]))))
	r := img.Bounds()
	minX, minY, maxX, maxY = max(minX, r.Min.X), max(minY, r.Min.Y), min(maxX, r.Max.X-1), min(maxY, r.Max.Y-1)
	edge := func(p, q [2]float64, x, y float64) float64 { return (q[0]-p[0])*(y-p[1]) - (q[1]-p[1])*(x-p[0]) }
	area := edge(a, b, c[0], c[1])
	if area == 0 {
		return
	}
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			w0, w1, w2 := edge(b, c, px, py), edge(c, a, px, py), edge(a, b, px, py)
			if (area > 0 && w0 >= 0 && w1 >= 0 && w2 >= 0) || (area < 0 && w0 <= 0 && w1 <= 0 && w2 <= 0) {
				img.SetRGBA(x, y, col)
			}
		}
	}
}

// fillLine draws a line of width wd from a to b.
func fillLine(img *image.RGBA, a, b [2]float64, wd float64, col color.RGBA) {
	dx, dy := b[0]-a[0], b[1]-a[1]
	l := math.Hypot(dx, dy)
	if l == 0 {
		return
	}
	nx, ny := -dy/l*wd/2, dx/l*wd/2
	p := [4][2]float64{{a[0] + nx, a[1] + ny}, {b[0] + nx, b[1] + ny}, {b[0] - nx, b[1] - ny}, {a[0] - nx, a[1] - ny}}
	fillTri(img, p[0], p[1], p[2], col)
	fillTri(img, p[0], p[2], p[3], col)
}

// fillDot draws a round dot with a dark ring.
func fillDot(img *image.RGBA, c [2]float64, rad float64, col color.RGBA) {
	r := img.Bounds()
	for y := int(c[1] - rad - 2); y <= int(c[1]+rad+2); y++ {
		for x := int(c[0] - rad - 2); x <= int(c[0]+rad+2); x++ {
			if !(image.Point{x, y}).In(r) {
				continue
			}
			d := math.Hypot(float64(x)+0.5-c[0], float64(y)+0.5-c[1])
			switch {
			case d <= rad:
				img.SetRGBA(x, y, col)
			case d <= rad+1.5:
				img.SetRGBA(x, y, color.RGBA{0x10, 0x10, 0x10, 255})
			}
		}
	}
}

// halve scales a picture down to half its size: each pixel is the mean of four.
func halve(src *image.RGBA) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx()/2, b.Dy()/2))
	for y := 0; y < dst.Rect.Dy(); y++ {
		for x := 0; x < dst.Rect.Dx(); x++ {
			for k := 0; k < 3; k++ {
				s := 0
				for _, o := range [][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
					s += int(src.Pix[src.PixOffset(2*x+o[0], 2*y+o[1])+k])
				}
				dst.Pix[dst.PixOffset(x, y)+k] = uint8(s / 4)
			}
			dst.Pix[dst.PixOffset(x, y)+3] = 255
		}
	}
	return dst
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
			if mach.Has("z_tilt") {
				// Level the gantry first: the map is then of the bed, with no tilt of the gantry in it.
				lines = append([]string{"Z_TILT_ADJUST"}, lines...)
			}
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
		{"RANGE MM", fmt.Sprintf("%.2f", hi-lo)},
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
	pic := a.meshPicture(mapR, mach, m)
	label(c, foot, fmt.Sprintf("%d × %d points · height × %.0f · %s", nx, ny, pic.exag, fileDate(m.Time.Local(), a.Now(), a.Clock24)), gfx.Center)
	window(c, mapR)
	a.region("Map", mapR)
	if pic.img != nil {
		c.Image(pic.at, pic.img)
		c.Text(image.Rect(pic.front.X-60, pic.front.Y+2, pic.front.X+60, pic.front.Y+20), "FRONT", gfx.Face(true, 12), lcdDim, gfx.Center)
	}
}
