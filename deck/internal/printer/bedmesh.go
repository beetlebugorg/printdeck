package printer

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// BedMesh is a map of the bed from [bed_mesh] of Klipper: the probed Z at the points of a grid.
type BedMesh struct {
	Time time.Time `json:"time"`
	// Min and Max are the corners of the probed area, in pin positions.
	Min [2]float64 `json:"min"`
	Max [2]float64 `json:"max"`
	// Z has one row for each Y, from Min[1] to Max[1]. Each row goes from Min[0] to Max[0].
	Z [][]float64 `json:"z"`
}

// Size is the number of points in X and in Y.
func (m BedMesh) Size() (nx, ny int) {
	if len(m.Z) == 0 {
		return 0, 0
	}
	return len(m.Z[0]), len(m.Z)
}

// Range is the lowest and the highest Z of the mesh.
func (m BedMesh) Range() (lo, hi float64) {
	lo, hi = math.Inf(1), math.Inf(-1)
	for _, row := range m.Z {
		for _, z := range row {
			lo, hi = math.Min(lo, z), math.Max(hi, z)
		}
	}
	if lo > hi {
		return 0, 0
	}
	return lo, hi
}

// At is the Z at the bed position x, y, from the four nearest points. A position outside the
// mesh gets the Z of the nearest edge.
func (m BedMesh) At(x, y float64) float64 {
	nx, ny := m.Size()
	if nx == 0 {
		return 0
	}
	idx := func(v, lo, hi float64, n int) (int, float64) {
		if n < 2 || hi <= lo {
			return 0, 0
		}
		f := math.Max(0, math.Min(float64(n-1), (v-lo)/(hi-lo)*float64(n-1)))
		i := min(int(f), n-2)
		return i, f - float64(i)
	}
	i, fx := idx(x, m.Min[0], m.Max[0], nx)
	j, fy := idx(y, m.Min[1], m.Max[1], ny)
	z := func(i, j int) float64 { return m.Z[min(j, ny-1)][min(i, nx-1)] }
	a := z(i, j)*(1-fx) + z(i+1, j)*fx
	b := z(i, j+1)*(1-fx) + z(i+1, j+1)*fx
	return a*(1-fy) + b*fy
}

// points is the number of probed points.
func (m BedMesh) points() int {
	nx, ny := m.Size()
	return nx * ny
}

// same reports that two meshes have the same area and points.
func (m BedMesh) same(o BedMesh) bool {
	if m.Min != o.Min || m.Max != o.Max || len(m.Z) != len(o.Z) {
		return false
	}
	for j := range m.Z {
		if !slices.Equal(m.Z[j], o.Z[j]) {
			return false
		}
	}
	return true
}

// meshFromStatus reads probed_matrix, mesh_min and mesh_max of the bed_mesh status of Klipper.
// ok is false when Klipper has no mesh.
func meshFromStatus(s map[string]any) (BedMesh, bool) {
	rows, _ := s["probed_matrix"].([]any)
	var m BedMesh
	for _, r := range rows {
		vals, _ := r.([]any)
		if len(vals) == 0 {
			return BedMesh{}, false
		}
		row := make([]float64, len(vals))
		for i, v := range vals {
			row[i] = num(v)
		}
		if len(m.Z) > 0 && len(row) != len(m.Z[0]) {
			return BedMesh{}, false
		}
		m.Z = append(m.Z, row)
	}
	lo, okLo := s["mesh_min"].([]any)
	hi, okHi := s["mesh_max"].([]any)
	if len(m.Z) < 2 || !okLo || !okHi || len(lo) != 2 || len(hi) != 2 {
		return BedMesh{}, false
	}
	m.Min = [2]float64{num(lo[0]), num(lo[1])}
	m.Max = [2]float64{num(hi[0]), num(hi[1])}
	return m, true
}

// MeshStore keeps the map of the bed in a file, so that the deck shows it after a restart of
// Klipper, and after BED_MESH_CLEAR. The map is the last mesh with the most points: the 3 x 3 mesh
// of the start of a print does not replace the 7 x 7 map of the full probe.
type MeshStore struct {
	path string
	mu   sync.Mutex
	last *BedMesh
}

// NewMeshStore reads the mesh of the file at path. A missing file is no mesh.
func NewMeshStore(path string) *MeshStore {
	s := &MeshStore{path: path}
	if b, err := os.ReadFile(path); err == nil {
		var m BedMesh
		if json.Unmarshal(b, &m) == nil && len(m.Z) > 0 {
			s.last = &m
		}
	}
	return s
}

// Last is the last mesh, or nil.
func (s *MeshStore) Last() *BedMesh {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

// see keeps m when it is a new mesh with as many points as the map or more, with the time now, and
// writes it to the file.
func (s *MeshStore) see(m BedMesh, now time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.last != nil && (s.last.same(m) || m.points() < s.last.points()) {
		s.mu.Unlock()
		return
	}
	m.Time = now
	s.last = &m
	s.mu.Unlock()
	b, _ := json.Marshal(m)
	os.MkdirAll(filepath.Dir(s.path), 0o700)
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err == nil {
		os.Rename(tmp, s.path)
	}
}
