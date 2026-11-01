// Package modeldiff compares two versions of a model (STL): the sizes, the volumes, and the
// volume that was added and removed. It is the comparison of the plan page (viewer3d.js:
// modelDiff), for the comments of the service on GitHub, with a picture of the change.
package modeldiff

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ReadSTL returns the triangles of a binary or text STL file: 9 numbers for each triangle.
func ReadSTL(data []byte) ([]float64, error) {
	if len(data) >= 84 {
		n := int(binary.LittleEndian.Uint32(data[80:]))
		if len(data) == 84+50*n {
			t := make([]float64, 0, n*9)
			for i := 0; i < n; i++ {
				for k := 0; k < 9; k++ {
					t = append(t, float64(math.Float32frombits(binary.LittleEndian.Uint32(data[84+50*i+12+4*k:]))))
				}
			}
			return t, nil
		}
	}
	var t []float64
	for _, m := range vertex.FindAllSubmatch(data, -1) {
		for _, s := range m[1:] {
			v, err := strconv.ParseFloat(string(s), 64)
			if err != nil {
				return nil, fmt.Errorf("modeldiff: a bad vertex: %s", s)
			}
			t = append(t, v)
		}
	}
	if len(t) < 9 {
		return nil, errors.New("modeldiff: no triangles")
	}
	return t[:len(t)/9*9], nil
}

var vertex = regexp.MustCompile(`vertex\s+(\S+)\s+(\S+)\s+(\S+)`)

// box is the box around triangles: x0, y0, z0, x1, y1, z1.
type box [6]float64

