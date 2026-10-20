package motion

import (
	"encoding/json"
	"os"
	"testing"
)

// The Go queue gives the same moves as Klipper's trapq.c for the operations of
// testdata/trapq.json (from tools/klipper-reference/dumptrapq.py), to the last bit.
func TestTrapQMatchesKlipper(t *testing.T) {
	b, err := os.ReadFile("testdata/trapq.json")
	if err != nil {
		t.Fatal(err)
	}
	var ops [][]json.RawMessage
	if err := json.Unmarshal(b, &ops); err != nil {
		t.Fatal(err)
	}
	tq := NewTrapQ()
	for i, op := range ops {
		var name string
		var a []float64
		json.Unmarshal(op[0], &name)
		json.Unmarshal(op[1], &a)
		switch name {
		case "append":
			tq.Append(a[0], a[1], a[2], a[3], a[4], a[5], a[6], a[7], a[8], a[9], a[10], a[11], a[12])
		case "finalize":
			tq.FinalizeMoves(a[0], a[1])
		case "set_position":
			tq.SetPosition(a[0], a[1], a[2], a[3])
		case "extract":
			var want [][10]float64
			json.Unmarshal(op[2], &want)
			got := tq.ExtractOld(int(a[0]), a[1], a[2])
			if len(got) != len(want) {
				t.Fatalf("op %d: %d moves, want %d", i, len(got), len(want))
			}
			for j, m := range got {
				g := [10]float64{m.PrintTime, m.MoveT, m.StartV, m.Accel, m.StartX, m.StartY, m.StartZ, m.XR, m.YR, m.ZR}
				if g != want[j] {
					t.Errorf("op %d move %d:\n got %v\nwant %v", i, j, g, want[j])
				}
			}
		}
	}
}
