package mathutil

import (
	"math"
	"testing"
)

// The plane z = 0.5 + 0.01*x - 0.02*y through four points is found exactly.
func TestSolvePlane(t *testing.T) {
	var eqs, ans [][]float64
	for _, p := range [][2]float64{{10, 10}, {200, 10}, {10, 200}, {200, 200}, {100, 120}} {
		eqs = append(eqs, []float64{1, p[0], p[1]})
		ans = append(ans, []float64{0.5 + 0.01*p[0] - 0.02*p[1]})
	}
	res := SolveLinearEquations(eqs, ans, false)
	for i, want := range []float64{0.5, 0.01, -0.02} {
		if math.Abs(res[i][0]-want) > 1e-12 {
			t.Errorf("res[%d] = %v, want %v", i, res[i][0], want)
		}
	}
	if SolveLinearEquations([][]float64{{1, 1}, {2, 2}}, [][]float64{{1}, {2}}, false) != nil {
		t.Error("singular system solved")
	}
}

func TestHypotMatchesPython(t *testing.T) {
	bad := 0
	for _, c := range hypotCases {
		if got := Hypot(c[0], c[1]); got != c[2] {
			bad++
			t.Errorf("Hypot(%v, %v) = %v, Python gives %v", c[0], c[1], got, c[2])
		}
	}
	goBad := 0
	for _, c := range hypotCases {
		if math.Hypot(c[0], c[1]) != c[2] {
			goBad++
		}
	}
	t.Logf("%d cases, %d differ from Python; math.Hypot differs in %d", len(hypotCases), bad, goBad)
}

// The values of Python 3: round(x, 2) and round(x, 6).
func TestRound(t *testing.T) {
	for _, c := range []struct {
		x    float64
		n    int
		want float64
	}{
		{2.675, 2, 2.67}, {0.125, 2, 0.12}, {0.375, 2, 0.38}, {-1.005, 2, -1.0},
		{1.0000005, 6, 1.000001}, {-0.0000005, 6, -0.0}, {123.456789, 2, 123.46},
	} {
		if got := Round(c.x, c.n); got != c.want {
			t.Errorf("Round(%v, %d) = %v, want %v", c.x, c.n, got, c.want)
		}
	}
}
