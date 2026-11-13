package printer

import (
	"math"
	"strings"
	"testing"
)

// The shake test turns the shaper off, shakes X and then Y with an even count of moves at each
// step, and sets the shaper and the acceleration of printer.cfg again at the end.
func TestShakeTest(t *testing.T) {
	m := Machine{HasMesh: true, MeshMin: [2]float64{20, 15}, MeshMax: [2]float64{265, 265}, HasShaper: true,
		ShaperType: "mzv", ShaperFreqX: 28, ShaperFreqY: 33.9, MaxAccel: 1500}
	g := ShakeTest(m, "XY")
	for _, want := range []string{"G28\n", "SET_INPUT_SHAPER SHAPER_FREQ_X=0 SHAPER_FREQ_Y=0\n", "G4 P5000\n", "G4 P3000\n",
		"SET_VELOCITY_LIMIT ACCEL=960 MINIMUM_CRUISE_RATIO=0\n", "SET_VELOCITY_LIMIT ACCEL=5400 MINIMUM_CRUISE_RATIO=0\n",
		"SET_INPUT_SHAPER SHAPER_TYPE=mzv SHAPER_FREQ_X=28 SHAPER_FREQ_Y=33.9\n", "SET_VELOCITY_LIMIT ACCEL=1500 MINIMUM_CRUISE_RATIO=0\n"} {
		if !strings.Contains(g, want) {
			t.Errorf("no %q", want)
		}
	}
	if strings.Contains(g, "%!") {
		t.Fatal("a format error in the G-code")
	}
	freqs := ShakeFreqs()
	if len(freqs) != 38 || freqs[0] != ShakeStart || freqs[len(freqs)-1] != ShakeEnd {
		t.Fatalf("frequencies %v", freqs)
	}
	// The moves of each axis: 4 × f for each step of 2 s, out to the middle + d and back.
	moves := map[string]int{}
	for _, l := range strings.Split(g, "\n") {
		if strings.HasPrefix(l, "G1 X") && strings.HasSuffix(l, " F1800") {
			moves["X"]++
		}
		if strings.HasPrefix(l, "G1 Y") && strings.HasSuffix(l, " F1800") {
			moves["Y"]++
		}
	}
	want := 0
	for _, f := range freqs {
		want += int(math.Round(ShakeHold * 2 * f))
	}
	if moves["X"] != want || moves["Y"] != want {
		t.Fatalf("moves %v, want %d each", moves, want)
	}
	// 16 Hz: 960 mm/s², out 3.75 / 16 = 0.234 mm from the middle (142.5, 140).
	if !strings.Contains(g, "G1 X142.7344 F1800\nG1 X142.5000 F1800\n") {
		t.Error("no 16 Hz move of 0.234 mm")
	}
}

// A run of one axis shakes only that axis, with no quiet gap.
func TestShakeTestOneAxis(t *testing.T) {
	m := Machine{HasShaper: true, ShaperType: "mzv", ShaperFreqX: 28, ShaperFreqY: 33.9, MaxAccel: 1500}
	g := ShakeTest(m, "Y")
	if strings.Contains(g, "G1 X100.2344") || strings.Contains(g, "G4 P3000") || !strings.Contains(g, "G1 Y100.2344 F1800") {
		t.Fatal("not a Y run")
	}
}
