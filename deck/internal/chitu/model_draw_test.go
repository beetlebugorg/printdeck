package chitu

import (
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
	m.cells = make([]bool, m.W*m.H*m.D)
	for z := 0; z < m.D; z++ {
		m.layerOf = append(m.layerOf, z*10+1)
		for y := 0; y < m.H; y++ {
			for x := 0; x < m.W; x++ {
				box := z < 15 && x >= 10 && x < 50 && y >= 8 && y < 32
				tower := math.Hypot(float64(x-30), float64(y-20)) < 8
				m.cells[(z*m.H+y)*m.W+x] = box || tower
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
