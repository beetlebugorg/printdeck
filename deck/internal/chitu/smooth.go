package chitu

import (
	"encoding/binary"
	"math"
)

// The smooth mesh of a model for the 3D view of a page (part.bin), little-endian:
//
//	"PDM2", float32 the cell in mm (X and Y), float32 the slice in mm (Z),
//	uint16 W, H, D, uint16 0, uint32 the count of the vertices, uint32 the count of the triangles,
//	then each vertex: uint16 x, y, z in 1/64 of a cell from -1 (so x = v/64 - 1),
//	then each triangle: uint32 a, b, c.
//
// X goes to the right and Y to the front, as the screen of the printer shows the layers, and Z up
// from the first layer. The page colors a vertex by its height: the layers that are done.
//
// The surface is a surface net of the cells, blurred with their 3 x 3 x 3 neighbours: a vertex in
// each box of 8 cells that the surface goes through, at the middle of the points where the
// surface crosses the edges of the box, and a quad for each edge that the surface crosses.

// SmoothLimit is the most vertices of a smooth mesh. A larger mesh comes from a model with cells
// twice as big.
const SmoothLimit = 300000

// SmoothMesh returns the smooth mesh of the model.
func (m *Model) SmoothMesh() []byte {
	mm := m
	for {
		b, ok := mm.smooth(SmoothLimit)
		if ok || mm.W < 8 {
			return b
		}
		mm = mm.half()
	}
}

