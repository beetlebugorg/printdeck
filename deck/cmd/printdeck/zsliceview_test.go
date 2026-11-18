//go:build zslice

package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/internal/ui"
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

// The GPU check of the system page draws frames of the newest G-code file.
func TestGPUCheck(t *testing.T) {
	path := os.Getenv("ZSVIEW_GCODE")
	if path == "" {
		t.Skip("no ZSVIEW_GCODE")
	}
	out := gpuCheck(filepath.Dir(path))
	if out["ok"] != true {
		t.Fatalf("%v", out)
	}
	t.Logf("%v", out)
}

// The home screen shows the last print as a 3D view that the finger turns, with the scrubber.
func TestHomeViewer(t *testing.T) {
	path := os.Getenv("ZSVIEW_GCODE")
	if path == "" {
		t.Skip("no ZSVIEW_GCODE")
	}
	v := startZsliceViewer(filepath.Dir(path))
	if v == nil {
		t.Skip("no Vulkan")
	}
	a := ui.New()
	a.Viewer = v
	rec := printer.Record{Path: filepath.Base(path), File: "benchy.gcode", Result: "complete", Duration: 102 * time.Minute, End: time.Now()}
	a.Printer = &ui.DemoPrinter{View: -1, State_: printer.State{Reachable: true, Connected: true, Text: "Ready"}, History_: []printer.Record{rec}}
	shot := func(name string) {
		c := gfx.New(a.Screen().W, a.Screen().H)
		a.Draw(c)
		if dir := os.Getenv("ZSVIEW_SHOTS"); dir != "" {
			f, _ := os.Create(filepath.Join(dir, name+".png"))
			png.Encode(f, c.RGBA)
			f.Close()
		}
	}
	shot("home-viewer")
	r, ok := a.KeyRect("Model")
	if !ok {
		t.Fatal("no model on the home screen")
	}
	v.Orbit(0, 100, 100)
	v.Orbit(1, 180, 120)
	v.Orbit(2, 180, 120)
	v.Zoom(1)
	shot("home-viewer-turned")
	_ = r
}
