package printer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNeedsSetup(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		cfg  string
		want bool
	}{
		// The placeholder of the image.
		{"# Put the printer configuration above this line.\n[include /usr/share/printdeck/klipper/printdeck.cfg]\n", true},
		{"[printer]\nkinematics: cartesian\n[mcu]\nserial: /dev/ttyACM0\n", false},
		{"[MCU]\nserial: /dev/ttyACM0\n", false},
		{"[mcu extra]\nserial: /dev/ttyACM1\n", false},
		{"[include printer-ender3.cfg]\n", false},
		{"#[mcu]\n", true},
		{"", true},
	} {
		p := filepath.Join(dir, "printer.cfg")
		os.WriteFile(p, []byte(c.cfg), 0o644)
		if got := NeedsSetup(p); got != c.want {
			t.Errorf("NeedsSetup(%q) = %v, want %v", c.cfg, got, c.want)
		}
	}
	if !NeedsSetup(filepath.Join(dir, "missing.cfg")) {
		t.Error("a missing printer.cfg needs the setup")
	}
}
