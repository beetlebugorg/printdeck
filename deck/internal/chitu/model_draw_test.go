package chitu

import (
	"encoding/binary"
	"image/color"
	"image/png"
	"math"
	"os"
	"testing"
)

// A test shape: a box with a round tower on it. The picture has the cured color at the bottom and
// gray at the top. PWF_MODEL_PNG writes the picture, to look at it.
func TestModelDraw(t *testing.T) {
	m := &Model{W: 60, H: 40, D: 50}
	for z := 0; z < m.D; z++ {
		m.layerOf = append(m.layerOf, z*10+1)
		for y := 0; y < m.H; y++ {
			for x := 0; x < m.W; x++ {
				box := z < 15 && x >= 10 && x < 50 && y >= 8 && y < 32
				tower := math.Hypot(float64(x-30), float64(y-20)) < 8
				if box || tower {
					m.set(x, y, z)
				}
			}
		}
	}
	img := m.Draw(480, 360, 200, color.RGBA{232, 118, 43, 255})
	if p := os.Getenv("PWF_MODEL_PNG"); p != "" {
		f, _ := os.Create(p)
		png.Encode(f, img)
		f.Close()
	}
	cured, gray := 0, 0
	for i := 0; i < len(img.Pix); i += 4 {
		r, g, b := img.Pix[i], img.Pix[i+1], img.Pix[i+2]
		switch {
		case r > g+30 && r > b+30:
			cured++
		case r > 60 && absDiff(r, b) < 20:
			gray++
		}
	}
	if cured < 2000 || gray < 500 {
		t.Errorf("cured %d, gray %d pixels", cured, gray)
	}
}

func absDiff(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}

// The mesh of a box of 2 x 1 x 1 cells: four faces along X (each a run of 2), and the two ends.
func TestModelMesh(t *testing.T) {
	m := &Model{W: 2, H: 1, D: 1, layerOf: []int{1}}
	m.set(0, 0, 0)
	m.set(1, 0, 0)
	b := m.Mesh(0.6, 0.6)
	if string(b[:4]) != "PDM1" {
		t.Fatalf("magic %q", b[:4])
	}
	n := binary.LittleEndian.Uint32(b[20:])
	if n != 6 {
		t.Errorf("%d faces, want 6", n)
	}
	if len(b) != 24+4+6*8 {
		t.Errorf("%d bytes", len(b))
	}
}
