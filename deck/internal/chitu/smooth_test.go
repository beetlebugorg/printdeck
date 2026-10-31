package chitu

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"math"
	"os"
	"testing"
)

// The smooth mesh of a ball: its triangles face out (a positive volume), and its volume is close
// to the volume of the ball.
func TestSmoothMeshBall(t *testing.T) {
	const r = 10.0
	m := &Model{W: 30, H: 30, D: 30, CellMM: 1, SliceMM: 1}
	for z := 0; z < m.D; z++ {
		m.layerOf = append(m.layerOf, z+1)
		for y := 0; y < m.H; y++ {
			for x := 0; x < m.W; x++ {
				if math.Hypot(math.Hypot(float64(x)-14.5, float64(y)-14.5), float64(z)-14.5) < r {
					m.set(x, y, z)
				}
			}
		}
	}
	b := m.SmoothMesh()
	if string(b[:4]) != "PDM2" {
		t.Fatalf("magic %q", b[:4])
	}
	nv, nt := int(binary.LittleEndian.Uint32(b[20:])), int(binary.LittleEndian.Uint32(b[24:]))
	vb := b[28:]
	tb := vb[nv*6:]
	if len(tb) != nt*12 || nt == 0 {
		t.Fatalf("%d vertices, %d triangles, %d bytes of triangles", nv, nt, len(tb))
	}
	vert := func(i uint32) [3]float64 {
		var p [3]float64
		for k := range 3 {
			p[k] = float64(binary.LittleEndian.Uint16(vb[int(i)*6+k*2:]))/64 - 1
		}
		return p
	}
	vol := 0.0
	for i := 0; i < nt; i++ {
		a, bb, c := vert(binary.LittleEndian.Uint32(tb[i*12:])), vert(binary.LittleEndian.Uint32(tb[i*12+4:])), vert(binary.LittleEndian.Uint32(tb[i*12+8:]))
		// The signed volume of the tetrahedron with the origin; Y goes to the front, so the
		// space has the other hand: the sign turns.
		vol -= (a[0]*(bb[1]*c[2]-bb[2]*c[1]) - a[1]*(bb[0]*c[2]-bb[2]*c[0]) + a[2]*(bb[0]*c[1]-bb[1]*c[0])) / 6
	}
	want := 4.0 / 3 * math.Pi * r * r * r
	t.Logf("%d vertices, %d triangles, volume %.0f, ball %.0f", nv, nt, vol, want)
	if math.Abs(math.Abs(vol)-want)/want > 0.15 {
		t.Errorf("volume %.0f, want about %.0f", vol, want)
	}
	if vol < 0 {
		t.Errorf("the triangles face in: volume %.0f", vol)
	}
}

// PWF_PART_BIN writes the gzip mesh of a test shape, for a look at the 3D view of a page.
func TestSmoothMeshFile(t *testing.T) {
	p := os.Getenv("PWF_PART_BIN")
	if p == "" {
		t.Skip()
	}
	m := &Model{W: 120, H: 80, D: 90, CellMM: 0.43, SliceMM: 0.45}
	for z := 0; z < m.D; z++ {
		m.layerOf = append(m.layerOf, z*9+1)
		for y := 0; y < m.H; y++ {
			for x := 0; x < m.W; x++ {
				box := z < 25 && x >= 20 && x < 100 && y >= 15 && y < 65
				tower := math.Hypot(float64(x-60), float64(y-40)) < 16+4*math.Sin(float64(z)/6)
				if box || tower {
					m.set(x, y, z)
				}
			}
		}
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(m.SmoothMesh())
	zw.Close()
	os.WriteFile(p, buf.Bytes(), 0o644)
}