func boxOf(t []float64) box {
	b := box{math.Inf(1), math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for i := 0; i < len(t); i += 3 {
		for k := 0; k < 3; k++ {
			b[k] = math.Min(b[k], t[i+k])
			b[k+3] = math.Max(b[k+3], t[i+k])
		}
	}
	return b
}

// volumeOf is the volume of a closed mesh in mm³.
func volumeOf(t []float64) float64 {
	v := 0.0
	for i := 0; i+8 < len(t); i += 9 {
		ax, ay, az, bx, by, bz, cx, cy, cz := t[i], t[i+1], t[i+2], t[i+3], t[i+4], t[i+5], t[i+6], t[i+7], t[i+8]
		v += ax*(by*cz-bz*cy) - ay*(bx*cz-bz*cx) + az*(bx*cy-by*cx)
	}
	return math.Abs(v / 6)
}

// Grid is a grid of cells: its origin O, the size C of a cell, and the counts of the cells.
type Grid struct {
	O          [3]float64
	C          float64
	NX, NY, NZ int
}

// gridFor is a grid around both boxes with about n cells on the longest side.
func gridFor(a, b box, n float64) Grid {
	var lo, hi [3]float64
	for k := 0; k < 3; k++ {
		lo[k], hi[k] = math.Min(a[k], b[k]), math.Max(a[k+3], b[k+3])
	}
	c := math.Max(0.2, math.Max(hi[0]-lo[0], math.Max(hi[1]-lo[1], hi[2]-lo[2]))/n)
	g := Grid{C: c}
	for k := 0; k < 3; k++ {
		g.O[k] = lo[k] - c
	}
	g.NX = int(math.Ceil((hi[0]-lo[0])/c)) + 2
	g.NY = int(math.Ceil((hi[1]-lo[1])/c)) + 2
	g.NZ = int(math.Ceil((hi[2]-lo[2])/c)) + 2
	return g
}

// voxels fills the grid g with the inside of the mesh t, moved by d: for each column of the grid,
// the crossings of the surface along Z in pairs. The cell (x, y, z) is at (x*NY+y)*NZ+z.
func voxels(t []float64, d [3]float64, g Grid) []bool {
	cols := make([][]float64, g.NX*g.NY)
	for i := 0; i+8 < len(t); i += 9 {
		ax, ay, az := t[i]+d[0], t[i+1]+d[1], t[i+2]+d[2]
		bx, by, bz := t[i+3]+d[0], t[i+4]+d[1], t[i+5]+d[2]
		cx, cy, cz := t[i+6]+d[0], t[i+7]+d[1], t[i+8]+d[2]
		den := (by-cy)*(ax-cx) + (cx-bx)*(ay-cy)
		if math.Abs(den) < 1e-12 {
			continue // a triangle on its edge from above
		}
		i0 := max(0, int(math.Ceil((math.Min(ax, math.Min(bx, cx))-g.O[0])/g.C-0.5)))
		i1 := min(g.NX-1, int(math.Floor((math.Max(ax, math.Max(bx, cx))-g.O[0])/g.C-0.5)))
		j0 := max(0, int(math.Ceil((math.Min(ay, math.Min(by, cy))-g.O[1])/g.C-0.5)))
		j1 := min(g.NY-1, int(math.Floor((math.Max(ay, math.Max(by, cy))-g.O[1])/g.C-0.5)))
		for x := i0; x <= i1; x++ {
			px := g.O[0] + (float64(x)+0.5)*g.C
			for y := j0; y <= j1; y++ {
				py := g.O[1] + (float64(y)+0.5)*g.C
				w0 := ((by-cy)*(px-cx) + (cx-bx)*(py-cy)) / den
				w1 := ((cy-ay)*(px-cx) + (ax-cx)*(py-cy)) / den
				w2 := 1 - w0 - w1
				if w0 < 0 || w1 < 0 || w2 < 0 {
					continue
				}
				k := x*g.NY + y
				cols[k] = append(cols[k], w0*az+w1*bz+w2*cz)
			}
		}
	}
	out := make([]bool, g.NX*g.NY*g.NZ)
	for k, zs := range cols {
		if len(zs) < 2 {
			continue
		}
		sort.Float64s(zs)
		for m := 0; m+1 < len(zs); m += 2 {
			z0 := max(0, int(math.Ceil((zs[m]-g.O[2])/g.C-0.5)))
			z1 := min(g.NZ-1, int(math.Floor((zs[m+1]-g.O[2])/g.C-0.5)))
			for z := z0; z <= z1; z++ {
				out[k*g.NZ+z] = true
			}
		}
	}
	return out
}

// Diff is the comparison of an old and a new model.
type Diff struct {
	Same             bool       // the same triangles
	OldSize, NewSize [3]float64 // mm
	OldVolume        float64    // mm³
	NewVolume        float64    // mm³
	Added, Removed   float64    // mm³
	Grid             Grid       // the grid of Old and New
	Old, New         []bool     // the cells of the old model (moved) and of the new model
	Move             [3]float64 // the move of the old model onto the new model
}

// fine and coarse are the counts of cells on the longest side of the grid of the result and of
// the grids that find the move.
const fine, coarse = 140, 48

// Compare compares the old model a and the new model b. The Action saves each model in the
// middle of the bed at Z 0, so the old model goes to the middle, or to the low or the high side
// of each axis, and Compare keeps the place with the fewest cells that differ.
func Compare(a, b []float64) Diff {
	ba, bb := boxOf(a), boxOf(b)
	same := len(a) == len(b)
	for i := 0; same && i < len(a); i++ {
		same = a[i] == b[i]
	}
	best := [3]float64{}
	if !same {
		bestN := math.MaxInt
		opts := func(k int) []float64 { return []float64{0, bb[k] - ba[k], bb[k+3] - ba[k+3]} }
		for _, dx := range opts(0) {
			for _, dy := range opts(1) {
				for _, dz := range []float64{0, bb[5] - ba[5]} {
					d := [3]float64{dx, dy, dz}
					g := gridFor(moved(ba, d), bb, coarse)
					ga, gb := voxels(a, d, g), voxels(b, [3]float64{}, g)
					n := 0
					for i := range ga {
						if ga[i] != gb[i] {
							n++
						}
					}
					if n < bestN {
						bestN, best = n, d
					}
				}
			}
		}
	}
	g := gridFor(moved(ba, best), bb, fine)
	out := Diff{Same: same, Grid: g, Move: best, OldVolume: volumeOf(a), NewVolume: volumeOf(b),
		OldSize: [3]float64{ba[3] - ba[0], ba[4] - ba[1], ba[5] - ba[2]}, NewSize: [3]float64{bb[3] - bb[0], bb[4] - bb[1], bb[5] - bb[2]}}
	out.Old, out.New = voxels(a, best, g), voxels(b, [3]float64{}, g)
	if !same {
		added, removed := 0, 0
		for i := range out.Old {
			switch {
			case out.New[i] && !out.Old[i]:
				added++
			case out.Old[i] && !out.New[i]:
				removed++
			}
		}
		cell := g.C * g.C * g.C
		out.Added, out.Removed = float64(added)*cell, float64(removed)*cell
	}
	return out
}

func moved(b box, d [3]float64) box {
	return box{b[0] + d[0], b[1] + d[1], b[2] + d[2], b[3] + d[0], b[4] + d[1], b[5] + d[2]}
}

// SameShape is the text of a model with no change of its shape.
const SameShape = "The same shape"

// Text is a short text of the changes: "2.0 mm taller · +3.10 cm³ (+4.0%)".
func (d Diff) Text() string {
	if d.Same {
		return SameShape
	}
	var out []string
	words := [3][2]string{{"wider", "narrower"}, {"deeper", "shallower"}, {"taller", "lower"}}
	for k := 0; k < 3; k++ {
		dd := d.NewSize[k] - d.OldSize[k]
		if math.Abs(dd) >= 0.05 {
			w := words[k][1]
			if dd > 0 {
				w = words[k][0]
			}
			out = append(out, fmt.Sprintf("%.1f mm %s", math.Abs(dd), w))
		}
	}
	dv := (d.NewVolume - d.OldVolume) / 1000
	pct := 0.0
	if d.OldVolume > 0 {
		pct = 100 * (d.NewVolume - d.OldVolume) / d.OldVolume
	}
	if math.Abs(dv) >= 0.01 {
		out = append(out, fmt.Sprintf("%s%.2f cm³ (%s%.1f%%)", sign(dv), math.Abs(dv), sign(pct), math.Abs(pct)))
	}
	if len(out) == 0 {
		if m := (d.Added + d.Removed) / 1000; m >= 0.01 {
			return fmt.Sprintf("Same size and volume, %.2f cm³ moved", m)
		}
		return "Small changes of the surface"
	}
	return strings.Join(out, " · ")
}

func sign(v float64) string {
	if v > 0 {
		return "+"
	}
	return "−"
}
