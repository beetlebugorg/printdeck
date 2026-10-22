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
	got, cost, ok := s.Use(func(float64) float64 { return 100 })
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
	if _, _, ok := again.Use(func(float64) float64 { return 1 }); ok {
		t.Error("a print used a roll that is out of the printer")
	}
	again.Remove(sp.ID)
	if len(again.List()) != 0 {
		t.Error("the roll is still in the list")
	}
}

// A roll with a material of Cura uses the density of that material, so the deck and Cura count
// the same grams. Without one, the roll uses the density of its type.
func TestSpoolDensity(t *testing.T) {
	s := &Spools{}
	sp := s.Add("Silk Gold", "PLA", 1250, 250, 20, time.Unix(1, 0))
	var used float64
	s.Use(func(rho float64) float64 { used = rho; return 0 })
	if used != Density("PLA") {
		t.Fatalf("no Cura material: density %v, want %v", used, Density("PLA"))
	}
	s.SetSlicerMaterials([]SlicerMaterial{{ID: "silk_gold", Name: "Silk Gold", Material: "PLA", Density: 1.31}})
	s.SetCuraMaterial(sp.ID, SlicerMaterial{ID: "silk_gold"})
	s.Use(func(rho float64) float64 { used = rho; return 0 })
	if used != 1.31 {
		t.Fatalf("with a Cura material: density %v, want 1.31", used)
	}
}
