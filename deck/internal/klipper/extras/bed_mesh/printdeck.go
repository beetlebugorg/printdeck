package bed_mesh

// Printdeck additions to the port of Klipper. The printdeck_mesh module uses them: it keeps the map of a
// full probe, and makes a new mesh from that map and a few probe points.

import "slices"

// Param is one parameter of a mesh.
type Param struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// Params returns the parameters of the mesh, in their order.
func (m *ZMesh) Params() []Param {
	var out []Param
	for _, k := range m.meshParams.Keys() {
		v, _ := m.meshParams.Get(k)
		out = append(out, Param{k, v})
	}
	return out
}

// Probed returns a copy of the probed points: one row for each Y, from min_y to max_y.
func (m *ZMesh) Probed() [][]float64 {
	out := make([][]float64, len(m.probedMatrix))
	for i, row := range m.probedMatrix {
		out[i] = slices.Clone(row)
	}
	return out
}

// Area returns the corners of the probed area.
func (m *ZMesh) Area() (minX, minY, maxX, maxY float64) {
	return m.meshXMin, m.meshYMin, m.meshXMax, m.meshYMax
}

// NewMesh makes a mesh of the parameters and the probed points, the same as BED_MESH_PROFILE LOAD.
// A count from JSON (a float) becomes an int again.
func NewMesh(params []Param, probed [][]float64, name string) (*ZMesh, error) {
	p := NewMeshParams()
	for _, kv := range params {
		v := kv.Value
		if f, ok := v.(float64); ok && f == float64(int(f)) && (kv.Key == "x_count" || kv.Key == "y_count" ||
			kv.Key == "mesh_x_pps" || kv.Key == "mesh_y_pps") {
			v = int(f)
		}
		p.Set(kv.Key, v)
	}
	m := NewZMesh(p, name)
	if err := m.buildMesh(probed); err != nil {
		return nil, err
	}
	return m, nil
}

// NewMeshParams returns empty mesh parameters.
func NewMeshParams() *MeshParams { return copyParams(&MeshParams{}) }

// OnCalibrate adds a function that gets the mesh of each BED_MESH_CALIBRATE, after it is set.
func (b *BedMesh) OnCalibrate(fn func(*ZMesh)) { b.onCalibrate = append(b.onCalibrate, fn) }
