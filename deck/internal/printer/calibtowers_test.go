package printer

import (
	"math"
	"strings"
	"testing"

	"printdeck/internal/camera"
	"printdeck/internal/gcode"
)

// The calibration towers: the start and the end of the deck, the temperatures, 0.2 mm layers up
// to the top of the caps, and each move inside a tower at its place.
func TestCalibTowers(t *testing.T) {
	taz := Machine{NozzleDiameter: 0.5, FilamentDiameter: 2.85, MinX: -50, MaxX: 293, MinY: -17, MaxY: 308,
		Macros: []string{"PRINT_START", "PRINT_END"}}
	tw := CalibTowers(taz, 235, 80, "PLA")
	g := tw.Gcode
	for _, want := range []string{"PRINT_START BED=80 EXTRUDER=235 WIPE=185 LAYER=0.20 SPEED=15", "PRINT_END",
		";LAYER_COUNT:105", ";PRINTDECK_MATERIAL: PLA", "M106 S255", ";TIME:", ";Filament used: "} {
		if !strings.Contains(g, want) {
			t.Errorf("no %q", want)
		}
	}
	if strings.Contains(g, "M84") || strings.Contains(g, "G28") {
		t.Error("the start or the end of the deck has a homing or the motors off")
	}
	// The print area of the TAZ: 0..293 in both axes, the towers 25 mm inside it.
	want := [][2]float64{{25, 25}, {268, 25}, {268, 268}, {25, 268}, {146.5, 146.5}}
	for i, p := range tw.Places {
		if math.Abs(p[0]-want[i][0]) > 1e-9 || math.Abs(p[1]-want[i][1]) > 1e-9 {
			t.Errorf("tower %d at %v, want %v", i+1, p, want[i])
		}
	}
	gc, err := gcode.Parse(strings.NewReader(g))
	if err != nil {
		t.Fatal(err)
	}
	if len(gc.LayerPos) != 105 {
		t.Errorf("%d layers", len(gc.LayerPos))
	}
	// Each extruding move is inside a tower, under the top of the cap. The caps are smaller.
	hits := make([]int, len(tw.Places))
	top := 0.0
	for _, s := range gc.Segs {
		in := -1
		for i, p := range tw.Places {
			half := camera.TowerSide / 2
			if float64(s.Z1) > camera.TowerHeight+0.01 {
				half = camera.TowerCapSide / 2
			}
			if math.Abs(float64(s.X0)-p[0]) <= half+1e-3 && math.Abs(float64(s.X1)-p[0]) <= half+1e-3 &&
				math.Abs(float64(s.Y0)-p[1]) <= half+1e-3 && math.Abs(float64(s.Y1)-p[1]) <= half+1e-3 {
				in = i
			}
		}
		if in < 0 {
			t.Fatalf("a move outside the towers: %+v", s)
		}
		hits[in]++
		top = max(top, float64(s.Z1))
	}
	for i, n := range hits {
		if n < 105*4 {
			t.Errorf("tower %d has %d moves", i+1, n)
		}
	}
	if math.Abs(top-camera.TowerTop) > 1e-3 {
		t.Errorf("top %.3f, want %.1f", top, camera.TowerTop)
	}
	// The solid volume of the towers, in mm of filament of 2.85 mm, and the time.
	vol := float64(camera.TowerCount) * (camera.TowerSide*camera.TowerSide*camera.TowerHeight + camera.TowerCapSide*camera.TowerCapSide*camera.TowerCap)
	solid := vol / (math.Pi * 2.85 * 2.85 / 4)
	t.Logf("TAZ: %d layers, %.0f min of moves, %.2f m of filament (solid %.2f m)", tw.Layers, tw.Seconds/60, tw.Filament/1000, solid/1000)
	if tw.Filament < 0.8*solid || tw.Filament > 1.3*solid {
		t.Errorf("filament %.0f mm, solid %.0f mm", tw.Filament, solid)
	}
	if tw.Seconds < 10*60 || tw.Seconds > 60*60 {
		t.Errorf("time %.0f s", tw.Seconds)
	}

	// No macros: heat, home, and the motors off at the end. A small bed gets a smaller inset.
	small := Machine{MinX: 0, MaxX: 60, MinY: 0, MaxY: 80}
	tw = CalibTowers(small, 210, 60, "")
	for _, want := range []string{"M140 S60", "M104 S210", "G28", "M190 S60", "M109 S210", "M84"} {
		if !strings.Contains(tw.Gcode, want) {
			t.Errorf("no %q", want)
		}
	}
	if p := tw.Places; p[0] != [2]float64{15, 15} || p[2] != [2]float64{45, 45} {
		t.Errorf("small bed places %v", p)
	}
	t.Logf("1.75 mm, 0.4 mm nozzle: %.0f min of moves, %.2f m of filament", tw.Seconds/60, tw.Filament/1000)
}
