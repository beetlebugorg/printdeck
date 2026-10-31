package chitu

import (
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"io"
	"math"
	"sync"

	xdraw "golang.org/x/image/draw"
)

// Model is the part of a .cbddlp file as a grid of cells: about 1 mm across, and the same height,
// from every few layers. The deck draws it as the part looks (Draw), so a page shows what prints.
type Model struct {
	W, H, D int      // the cells across (X), deep (Y) and high (Z)
	cells   []uint64 // a bit for each cell, [z][y][x]
	// layerOf is the first layer of each slice of cells, from 1.
	layerOf []int
	// CellMM is the width of a cell, and SliceMM the height of a slice, in mm.
	CellMM, SliceMM float64

	coarseOnce sync.Once
	coarse     *Model
}

// modelCell is the size of a cell of the model in pixels of the screen of the printer: about
// 0.29 mm on the Phenom. The pictures of the deck come from a model with cells twice as big
// (Coarse).
const modelCell = 4

// ReadModel reads the layers of the .cbddlp file r with the header h into a Model. It reads a
// layer for each modelCell pixels of height.
func ReadModel(r io.ReaderAt, h *Sliced) (*Model, error) {
	if h.Magic != MagicCBDDLP || h.Layers == 0 || h.ResX == 0 || h.ResY == 0 {
		return nil, errors.New("chitu: no layers that the deck reads")
	}
	// The height of a cell: the width of a cell, in mm, from the width of the screen.
	pixelMM := float64(h.BedX) / float64(h.ResX)
	if pixelMM <= 0 || pixelMM > 1 {
		pixelMM = 0.072 // the Phenom: 276 mm over 3840 pixels
	}
	step := 1
	if h.LayerHeight > 0 {
		step = max(1, int(math.Round(pixelMM*modelCell/float64(h.LayerHeight))))
	}
	w := (int(h.ResX) + modelCell - 1) / modelCell
	m := &Model{W: w, CellMM: pixelMM * modelCell, SliceMM: float64(h.LayerHeight) * float64(step)}
	for n := 0; n < int(h.Layers); n += step {
		img, err := LayerImage(r, h, n, w)
		if err != nil {
			return nil, err
		}
		b := img.Bounds()
		if m.H == 0 {
			m.W, m.H = b.Dx(), b.Dy()
		}
		if m.cells == nil {
			m.cells = make([]uint64, 0, (m.W*m.H*int(h.Layers)/step+63)/64+1)
		}
		base := m.D * m.W * m.H
		need := (base + m.W*m.H + 63) / 64
		for len(m.cells) < need {
			m.cells = append(m.cells, 0)
		}
		for y := 0; y < min(m.H, b.Dy()); y++ {
			for x := 0; x < min(m.W, b.Dx()); x++ {
				if img.Pix[y*img.Stride+x] > 0 {
					i := base + y*m.W + x
					m.cells[i/64] |= 1 << (i % 64)
				}
			}
		}
		m.layerOf = append(m.layerOf, n+1)
		m.D++
	}
	if m.D == 0 {
		return nil, errors.New("chitu: no layers")
	}
	return m, nil
}

func (m *Model) at(x, y, z int) bool {
	if x < 0 || y < 0 || z < 0 || x >= m.W || y >= m.H || z >= m.D {
		return false
	}
	i := (z*m.H+y)*m.W + x
	return i/64 < len(m.cells) && m.cells[i/64]&(1<<(i%64)) != 0
}

// set makes the cell (x, y, z) full.
func (m *Model) set(x, y, z int) {
	i := (z*m.H+y)*m.W + x
	for len(m.cells) <= i/64 {
		m.cells = append(m.cells, 0)
	}
	m.cells[i/64] |= 1 << (i % 64)
}

