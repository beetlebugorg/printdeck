package batch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"printdeck/internal/printer"
)

// unsupported are the example configs that the Go host does not load yet, with the start of
// their error. Remove a config from the list when its modules are ported.
var unsupported = map[string]string{}

// chipDicts returns the firmware dictionaries to try for a chip (testdata/dict, from
// tools/klipper-reference/make-dicts.sh): the default build, then the serial build.
func chipDicts(mcu string) []string {
	switch {
	case mcu == "gd32f103":
		mcu = "stm32f103"
	case strings.HasPrefix(mcu, "gd32f4"):
		mcu = "stm32f446"
	}
	out := []string{"../testdata/dict/" + mcu + ".dict"}
	if _, err := os.Stat("../testdata/dict/" + mcu + "-serial.dict"); err == nil {
		out = append(out, "../testdata/dict/"+mcu+"-serial.dict")
	}
	return out
}

// Each example config of Klipper that the deck offers on the setup of a new printer loads in the
// Go host, with the Printdeck macros at the end, and the printer homes (testdata/load.gcode).
func TestExamplesLoad(t *testing.T) {
	macros, err := filepath.Abs("../../../../os/board/rootfs/usr/share/printdeck/klipper/printdeck.cfg")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, e := range printer.Examples() {
		text, err := printer.ExampleConfig(e.File, "/dev/ttyACM0")
		if err != nil {
			t.Fatal(err)
		}
		text = strings.Replace(text, "/usr/share/printdeck/klipper/printdeck.cfg", macros, 1)
		cfg := filepath.Join(dir, e.File)
		if err := os.WriteFile(cfg, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		var result, first string
		for _, dict := range chipDicts(e.MCU) {
			var msg string
			result, msg = Run(cfg, dict, "../testdata/load.gcode", filepath.Join(dir, "out.serial"))
			first, _, _ = strings.Cut(msg, "\n")
			if !strings.Contains(first, " is reserved for ") {
				break
			}
		}
		want, known := unsupported[e.File]
		switch {
		case result == "exit" && known:
			t.Errorf("%s loads now: remove it from unsupported", e.File)
		case result != "exit" && !known:
			t.Errorf("%s: %s: %s", e.File, result, first)
		case result != "exit" && !strings.HasPrefix(first, want):
			t.Errorf("%s: the error is now %q, want %q", e.File, first, want)
		}
	}
}
