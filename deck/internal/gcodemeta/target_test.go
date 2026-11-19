package gcodemeta

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSlicedFor(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(body), 0o644)
		return p
	}
	for _, c := range []struct{ body, want string }{
		{";FLAVOR:Marlin\n;MACHINE:TAZ Workhorse\nG28\n", "TAZ Workhorse"},
		{";FLAVOR:Griffin\n;TARGET_MACHINE.NAME:Ultimaker S5\n", "Ultimaker S5"},
		{"G28\n; printer_settings_id = My X2D\n; printer_model = Bambu Lab X2D\n", "Bambu Lab X2D"},
		{"G28\n; printer_settings_id = \"Prusa MK4 0.4\"\n", "Prusa MK4 0.4"},
		{"G28\nG1 X10\n", ""},
	} {
		if got := SlicedFor(write("a.gcode", c.body)); got != c.want {
			t.Errorf("%q: %q, want %q", c.body, got, c.want)
		}
	}
	// A 3MF: the G-code of its first plate, with the printer at its end after a long body.
	p := filepath.Join(dir, ".upload-1")
	f, _ := os.Create(p)
	z := zip.NewWriter(f)
	w, _ := z.Create("Metadata/plate_1.gcode")
	w.Write([]byte("; HEADER_BLOCK_START\n" + strings.Repeat("G1 X1 Y1\n", 200000) + "; printer_model = Bambu Lab X2D\n"))
	z.Close()
	f.Close()
	if got := SlicedFor(p); got != "Bambu Lab X2D" {
		t.Errorf("3MF: %q", got)
	}
}
