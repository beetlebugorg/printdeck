package printdeck_timelapse

import (
	"math"
	"strings"
	"testing"

	"printdeck/internal/klipper/webhooks"
)

func TestIsLayerChange(t *testing.T) {
	for c, want := range map[string]bool{"LAYER:0": true, "LAYER:-2": true, "LAYER_CHANGE": true, " LAYER:12": true,
		"LAYER_COUNT:40": false, "TYPE:WALL-OUTER": false, "Z:0.3": false} {
		if got := isLayerChange(c); got != want {
			t.Errorf("isLayerChange(%q) = %v, want %v", c, got, want)
		}
	}
}

// The first layer change is the start of the first layer: no frame. The second ends the first
// layer: the first layer picture and a frame. EVERY skips the other layers.
func TestPlan(t *testing.T) {
	cases := []struct {
		changes      int
		enable, stop bool
		first, every int
		pic, lapse   bool
	}{
		{1, true, false, FirstPicture, 1, false, false},
		{2, true, false, FirstPicture, 1, true, true},
		{2, false, false, FirstPause, 1, true, false},
		{3, true, false, FirstPicture, 1, false, true},
		{3, true, false, FirstOff, 2, false, true},
		{4, true, false, FirstOff, 2, false, false},
		{5, true, true, FirstOff, 1, false, false},
		{2, false, false, FirstOff, 1, false, false},
	}
	for _, c := range cases {
		pic, lapse := plan(c.changes, c.enable, c.stop, c.first, c.every)
		if pic != c.pic || lapse != c.lapse {
			t.Errorf("plan(%+v) = %v, %v", c, pic, lapse)
		}
	}
}

func TestParkMove(t *testing.T) {
	lo, hi := [3]float64{-50, -17, -2}, [3]float64{293, 308, 297}
	// The default: the left, at the Y of the print, 1 mm over the layer, and at least min_z.
	x, y, dz := parkMove([3]float64{100, 100, 0.3}, lo, hi, math.NaN(), math.NaN(), 1, 10)
	if x != -45 || y != 100 || math.Abs(dz-9.7) > 1e-9 {
		t.Errorf("park = %v %v %v", x, y, dz)
	}
	// High over the bed: the lift only.
	_, _, dz = parkMove([3]float64{100, 100, 50}, lo, hi, math.NaN(), math.NaN(), 1, 10)
	if dz != 1 {
		t.Errorf("dz = %v, want 1", dz)
	}
	// A park position outside the axes, and a nozzle at the top: inside the limits.
	x, y, dz = parkMove([3]float64{100, 100, 296.5}, lo, hi, 400, -30, 1, 0)
	if x != 293 || y != -17 || dz != 0.5 {
		t.Errorf("park = %v %v %v", x, y, dz)
	}
}

// The way back is the reverse of the park: the same moves with the other sign.
func TestMoveScripts(t *testing.T) {
	out, back := moveScripts(-145, 203, 9.7, 1, 15, 100, 10)
	want := "SAVE_GCODE_STATE NAME=printdeck_timelapse\nM83\nG1 E-1.000 F900\nG91\nG1 Z9.700 F600\nG1 X-145.000 Y203.000 F6000\nM400"
	if out != want {
		t.Errorf("park:\n%s\nwant:\n%s", out, want)
	}
	want = "G1 X145.000 Y-203.000 F6000\nG1 Z-9.700 F600\nG1 E1.000 F900\nRESTORE_GCODE_STATE NAME=printdeck_timelapse"
	if back != want {
		t.Errorf("back:\n%s\nwant:\n%s", back, want)
	}
	// A cold nozzle, and no lift: no E moves and no Z moves.
	out, back = moveScripts(10, 10, 0, 0, 15, 100, 10)
	if strings.Contains(out+back, "G1 E") || strings.Contains(out+back, "G1 Z") {
		t.Errorf("moves with no retract and no lift:\n%s\n%s", out, back)
	}
}

// The API decodes a JSON number with no fraction as an int64.
func TestHandleTaken(t *testing.T) {
	tl := &Timelapse{}
	for _, n := range []any{int64(3), 2, float64(5)} {
		tl.handleTaken(webhooks.NewWebRequest(nil, 1, "printdeck/timelapse/taken", map[string]any{"frame": n}))
	}
	if tl.taken != 5 {
		t.Errorf("taken = %d, want 5", tl.taken)
	}
}
