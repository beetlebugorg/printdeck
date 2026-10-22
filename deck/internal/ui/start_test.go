package ui

import (
	"encoding/json"
	"testing"

	"printdeck/internal/gfx"
)

// A remote screen records its frames and has no pixels. The start screen must draw on it too.
func TestStartRecorded(t *testing.T) {
	c, rec := gfx.NewRecording(800, 480)
	drawStart(c, "v1.0.0")
	var ops [][]any
	if err := json.Unmarshal(rec.JSON(), &ops); err != nil {
		t.Fatal(err)
	}
	if len(ops) == 0 || ops[0][0] != "i" {
		t.Fatalf("the first command is not the image of the grid: %v", ops[:min(1, len(ops))])
	}
}
