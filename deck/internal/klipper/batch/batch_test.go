package batch

import (
	"path/filepath"
	"strings"
	"testing"

	"os"

	"printdeck/internal/klipper/reftest"
)

const (
	printerCfg = "../../../../printer/printer.cfg"
	dictFile   = "../testdata/klipper.dict"
	gcodeFile  = "../testdata/basic.gcode"
)

// The config commands of the Go host are the config commands of the real Klipper host for the
// printer of printer/printer.cfg: the same lines in the same order, to finalize_config with the
// same CRC (testdata/basic.txt, from tools/klipper-reference).
func TestConfigMatchesKlipper(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.serial")
	result, msg := Run(printerCfg, dictFile, gcodeFile, out)
	if result != "exit" {
		t.Fatalf("run result %q: %s", result, msg)
	}
	got, err := Decode(dictFile, out)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := os.ReadFile("../testdata/basic.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := configLines(string(ref))
	gotLines := configLines(got)
	for i := range want {
		if i >= len(gotLines) {
			t.Fatalf("line %d missing: want %q", i+1, want[i])
		}
		if gotLines[i] != want[i] {
			t.Fatalf("line %d:\n got %q\nwant %q", i+1, gotLines[i], want[i])
		}
	}
}

// configLines returns the lines to finalize_config.
func configLines(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		out = append(out, l)
		if strings.HasPrefix(l, "finalize_config ") {
			break
		}
	}
	return out
}

// The whole command stream of the Go host for testdata/basic.gcode is the stream of the real
// Klipper host, for each object (see reftest).
func TestStreamMatchesKlipper(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.serial")
	result, msg := Run(printerCfg, dictFile, gcodeFile, out)
	if result != "exit" {
		t.Fatalf("run result %q: %s", result, msg)
	}
	got, err := Decode(dictFile, out)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := os.ReadFile("../testdata/basic.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := reftest.Compare(got, string(ref)); err != nil {
		os.WriteFile(filepath.Join(os.TempDir(), "printdeck-basic-got.txt"), []byte(got), 0o644)
		t.Fatal(err)
	}
}
