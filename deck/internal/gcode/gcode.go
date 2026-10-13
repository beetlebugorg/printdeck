// Package gcode reads the extrusion moves of a G-code file and draws them as a 3D picture.
package gcode

import (
	"bufio"
	"image"
	"image/color"
	"io"
	"math"
	"strconv"
	"strings"
)

// Seg is one extruding move. Pos is the byte offset of the end of its line in the file,
// which is the number OctoPrint reports as the progress of the print.
type Seg struct {
	X0, Y0, Z0, X1, Y1, Z1 float32
	Pos                    int64
	Kind                   Kind    // what the slicer says it is: outer wall, infill, ...
	Feed                   float32 // commanded speed, mm/s
}

type Model struct {
	Segs []Seg
	MaxZ float32

	// Speeds is the speed of each kind of move, and of the first layer and travel moves.
	Speeds []Speed

	// Klipper start and end macros: PRINT_START BED=60 EXTRUDER=220 WIPE=160 ... and PRINT_END.
	// StartEnd is the offset after the PRINT_START line, EndStart the offset of the PRINT_END line.
	// Both are 0 if the file has no such macro.
	StartEnd, EndStart         int64
	BedTemp, ExtTemp, WipeTemp float64

	LayerPos   []int64   // offset of each ";LAYER:n" marker
	FanPos     []int64   // offset after each fan command
	FanVal     []float32 // fan speed 0..1 after the command
	LayerCount int

	// Objects is the number of objects on the bed, from the object marks of the slicer; 0 if it has none.
	Objects int
}

// Parse reads G-code. Moves that do not extrude are skipped.
func Parse(r io.Reader) (*Model, error) {
	br := bufio.NewReaderSize(r, 1<<16)
	m := &Model{}
	var x, y, z, e float64
	relXYZ, relE := false, false
	feed := 0.0 // mm/min, modal
	kind := KindOther
	st := newSpeedStats()
	objects := map[string]map[string]bool{} // names by kind of mark: each slicer names an object its own way per mark
	var pos int64
	for {
		line, err := br.ReadString('\n')
		pos += int64(len(line))
		if t, ok := strings.CutPrefix(line, ";TYPE:"); ok {
			kind = kindOf(strings.TrimSpace(t))
		} else if strings.HasPrefix(line, ";LAYER:") || strings.HasPrefix(line, ";LAYER_CHANGE") {
			// Cura writes ";LAYER:n", PrusaSlicer and OrcaSlicer ";LAYER_CHANGE".
			m.LayerPos = append(m.LayerPos, pos-int64(len(line)))
		} else if n, ok := layerCount(line); ok {
			m.LayerCount = n
		} else if mark, name, ok := objectName(line); ok {
			if objects[mark] == nil {
				objects[mark] = map[string]bool{}
			}
			objects[mark][name] = true
		}
		if i := strings.IndexByte(line, ';'); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(line)
		if len(f) > 0 {
			switch strings.ToUpper(f[0]) {
			case "PRINT_START", "START_PRINT":
				m.StartEnd = pos
				for _, w := range f[1:] {
					if v, ok := strings.CutPrefix(strings.ToUpper(w), "BED="); ok {
						m.BedTemp, _ = strconv.ParseFloat(v, 64)
					} else if v, ok := strings.CutPrefix(strings.ToUpper(w), "EXTRUDER="); ok {
						m.ExtTemp, _ = strconv.ParseFloat(v, 64)
					} else if v, ok := strings.CutPrefix(strings.ToUpper(w), "WIPE="); ok {
						m.WipeTemp, _ = strconv.ParseFloat(v, 64)
					}
				}
			case "M106", "M107", "SET_FAN_SPEED":
				v := float32(0)
				switch strings.ToUpper(f[0]) {
				case "M106":
					v = 1
					for _, w := range f[1:] {
						if x, ok := word(w, 'S'); ok {
							v = float32(min(max(x/255, 0), 1))
						}
					}
				case "SET_FAN_SPEED":
					for _, w := range f[1:] {
						if x, ok := strings.CutPrefix(strings.ToUpper(w), "SPEED="); ok {
							y, _ := strconv.ParseFloat(x, 32)
							v = float32(min(max(y, 0), 1))
						}
					}
				}
				m.FanPos, m.FanVal = append(m.FanPos, pos), append(m.FanVal, v)
			case "PRINT_END":
				m.EndStart = pos - int64(len(line))
			case "G90":
				relXYZ, relE = false, false
			case "G91":
				relXYZ, relE = true, true
			case "M82":
				relE = false
			case "M83":
				relE = true
			case "G92":
				for _, w := range f[1:] {
					if v, ok := word(w, 'E'); ok {
						e = v
					}
				}
			case "G0", "G1", "G00", "G01":
				nx, ny, nz, ne := x, y, z, e
				hasE := false
				for _, w := range f[1:] {
					if v, ok := word(w, 'X'); ok {
						nx = pick(relXYZ, x, v)
					} else if v, ok := word(w, 'Y'); ok {
						ny = pick(relXYZ, y, v)
					} else if v, ok := word(w, 'Z'); ok {
						nz = pick(relXYZ, z, v)
					} else if v, ok := word(w, 'E'); ok {
						hasE = true
						ne = pick(relE, e, v)
					} else if v, ok := word(w, 'F'); ok {
						feed = v
					}
				}
				length := math.Hypot(nx-x, ny-y)
				if hasE && ne > e && length > 0 {
					m.Segs = append(m.Segs, Seg{float32(x), float32(y), float32(z), float32(nx), float32(ny), float32(nz), pos, kind, float32(feed / 60)})
					m.MaxZ = max(m.MaxZ, float32(nz))
					st.add(kind, len(m.LayerPos) <= 1, feed/60, length)
				} else if length > 0 && !hasE {
					st.addTravel(feed/60, length)
				}
				x, y, z, e = nx, ny, nz, ne
			}
		}
		if err != nil {
			break
		}
	}
	m.Speeds = st.result()
	for _, names := range objects {
		m.Objects = max(m.Objects, len(names))
	}
	return m, nil
}

