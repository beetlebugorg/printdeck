package bed_mesh

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"printdeck/internal/klipper/jinja"
)

// The meshes of the Go ZMesh are the meshes of Klipper's ZMesh (testdata/meshes.json, from
// tools/klipper-reference/dumpmesh.py): the matrix, calc_z, the average and the range.
func TestZMeshMatchesKlipper(t *testing.T) {
	b, err := os.ReadFile("testdata/meshes.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Algo           string
		Pts            [][]float64
		Xc, Yc, Xp, Yp int
		Matrix         [][]float64
		Calc           []float64
		Avg            float64
		Range          []float64
	}
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	points := [][2]float64{{50, 20}, {77.7, 33.3}, {150, 135}, {249.9, 249.9}, {10, 300}, {123.456, 201.2}}
	for _, c := range cases {
		p := jinja.NewDict()
		p.Set("min_x", 50.0)
		p.Set("max_x", 250.0)
		p.Set("min_y", 20.0)
		p.Set("max_y", 250.0)
		p.Set("x_count", c.Xc)
		p.Set("y_count", c.Yc)
		p.Set("mesh_x_pps", c.Xp)
		p.Set("mesh_y_pps", c.Yp)
		p.Set("algo", c.Algo)
		p.Set("tension", 0.2)
		m := NewZMesh(p, "t")
		if err := m.buildMesh(c.Pts); err != nil {
			t.Fatal(err)
		}
		got := m.MeshMatrix()
		for i := range c.Matrix {
			if !slices.Equal(got[i], c.Matrix[i]) {
				t.Errorf("%s row %d:\n got %v\nwant %v", c.Algo, i, got[i], c.Matrix[i])
			}
		}
		for i, pt := range points {
			if z := m.CalcZ(pt[0], pt[1]); z != c.Calc[i] {
				t.Errorf("%s calc_z%v = %v, want %v", c.Algo, pt, z, c.Calc[i])
			}
		}
		if avg := m.ZAverage(); avg != c.Avg {
			t.Errorf("%s average %v, want %v", c.Algo, avg, c.Avg)
		}
		if lo, hi := m.ZRange(); lo != c.Range[0] || hi != c.Range[1] {
			t.Errorf("%s range %v %v, want %v", c.Algo, lo, hi, c.Range)
		}
	}
}
