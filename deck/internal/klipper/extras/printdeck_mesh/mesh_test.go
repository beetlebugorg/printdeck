package printdeck_mesh

import (
	"encoding/json"
	"math"
	"testing"

	"printdeck/internal/klipper/extras/bed_mesh"
)

// testBase is a saved map of 7 x 7 points, as the TAZ has it: 15 to 265 in X, 15 to 230 in Y.
func testBase() *Base {
	params := []bed_mesh.Param{{Key: "min_x", Value: 15.}, {Key: "max_x", Value: 265.}, {Key: "min_y", Value: 15.}, {Key: "max_y", Value: 230.},
		{Key: "x_count", Value: 7}, {Key: "y_count", Value: 7}, {Key: "mesh_x_pps", Value: 2}, {Key: "mesh_y_pps", Value: 2}, {Key: "algo", Value: "bicubic"}, {Key: "tension", Value: .2}}
	var probed [][]float64
	for j := range 7 {
		row := make([]float64, 7)
		for i := range row {
			row[i] = -0.74 - 0.18*float64(j) + 0.01*float64((i-3)*(i-3))
		}
		probed = append(probed, row)
	}
	return &Base{Params: params, Probed: probed}
}

// The new mesh is the saved map plus the plane, also after the map goes through the state file.
func TestShifted(t *testing.T) {
	var b Base
	raw, _ := json.Marshal(testBase())
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	base, err := bed_mesh.NewMesh(b.Params, b.Probed, "base")
	if err != nil {
		t.Fatal(err)
	}
	fit := Plane{-0.2, 0.001, -0.0005}
	m, err := shifted(&b, fit)
	if err != nil {
		t.Fatal(err)
	}
	for _, xy := range [][2]float64{{15, 15}, {265, 230}, {140, 120}, {98.3, 201.7}} {
		want := base.CalcZ(xy[0], xy[1]) + fit.At(xy[0], xy[1])
		if got := m.CalcZ(xy[0], xy[1]); math.Abs(got-want) > 0.002 {
			t.Errorf("z at %v: %.4f, want %.4f", xy, got, want)
		}
	}
	if m.ProfileName() != "printdeck" {
		t.Errorf("profile %q", m.ProfileName())
	}
}

// The extended mesh keeps the z inside the probed area, and continues the slope beyond it. The
// mesh of Klipper keeps the z of the edge.
func TestExtended(t *testing.T) {
	b := testBase()
	base, err := bed_mesh.NewMesh(b.Params, b.Probed, "base")
	if err != nil {
		t.Fatal(err)
	}
	m, err := extended(base)
	if err != nil {
		t.Fatal(err)
	}
	x0, y0, x1, y1 := m.Area()
	if x0 >= 0 || y0 >= 0 || x1 <= 280 || y1 <= 265 {
		t.Errorf("area %v %v %v %v does not cover the bed", x0, y0, x1, y1)
	}
	for _, xy := range [][2]float64{{15, 15}, {140, 120}, {265, 230}} {
		if d := math.Abs(m.CalcZ(xy[0], xy[1]) - base.CalcZ(xy[0], xy[1])); d > 0.005 {
			t.Errorf("z at %v changes by %.4f", xy, d)
		}
	}
	// The map drops 0.18 mm for each row of 35.8 mm. At Y 265, 35 mm past the last row, the
	// extended mesh is about 0.18 mm lower than the edge.
	edge, back := base.CalcZ(140, 230), m.CalcZ(140, 265)
	if d := edge - back; d < 0.15 || d > 0.2 {
		t.Errorf("drop from Y 230 to Y 265: %.3f mm (Klipper: %.3f)", d, edge-base.CalcZ(140, 265))
	}
}
