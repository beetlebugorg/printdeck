package gcode

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// curaTwo is a short Cura file with two objects in two layers.
const curaTwo = `;FLAVOR:Marlin
;TIME:100
;Generated with Cura_SteamEngine 5.7.0
M140 S60
PRINT_START BED=60
G92 E0
;LAYER_COUNT:2
;LAYER:0
M107
;MESH:bed level test.stl(1)
G0 X10 Y10 Z0.3
;TYPE:WALL-OUTER
G1 X20 Y10 E1
G1 X20 Y20 E2
G1 X10 Y20 E3
G1 X10 Y10 E4
G1 X15 Y15 E4.5
;MESH:NONMESH
G0 X100 Y100
G1 X300 Y300 E4.5
;MESH:Body1.stl
G1 X110 Y100 E5
G1 X110 Y110 E6
G1 X100 Y100 E7
;TIME_ELAPSED:10
;LAYER:1
;MESH:Body1.stl
G0 X100 Y100 Z0.5
G1 X110 Y100 E8
;MESH:bed level test.stl(1)
G1 X12 Y12 E9
;TIME_ELAPSED:20
;End of Gcode
PRINT_END
`

const curaTwoOut = `;FLAVOR:Marlin
;TIME:100
;Generated with Cura_SteamEngine 5.7.0
EXCLUDE_OBJECT_DEFINE NAME=bed_level_test_stl_1 CENTER=15.000,15.000 POLYGON=[[10.000,10.000],[20.000,10.000],[20.000,20.000],[10.000,20.000]]
EXCLUDE_OBJECT_DEFINE NAME=Body1_stl CENTER=105.000,105.000 POLYGON=[[100.000,100.000],[110.000,100.000],[110.000,110.000]]
M140 S60
PRINT_START BED=60
G92 E0
;LAYER_COUNT:2
;LAYER:0
M107
;MESH:bed level test.stl(1)
EXCLUDE_OBJECT_START NAME=bed_level_test_stl_1
G0 X10 Y10 Z0.3
;TYPE:WALL-OUTER
G1 X20 Y10 E1
G1 X20 Y20 E2
G1 X10 Y20 E3
G1 X10 Y10 E4
G1 X15 Y15 E4.5
EXCLUDE_OBJECT_END NAME=bed_level_test_stl_1
;MESH:NONMESH
G0 X100 Y100
G1 X300 Y300 E4.5
;MESH:Body1.stl
EXCLUDE_OBJECT_START NAME=Body1_stl
G1 X110 Y100 E5
G1 X110 Y110 E6
G1 X100 Y100 E7
EXCLUDE_OBJECT_END NAME=Body1_stl
;TIME_ELAPSED:10
;LAYER:1
;MESH:Body1.stl
EXCLUDE_OBJECT_START NAME=Body1_stl
G0 X100 Y100 Z0.5
G1 X110 Y100 E8
EXCLUDE_OBJECT_END NAME=Body1_stl
;MESH:bed level test.stl(1)
EXCLUDE_OBJECT_START NAME=bed_level_test_stl_1
G1 X12 Y12 E9
EXCLUDE_OBJECT_END NAME=bed_level_test_stl_1
;TIME_ELAPSED:20
;End of Gcode
PRINT_END
`

