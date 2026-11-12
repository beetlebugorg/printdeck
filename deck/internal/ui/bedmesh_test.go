package ui

import (
	"strings"
	"testing"
	"time"

	"printdeck/internal/printer"
)

// testMesh is a 7 × 7 mesh of a bed that is low at the front left and has a bump in the middle.
func testMesh() *printer.BedMesh {
	m := &printer.BedMesh{Time: time.Unix(1700000000, 0), Min: [2]float64{15, 15}, Max: [2]float64{235, 265}}
	for j := 0; j < 7; j++ {
		row := make([]float64, 7)
		for i := range row {
			dx, dy := float64(i-3), float64(j-3)
			row[i] = 0.02*float64(i) + 0.03*float64(j) + 0.08 - 0.01*(dx*dx+dy*dy)
		}
		m.Z = append(m.Z, row)
	}
	return m
}

// The Bed mesh key of Maintenance opens the page. The page shows the range, the heights of the
// probed points, and the Probe key.
func TestBedMeshPage(t *testing.T) {
	m := testMesh()
	mach := printer.Machine{Modules: []string{"bed_mesh", "probe"}, MeshMin: m.Min, MeshMax: m.Max, HasMesh: true}
	for _, wide := range []bool{false, true} {
		st := idle
		st.Homed = "xyz"
		st.Mesh = m
		a, pr, cv := newPrintApp(st)
		pr.Machine_ = mach
		a.Wide = wide
		a.pages = []page{pageMachine, pageMaintenance}
		tapLabel(t, a, cv, "Bed mesh")
		if a.topPage() != pageBedMesh {
			t.Fatalf("wide %v: page %v after Bed mesh", wide, a.topPage())
		}
		name := "bed-mesh"
		if wide {
			name += "-wide"
		}
		f := simFrame(t, a, name)
		lo, hi := m.Range()
		for _, s := range []string{"RANGE", "BACK", "FRONT", "7 × 7 POINTS"} {
			if !strings.Contains(f, s) {
				t.Errorf("%s: no %q on the page", name, s)
			}
		}
		if mid := (lo + hi) / 2; !strings.Contains(f, `"`+heightText(m.Z[0][0]-mid)+`"`) {
			t.Errorf("%s: no height of the front left point", name)
		}
		tapLabel(t, a, cv, "Probe")
		if a.confirm == nil || a.confirm.question != "Probe the full bed now?" {
			t.Fatalf("%s: Probe asks %+v", name, a.confirm)
		}
	}
}

// The page with no mesh asks for a probe.
func TestBedMeshEmpty(t *testing.T) {
	a, pr, _ := newPrintApp(idle)
	pr.Machine_ = printer.Machine{Modules: []string{"bed_mesh"}}
	a.pages = []page{pageBedMesh}
	f := simFrame(t, a, "bed-mesh-empty")
	if !strings.Contains(f, "No map yet") {
		t.Error("no text for a printer with no mesh")
	}
}

// The map has the area of the full bed, and a mesh of a smaller area shows in its part of it.
func TestMeshArea(t *testing.T) {
	m := testMesh()
	mach := printer.Machine{MeshMin: [2]float64{0, 0}, MeshMax: [2]float64{280, 280}, HasMesh: true}
	if lo, hi := meshArea(mach, *m); lo != mach.MeshMin || hi != mach.MeshMax {
		t.Errorf("area %v %v, want the full bed", lo, hi)
	}
	mach.MeshMax = [2]float64{200, 200}
	if lo, hi := meshArea(mach, *m); lo != m.Min || hi != m.Max {
		t.Errorf("area %v %v, want the mesh", lo, hi)
	}
}
