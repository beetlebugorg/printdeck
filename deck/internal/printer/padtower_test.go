package printer

import (
	"strings"
	"testing"
)

func TestPadTower(t *testing.T) {
	m := Machine{NozzleDiameter: 0.5, FilamentDiameter: 2.85, MinX: -20, MaxX: 300, MinY: -20, MaxY: 300,
		ProbeOffset: [3]float64{68, -17, 3}, Macros: []string{"PRINT_START", "PRINT_END"}}
	for _, correct := range []bool{false, true} {
		g := PadTower(m, 210, 60, "PLA", correct)
		flag := "CORRECT=0"
		if correct {
			flag = "CORRECT=1"
		}
		for _, want := range []string{"PRINT_START BED=60 EXTRUDER=210", "PAD_RESET\n", ";LAYER_COUNT:80",
			"G1 Z20.000", "PAD_RESET RESULTS=0\nPRINT_END", "PAD_CHECK X=157.000 Y=140.000 " + flag} {
			if !strings.Contains(g, want) {
				t.Errorf("correct %v: no %q", correct, want)
			}
		}
		// The brim of the pad on the first layer: 10 loops, the outer one 5.6 mm out from the 16 mm pad.
		if n := strings.Count(g, ";TYPE:SKIRT"); n != 1 {
			t.Errorf("%d brims of the pad, want 1", n)
		}
		if !strings.Contains(g, "G1 X143.680 Y126.680 F9000") {
			t.Error("no outer loop of the brim of the pad")
		}
		if n := strings.Count(g, "PAD_CHECK"); n != 80 {
			t.Errorf("%d PAD_CHECK, want 80", n)
		}
		// Each retract has its unretract, there is no extrusion while retracted, and the check is
		// over the pad with the filament retracted.
		retracted := false
		for i, l := range strings.Split(g, "\n") {
			switch {
			case strings.HasPrefix(l, "G1 E-"):
				if retracted {
					t.Fatalf("line %d: retract twice", i)
				}
				retracted = true
			case strings.HasPrefix(l, "G1 E"):
				if !retracted {
					t.Fatalf("line %d: unretract with no retract", i)
				}
				retracted = false
			case strings.HasPrefix(l, "G1 X") && strings.Contains(l, " E") && retracted:
				t.Fatalf("line %d: extrusion while retracted", i)
			case strings.HasPrefix(l, "PAD_CHECK") && !retracted:
				t.Fatalf("line %d: check with no retract", i)
			}
		}
		if retracted {
			t.Error("retracted at the end")
		}
	}
}

func TestProbePad(t *testing.T) {
	m := Machine{NozzleDiameter: 0.5, FilamentDiameter: 2.85, MinX: -20, MaxX: 300, MinY: -20, MaxY: 300,
		Macros: []string{"PRINT_START", "PRINT_END"}}
	g := ProbePad(m, 210, 60, "PLA")
	for _, want := range []string{"PRINT_START BED=60 EXTRUDER=210", ";LAYER_COUNT:12", "G1 Z3.000 F600",
		"PAD_FIND_OFFSET X=140.000 Y=140.000 SIDE=20 HEIGHT=3.000\nG1 E1.000 F1800\nPRINT_END",
		"G1 X130.280 Y130.280 F9000"} {
		if !strings.Contains(g, want) {
			t.Errorf("no %q", want)
		}
	}
	// The pad is solid: no extrusion past its outer wall, and the E moves pair up.
	retracted := false
	for i, l := range strings.Split(g, "\n") {
		switch {
		case strings.HasPrefix(l, "G1 E-"):
			if retracted {
				t.Fatalf("line %d: retract twice", i)
			}
			retracted = true
		case strings.HasPrefix(l, "G1 E"):
			retracted = false
		case strings.HasPrefix(l, "G1 X") && strings.Contains(l, " E") && retracted:
			t.Fatalf("line %d: extrusion while retracted", i)
		case strings.HasPrefix(l, "PAD_FIND_OFFSET") && !retracted:
			t.Fatalf("line %d: find with no retract", i)
		}
	}
}
