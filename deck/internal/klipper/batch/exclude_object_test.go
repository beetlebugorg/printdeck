package batch

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"printdeck/internal/gcode"
)

// The object data that the deck adds to a Cura file goes into printer.exclude_object.objects:
// the names in upper case, the centers and the polygons.
func TestCuraObjectDefines(t *testing.T) {
	cura := ";Generated with Cura_SteamEngine 5.7.0\nG28\n" +
		";MESH:bed level test.stl(1)\nG0 X10 Y10\nG1 X20 Y10 E1\nG1 X20 Y20 E2\nG1 X10 Y20 E3\n" +
		";MESH:NONMESH\nG0 X50 Y50\n;MESH:Body1.stl\nG0 X100 Y100\nG1 X110 Y100 E4\nG1 X110 Y110 E5\n"
	o, err := gcode.ReadObjects(strings.NewReader(cura))
	if err != nil || o == nil {
		t.Fatalf("objects %v %v", o, err)
	}
	var b bytes.Buffer
	if err := o.Write(strings.NewReader(cura), &b); err != nil {
		t.Fatal(err)
	}
	var defines []string
	for _, l := range strings.Split(b.String(), "\n") {
		if strings.HasPrefix(l, "EXCLUDE_OBJECT_DEFINE ") {
			defines = append(defines, l)
		}
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "objects.gcode")
	os.WriteFile(file, []byte(strings.Join(defines, "\n")+"\nEXCLUDE_OBJECT_DEFINE JSON=1\n"), 0o644)
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)
	result, msg := Run(klipperConfig(t), dictFile, file, filepath.Join(dir, "out.serial"))
	if result != "exit" {
		t.Fatalf("run result %q: %s", result, msg)
	}
	want := `Known objects: [{"name": "BED_LEVEL_TEST_STL_1", "center": [15.0, 15.0], ` +
		`"polygon": [[10.0, 10.0], [20.0, 10.0], [20.0, 20.0], [10.0, 20.0]]}, ` +
		`{"name": "BODY1_STL", "center": [105.0, 105.0], "polygon": [[100.0, 100.0], [110.0, 100.0], [110.0, 110.0]]}]`
	if !strings.Contains(logs.String(), want) {
		t.Errorf("no %s in the log:\n%s", want, logs.String())
	}
}
