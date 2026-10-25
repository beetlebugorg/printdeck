package batch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"printdeck/internal/printer"
)

// The example configs of Klipper for atmega2560 boards that the Go host loads. The deck offers
// each one on the setup of a new printer, with the Printdeck macros at the end.
var loadedExamples = []string{
	"printer-anycubic-4max-2018.cfg",
	"printer-anycubic-i3-mega-2017.cfg",
	"printer-creality-cr10-v3-2020.cfg",
	"printer-creality-cr10s-2017.cfg",
	"printer-longer-lk4-pro-2019.cfg",
	"printer-lulzbot-mini2-2018.cfg",
	"printer-lulzbot-taz6-2017.cfg",
	"printer-lulzbot-taz6-dual-v3-2017.cfg",
	"printer-makergear-m2-2016.cfg",
	"printer-sovol-sv01-2020.cfg",
	"printer-velleman-k8200-2013.cfg",
	"printer-wanhao-duplicator-i3-plus-2017.cfg",
}

func TestExamplesLoad(t *testing.T) {
	macros, err := filepath.Abs("../../../../os/board/rootfs/usr/share/printdeck/klipper/printdeck.cfg")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, file := range loadedExamples {
		text, err := printer.ExampleConfig(file, "/dev/ttyACM0")
		if err != nil {
			t.Fatal(err)
		}
		text = strings.Replace(text, "/usr/share/printdeck/klipper/printdeck.cfg", macros, 1)
		cfg := filepath.Join(dir, file)
		if err := os.WriteFile(cfg, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		result, msg := Run(cfg, dictFile, gcodeFile, filepath.Join(dir, "out.serial"))
		if result != "exit" {
			first, _, _ := strings.Cut(msg, "\n")
			t.Errorf("%s: %s: %s", file, result, first)
		}
	}
}