// smooth builds the mesh. ok is false when it has more than limit vertices.
func (m *Model) smooth(limit int) ([]byte, bool) {
	W, H, D := m.W, m.H, m.D
	// box returns the 3 x 3 count of the full cells around each cell of the slice z.
	box := func(z int) []uint8 {
		out := make([]uint8, W*H)
		if z < 0 || z >= D {
			return out
		}
		for y := 0; y < H; y++ {
			for x := 0; x < W; x++ {
				if !m.at(x, y, z) {
					continue
				}
				for dy := -1; dy <= 1; dy++ {
					for dx := -1; dx <= 1; dx++ {
						if xx, yy := x+dx, y+dy; xx >= 0 && yy >= 0 && xx < W && yy < H {
							out[yy*W+xx]++
						}
					}
				}
			}
		}
		return out
	}
	// The field of a slice: the share of the full cells of the 3 x 3 x 3 cells around.
	boxes := map[int][]uint8{}
	getBox := func(z int) []uint8 {
		if b, ok := boxes[z]; ok {
			return b
		}
		b := box(z)
		boxes[z] = b
		return b
	}
	field := func(z int) []float32 {
		a, b, c := getBox(z-1), getBox(z), getBox(z+1)
		out := make([]float32, W*H)
		for i := range out {
			out[i] = float32(int(a[i])+int(b[i])+int(c[i])) / 27
		}
		return out
	}
	const iso = 0.5
	at := func(f []float32, x, y int) float32 {
		if x < 0 || y < 0 || x >= W || y >= H {
			return 0
		}
		return f[y*W+x]
	}
	var verts []uint16
	var tris []uint32
	// The vertex of each box of the layer of boxes now and of the layer before, or -1.
	cw, ch := W+1, H+1
	prev := make([]int32, cw*ch)
	cur := make([]int32, cw*ch)
	for i := range prev {
		prev[i] = -1
	}
	f0 := field(-1)
	edges := [12][2]int{{0, 1}, {2, 3}, {4, 5}, {6, 7}, {0, 2}, {1, 3}, {4, 6}, {5, 7}, {0, 4}, {1, 5}, {2, 6}, {3, 7}}
	for cz := -1; cz < D; cz++ {
		f1 := field(cz + 1)
		for i := range cur {
			cur[i] = -1
		}
		for cy := -1; cy < H; cy++ {
			for cx := -1; cx < W; cx++ {
				// The 8 corners: bit 0 is +X, bit 1 is +Y, bit 2 is +Z.
				var v [8]float32
				mask := 0
				for k := 0; k < 8; k++ {
					f := f0
					if k&4 != 0 {
						f = f1
					}
					v[k] = at(f, cx+k&1, cy+(k>>1)&1)
					if v[k] >= iso {
						mask |= 1 << k
					}
				}
				if mask == 0 || mask == 255 {
					continue
				}
				// The vertex: the middle of the crossings on the edges.
				var sx, sy, sz float64
				n := 0
				for _, e := range edges {
					a, b := e[0], e[1]
					if (mask>>a)&1 == (mask>>b)&1 {
						continue
					}
					t := float64((iso - v[a]) / (v[b] - v[a]))
					ax, ay, az := float64(a&1), float64((a>>1)&1), float64((a>>2)&1)
					bx, by, bz := float64(b&1), float64((b>>1)&1), float64((b>>2)&1)
					sx += ax + t*(bx-ax)
					sy += ay + t*(by-ay)
					sz += az + t*(bz-az)
					n++
				}
				q := func(base int, off float64) uint16 { return uint16(math.Round((float64(base) + off + 1) * 64)) }
				idx := int32(len(verts) / 3)
				verts = append(verts, q(cx, sx/float64(n)), q(cy, sy/float64(n)), q(cz, sz/float64(n)))
				if len(verts)/3 > limit {
					return nil, false
				}
				ci := (cy+1)*cw + (cx + 1)
				cur[ci] = idx
				// The quads: for each edge from corner 0 that the surface crosses, the four boxes
				// around that edge. The boxes before in X, Y and Z have their vertices already.
				for axis := 0; axis < 3; axis++ {
					// The corner at the end of the edge: 1 (+X), 2 (+Y) or 4 (+Z).
					if mask&1 == (mask>>(1<<axis))&1 {
						continue
					}
					var q [4]int32
					switch axis {
					case 0: // the edge along X: the boxes before in Y and Z
						if cy < 0 || cz < 0 {
							continue
						}
						q = [4]int32{cur[ci], cur[ci-cw], prev[ci-cw], prev[ci]}
					case 1: // along Y: the boxes before in X and Z
						if cx < 0 || cz < 0 {
							continue
						}
						q = [4]int32{cur[ci], prev[ci], prev[ci-1], cur[ci-1]}
					case 2: // along Z: the boxes before in X and Y
						if cx < 0 || cy < 0 {
							continue
						}
						q = [4]int32{cur[ci], cur[ci-1], cur[ci-1-cw], cur[ci-cw]}
					}
					if q[0] < 0 || q[1] < 0 || q[2] < 0 || q[3] < 0 {
						continue
					}
					// The side: the quad faces away from the full corner.
					if mask&1 != 0 {
						q[1], q[3] = q[3], q[1]
					}
					tris = append(tris, uint32(q[0]), uint32(q[1]), uint32(q[2]), uint32(q[0]), uint32(q[2]), uint32(q[3]))
				}
			}
		}
		prev, cur = cur, prev
		f0 = f1
		delete(boxes, cz-1)
	}
	out := []byte("PDM2")
	out = binary.LittleEndian.AppendUint32(out, math.Float32bits(float32(m.CellMM)))
	out = binary.LittleEndian.AppendUint32(out, math.Float32bits(float32(m.SliceMM)))
	for _, v := range []int{W, H, D, 0} {
		out = binary.LittleEndian.AppendUint16(out, uint16(v))
	}
	out = binary.LittleEndian.AppendUint32(out, uint32(len(verts)/3))
	out = binary.LittleEndian.AppendUint32(out, uint32(len(tris)/3))
	for _, v := range verts {
		out = binary.LittleEndian.AppendUint16(out, v)
	}
	for _, t := range tris {
		out = binary.LittleEndian.AppendUint32(out, t)
	}
	return out, true
}
