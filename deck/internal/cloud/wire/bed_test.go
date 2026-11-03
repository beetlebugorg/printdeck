package wire

import "testing"

// The bed has the long side of each part on its long side, and the highest part. A part with no
// size has no effect.
func TestBed(t *testing.T) {
	var b Bed
	if b.String() != "" {
		t.Fatalf("no parts: %q", b.String())
	}
	b.Add(PlanPart{SizeX: 40, SizeY: 120.2, SizeZ: 5})
	b.Add(PlanPart{SizeX: 90, SizeY: 30, SizeZ: 60})
	b.Add(PlanPart{})
	if b != (Bed{Long: 120.2, Short: 40, High: 60}) {
		t.Fatalf("bed %+v", b)
	}
	if got := b.String(); got != "121 × 40 × 60 mm" {
		t.Errorf("String: %q", got)
	}
}