func pick(rel bool, cur, v float64) float64 {
	if rel {
		return cur + v
	}
	return v
}

func word(w string, letter byte) (float64, bool) {
	if len(w) < 2 || (w[0]|0x20) != (letter|0x20) {
		return 0, false
	}
	v, err := strconv.ParseFloat(w[1:], 64)
	return v, err == nil
}

// View angles: the model is turned 35 degrees around the vertical axis and seen from 40 degrees above.
var (
	sinA, cosA = math.Sincos(35 * math.Pi / 180)
	sinE, cosE = math.Sincos(40 * math.Pi / 180)
)

func project(x, y, z float32) (u, v float64) {
	fx, fy, fz := float64(x), float64(y), float64(z)
	return fx*cosA - fy*sinA, -((fx*sinA+fy*cosA)*sinE + fz*cosE)
}

// Render draws the model into a w x h picture. The part up to file offset pos is in colour, with the
// layer that starts at curStart (the one being printed) in the colour hot and a mark at the end, and the rest
// is a faint outline. The result has a transparent background.
func (m *Model) Render(w, h int, pos, curStart int64, warm, cool, hot, ghost color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if len(m.Segs) == 0 {
		return img
	}
	minU, minV, maxU, maxV := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for i := range m.Segs {
		s := &m.Segs[i]
		for _, p := range [2][3]float32{{s.X0, s.Y0, s.Z0}, {s.X1, s.Y1, s.Z1}} {
			u, v := project(p[0], p[1], p[2])
			minU, maxU = math.Min(minU, u), math.Max(maxU, u)
			minV, maxV = math.Min(minV, v), math.Max(maxV, v)
		}
	}
	const pad = 8.0
	scale := math.Min((float64(w)-2*pad)/(maxU-minU), (float64(h)-2*pad)/(maxV-minV))
	ox := (float64(w) - (maxU-minU)*scale) / 2
	oy := (float64(h) - (maxV-minV)*scale) / 2
	pt := func(x, y, z float32) (int, int) {
		u, v := project(x, y, z)
		return int(ox + (u-minU)*scale), int(oy + (v-minV)*scale)
	}
	last := -1 // the last move that is done: where the nozzle is
	for pass := 0; pass < 2; pass++ {
		for i := range m.Segs {
			s := &m.Segs[i]
			done := s.Pos <= pos
			if (pass == 0) == done {
				continue
			}
			col := ghost
			if done {
				t := float64(s.Z1 / max(m.MaxZ, 0.001))
				col = mix(warm, cool, t)
				if s.Pos > curStart {
					col = hot
				}
				last = i
			}
			x0, y0 := pt(s.X0, s.Y0, s.Z0)
			x1, y1 := pt(s.X1, s.Y1, s.Z1)
			line(img, x0, y0, x1, y1, col)
		}
	}
	if last >= 0 {
		sg := &m.Segs[last]
		x, y := pt(sg.X1, sg.Y1, sg.Z1)
		dot(img, x, y, 5, color.RGBA{255, 255, 255, 255})
	}
	return img
}