// Draw draws the model w x h pixels big, from the front and the right and over it, with the first
// layer at the bottom as the slicer shows it. The cells of the layers up to done are in the color
// cured, and the others in gray, so the picture shows how far the print is. It draws at twice the
// size and makes the picture smaller, for smooth edges, with a dark line where a surface is in
// front of another.
func (m *Model) Draw(w, h, done int, cured color.RGBA) *image.RGBA {
	const ss = 2 // the supersampling
	W, H := w*ss, h*ss
	big := image.NewRGBA(image.Rect(0, 0, W, H))
	bg := color.RGBA{27, 28, 26, 255}
	for i := 0; i < len(big.Pix); i += 4 {
		big.Pix[i], big.Pix[i+1], big.Pix[i+2], big.Pix[i+3] = bg.R, bg.G, bg.B, 255
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	// The bounds of the part, so the picture shows the part and not the whole plate.
	x0, y0, x1, y1 := m.W, m.H, -1, -1
	for z := 0; z < m.D; z++ {
		for y := 0; y < m.H; y++ {
			for x := 0; x < m.W; x++ {
				if m.at(x, y, z) {
					x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
				}
			}
		}
	}
	if x1 < 0 {
		xdraw.ApproxBiLinear.Scale(out, out.Bounds(), big, big.Bounds(), xdraw.Src, nil)
		return out
	}
	// The view: a turn of 30 degrees about the vertical, and 35 degrees down.
	yaw, pitch := 30*math.Pi/180, 35*math.Pi/180
	cyw, syw, cp, sp := math.Cos(yaw), math.Sin(yaw), math.Cos(pitch), math.Sin(pitch)
	// A slice can be thinner than a cell is wide: the heights go in cells.
	zs := 1.0
	if m.CellMM > 0 && m.SliceMM > 0 {
		zs = m.SliceMM / m.CellMM
	}
	cx, cyy, cz := float64(x0+x1+1)/2, float64(y0+y1+1)/2, float64(m.D)/2
	project := func(x, y, z float64) (sx, sy, depth float64) {
		z *= zs
		// The model: X to the right, Y to the back (the rows of the screen go to the front), Z up.
		// The camera is at the front and over the part: a cell at the back is higher in the
		// picture, and a cell at the front or at the top is nearer.
		px, py, pz := x-cx, cyy-y, z-cz*zs
		rx := px*cyw + py*syw
		ry := -px*syw + py*cyw
		return rx, pz*cp + ry*sp, -ry*cp + pz*sp
	}
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, cx3 := range []float64{float64(x0), float64(x1 + 1)} {
		for _, cy3 := range []float64{float64(y0), float64(y1 + 1)} {
			for _, cz3 := range []float64{0, float64(m.D)} {
				sx, sy, _ := project(cx3, cy3, cz3)
				minX, maxX, minY, maxY = math.Min(minX, sx), math.Max(maxX, sx), math.Min(minY, sy), math.Max(maxY, sy)
			}
		}
	}
	scale := 0.92 * math.Min(float64(W)/(maxX-minX), float64(H)/(maxY-minY))
	ox, oy := float64(W)/2-scale*(minX+maxX)/2, float64(H)/2+scale*(minY+maxY)/2
	zbuf := make([]float32, W*H)
	for i := range zbuf {
		zbuf[i] = float32(math.Inf(-1))
	}
	// The light comes from the front left and from over the part.
	lx, ly, lz := -0.45, -0.55, 0.7
	ln := math.Sqrt(lx*lx + ly*ly + lz*lz)
	lx, ly, lz = lx/ln, ly/ln, lz/ln
	size := int(math.Ceil(scale*math.Max(1, zs)*1.15)) + 1
	gray := color.RGBA{158, 160, 164, 255}
	for z := 0; z < m.D; z++ {
		base := gray
		if m.layerOf[z] <= done {
			base = cured
		}
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				if !m.at(x, y, z) {
					continue
				}
				if m.at(x+1, y, z) && m.at(x-1, y, z) && m.at(x, y+1, z) && m.at(x, y-1, z) && m.at(x, y, z+1) && m.at(x, y, z-1) {
					continue // inside the part
				}
				// The normal: the slope of the cells over the 3 x 3 x 3 cells around, which is
				// smoother than the six neighbours.
				var nx, ny, nz float64
				for dz := -1; dz <= 1; dz++ {
					for dy := -1; dy <= 1; dy++ {
						for dx := -1; dx <= 1; dx++ {
							if (dx != 0 || dy != 0 || dz != 0) && !m.at(x+dx, y+dy, z+dz) {
								nx += float64(dx)
								ny -= float64(dy) // the rows go to the front: -Y
								nz += float64(dz)
							}
						}
					}
				}
				nl := math.Sqrt(nx*nx + ny*ny + nz*nz)
				if nl == 0 {
					nz, nl = 1, 1
				}
				diffuse := math.Max(0, (nx*lx+ny*ly+nz*lz)/nl)
				shade := 0.32 + 0.68*diffuse
				col := color.RGBA{uint8(math.Min(255, float64(base.R)*shade)), uint8(math.Min(255, float64(base.G)*shade)),
					uint8(math.Min(255, float64(base.B)*shade)), 255}
				sx, sy, depth := project(float64(x)+0.5, float64(y)+0.5, float64(z)+0.5)
				px, py := int(ox+sx*scale), int(oy-sy*scale)
				d := float32(depth)
				for qy := py - size/2; qy <= py+size/2; qy++ {
					if qy < 0 || qy >= H {
						continue
					}
					for qx := px - size/2; qx <= px+size/2; qx++ {
						if qx < 0 || qx >= W || zbuf[qy*W+qx] >= d {
							continue
						}
						zbuf[qy*W+qx] = d
						o := big.PixOffset(qx, qy)
						big.Pix[o], big.Pix[o+1], big.Pix[o+2] = col.R, col.G, col.B
					}
				}
			}
		}
	}
	// The outline: a dark line where the depth jumps, at the edge of the part and where one
	// surface is in front of another.
	edge := float32(2.5)
	for y := 1; y < H-1; y++ {
		for x := 1; x < W-1; x++ {
			z := zbuf[y*W+x]
			if math.IsInf(float64(z), -1) {
				continue
			}
			for _, n := range [4]float32{zbuf[y*W+x-1], zbuf[y*W+x+1], zbuf[(y-1)*W+x], zbuf[(y+1)*W+x]} {
				if math.IsInf(float64(n), -1) || z-n > edge {
					o := big.PixOffset(x, y)
					big.Pix[o], big.Pix[o+1], big.Pix[o+2] = big.Pix[o]/3, big.Pix[o+1]/3, big.Pix[o+2]/3
					break
				}
			}
		}
	}
	xdraw.CatmullRom.Scale(out, out.Bounds(), big, big.Bounds(), xdraw.Src, nil)
	return out
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// The mesh of a model for the 3D view of a page (part.bin), little-endian:
//
//	"PDM1", float32 the cell in mm (X and Y), float32 the slice in mm (Z),
//	uint16 W, H, D, uint16 0, uint32 the count of the faces,
//	uint32 the first layer of each of the D slices,
//	then each face: uint16 x, y, z, uint8 side (0 -X, 1 +X, 2 -Y, 3 +Y, 4 -Z, 5 +Z), uint8 n.
//
// A face is a side of the cell (x, y, z) with no cell next to it. A face of the sides 2 to 5 goes
// on along X for n cells. Y goes to the front, as the rows of the screen.

