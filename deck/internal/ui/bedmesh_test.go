package ui

import (
	"image"
	"image/color"
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

// The Bed mesh key of Maintenance opens the page. The page shows the 3D picture, the range, the
// scale of the heights, and the Probe key.
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
		for _, s := range []string{"RANGE", "FRONT", "7 × 7 POINTS", "HEIGHT × "} {
			if !strings.Contains(f, s) {
				t.Errorf("%s: no %q on the page", name, s)
			}
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

// The picture has the colors of the mesh: blue at the low front left, orange at the high middle.
func TestRenderMesh(t *testing.T) {
	m := testMesh()
	img, front, exag := renderMesh(*m, image.Pt(400, 300), m.Min, m.Max, color.RGBA{0, 0, 0, 255})
	if img == nil || img.Bounds().Size() != image.Pt(400, 300) {
		t.Fatalf("picture %v", img)
	}
	if !front.In(img.Bounds()) {
		t.Errorf("front label at %v", front)
	}
	if lo, hi := m.Range(); exag < 100 || exag > 1000 || hi-lo < 0.2 {
		t.Errorf("exaggeration %v", exag)
	}
	var blue, orange int
	for i := 0; i < len(img.Pix); i += 4 {
		r, g, b := img.Pix[i], img.Pix[i+1], img.Pix[i+2]
		if b > r+40 && b > g {
			blue++
		}
		if r > b+80 && r > g {
			orange++
		}
	}
	if blue == 0 || orange == 0 {
		t.Errorf("blue %d, orange %d pixels", blue, orange)
	}
}

// With [z_tilt], the full probe levels the gantry first.
func TestBedMeshProbeZTilt(t *testing.T) {
	st := idle
	st.Homed = "xyz"
	a, pr, cv := newPrintApp(st)
	pr.Machine_ = printer.Machine{Modules: []string{"bed_mesh", "probe", "z_tilt"}}
	a.pages = []page{pageBedMesh}
	simFrame(t, a, "bed-mesh-ztilt")
	tapLabel(t, a, cv, "Probe")
	if a.confirm == nil {
		t.Fatal("no confirm")
	}
	a.confirm.onYes()
	want := "action Bed mesh: Z_TILT_ADJUST; BED_MESH_CALIBRATE"
	for i := 0; i < 100; i++ {
		pr.mu.Lock()
		calls := strings.Join(pr.calls, "\n")
		pr.mu.Unlock()
		if strings.Contains(calls, want) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("no %q", want)
}
