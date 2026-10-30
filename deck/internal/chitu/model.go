package chitu

import (
	"errors"
	"image"
	"image/color"
	"io"
	"math"
)

// Model is the part of a .cbddlp file as a grid of cells: about 1 mm across, and the same height,
// from every few layers. The deck draws it as the part looks (Draw), so a page shows what prints.
type Model struct {
	W, H, D int    // the cells across (X), deep (Y) and high (Z)
	cells   []bool // [z][y][x]
	// layerOf is the first layer of each slice of cells, from 1.
	layerOf []int
}

// modelCell is the size of a cell of the model in pixels of the screen of the printer.
const modelCell = 16

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
	m := &Model{W: w}
	for n := 0; n < int(h.Layers); n += step {
		img, err := LayerImage(r, h, n, w)
		if err != nil {
			return nil, err
		}
		b := img.Bounds()
		if m.H == 0 {
			m.W, m.H = b.Dx(), b.Dy()
		}
		slice := make([]bool, m.W*m.H)
		for y := 0; y < min(m.H, b.Dy()); y++ {
			for x := 0; x < min(m.W, b.Dx()); x++ {
				slice[y*m.W+x] = img.Pix[y*img.Stride+x] > 0
			}
		}
		m.cells = append(m.cells, slice...)
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
	return m.cells[(z*m.H+y)*m.W+x]
}

// Draw draws the model w x h pixels big, from the front and the right and over it, with the first
// layer at the bottom as the slicer shows it. The cells of the layers up to done are in the color
// cured, and the others in gray, so the picture shows how far the print is.
func (m *Model) Draw(w, h, done int, cured color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	bg := color.RGBA{27, 28, 26, 255}
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = bg.R, bg.G, bg.B, 255
	}
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
		return img
	}
	// The view: a turn of 30 degrees about the vertical, and 35 degrees down.
	yaw, pitch := 30*math.Pi/180, 35*math.Pi/180
	cy, sy, cp, sp := math.Cos(yaw), math.Sin(yaw), math.Cos(pitch), math.Sin(pitch)
	cx, cyy, cz := float64(x0+x1)/2, float64(y0+y1)/2, float64(m.D)/2
	project := func(x, y, z float64) (sx, sy2, depth float64) {
		// The model: X to the right, Y to the back (the rows of the screen go to the front), Z up.
		px, py, pz := x-cx, cyy-y, z-cz
		rx := px*cy + py*sy
		ry := -px*sy + py*cy
		// The camera is at the front and over the part: a cell at the back is higher in the
		// picture, and a cell at the front or at the top is nearer.
		return rx, pz*cp + ry*sp, -ry*cp + pz*sp
	}
	// The scale: the corners of the bounds fit in the picture.
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, c := range [][3]float64{{float64(x0), float64(y0), 0}, {float64(x1 + 1), float64(y0), 0}, {float64(x0), float64(y1 + 1), 0},
		{float64(x1 + 1), float64(y1 + 1), 0}, {float64(x0), float64(y0), float64(m.D)}, {float64(x1 + 1), float64(y0), float64(m.D)},
		{float64(x0), float64(y1 + 1), float64(m.D)}, {float64(x1 + 1), float64(y1 + 1), float64(m.D)}} {
		sx, sy2, _ := project(c[0], c[1], c[2])
		minX, maxX, minY, maxY = math.Min(minX, sx), math.Max(maxX, sx), math.Min(minY, sy2), math.Max(maxY, sy2)
	}
	scale := 0.9 * math.Min(float64(w)/(maxX-minX), float64(h)/(maxY-minY))
	ox, oy := float64(w)/2-scale*(minX+maxX)/2, float64(h)/2+scale*(minY+maxY)/2
	zbuf := make([]float64, w*h)
	for i := range zbuf {
		zbuf[i] = math.Inf(-1)
	}
	// The light comes from the front left and from over the part.
	lx, ly, lz := -0.4, -0.6, 0.7
	ln := math.Sqrt(lx*lx + ly*ly + lz*lz)
	lx, ly, lz = lx/ln, ly/ln, lz/ln
	size := int(math.Ceil(scale)) + 1
	for z := 0; z < m.D; z++ {
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				if !m.at(x, y, z) {
					continue
				}
				// The normal from the empty neighbours; a cell with none is inside.
				nx := b2f(!m.at(x+1, y, z)) - b2f(!m.at(x-1, y, z))
				ny := b2f(!m.at(x, y-1, z)) - b2f(!m.at(x, y+1, z))
				nz := b2f(!m.at(x, y, z+1)) - b2f(!m.at(x, y, z-1))
				if nx == 0 && ny == 0 && nz == 0 {
					if m.at(x+1, y, z) && m.at(x-1, y, z) && m.at(x, y+1, z) && m.at(x, y-1, z) && m.at(x, y, z+1) && m.at(x, y, z-1) {
						continue
					}
					nz = 1
				}
				nl := math.Sqrt(nx*nx + ny*ny + nz*nz)
				shade := 0.35 + 0.65*math.Max(0, (nx*lx+ny*ly+nz*lz)/nl)
				base := color.RGBA{150, 152, 156, 255}
				if m.layerOf[z] <= done {
					base = cured
				}
				col := color.RGBA{uint8(float64(base.R) * shade), uint8(float64(base.G) * shade), uint8(float64(base.B) * shade), 255}
				sx, sy2, depth := project(float64(x)+0.5, float64(y)+0.5, float64(z)+0.5)
				px, py := int(ox+sx*scale), int(oy-sy2*scale)
				for dy := -size / 2; dy <= size/2; dy++ {
					for dx := -size / 2; dx <= size/2; dx++ {
						qx, qy := px+dx, py+dy
						if qx < 0 || qy < 0 || qx >= w || qy >= h || zbuf[qy*w+qx] >= depth {
							continue
						}
						zbuf[qy*w+qx] = depth
						img.SetRGBA(qx, qy, col)
					}
				}
			}
		}
	}
	return img
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