// MeshLimit is the most faces of a mesh. A larger mesh comes from a model with cells twice as big.
const MeshLimit = 150000

// Mesh returns the mesh of the model, with cells of cellMM and slices of sliceMM.
func (m *Model) Mesh(cellMM, sliceMM float64) []byte {
	mm := m
	for {
		faces := mm.faces()
		if len(faces)/8 <= MeshLimit || mm.W < 8 {
			return mm.encode(faces, cellMM, sliceMM)
		}
		mm, cellMM = mm.half(), cellMM*2
	}
}

// half returns the model with cells twice as wide and deep, the same slices.
func (m *Model) half() *Model {
	h := &Model{W: (m.W + 1) / 2, H: (m.H + 1) / 2, D: m.D, layerOf: m.layerOf, CellMM: m.CellMM * 2, SliceMM: m.SliceMM}
	h.cells = make([]uint64, (h.W*h.H*h.D+63)/64)
	for z := 0; z < m.D; z++ {
		for y := 0; y < m.H; y++ {
			for x := 0; x < m.W; x++ {
				if m.at(x, y, z) {
					h.set(x/2, y/2, z)
				}
			}
		}
	}
	return h
}

// faces returns the faces of the mesh, 8 bytes each.
func (m *Model) faces() []byte {
	var out []byte
	put := func(x, y, z, side, n int) {
		out = binary.LittleEndian.AppendUint16(out, uint16(x))
		out = binary.LittleEndian.AppendUint16(out, uint16(y))
		out = binary.LittleEndian.AppendUint16(out, uint16(z))
		out = append(out, byte(side), byte(n))
	}
	dirs := [6][3]int{{-1, 0, 0}, {1, 0, 0}, {0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}}
	for z := 0; z < m.D; z++ {
		for y := 0; y < m.H; y++ {
			for side, d := range dirs {
				for x := 0; x < m.W; x++ {
					if !m.at(x, y, z) || m.at(x+d[0], y+d[1], z+d[2]) {
						continue
					}
					n := 1
					if side >= 2 {
						for n < 255 && m.at(x+n, y, z) && !m.at(x+n+d[0], y+d[1], z+d[2]) {
							n++
						}
					}
					put(x, y, z, side, n)
					x += n - 1
				}
			}
		}
	}
	return out
}

