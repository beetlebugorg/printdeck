package amp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindow(t *testing.T) {
	a := New("x", 100, -40, -10)
	for _, c := range []struct{ p, step int }{{100, 187}, {50, 157}, {1, 128}, {0, 0}} {
		if got := a.stepFor(c.p); got != c.step {
			t.Errorf("stepFor(%d) = %d, want %d", c.p, got, c.step)
		}
	}
	// A step turns back into a percent that gives the same step.
	for p := 1; p <= 100; p++ {
		if got := a.stepFor(a.percentFor(a.stepFor(p))); got != a.stepFor(p) {
			t.Errorf("percent %d: step %d came back as step %d", p, a.stepFor(p), got)
		}
	}
	// Louder than the ceiling shows as 100%, quieter than the floor as 0%.
	if a.percentFor(207) != 100 || a.percentFor(100) != 0 {
		t.Errorf("out of window: %d %d", a.percentFor(207), a.percentFor(100))
	}
}

func TestSave(t *testing.T) {
	a := New("x", 80, -40, -10)
	a.File = filepath.Join(t.TempDir(), "volume")
	a.save(42)
	b, err := os.ReadFile(a.File)
	if err != nil || string(b) != "42\n" {
		t.Fatalf("saved %q, %v", b, err)
	}
	// The card does not exist, so only the saved volume is read, and clipped to the maximum.
	os.WriteFile(a.File, []byte("95\n"), 0o644)
	if v := a.Restore(); v != 80 || a.Volume() != 80 {
		t.Fatalf("Restore = %d, Volume = %d", v, a.Volume())
	}
}
