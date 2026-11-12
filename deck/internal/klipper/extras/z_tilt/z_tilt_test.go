package z_tilt

import (
	"math"
	"testing"
)

// The adjustments are the same as those of Klipper (mathutil.solve_linear_equations of klippy, run
// on the same points): 2 points left and right (underdetermined), and 3 points.
func TestAdjustmentsMatchKlipper(t *testing.T) {
	zpos := [][]float64{{-70, 140}, {350, 140}}
	cases := []struct {
		pts  [][3]float64
		want []float64
	}{
		{[][3]float64{{15, 140, -0.812}, {265, 140, -1.137}}, []float64{-0.7015, -1.2475}},
		{[][3]float64{{15, 60, -0.5}, {265, 60, -0.9}, {140, 220, -1.4}}, []float64{-0.7139999999999996, -1.3860000000000001}},
	}
	for _, c := range cases {
		var eqs, ans [][]float64
		for _, p := range c.pts {
			eqs = append(eqs, []float64{1, p[0], p[1]})
			ans = append(ans, []float64{p[2]})
		}
		got := Adjustments(eqs, ans, zpos)
		for i := range got {
			if math.Abs(got[i]-c.want[i]) > 1e-9 {
				t.Errorf("%v: %v, want %v", c.pts, got, c.want)
			}
		}
	}
}
