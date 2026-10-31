package printer_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"printdeck/internal/gcodestore"
	"printdeck/internal/printer"
)

// A file that a slicer sent again under the same name after the print is another file: the
// record has no picture, and not the picture of the new file.
func TestRecordPictureOfAChangedFile(t *testing.T) {
	dir := t.TempDir()
	var g strings.Builder
	g.WriteString("G90\nM82\nG1 Z0.2 F600\n")
	for i := 0; i < 20; i++ {
		g.WriteString("G1 X10 Y10 E1\nG1 X40 Y10 E2\nG1 X40 Y40 E3\nG1 X10 Y40 E4\nG1 Z" + string(rune('1'+i%9)) + "\n")
	}
	path := filepath.Join(dir, "a.gcode")
	if err := os.WriteFile(path, []byte(g.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	v := printer.NewView(&gcodestore.Store{Dir: dir})
	size, mod, ok := v.Stamp("a.gcode")
	if !ok || size == 0 {
		t.Fatal("no stamp")
	}
	picture := func(r printer.Record) bool {
		for range 200 {
			img, loading := v.RecordPicture(r)
			if !loading {
				return img != nil
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("timeout")
		return false
	}
	end := time.Now()
	if !picture(printer.Record{Path: "a.gcode", Result: "complete", End: end, FileSize: size, FileMod: mod}) {
		t.Fatal("the same file has no picture")
	}
	if picture(printer.Record{Path: "a.gcode", Result: "complete", End: end.Add(time.Second), FileSize: size + 1, FileMod: mod}) {
		t.Fatal("a changed file has a picture")
	}
}
