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
	m := &Model{W: 240, H: 160, D: 180, CellMM: 0.29, SliceMM: 0.3}
	for z := 0; z < m.D; z++ {
		m.layerOf = append(m.layerOf, z*9+1)
		for y := 0; y < m.H; y++ {
			for x := 0; x < m.W; x++ {
				box := z < 50 && x >= 40 && x < 200 && y >= 30 && y < 130
				tower := math.Hypot(float64(x-120), float64(y-80)) < 32+8*math.Sin(float64(z)/12)
				hole := math.Hypot(float64(x-80), float64(y-60)) < 10
				if (box || tower) && !hole {
					m.set(x, y, z)
				}
			}
		}
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(m.Voxels())
	zw.Close()
	os.WriteFile(p, buf.Bytes(), 0o644)
}

// Each triangle of the ball faces out: its normal points away from the middle of the ball.
func TestSmoothMeshFacesOut(t *testing.T) {
	m := &Model{W: 30, H: 30, D: 30, CellMM: 1, SliceMM: 1}
	for z := 0; z < m.D; z++ {
		m.layerOf = append(m.layerOf, z+1)
		for y := 0; y < m.H; y++ {
			for x := 0; x < m.W; x++ {
				if math.Hypot(math.Hypot(float64(x)-14.5, float64(y)-14.5), float64(z)-14.5) < 10 {
					m.set(x, y, z)
				}
			}
		}
	}
	b, _ := m.smooth(1 << 30)
	nv, nt := int(binary.LittleEndian.Uint32(b[20:])), int(binary.LittleEndian.Uint32(b[24:]))
	vb := b[28:]
	tb := vb[nv*6:]
	vert := func(i uint32) [3]float64 {
		var p [3]float64
		for k := range 3 {
			p[k] = float64(binary.LittleEndian.Uint16(vb[int(i)*6+k*2:]))/64 - 1
		}
		return p
	}
	var in [3]int // the triangles that face in, by the main axis of their normal
	for i := 0; i < nt; i++ {
		a, bb, c := vert(binary.LittleEndian.Uint32(tb[i*12:])), vert(binary.LittleEndian.Uint32(tb[i*12+4:])), vert(binary.LittleEndian.Uint32(tb[i*12+8:]))
		u := [3]float64{bb[0] - a[0], bb[1] - a[1], bb[2] - a[2]}
		v := [3]float64{c[0] - a[0], c[1] - a[1], c[2] - a[2]}
		n := [3]float64{u[1]*v[2] - u[2]*v[1], u[2]*v[0] - u[0]*v[2], u[0]*v[1] - u[1]*v[0]}
		mid := [3]float64{(a[0]+bb[0]+c[0])/3 - 14, (a[1]+bb[1]+c[1])/3 - 14, (a[2]+bb[2]+c[2])/3 - 14}
		axis := 0
		for k := 1; k < 3; k++ {
			if math.Abs(n[k]) > math.Abs(n[axis]) {
				axis = k
			}
		}
		if n[0]*mid[0]+n[1]*mid[1]+n[2]*mid[2] < 0 {
			in[axis]++
		}
	}
	t.Logf("triangles %d, facing in by axis %v", nt, in)
}

// The share of resin in a cell: 8 x 4 pixels in two cells of 4 x 4, with the first 6 columns lit.
func TestCoverage(t *testing.T) {
	const resX, resY = 8, 4
	var code []byte
	for range resY {
		code = append(code, 0x80|6, 2)
	}
	data := make([]byte, 200)
	put := func(off int, v uint32) { binary.LittleEndian.PutUint32(data[off:], v) }
	put(0, MagicCBDDLP)
	put(8, math.Float32bits(float32(resX)*0.072))
	put(32, math.Float32bits(0.288))
	put(52, resX)
	put(56, resY)
	put(64, 120)
	put(68, 1)
	put(120+12, 160)
	put(120+16, uint32(len(code)))
	copy(data[160:], code)
	h, err := ReadSliced(data)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Coverage(bytes.NewReader(data), h, 4)
	if err != nil {
		t.Fatal(err)
	}
	W, H, D := binary.LittleEndian.Uint16(b[12:]), binary.LittleEndian.Uint16(b[14:]), binary.LittleEndian.Uint16(b[16:])
	if W != 2 || H != 1 || D != 1 {
		t.Fatalf("size %d x %d x %d", W, H, D)
	}
	body := b[20+4*int(D)+4:]
	if body[0] != 255 || body[1] != 1 || body[2] != 128 || body[3] != 1 {
		t.Errorf("the cells %v, want 255 x1, 128 x1", body)
	}
}

// PWF_PART_PDC writes part.bin of a sliced test file: a ball on a disc, 300 layers of 0.05 mm on a
// screen of 800 x 450 pixels of 0.072 mm.
func TestCoverageFile(t *testing.T) {
	p := os.Getenv("PWF_PART_PDC")
	if p == "" {
		t.Skip()
	}
	const resX, resY, layers = 800, 450, 300
	data := make([]byte, 120+layers*36)
	put := func(off int, v uint32) { binary.LittleEndian.PutUint32(data[off:], v) }
	put(0, MagicCBDDLP)
	put(8, math.Float32bits(resX*0.072))
	put(32, math.Float32bits(0.05))
	put(52, resX)
	put(56, resY)
	put(64, 120)
	put(68, layers)
	for n := 0; n < layers; n++ {
		z := float64(n) * 0.05
		var code []byte
		run := func(lit bool, k int) {
			for k > 0 {
				c := min(k, 125)
				b := byte(c)
				if lit {
					b |= 0x80
				}
				code = append(code, b)
				k -= c
			}
		}
		for y := 0; y < resY; y++ {
			lit, k := false, 0
			for x := 0; x < resX; x++ {
				px, py := (float64(x)-400)*0.072, (float64(y)-225)*0.072
				on := z < 2 && math.Hypot(px, py) < 14 || math.Sqrt(px*px+py*py+(z-9)*(z-9)) < 8
				if on != lit {
					run(lit, k)
					lit, k = on, 0
				}
				k++
			}
			run(lit, k)
		}
		put(120+n*36+12, uint32(len(data)))
		put(120+n*36+16, uint32(len(code)))
		data = append(data, code...)
	}
	h, err := ReadSliced(data)
	if err != nil {
		t.Fatal(err)
	}
	cells, err := Coverage(bytes.NewReader(data), h, 4)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(cells)
	zw.Close()
	os.WriteFile(p, buf.Bytes(), 0o644)
	t.Logf("%d bytes, %d packed", len(cells), buf.Len())
}
