package printer

import (
	"math"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// meshStatus is the bed_mesh status of Klipper for a grid of nx × ny points at height z.
func meshStatus(nx, ny int, z float64) map[string]any {
	var rows []any
	for j := 0; j < ny; j++ {
		var row []any
		for i := 0; i < nx; i++ {
			row = append(row, z+float64(i)*0.01)
		}
		rows = append(rows, row)
	}
	return map[string]any{"probed_matrix": rows, "mesh_min": []any{15., 15.}, "mesh_max": []any{235., 265.}}
}

func TestMeshFromStatus(t *testing.T) {
	m, ok := meshFromStatus(meshStatus(3, 2, -1))
	if !ok || m.Min != [2]float64{15, 15} || m.Max != [2]float64{235, 265} {
		t.Fatalf("mesh %+v ok %v", m, ok)
	}
	if nx, ny := m.Size(); nx != 3 || ny != 2 {
		t.Errorf("size %d × %d", nx, ny)
	}
	if lo, hi := m.Range(); lo != -1 || hi != -0.98 {
		t.Errorf("range %v %v", lo, hi)
	}
	if z := m.At(125, 140); z < -0.991 || z > -0.989 {
		t.Errorf("z in the middle %v, want -0.99", z)
	}
	// Klipper with no mesh has one empty row.
	if _, ok := meshFromStatus(map[string]any{"probed_matrix": []any{[]any{}}}); ok {
		t.Error("a mesh from an empty matrix")
	}
}

// The store keeps the map with the most points, and reads it again from its file.
func TestMeshStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bed-mesh.json")
	s := NewMeshStore(path)
	if s.Last() != nil {
		t.Fatal("a mesh in a new store")
	}
	full, _ := meshFromStatus(meshStatus(7, 7, -1))
	quick, _ := meshFromStatus(meshStatus(3, 3, -2))
	t0 := time.Unix(1000, 0)
	s.see(full, t0, 22)
	s.see(quick, t0.Add(time.Hour), 22)
	if got := s.Last(); got == nil || got.points() != 49 || !got.Time.Equal(t0) {
		t.Fatalf("map %+v, want the 7 × 7 mesh", got)
	}
	s.see(full, t0.Add(2*time.Hour), 22)
	if !s.Last().Time.Equal(t0) {
		t.Error("the same mesh again changed the time")
	}
	again, _ := meshFromStatus(meshStatus(7, 7, -1.5))
	s.see(again, t0.Add(3*time.Hour), 60)
	if got := NewMeshStore(path).Last(); got == nil || got.Z[0][0] != -1.5 {
		t.Errorf("map from the file %+v, want the new 7 × 7 mesh", got)
	}
}

// The store keeps the last map at each temperature step, and the difference of two maps is the
// change from the heat.
func TestHeatMaps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bed-mesh.json")
	s := NewMeshStore(path)
	t0 := time.Unix(1000, 0)
	cold, _ := meshFromStatus(meshStatus(7, 7, -1))
	hot, _ := meshFromStatus(meshStatus(7, 7, -0.9))
	hot.Z[3][3] = -0.85
	hot2, _ := meshFromStatus(meshStatus(7, 7, -0.8))
	s.see(cold, t0, 24.2)
	s.see(hot, t0.Add(time.Hour), 60.1)
	s.see(hot2, t0.Add(2*time.Hour), 59.6) // the same step: it replaces the map at 60
	maps := NewMeshStore(path).Temps()
	if len(maps) != 2 || maps[0].BedTemp != 24.2 || maps[1].BedTemp != 59.6 {
		t.Fatalf("maps at %v, want 24.2 and 59.6", maps)
	}
	s.see(hot, t0.Add(3*time.Hour), 60) // the map at 60 again, with the bump
	big, _ := meshFromStatus(meshStatus(9, 9, -1))
	s2 := NewMeshStore(filepath.Join(t.TempDir(), "bed-mesh.json"))
	s2.see(big, t0, 22)
	s2.see(cold, t0.Add(time.Hour), 22)
	if got := s2.Last(); got == nil || got.points() != 49 {
		t.Errorf("a 7 x 7 map after a 9 x 9 map: %v", got)
	}
	maps = s.Temps()
	d, ok := HeatChange(maps[1], maps[0])
	bump := -0.85 - cold.Z[3][3]
	if !ok || math.Abs(d.Z[0][0]-0.1) > 1e-9 || math.Abs(d.Z[3][3]-bump) > 1e-9 || d.BedTemp != 60 || d.ColdTemp != 24.2 {
		t.Errorf("change %v %v, want 0.1 and %.3f at the bump, 60 less 24.2", ok, d, bump)
	}
	small, _ := meshFromStatus(meshStatus(5, 5, -1))
	if _, ok := HeatChange(small, cold); ok {
		t.Error("maps of other sizes have a change")
	}
}

func TestHeatSweep(t *testing.T) {
	if got := HeatTemps(60); !slices.Equal(got, []int{35, 40, 45, 50, 55, 60}) {
		t.Errorf("temps to 60: %v", got)
	}
	if got := HeatTemps(62); !slices.Equal(got, []int{35, 40, 45, 50, 55, 60, 62}) {
		t.Errorf("temps to 62: %v", got)
	}
	got := HeatSweepGcode(Machine{Modules: []string{"z_tilt"}}, 40)
	want := ";FLAVOR:Klipper\n;Generated with Printdeck: the heat sweep of the bed map\n" +
		"G28\nZ_TILT_ADJUST\nM104 S0\nM140 S0\nTEMPERATURE_WAIT SENSOR=heater_bed MAXIMUM=30\nBED_MESH_CALIBRATE\n" +
		"M190 S35\nG4 P240000\nBED_MESH_CALIBRATE\nM190 S40\nG4 P240000\nBED_MESH_CALIBRATE\nM140 S0\n"
	if got != want {
		t.Errorf("file:\n%s\nwant:\n%s", got, want)
	}
	if m := HeatMinutes(60); m < 60 || m > 90 {
		t.Errorf("%d minutes to 60", m)
	}
}
