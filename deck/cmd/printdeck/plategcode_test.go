package main

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The view reads the G-code of the first plate of a sliced 3MF, from a copy out of it.
func TestPlateGcode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "part.gcode.3mf")
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create("Metadata/slice_info.config")
	w.Write([]byte(`<config><plate><metadata key="index" value="1"/></plate></config>`))
	w, _ = z.Create("Metadata/plate_1.gcode")
	w.Write([]byte("G28\nG1 X10 Y10 Z0.2 E1\n"))
	z.Close()
	os.WriteFile(p, b.Bytes(), 0o644)
	out, err := plateGcode(p)
	if err != nil {
		t.Fatal(err)
	}
	if g, _ := os.ReadFile(out); string(g) != "G28\nG1 X10 Y10 Z0.2 E1\n" {
		t.Errorf("copy %q", g)
	}
	if again, _ := plateGcode(p); again != out {
		t.Errorf("a second copy: %s, %s", again, out)
	}
}
