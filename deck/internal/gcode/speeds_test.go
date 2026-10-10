package gcode

import (
	"os"
	"strings"
	"testing"
)

const sliced = `;LAYER_COUNT:2
;LAYER:0
;TYPE:SKIRT
G1 F1080 X10 Y0 E1
;TYPE:WALL-OUTER
G1 F1080 X10 Y10 E2
;LAYER:1
;TYPE:WALL-OUTER
G1 F1800 X0 Y10 E3
G1 X0 Y0 E4
;TYPE:FILL
G1 F3300 X5 Y5 E5
G0 F9000 X50 Y50
`

func TestSpeeds(t *testing.T) {
	m, _ := Parse(strings.NewReader(sliced))
	got := map[string]float64{}
	for _, s := range m.Speeds {
		got[s.Name] = s.MMs
	}
	// Outer wall: 10 mm at 18 mm/s on the first layer, 20 mm at 30 mm/s: most of it is at 30.
	if got["Outer wall"] != 30 || got["Infill"] != 55 || got[FirstLayer] != 18 || got[Travel] != 150 || got["Skirt"] != 18 {
		t.Errorf("speeds %v", got)
	}
	// Past the end of the first move of layer 1.
	if s, ok := m.At(m.LayerPos[1] + 50); !ok || s.Kind != KindOuterWall || s.Feed != 30 {
		t.Errorf("At: %+v %v", s, ok)
	}
}

func TestOrcaLayers(t *testing.T) {
	m, _ := Parse(strings.NewReader("; total layer number: 2\n;LAYER_CHANGE\n;Z:0.2\nG1 X1 Y1 E1\n;LAYER_CHANGE\n;Z:0.4\nG1 X2 Y2 E2\n"))
	if m.LayerCount != 2 || len(m.LayerPos) != 2 {
		t.Errorf("count %d, markers %d", m.LayerCount, len(m.LayerPos))
	}
}

// TestFiles prints the speeds of real files: GCODE_FILES=a.gcode,b.gcode
func TestFiles(t *testing.T) {
	for _, p := range strings.Split(os.Getenv("GCODE_FILES"), ",") {
		if p == "" {
			t.Skip()
		}
		f, _ := os.Open(p)
		m, _ := Parse(f)
		f.Close()
		t.Logf("%s: %d layers (%d markers), %d moves", p[strings.LastIndex(p, "/")+1:], m.LayerCount, len(m.LayerPos), len(m.Segs))
		for _, s := range m.Speeds {
			t.Logf("   %-12s %5.1f mm/s", s.Name, s.MMs)
		}
	}
}
