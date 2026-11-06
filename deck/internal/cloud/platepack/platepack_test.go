package platepack

import (
	"encoding/json"
	"os"
	"testing"
)

// testdata/pack_cases.json is a copy of the cases of printdeck-action, with the places of
// platepack.py. The Go copy must give the same places, so the plan page shows the plates of the
// slice. Copy the file again after a change to platepack.py.
func TestCasesOfPlatepackPy(t *testing.T) {
	b, err := os.ReadFile("testdata/pack_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name     string       `json:"name"`
		Outlines []Outline    `json:"outlines"`
		Bed      [2]float64   `json:"bed"`
		Margin   float64      `json:"margin"`
		Places   [][3]float64 `json:"places"`
		Fit      int          `json:"fit"`
	}
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		got, fit := Plate(c.Outlines, c.Bed[0], c.Bed[1], c.Margin)
		if fit != c.Fit {
			t.Errorf("%s: %d fit, want %d", c.Name, fit, c.Fit)
		}
		if (got == nil) != (c.Places == nil) {
			t.Errorf("%s: places %v, want %v", c.Name, got, c.Places)
			continue
		}
		for i, w := range c.Places {
			g := got[i]
			if g.X != w[0] || g.Y != w[1] || float64(g.K) != w[2] {
				t.Errorf("%s: object %d at %+v, want %v", c.Name, i, g, w)
			}
		}
	}
}
