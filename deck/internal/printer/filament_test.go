package printer

import (
	"math"
	"testing"
)

// 1 m of 1.75 mm PLA is 2.98 g, and 1 m of 2.85 mm PLA is 7.91 g. A material that the deck does
// not know counts as PLA.
func TestFilamentGrams(t *testing.T) {
	for _, c := range []struct {
		mm, d    float64
		material string
		want     float64
	}{
		{1000, 1.75, "PLA", 2.98},
		{1000, 2.85, "pla", 7.91},
		{1000, 2.85, "ABS", 6.63},
		{1000, 2.85, "Nylon", 7.91},
	} {
		if g := FilamentGrams(c.mm, c.d, c.material); math.Abs(g-c.want) > 0.01 {
			t.Errorf("%v mm of %v mm %s: %.3f g, want %.2f", c.mm, c.d, c.material, g, c.want)
		}
	}
}

func TestWeight(t *testing.T) {
	for g, want := range map[float64]string{4.24: "4.2 g", 92.4: "92 g", 1240: "1.24 kg"} {
		if s := Weight(g); s != want {
			t.Errorf("Weight(%v) = %q, want %q", g, s, want)
		}
	}
}
