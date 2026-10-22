package printer

import (
	"math"
	"path/filepath"
	"testing"
	"time"
)

// A new roll goes in the printer. A print uses its filament and costs its share of the price.
// Weighing sets the filament again, and the rolls stay in the file.
func TestSpools(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spools.json")
	s := &Spools{Path: path}
	sp := s.Add("Black eSun", "PLA", 1250, 250, 20, time.Unix(1, 0))
	if sp.Left() != 1000 || s.LastReel() != 250 {
		t.Fatalf("left %v, reel %v", sp.Left(), s.LastReel())
	}
	got, cost, ok := s.Use(func(m string) float64 { return 100 })
	if !ok || got.Left() != 900 || math.Abs(cost-2) > 1e-9 {
		t.Fatalf("after a print: %v %v %v", got.Left(), cost, ok)
	}
	s.Weigh(sp.ID, 1050)
	again := &Spools{Path: path}
	in, ok := again.In()
	if !ok || in.Left() != 800 || in.Name != "Black eSun" {
		t.Fatalf("from the file: %+v %v", in, ok)
	}
	again.Put("")
	if _, _, ok := again.Use(func(string) float64 { return 1 }); ok {
		t.Error("a print used a roll that is out of the printer")
	}
	again.Remove(sp.ID)
	if len(again.List()) != 0 {
		t.Error("the roll is still in the list")
	}
}
