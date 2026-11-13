package printer

import (
	"path/filepath"
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
	s.see(full, t0)
	s.see(quick, t0.Add(time.Hour))
	if got := s.Last(); got == nil || got.points() != 49 || !got.Time.Equal(t0) {
		t.Fatalf("map %+v, want the 7 × 7 mesh", got)
	}
	s.see(full, t0.Add(2*time.Hour))
	if !s.Last().Time.Equal(t0) {
		t.Error("the same mesh again changed the time")
	}
	again, _ := meshFromStatus(meshStatus(7, 7, -1.5))
	s.see(again, t0.Add(3*time.Hour))
	if got := NewMeshStore(path).Last(); got == nil || got.Z[0][0] != -1.5 {
		t.Errorf("map from the file %+v, want the new 7 × 7 mesh", got)
	}
}
