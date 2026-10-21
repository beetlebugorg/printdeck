package gcode

import (
	"strings"
	"testing"
)

const sample = `;LAYER_COUNT:3
;LAYER:0
G1 X10 Y0 Z0.3 E1
G1 X10 Y10 E2
;LAYER:1
G1 Z0.6
G1 X0 Y10 E3
;LAYER:2
G1 Z0.9
G1 X0 Y0 E4
`

func TestLayers(t *testing.T) {
	m, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if m.LayerCount != 3 || len(m.LayerPos) != 3 {
		t.Fatalf("layers: count %d, markers %d", m.LayerCount, len(m.LayerPos))
	}
	// The end of layer 1 is where the marker of layer 2 starts, and layer 0 means nothing.
	if m.LayerEnd(0) != -1 || m.LayerEnd(1) != m.LayerPos[1] || m.LayerEnd(3) < 1<<40 {
		t.Errorf("LayerEnd: %d %d %d", m.LayerEnd(0), m.LayerEnd(1), m.LayerEnd(3))
	}
	if m.LayerStart(2) != m.LayerPos[1] || m.LayerStart(0) != 0 {
		t.Errorf("LayerStart: %d %d", m.LayerStart(2), m.LayerStart(0))
	}
	// The moves of a layer are all inside its end, and the head is at the Z of the layer.
	if got := m.ZAt(m.LayerEnd(1)); got != 0.3 {
		t.Errorf("ZAt(end of layer 1) = %v, want 0.3", got)
	}
	if got := m.ZAt(m.LayerEnd(2)); got != 0.6 {
		t.Errorf("ZAt(end of layer 2) = %v, want 0.6", got)
	}
}

func TestObjects(t *testing.T) {
	for name, tc := range map[string]struct {
		gcode string
		want  int
	}{
		"none":     {sample, 0},
		"cura one": {";MESH:cup.stl\nG1 X1 E1\n;MESH:NONMESH\nG0 X2\n;MESH:cup.stl\n", 1},
		"cura two": {";MESH:cup.stl\nG1 X1 E1\n;MESH:NONMESH\n;MESH:legs.stl\nG1 X2 E2\n;MESH:cup.stl\n", 2},
		"orca two": {"EXCLUDE_OBJECT_DEFINE NAME=a_id_0 CENTER=1,1\nEXCLUDE_OBJECT_DEFINE NAME=b_id_1 CENTER=2,2\n" +
			"; printing object a id:0 copy 0\nG1 X1 E1\n; printing object b id:1 copy 0\n", 2},
	} {
		m, _ := Parse(strings.NewReader(tc.gcode))
		if m.Objects != tc.want {
			t.Errorf("%s: %d objects, want %d", name, m.Objects, tc.want)
		}
	}
}

// Cura: the total in the header, and the time at the end of each layer.
func TestTimelineCura(t *testing.T) {
	src := ";TIME:600\n;LAYER:0\nG1 X10 Y0 E1 F600\n;TIME_ELAPSED:100\n;LAYER:1\nG1 X20 Y0 E2\n;TIME_ELAPSED:600\n"
	m, _ := Parse(strings.NewReader(src))
	if !m.TimeSlicer || m.TimeTotal != 600 {
		t.Fatalf("slicer %v, total %v", m.TimeSlicer, m.TimeTotal)
	}
	first := int64(strings.Index(src, ";TIME_ELAPSED:100") + len(";TIME_ELAPSED:100\n"))
	if el, total, ok := m.TimeAt(first); !ok || el != 100 || total != 600 {
		t.Errorf("at the first mark: %v %v %v", el, total, ok)
	}
	if el, _, _ := m.TimeAt(int64(len(src))); el != 600 {
		t.Errorf("at the end: %v", el)
	}
}

// PrusaSlicer and OrcaSlicer: M73 with the minutes left.
func TestTimelineM73(t *testing.T) {
	src := "M73 P0 R10\nG1 X10 Y0 E1 F600\nM73 P50 R5\nG1 X20 Y0 E2\nM73 P100 R0\n"
	m, _ := Parse(strings.NewReader(src))
	mid := int64(strings.Index(src, "M73 P50") + len("M73 P50 R5\n"))
	if el, total, ok := m.TimeAt(mid); !ok || el != 300 || total != 600 || !m.TimeSlicer {
		t.Errorf("at the middle: %v of %v, ok %v", el, total, ok)
	}
}

// A file with no marks gets the time of its moves: 10 mm at 10 mm/s and 20 mm at 10 mm/s.
func TestTimelineMoves(t *testing.T) {
	src := ";LAYER:0\nG1 X10 Y0 E1 F600\n;LAYER:1\nG1 X10 Y20 E2\n"
	m, _ := Parse(strings.NewReader(src))
	if m.TimeSlicer || m.TimeTotal != 3 {
		t.Errorf("slicer %v, total %v", m.TimeSlicer, m.TimeTotal)
	}
}