func addObjects(t *testing.T, in string) (string, bool) {
	t.Helper()
	o, err := ReadObjects(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if o == nil {
		return in, false
	}
	var b bytes.Buffer
	if err := o.Write(strings.NewReader(in), &b); err != nil {
		t.Fatal(err)
	}
	return b.String(), true
}

func TestAddObjectsCura(t *testing.T) {
	got, ok := addObjects(t, curaTwo)
	if !ok || got != curaTwoOut {
		t.Fatalf("got:\n%s", got)
	}
	// A second pass changes nothing.
	if again, ok := addObjects(t, got); ok || again != got {
		t.Errorf("a second pass changed the file")
	}
	// The deck counts each object one time.
	m, err := Parse(strings.NewReader(got))
	if err != nil || m.Objects != 2 {
		t.Errorf("objects %d (%v), want 2", m.Objects, err)
	}
}

// Each EXCLUDE_OBJECT_START has an EXCLUDE_OBJECT_END in the same layer.
func TestAddObjectsPairs(t *testing.T) {
	got, _ := addObjects(t, curaTwo)
	cur := ""
	for i, l := range strings.Split(got, "\n") {
		switch {
		case strings.HasPrefix(l, "EXCLUDE_OBJECT_START NAME="):
			if cur != "" {
				t.Fatalf("line %d: a START in %s", i+1, cur)
			}
			cur = strings.TrimPrefix(l, "EXCLUDE_OBJECT_START NAME=")
		case strings.HasPrefix(l, "EXCLUDE_OBJECT_END NAME="):
			if n := strings.TrimPrefix(l, "EXCLUDE_OBJECT_END NAME="); n != cur {
				t.Fatalf("line %d: END of %s in %q", i+1, n, cur)
			}
			cur = ""
		case strings.HasPrefix(l, ";LAYER:"), strings.HasPrefix(l, ";TIME_ELAPSED:"):
			if cur != "" {
				t.Fatalf("line %d: %s with %s open", i+1, l, cur)
			}
		}
	}
	if cur != "" {
		t.Errorf("%s has no END", cur)
	}
}

func TestAddObjectsNoChange(t *testing.T) {
	for name, in := range map[string]string{
		"no objects": "; a file\nG28\nG1 X10 Y10 E1\n",
		"orca": "; HEADER\nEXCLUDE_OBJECT_DEFINE NAME=a_id_0 CENTER=1,1 POLYGON=[[0,0],[2,0],[2,2]]\n" +
			"PRINT_START\n; printing object a_id_0\nEXCLUDE_OBJECT_START NAME=a_id_0\nG1 X1 Y1 E1\nEXCLUDE_OBJECT_END NAME=a_id_0\n",
		"nonmesh only": ";MESH:NONMESH\nG0 X1 Y1\n",
	} {
		if got, ok := addObjects(t, in); ok || got != in {
			t.Errorf("%s: changed:\n%s", name, got)
		}
	}
}

// The end of the last object of a file with no newline at its end.
func TestAddObjectsNoNewline(t *testing.T) {
	got, _ := addObjects(t, ";MESH:a\nG1 X1 Y1 E1\nG1 X2 Y1 E2")
	want := "EXCLUDE_OBJECT_DEFINE NAME=a CENTER=1.500,1.000 POLYGON=[[1.000,1.000],[2.000,1.000]]\n" +
		";MESH:a\nEXCLUDE_OBJECT_START NAME=a\nG1 X1 Y1 E1\nG1 X2 Y1 E2\nEXCLUDE_OBJECT_END NAME=a\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestObjectID(t *testing.T) {
	for in, want := range map[string]string{
		"Body1.stl":             "Body1_stl",
		"bed level test.stl(1)": "bed_level_test_stl_1",
		"cup-2 (copy).3mf":      "cup_2_copy_3mf",
		"__x__":                 "x",
		"???":                   "object",
		"Würfel.stl":            "Würfel_stl",
		"a_b":                   "a_b",
	} {
		if got := ObjectID(in); got != want {
			t.Errorf("ObjectID(%q) = %q, want %q", in, got, want)
		}
	}
}

// A large file goes through the file path with little memory: a ring of many points per layer.
// The polygon has at most polygonMax vertices, and they are on the ring.
func TestAddObjectsLargeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ring.gcode")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := bufio.NewWriter(f)
	fmt.Fprintf(w, ";Generated with Cura_SteamEngine 5.7.0\nM83\n")
	const layers, steps = 100, 5000
	for l := range layers {
		fmt.Fprintf(w, ";LAYER:%d\n;MESH:ring.stl\nG0 X150 Y100 Z%.2f\n", l, 0.2*float64(l+1))
		for i := 1; i <= steps; i++ {
			a := 2 * math.Pi * float64(i) / steps
			fmt.Fprintf(w, "G1 X%.3f Y%.3f E0.01\n", 100+50*math.Cos(a), 100+50*math.Sin(a))
		}
		fmt.Fprintf(w, ";MESH:NONMESH\nG0 X0 Y0\n;TIME_ELAPSED:%d\n", l)
	}
	w.Flush()
	f.Close()
	if err := AddObjects(path); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if n := strings.Count(s, "EXCLUDE_OBJECT_START NAME=ring_stl\n"); n != layers {
		t.Errorf("%d STARTs, want %d", n, layers)
	}
	if n := strings.Count(s, "EXCLUDE_OBJECT_END NAME=ring_stl\n"); n != layers {
		t.Errorf("%d ENDs, want %d", n, layers)
	}
	def := strings.SplitN(s, "\n", 3)[1]
	_, poly, ok := strings.Cut(def, " POLYGON=")
	if !strings.HasPrefix(def, "EXCLUDE_OBJECT_DEFINE NAME=ring_stl CENTER=") || !ok {
		t.Fatalf("line 2: %.80s", def)
	}
	var pts [][2]float64
	if err := json.Unmarshal([]byte(poly), &pts); err != nil {
		t.Fatal(err)
	}
	if len(pts) > polygonMax || len(pts) < polygonMax/2 {
		t.Errorf("%d vertices", len(pts))
	}
	for _, p := range pts {
		if r := math.Hypot(p[0]-100, p[1]-100); math.Abs(r-50) > 0.01 {
			t.Errorf("vertex %v is %.3f mm from the center", p, r)
		}
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("the folder has %d files", len(entries))
	}
}