func (m *Model) encode(faces []byte, cellMM, sliceMM float64) []byte {
	out := []byte("PDM1")
	out = binary.LittleEndian.AppendUint32(out, math.Float32bits(float32(cellMM)))
	out = binary.LittleEndian.AppendUint32(out, math.Float32bits(float32(sliceMM)))
	for _, v := range []int{m.W, m.H, m.D, 0} {
		out = binary.LittleEndian.AppendUint16(out, uint16(v))
	}
	out = binary.LittleEndian.AppendUint32(out, uint32(len(faces)/8))
	for _, l := range m.layerOf {
		out = binary.LittleEndian.AppendUint32(out, uint32(l))
	}
	return append(out, faces...)
}

// Coarse returns the model with cells twice as wide and deep, for the pictures of the deck: a
// picture of the fine model takes too long. The model keeps it.
func (m *Model) Coarse() *Model {
	m.coarseOnce.Do(func() { m.coarse = m.half() })
	return m.coarse
}

// Voxels returns the cells of the model for the 3D view of a page (part.bin, before gzip),
// little-endian:
//
//	"PDV1", float32 the cell in mm (X and Y), float32 the slice in mm (Z),
//	uint16 W, H, D, uint16 0, uint32 the first layer of each of the D slices,
//	then the cells: a bit for each cell, in uint32 words, cell i = (z*H + y)*W + x at bit i%32 of
//	word i/32.
//
// The page builds the smooth surface of the cells (the surface net of smooth.go).
func (m *Model) Voxels() []byte {
	out := []byte("PDV1")
	out = binary.LittleEndian.AppendUint32(out, math.Float32bits(float32(m.CellMM)))
	out = binary.LittleEndian.AppendUint32(out, math.Float32bits(float32(m.SliceMM)))
	for _, v := range []int{m.W, m.H, m.D, 0} {
		out = binary.LittleEndian.AppendUint16(out, uint16(v))
	}
	for _, l := range m.layerOf {
		out = binary.LittleEndian.AppendUint32(out, uint32(l))
	}
	n := (m.W*m.H*m.D + 31) / 32
	for i := 0; i < n; i++ {
		w := m.cells[min(i/2, len(m.cells)-1)]
		if i/2 >= len(m.cells) {
			w = 0
		}
		out = binary.LittleEndian.AppendUint32(out, uint32(w>>(32*(i%2))))
	}
	return out
}
