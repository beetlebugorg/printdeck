package modeldiff

import (
	"image"
	"image/color"
	"math"
)

// The colors of the picture.
var (
	Background = color.RGBA{0xf6, 0xf8, 0xfa, 0xff}
	kept       = [3]float64{0x9a, 0xa0, 0xa8} // the model with no change
	added      = [3]float64{0x2d, 0xa4, 0x4e} // new material
	removed    = [3]float64{0xd1, 0x24, 0x2f} // material that is gone
)

// The faces of a cell that the camera sees, from the front, the right and above (the camera is
// at +X, -Y, +Z), with their light.
var faces = [3]struct {
	n     [3]int
	light float64
}{
	{[3]int{0, 0, 1}, 1.0},   // the top
	{[3]int{1, 0, 0}, 0.78},  // the right side
	{[3]int{0, -1, 0}, 0.62}, // the front
}

// Render draws the new model and the change in an isometric view, w x h pixels: the model with no
// change in gray, the added material in green, and the removed material in red.
func (d Diff) Render(w, h int) *image.RGBA {
	const ss = 2 // samples on each side of a pixel
	W, H := w*ss, h*ss
	g := d.Grid
	at := func(x, y, z int) int { return (x*g.NY+y)*g.NZ + z }
	kind := func(i int) int { // 0 none, 1 kept, 2 added, 3 removed
		switch {
		case d.New[i] && (d.Same || d.Old[i]):
			return 1
		case d.New[i]:
			return 2
		case d.Old[i] && !d.Same:
			return 3
		}
		return 0
	}
	solid := func(x, y, z int) bool {
		if x < 0 || y < 0 || z < 0 || x >= g.NX || y >= g.NY || z >= g.NZ {
			return false
		}
		return kind(at(x, y, z)) != 0
	}
	// The camera: from +X, -Y, +Z to the model.
	view := norm([3]float64{-1, 1, -1})
	right := norm(cross(view, [3]float64{0, 0, 1}))
	up := cross(right, view)
	proj := func(p [3]float64) (float64, float64, float64) {
		return dot(p, right), dot(p, up), dot(p, view)
	}
	// The box of the cells on the screen, for the scale.
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for i := 0; i < len(d.New); i++ {
		if kind(i) == 0 {
			continue
		}
		x, y, z := i/(g.NY*g.NZ), i/g.NZ%g.NY, i%g.NZ
		for _, c := range [][3]float64{{0, 0, 0}, {1, 1, 1}, {1, 0, 0}, {0, 1, 0}, {0, 0, 1}, {1, 1, 0}, {1, 0, 1}, {0, 1, 1}} {
			sx, sy, _ := proj([3]float64{float64(x) + c[0], float64(y) + c[1], float64(z) + c[2]})
			minX, maxX = math.Min(minX, sx), math.Max(maxX, sx)
			minY, maxY = math.Min(minY, sy), math.Max(maxY, sy)
		}
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = [4]uint8{Background.R, Background.G, Background.B, 0xff}[i%4]
	}
	if math.IsInf(minX, 1) {
		return img
	}
	pad := 0.06
	scale := math.Min(float64(W)*(1-2*pad)/(maxX-minX), float64(H)*(1-2*pad)/(maxY-minY))
	ox := float64(W)/2 - (minX+maxX)/2*scale
	oy := float64(H)/2 + (minY+maxY)/2*scale
	depth := make([]float64, W*H)
	for i := range depth {
		depth[i] = math.Inf(1)
	}
	col := make([][3]float64, W*H)
	for i := range col {
		col[i] = [3]float64{float64(Background.R), float64(Background.G), float64(Background.B)}
	}
	for i := 0; i < len(d.New); i++ {
		k := kind(i)
		if k == 0 {
			continue
		}
		x, y, z := i/(g.NY*g.NZ), i/g.NZ%g.NY, i%g.NZ
		base := [...][3]float64{{}, kept, added, removed}[k]
		for _, f := range faces {
			if solid(x+f.n[0], y+f.n[1], z+f.n[2]) {
				continue
			}
			// The four corners of the face.
			var q [4][3]float64
			c := [3]float64{float64(x) + 0.5 + 0.5*float64(f.n[0]), float64(y) + 0.5 + 0.5*float64(f.n[1]), float64(z) + 0.5 + 0.5*float64(f.n[2])}
			var u, v [3]float64
			switch {
			case f.n[2] != 0:
				u, v = [3]float64{0.5, 0, 0}, [3]float64{0, 0.5, 0}
			case f.n[0] != 0:
				u, v = [3]float64{0, 0.5, 0}, [3]float64{0, 0, 0.5}
			default:
				u, v = [3]float64{0.5, 0, 0}, [3]float64{0, 0, 0.5}
			}
			for j, s := range [4][2]float64{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}} {
				q[j] = [3]float64{c[0] + s[0]*u[0] + s[1]*v[0], c[1] + s[0]*u[1] + s[1]*v[1], c[2] + s[0]*u[2] + s[1]*v[2]}
			}
			var px, py [4]float64
			for j := range q {
				sx, sy, _ := proj(q[j])
				px[j], py[j] = ox+sx*scale, oy-sy*scale
			}
			_, _, dz := proj(c)
			shade := [3]float64{base[0] * f.light, base[1] * f.light, base[2] * f.light}
			fillQuad(px, py, W, H, func(p int) {
				if dz < depth[p] {
					depth[p], col[p] = dz, shade
				}
			})
		}
	}
	// Down to w x h: the mean of the samples of each pixel.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var s [3]float64
			for dy := 0; dy < ss; dy++ {
				for dx := 0; dx < ss; dx++ {
					c := col[(y*ss+dy)*W+x*ss+dx]
					s[0], s[1], s[2] = s[0]+c[0], s[1]+c[1], s[2]+c[2]
				}
			}
			n := float64(ss * ss)
			img.SetRGBA(x, y, color.RGBA{uint8(s[0] / n), uint8(s[1] / n), uint8(s[2] / n), 0xff})
		}
	}
	return img
}

// fillQuad calls set for each pixel of the convex quad (px, py) in a picture w x h.
func fillQuad(px, py [4]float64, w, h int, set func(int)) {
	y0 := max(0, int(math.Floor(math.Min(math.Min(py[0], py[1]), math.Min(py[2], py[3])))))
	y1 := min(h-1, int(math.Ceil(math.Max(math.Max(py[0], py[1]), math.Max(py[2], py[3])))))
	for y := y0; y <= y1; y++ {
		fy := float64(y) + 0.5
		lo, hi := math.Inf(1), math.Inf(-1)
		for j := 0; j < 4; j++ {
			ax, ay, bx, by := px[j], py[j], px[(j+1)%4], py[(j+1)%4]
			if (ay <= fy && by > fy) || (by <= fy && ay > fy) {
				x := ax + (fy-ay)/(by-ay)*(bx-ax)
				lo, hi = math.Min(lo, x), math.Max(hi, x)
			}
		}
		if lo > hi {
			continue
		}
		for x := max(0, int(math.Ceil(lo-0.5))); x <= min(w-1, int(math.Floor(hi-0.5))); x++ {
			set(y*w + x)
		}
	}
}

func dot(a, b [3]float64) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
func cross(a, b [3]float64) [3]float64 {
	return [3]float64{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}
func norm(a [3]float64) [3]float64 {
	l := math.Sqrt(dot(a, a))
	return [3]float64{a[0] / l, a[1] / l, a[2] / l}
}
