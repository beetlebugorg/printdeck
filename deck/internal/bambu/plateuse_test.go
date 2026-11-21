package bambu

import (
	"archive/zip"
	"bytes"
	"math"
	"strings"
	"testing"
)

const useGcode = `; the start: a purge line before the first T
M83
G1 X10 E2 ; purge
M620 S0A
T0
M621 S0A
M73 P0 R10
G1 X20 E5
G1 E-0.8 ; retract
G1 E0.8
M73 L1
G1 X30 E3
M73 P50 R5
M620 S1A
T1
G1 E10 ; the flush of the change
M621 S1A
M73 L2
G1 X40 E4
M82
G92 E0
G1 X50 E1
G1 X60 E1.5
M73 P100 R0
M83
T65535
G1 E-50 ; T65535 is no filament: the move stays on filament 2
`

func TestCountUse(t *testing.T) {
	u, err := CountUse(strings.NewReader(useGcode))
	if err != nil {
		t.Fatal(err)
	}
	// Filament 1: 2 (the purge) + 5 - 0.8 + 0.8 + 3. Filament 2: 10 + 4 + 1.5 - 50.
	want := []float64{10, 10 + 4 + 1.5 - 50}
	if len(u.Total) != 2 || math.Abs(u.Total[0]-want[0]) > 1e-9 || math.Abs(u.Total[1]-want[1]) > 1e-9 {
		t.Fatalf("Total = %v, want %v", u.Total, want)
	}
}

func TestPlateUseAt(t *testing.T) {
	u, err := CountUse(strings.NewReader(strings.Replace(useGcode, "G1 E-50", "G1 E0", 1)))
	if err != nil {
		t.Fatal(err)
	}
	// Filament 1 has 10 mm, filament 2 has 15.5 mm.
	for _, c := range []struct {
		layer, percent int
		want           [2]float64
	}{
		{0, 0, [2]float64{2.0 / 10, 0}},     // the mark of M73 P0: the purge only
		{1, 0, [2]float64{7.0 / 10, 0}},     // layer 1 starts
		{1, 49, [2]float64{7.0 / 10, 0}},    // in layer 1, before 50 %
		{1, 50, [2]float64{1, 0}},           // 50 %: filament 1 is done
		{2, 50, [2]float64{1, 10.0 / 15.5}}, // layer 2: after the flush of filament 2
		{2, 100, [2]float64{1, 1}},          // the end
		{99, 100, [2]float64{1, 1}},         // past the end
	} {
		got := u.At(c.layer, c.percent)
		if math.Abs(got[0]-c.want[0]) > 1e-9 || math.Abs(got[1]-c.want[1]) > 1e-9 {
			t.Errorf("At(%d, %d) = %v, want %v", c.layer, c.percent, got, c.want)
		}
	}
}

func TestPlateUseOf(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("Metadata/plate_2.gcode")
	w.Write([]byte("M83\nT0\nG1 E7\n"))
	zw.Close()
	u, err := PlateUseOf(bytes.NewReader(buf.Bytes()), int64(buf.Len()), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Total) != 1 || u.Total[0] != 7 {
		t.Fatalf("Total = %v", u.Total)
	}
	if _, err := PlateUseOf(bytes.NewReader(buf.Bytes()), int64(buf.Len()), 1); err == nil {
		t.Fatal("plate 1: no error")
	}
}