// dot draws a filled circle with a dark edge.
func dot(img *image.RGBA, cx, cy, r int, c color.RGBA) {
	for y := -r - 1; y <= r+1; y++ {
		for x := -r - 1; x <= r+1; x++ {
			d := x*x + y*y
			p := image.Pt(cx+x, cy+y)
			if !p.In(img.Bounds()) {
				continue
			}
			switch {
			case d <= r*r:
				img.SetRGBA(p.X, p.Y, c)
			case d <= (r+1)*(r+1):
				img.SetRGBA(p.X, p.Y, color.RGBA{20, 20, 20, 255})
			}
		}
	}
}

// LayerEnd returns the file offset up to which layer n (counting from 1) is printed. 0 gives -1: nothing yet.
func (m *Model) LayerEnd(n int) int64 {
	switch {
	case n <= 0:
		return -1
	case n >= len(m.LayerPos):
		return 1<<62 - 1
	}
	// A move is recorded at the end of its line. The last move of a layer ends where the next marker starts.
	return m.LayerPos[n]
}

// LayerStart returns the file offset where layer n (counting from 1) starts.
func (m *Model) LayerStart(n int) int64 {
	if n <= 0 || n > len(m.LayerPos) {
		return 0
	}
	return m.LayerPos[n-1]
}

func mix(a, b color.RGBA, t float64) color.RGBA {
	l := func(x, y uint8) uint8 { return uint8(float64(x)*(1-t) + float64(y)*t) }
	return color.RGBA{l(a.R, b.R), l(a.G, b.G), l(a.B, b.B), 255}
}

// line draws a 2 pixel wide Bresenham line.
func line(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	b := img.Bounds()
	for {
		for _, d := range [2]image.Point{{0, 0}, {1, 0}} {
			p := image.Pt(x0+d.X, y0+d.Y)
			if p.In(b) {
				img.SetRGBA(p.X, p.Y, c)
			}
		}
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

// ZAt returns the height of the print head at file offset pos.
func (m *Model) ZAt(pos int64) float32 {
	lo, hi := 0, len(m.Segs)
	for lo < hi {
		mid := (lo + hi) / 2
		if m.Segs[mid].Pos <= pos {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo == 0 {
		return 0
	}
	return m.Segs[lo-1].Z1
}

// LayerAt returns the layer (counting from 1) that file offset pos is in, or 0 before the first layer.
func (m *Model) LayerAt(pos int64) int {
	lo, hi := 0, len(m.LayerPos)
	for lo < hi {
		mid := (lo + hi) / 2
		if m.LayerPos[mid] <= pos {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

// FanAt returns the fan speed (0..1) that the G-code has set at file offset pos.
func (m *Model) FanAt(pos int64) float32 {
	lo, hi := 0, len(m.FanPos)
	for lo < hi {
		mid := (lo + hi) / 2
		if m.FanPos[mid] <= pos {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo == 0 {
		return 0
	}
	return m.FanVal[lo-1]
}

// At returns the move at file offset pos: the last one that is done.
func (m *Model) At(pos int64) (Seg, bool) {
	lo, hi := 0, len(m.Segs)
	for lo < hi {
		mid := (lo + hi) / 2
		if m.Segs[mid].Pos <= pos {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo == 0 {
		return Seg{}, false
	}
	return m.Segs[lo-1], true
}

// layerCount reads the number of layers: ";LAYER_COUNT:n" (Cura), "; total layer number: n" (OrcaSlicer).
func layerCount(line string) (int, bool) {
	for _, p := range []string{";LAYER_COUNT:", "; total layer number:", "; total layers count ="} {
		if v, ok := strings.CutPrefix(line, p); ok {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			return n, err == nil
		}
	}
	return 0, false
}

// objectName reads the name of an object: ";MESH:name" (Cura), "EXCLUDE_OBJECT_DEFINE NAME=name"
// (Klipper, OrcaSlicer and PrusaSlicer), "; printing object name" (OrcaSlicer and PrusaSlicer).
// Cura's ";MESH:NONMESH" is the travel between objects, not an object.
func objectName(line string) (mark, name string, ok bool) {
	line = strings.TrimSpace(line)
	if v, ok := strings.CutPrefix(line, ";MESH:"); ok {
		return "mesh", v, v != "NONMESH" && v != ""
	}
	if v, ok := strings.CutPrefix(line, "EXCLUDE_OBJECT_DEFINE "); ok {
		for _, w := range strings.Fields(v) {
			if n, ok := strings.CutPrefix(w, "NAME="); ok {
				return "define", n, n != ""
			}
		}
	}
	if v, ok := strings.CutPrefix(line, "; printing object "); ok {
		return "printing", v, v != ""
	}
	return "", "", false
}
