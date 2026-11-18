//go:build zslice

package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"printdeck/internal/printer"
)

// The zslice renderer draws the print of a G-code file at a position and up to a layer, at the
// size of the picture of the deck. ZSVIEW_GCODE names the file; ZSVIEW_SHOTS keeps the pictures.
func TestZsliceRenderer(t *testing.T) {
	path := os.Getenv("ZSVIEW_GCODE")
	if path == "" {
		t.Skip("no ZSVIEW_GCODE")
	}
	live := startZsliceView(filepath.Dir(path))
	if live == nil {
		t.Skip("no Vulkan")
	}
	st, _ := os.Stat(path)
	for name, c := range map[string]struct {
		pos   int64
		layer int
		hex   string
	}{"live-pos": {st.Size() / 2, 0, "#ff7a30"}, "live-layer": {0, 40, "#3a7bd5"}, "live-feature": {st.Size() / 3, 0, ""}} {
		img, ok := live.Draw(filepath.Base(path), c.pos, c.layer, c.hex)
		if !ok || img.Bounds().Dx() != printer.PreviewW || img.Bounds().Dy() != printer.PreviewH {
			t.Fatalf("%s: %v %v", name, ok, img)
		}
		if dir := os.Getenv("ZSVIEW_SHOTS"); dir != "" {
			f, _ := os.Create(filepath.Join(dir, name+".png"))
			png.Encode(f, img)
			f.Close()
		}
	}
	if _, ok := live.Draw("no-such-file.gcode", 10, 0, ""); ok {
		t.Fatal("a picture of a missing file")
	}
}
