package batch

import (
	"os"
	"path/filepath"
	"testing"

	"printdeck/internal/klipper/reftest"
)

// The command stream of the Go host with an input shaper is the stream of the real Klipper host
// (testdata/shaper.txt, from tools/klipper-reference): printer/printer.cfg with [input_shaper] at
// the end, fast X and Y moves, and SET_INPUT_SHAPER with other shaper types, a damping ratio, and
// a shaper turned off.
func TestShaperStreamMatchesKlipper(t *testing.T) {
	base, err := os.ReadFile(klipperConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(t.TempDir(), "printer-shaper.cfg")
	extra := "\n[input_shaper]\nshaper_freq_x: 48.5\nshaper_freq_y: 36.2\nshaper_type: mzv\n"
	if err := os.WriteFile(cfg, append(base, extra...), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out.serial")
	result, msg := Run(cfg, dictFile, "../testdata/shaper.gcode", out)
	if result != "exit" {
		t.Fatalf("run result %q: %s", result, msg)
	}
	got, err := Decode(dictFile, out)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := os.ReadFile("../testdata/shaper.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := reftest.Compare(got, string(ref)); err != nil {
		os.WriteFile(filepath.Join(os.TempDir(), "printdeck-shaper-got.txt"), []byte(got), 0o644)
		t.Fatal(err)
	}
}
