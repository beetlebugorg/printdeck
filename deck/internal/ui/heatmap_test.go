package ui

import (
	"strings"
	"testing"
	"time"

	"printdeck/internal/printer"
)

// heatMaps is a cold map with the shape of the plate, and two hot maps with a dome on it.
func heatMaps() []printer.BedMesh {
	var maps []printer.BedMesh
	for k, temp := range []float64{23, 40, 60} {
		m := printer.BedMesh{Time: time.Unix(1700000000+int64(k)*3600, 0), Min: [2]float64{20, 15},
			Max: [2]float64{265, 265}, BedTemp: temp}
		for j := 0; j < 7; j++ {
			row := make([]float64, 7)
			for i := range row {
				dx, dy := float64(i-3), float64(j-3)
				row[i] = 0.02*float64(i) - 0.03*float64(j) + (temp-23)/37*0.04*(1-(dx*dx+dy*dy)/18)
			}
			m.Z = append(m.Z, row)
		}
		maps = append(maps, m)
	}
	return maps
}

// The page shows the last hot map less the cold map, and a key for each hot map.
func TestHeatMapPage(t *testing.T) {
	a, pr, cv := newPrintApp(idle)
	pr.Machine_ = printer.Machine{Modules: []string{"bed_mesh", "probe"}, HasProbe: true}
	pr.HeatMaps_ = heatMaps()
	a.pages = []page{pageHeatMap}
	f := simFrame(t, a, "heat-change")
	for _, s := range []string{"60° LESS 23°", "40°", "RANGE MM", "+0.040"} {
		if !strings.Contains(f, s) {
			t.Errorf("no %q on the page", s)
		}
	}
	tapLabel(t, a, cv, "40°")
	if f := simFrame(t, a, "heat-change-40"); !strings.Contains(f, "40° LESS 23°") {
		t.Error("the 40° key does not show the map at 40°")
	}
	tapLabel(t, a, cv, "Test")
	if a.confirm == nil || len(a.confirm.choices) == 0 || a.confirm.choices[0].title != "Heat sweep" {
		t.Fatalf("Test asks %+v", a.confirm)
	}
	a.confirm.choices[0].tap()
	if a.confirm == nil || !strings.Contains(a.confirm.question, "each 5° from 35° to 60°") {
		t.Fatalf("Heat sweep asks %+v", a.confirm)
	}
	a.confirm = nil
	pr.HeatMaps_ = pr.HeatMaps_[:1]
	if f := simFrame(t, a, "heat-change-empty"); !strings.Contains(f, "No change yet") {
		t.Error("no text with one map")
	}
}
