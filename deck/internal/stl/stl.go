// Package stl reads STL meshes and draws them as a shaded 3D picture.
package stl

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"io"
	"math"
	"strconv"
	"strings"
)

type Tri struct{ V [3][3]float32 }

type Mesh struct {
	Tris       []Tri
	MinZ, MaxZ float32
}

// Parse reads a binary or ASCII STL.
func Parse(r io.Reader) (*Mesh, error) {
	data, err := io.ReadAll(io.LimitReader(r, 200<<20))
	if err != nil {
		return nil, err
	}
	m := &Mesh{}
	if len(data) >= 84 && 84+int(binary.LittleEndian.Uint32(data[80:]))*50 == len(data) {
		n := int(binary.LittleEndian.Uint32(data[80:]))
		m.Tris = make([]Tri, n)
		for i := 0; i < n; i++ {
			b := data[84+i*50+12:]
			for v := 0; v < 3; v++ {
				for k := 0; k < 3; k++ {
					m.Tris[i].V[v][k] = math.Float32frombits(binary.LittleEndian.Uint32(b[(v*3+k)*4:]))
				}
			}
		}
	} else {
		sc := bufio.NewScanner(bytes.NewReader(data))
		var cur Tri
		n := 0
		for sc.Scan() {
			f := strings.Fields(sc.Text())
			if len(f) == 4 && strings.EqualFold(f[0], "vertex") {
				for k := 0; k < 3; k++ {
					v, _ := strconv.ParseFloat(f[k+1], 32)
					cur.V[n][k] = float32(v)
				}
				if n++; n == 3 {
					m.Tris, n = append(m.Tris, cur), 0
				}
			}
		}
	}
	if len(m.Tris) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	m.MinZ, m.MaxZ = float32(math.Inf(1)), float32(math.Inf(-1))
	for _, t := range m.Tris {
		for _, v := range t.V {
			m.MinZ, m.MaxZ = min(m.MinZ, v[2]), max(m.MaxZ, v[2])
		}
	}
	return m, nil
}

var (
	sinA, cosA = math.Sincos(35 * math.Pi / 180)
	sinE, cosE = math.Sincos(40 * math.Pi / 180)
	// Direction to the light, in model space.
	light = norm([3]float64{-0.4, -0.5, 0.8})
)

func norm(v [3]float64) [3]float64 {
	l := math.Sqrt(v[0]*v[0] + v[1]*v[1] + v[2]*v[2])
	if l == 0 {
		return v
	}
	return [3]float64{v[0] / l, v[1] / l, v[2] / l}
}

// project returns screen coordinates and a depth (larger is nearer to the viewer).
func project(p [3]float32) (u, v, d float64) {
	x, y, z := float64(p[0]), float64(p[1]), float64(p[2])
	u = x*cosA - y*sinA
	depth := x*sinA + y*cosA
	return u, -(depth*sinE + z*cosE), -(depth * cosE) + z*sinE
}

// Render draws the mesh with flat shading. The part below the height zFrac (0..1 of the model height)
// is drawn in the printed colour, the rest in the ghost colour.
func (m *Mesh) Render(w, h int, zFrac float64, printed, ghost color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	minU, minV, maxU, maxV := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, t := range m.Tris {
		for _, p := range t.V {
			u, v, _ := project(p)
			minU, maxU = math.Min(minU, u), math.Max(maxU, u)
			minV, maxV = math.Min(minV, v), math.Max(maxV, v)
		}
	}
	const pad = 8.0
	scale := math.Min((float64(w)-2*pad)/(maxU-minU), (float64(h)-2*pad)/(maxV-minV))
	ox := (float64(w) - (maxU-minU)*scale) / 2
	oy := (float64(h) - (maxV-minV)*scale) / 2
	zbuf := make([]float64, w*h)
	for i := range zbuf {
		zbuf[i] = math.Inf(-1)
	}
	zcut := float64(m.MinZ) + zFrac*float64(m.MaxZ-m.MinZ)
	for _, t := range m.Tris {
		var sx, sy, sd [3]float64
		for i, p := range t.V {
			u, v, d := project(p)
			sx[i], sy[i], sd[i] = ox+(u-minU)*scale, oy+(v-minV)*scale, d
		}
		// Face normal for shading.
		a := [3]float64{float64(t.V[1][0] - t.V[0][0]), float64(t.V[1][1] - t.V[0][1]), float64(t.V[1][2] - t.V[0][2])}
		b := [3]float64{float64(t.V[2][0] - t.V[0][0]), float64(t.V[2][1] - t.V[0][1]), float64(t.V[2][2] - t.V[0][2])}
		n := norm([3]float64{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]})
		lum := math.Abs(n[0]*light[0] + n[1]*light[1] + n[2]*light[2]) // two-sided: meshes differ in winding
		shade := 0.35 + 0.65*lum
		cz := float64(t.V[0][2]+t.V[1][2]+t.V[2][2]) / 3
		base := ghost
		if cz <= zcut {
			base = printed
		}
		col := color.RGBA{uint8(float64(base.R) * shade), uint8(float64(base.G) * shade), uint8(float64(base.B) * shade), 255}
		raster(img, zbuf, sx, sy, sd, col)
	}
	return img
}

func raster(img *image.RGBA, zbuf []float64, sx, sy, sd [3]float64, c color.RGBA) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	minX := int(math.Floor(math.Min(sx[0], math.Min(sx[1], sx[2]))))
	maxX := int(math.Ceil(math.Max(sx[0], math.Max(sx[1], sx[2]))))
	minY := int(math.Floor(math.Min(sy[0], math.Min(sy[1], sy[2]))))
	maxY := int(math.Ceil(math.Max(sy[0], math.Max(sy[1], sy[2]))))
	minX, minY, maxX, maxY = max(minX, 0), max(minY, 0), min(maxX, w-1), min(maxY, h-1)
	den := (sy[1]-sy[2])*(sx[0]-sx[2]) + (sx[2]-sx[1])*(sy[0]-sy[2])
	if math.Abs(den) < 1e-9 {
		return
	}
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			l0 := ((sy[1]-sy[2])*(px-sx[2]) + (sx[2]-sx[1])*(py-sy[2])) / den
			l1 := ((sy[2]-sy[0])*(px-sx[2]) + (sx[0]-sx[2])*(py-sy[2])) / den
			l2 := 1 - l0 - l1
			const e = -0.01
			if l0 < e || l1 < e || l2 < e {
				continue
			}
			d := l0*sd[0] + l1*sd[1] + l2*sd[2]
			if i := y*w + x; d > zbuf[i] {
				zbuf[i] = d
				img.SetRGBA(x, y, c)
			}
		}
	}
}
