package ui

import (
	"context"
	"os"
	"path/filepath"
	"printdeck/internal/gcodestore"
	"runtime/debug"
	"testing"
	"time"

	"printdeck/internal/bambu"
	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// Menu on the page of an X2D print opens the menu, and each page of the menu draws.
func TestBambuPrintMenu(t *testing.T) {
	f, err := bambu.NewFake("00TEST000000000", "12345678")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.Send(map[string]any{"gcode_state": "RUNNING", "subtask_name": "part", "gcode_file": "/data/Metadata/plate_1.gcode",
		"task_id": "7", "layer_num": 5, "total_layer_num": 156, "mc_percent": 3, "mc_remaining_time": 180,
		"nozzle_temper": 220, "nozzle_target_temper": 220, "bed_temper": 60, "bed_target_temper": 60, "stg_cur": 0})
	// A store with the 3MF of the last print, and a history with that print, as on the deck.
	dir := t.TempDir()
	if src := os.Getenv("X2D_3MF"); src != "" {
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dir, "part.gcode.3mf"), data, 0o644)
	}
	hp := filepath.Join(dir, "history.json")
	os.WriteFile(hp, []byte(`[{"file":"part.gcode.3mf","path":"part.gcode.3mf","result":"complete","duration":3600000000000,"end":"2026-11-20T17:00:00Z"}]`), 0o644)
	b := printer.NewBambu(f.Addr(), f.Serial, f.Code, "", "X2D", &printer.History{Path: hp})
	b.Store = &gcodestore.Store{Dir: dir, ThreeMF: true}
	b.Client.Roots = f.Roots
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go b.Run(ctx, nil)
	for i := 0; i < 200 && !b.State().Printing; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if !b.State().Printing {
		t.Fatalf("not printing: %+v", b.State().Text)
	}
	a := New()
	a.Printer = b
	draw := func(what string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("%s: panic %v\n%s", what, r, debug.Stack())
			}
		}()
		c := gfx.New(a.Screen().W, a.Screen().H)
		a.Draw(c)
	}
	draw("print page")
	r, ok := a.KeyRect("Menu")
	if !ok {
		t.Fatal("no Menu key")
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("tap Menu: panic %v\n%s", r, debug.Stack())
			}
		}()
		tap(a, r)
	}()
	draw("after Menu")
	shot(t, a, "x2d-menu")
}
