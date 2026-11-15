package printer

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	// BedTemp is the temperature of the bed at the probe, in °C. 0 is not known.
	BedTemp float64 `json:"bed_temp,omitempty"`
	// ColdTemp is the bed temperature of the cold map of a difference (HeatChange), or 0.
	ColdTemp float64 `json:"-"`
}

// HeatChange is the change of the bed from the cold map to the hot map: hot less cold at each
// point. The plate and the gantry are in both maps, so only the change from the heat stays. ok is
// false when the two maps do not have the same area and points.
func HeatChange(hot, cold BedMesh) (BedMesh, bool) {
	hx, hy := hot.Size()
	cx, cy := cold.Size()
	if hx == 0 || hx != cx || hy != cy || hot.Min != cold.Min || hot.Max != cold.Max {
		return BedMesh{}, false
	}
	d := BedMesh{Time: hot.Time, Min: hot.Min, Max: hot.Max, BedTemp: hot.BedTemp, ColdTemp: cold.BedTemp}
	for j := range hot.Z {
		row := make([]float64, hx)
		for i := range row {
			row[i] = hot.Z[j][i] - cold.Z[j][i]
		}
		d.Z = append(d.Z, row)
	}
	return d, true
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
// of the start of a print does not replace the 7 x 7 map of the full probe. The store also keeps
// the last full map at each bed temperature, to TempStep °C, in a second file, for HeatChange.
type MeshStore struct {
	path  string
	mu    sync.Mutex
	last  *BedMesh
	temps []BedMesh // by temperature, low to high
}

// fullMap is the least points of a full map. A full map replaces the map of the store, also when
// the map has more points: a mesh with extend on has more points than one with extend off.
const fullMap = 25

// TempStep is the step of the bed temperatures of the maps of MeshStore, in °C.
const TempStep = 5.0

// NewMeshStore reads the meshes of the file at path, and of the file of the maps at each
// temperature next to it. A missing file is no mesh.
func NewMeshStore(path string) *MeshStore {
	s := &MeshStore{path: path}
	if b, err := os.ReadFile(path); err == nil {
		var m BedMesh
		if json.Unmarshal(b, &m) == nil && len(m.Z) > 0 {
			s.last = &m
		}
	}
	if b, err := os.ReadFile(s.tempsPath()); err == nil {
		json.Unmarshal(b, &s.temps)
	}
	return s
}

// tempsPath is the path of the file of the maps at each temperature: bed-mesh-temps.json next to
// bed-mesh.json.
func (s *MeshStore) tempsPath() string {
	ext := filepath.Ext(s.path)
	return strings.TrimSuffix(s.path, ext) + "-temps" + ext
}

// HeatMaps is the last full map at each bed temperature, low to high (MeshStore.Temps).
func (k *Klippy) HeatMaps() []BedMesh { return k.Mesh.Temps() }

// Temps is the last full map at each bed temperature, low to high.
func (s *MeshStore) Temps() []BedMesh {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.temps)
}

func writeJSON(path string, v any) {
	b, _ := json.Marshal(v)
	os.MkdirAll(filepath.Dir(path), 0o700)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err == nil {
		os.Rename(tmp, path)
	}
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

// see keeps m when it is a new mesh with as many points as the map or more, with the time now and
// the bed at bed °C, and writes it to the file. It also replaces the map of its temperature step.
func (s *MeshStore) see(m BedMesh, now time.Time, bed float64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.last != nil && (s.last.same(m) || m.points() < min(s.last.points(), fullMap)) {
		s.mu.Unlock()
		return
	}
	m.Time, m.BedTemp = now, bed
	s.last = &m
	step := math.Round(bed / TempStep)
	s.temps = slices.DeleteFunc(s.temps, func(o BedMesh) bool { return math.Round(o.BedTemp/TempStep) == step })
	s.temps = append(s.temps, m)
	slices.SortFunc(s.temps, func(x, y BedMesh) int { return int(math.Round(x.BedTemp - y.BedTemp)) })
	temps := slices.Clone(s.temps)
	s.mu.Unlock()
	writeJSON(s.path, m)
	writeJSON(s.tempsPath(), temps)
}

// The heat sweep: the full bed map at room temperature, and then at each step of 5 °C. The bed
// soaks at each step before the probe, so the plate is even. The gantry is leveled once, at the
// start: the changes between the maps are then of the bed and the frame only. The sweep is a
// G-code file that the deck prints: it runs for more than an hour, and a print has no time limit.
const (
	HeatCold      = 30 // °C: the sweep waits for the bed to cool to this first
	HeatStep      = 5  // °C, the same as TempStep of the store
	HeatSoak      = 4  // minutes at each step before the probe
	HeatSweepFile = "printdeck-heat-sweep.gcode"
)

// HeatTemps are the bed temperatures of a heat sweep up to top, after the map at room
// temperature.
func HeatTemps(top int) []int {
	var ts []int
	for t := HeatCold + HeatStep; t <= top; t += HeatStep {
		ts = append(ts, t)
	}
	if len(ts) == 0 || ts[len(ts)-1] != top {
		ts = append(ts, top)
	}
	return ts
}

// HeatMinutes is about the time of a heat sweep up to top, in minutes, after the bed cools: 6
// minutes for each map, and the soak and the heating of each step.
func HeatMinutes(top int) int {
	n := len(HeatTemps(top))
	return 5 + 6*(n+1) + (HeatSoak+1)*n
}

// sweepLines are the commands of a heat sweep up to top.
func sweepLines(m Machine, top int) []string {
	lines := []string{"G28"}
	if m.Has("z_tilt") {
		lines = append(lines, "Z_TILT_ADJUST")
	}
	lines = append(lines, "M104 S0", "M140 S0",
		fmt.Sprintf("TEMPERATURE_WAIT SENSOR=heater_bed MAXIMUM=%d", HeatCold),
		"BED_MESH_CALIBRATE")
	for _, t := range HeatTemps(top) {
		lines = append(lines, fmt.Sprintf("M190 S%d", t), fmt.Sprintf("G4 P%d", HeatSoak*60000), "BED_MESH_CALIBRATE")
	}
	return append(lines, "M140 S0")
}

// HeatSweepGcode is the G-code file of a heat sweep up to top.
func HeatSweepGcode(m Machine, top int) string {
	var b strings.Builder
	b.WriteString(";FLAVOR:Klipper\n;Generated with Printdeck: the heat sweep of the bed map\n")
	for _, l := range sweepLines(m, top) {
		b.WriteString(l + "\n")
	}
	return b.String()
}

// HeatSweep prints the heat sweep: the bed map at room temperature, then at each step of 5 °C up
// to top. It waits for the bed to cool first. Each map is kept for HeatMaps.
func (k *Klippy) HeatSweep(top int) error {
	return k.printTest(HeatSweepFile, HeatSweepGcode(k.Machine(), top))
}
