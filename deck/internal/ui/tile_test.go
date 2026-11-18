package ui

import (
	"encoding/json"
	"image"
	"testing"

	"printdeck/internal/gfx"
)

// The text of a tile of the Machine page stays inside the key, with a margin, at each height of
// the tile.
func TestTileTextInside(t *testing.T) {
	a := New()
	for h := 50; h <= 110; h += 4 {
		r := image.Rect(24, 100, 232, 100+h)
		c, rec := gfx.NewRecording(480, 800)
		drawMu.Lock()
		setSize(a.Screen())
		a.statusKey(c, r, "Software update", "CPU, memory, writes", dim, func() {})
		drawMu.Unlock()
		var ops [][]any
		if err := json.Unmarshal(rec.JSON(), &ops); err != nil {
			t.Fatal(err)
		}
		for _, op := range ops {
			if len(op) < 5 || op[0] != "t" {
				continue
			}
			y, s := op[2].(float64), op[3].(string)
			if int(y) < r.Min.Y+8 || int(y) > r.Max.Y-8 {
				t.Errorf("height %d: %q at y %v, outside %v", h, s, y, r)
			}
		}
	}
}
