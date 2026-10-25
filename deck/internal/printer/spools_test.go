package printer

import (
	"math"
	"os"
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

// AddOut adds a roll and keeps the roll in the printer. Put writes the file only when the roll
// in the printer changes.
func TestSpoolsAddOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spools.json")
	s := &Spools{Path: path}
	black := s.Add("Black", "PLA", 1250, 250, 20, time.Unix(1, 0))
	white := s.AddOut("White", "PLA", 1250, 250, 20, time.Unix(2, 0))
	if in, _ := s.In(); in.ID != black.ID || len(s.List()) != 2 || s.LastReel() != 250 {
		t.Fatalf("roll in the printer %q, %d rolls", in.ID, len(s.List()))
	}
	os.Remove(path)
	s.Put(black.ID)
	if _, err := os.Stat(path); err == nil {
		t.Error("Put of the same roll wrote the file")
	}
	s.Put(white.ID)
	if in, _ := (&Spools{Path: path}).In(); in.ID != white.ID {
		t.Errorf("roll in the file %q, want %q", in.ID, white.ID)
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

// A roll that was added part used: its bar and its price use a full roll of 1 kg.
func TestSpoolCapacity(t *testing.T) {
	part := Spool{Net: 221, Full: 221, Price: 20}
	if part.Capacity() != 1000 || part.Fraction() != 0.221 || part.Cost(100) != 2 {
		t.Errorf("part used: %v %v %v", part.Capacity(), part.Fraction(), part.Cost(100))
	}
	big := Spool{Net: 2010, Full: 2010}
	if big.Capacity() != 2010 || big.Fraction() != 1 {
		t.Errorf("2 kg: %v %v", big.Capacity(), big.Fraction())
	}
	small := Spool{Net: 100, Full: 100, Size: 250}
	if small.Capacity() != 250 || small.Fraction() != 0.4 {
		t.Errorf("250 g: %v %v", small.Capacity(), small.Fraction())
	}
}
