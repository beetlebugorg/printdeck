// Package gcode reads the extrusion moves of a G-code file and draws them as a 3D picture.
package gcode

import (
	"bufio"
	"image"
	"image/color"
	"io"
	"math"
	"sort"
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

// Change is a color change of a file: from the file offset Pos, the print has the filament of the
// roll with the ID Roll of the deck.
type Change struct {
	Pos  int64
	Roll string
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

	LayerPos []int64 // offset of each ";LAYER:n" marker
	// Changes are the color changes of the file (PRINTDECK_PAUSE ROLL), in their order.
	Changes    []Change
	FanPos     []int64   // offset after each fan command
	FanVal     []float32 // fan speed 0..1 after the command
	LayerCount int

	// Objects is the number of objects on the bed, from the object marks of the slicer; 0 if it has none.
	Objects int

	// The timeline of the print: TimePos are file offsets, TimeVal the seconds of printing at each
	// offset, from the start of the moves. The marks come from the slicer (Cura ";TIME_ELAPSED:",
	// the "M73 R" lines of PrusaSlicer and OrcaSlicer). A file with no marks gets a timeline from
	// the lengths and the speeds of its moves. TimeTotal is the time of the whole print.
	TimePos    []int64
	TimeVal    []float32
	TimeTotal  float32
	TimeSlicer bool // the timeline comes from the slicer
}

// TimeAt is the time of printing at the file offset pos, from the timeline: the seconds before
// pos, and the seconds of the whole print. ok is false for a file with no timeline.
func (m *Model) TimeAt(pos int64) (elapsed, total float64, ok bool) {
	n := len(m.TimePos)
	if n == 0 || m.TimeTotal <= 0 {
		return 0, 0, false
	}
	i := sort.Search(n, func(i int) bool { return m.TimePos[i] > pos })
	switch {
	case i == 0:
		return 0, float64(m.TimeTotal), true
	case i == n:
		return float64(m.TimeVal[n-1]), float64(m.TimeTotal), true
	}
	// Between two marks, the time follows the offset.
	p0, p1, t0, t1 := m.TimePos[i-1], m.TimePos[i], m.TimeVal[i-1], m.TimeVal[i]
	f := float64(pos-p0) / float64(max(1, p1-p0))
	return float64(t0) + f*float64(t1-t0), float64(m.TimeTotal), true
}

// timeline collects the marks of a timeline: offsets, and the seconds at each one.
type timeline struct {
	pos   []int64
	val   []float32
	total float64
}

func (t *timeline) add(pos int64, sec float64) {
	t.pos, t.val = append(t.pos, pos), append(t.val, float32(sec))
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
	// The timelines: the marks of the slicer, and the time of the moves at each layer.
	var slicer, moves timeline
	moveT, m73Total := 0.0, 0.0
	for {
		line, err := br.ReadString('\n')
		pos += int64(len(line))
		if t, ok := strings.CutPrefix(line, ";TYPE:"); ok {
			kind = kindOf(strings.TrimSpace(t))
		} else if strings.HasPrefix(line, ";LAYER:") || strings.HasPrefix(line, ";LAYER_CHANGE") {
			// Cura writes ";LAYER:n", PrusaSlicer and OrcaSlicer ";LAYER_CHANGE".
			m.LayerPos = append(m.LayerPos, pos-int64(len(line)))
			moves.add(pos-int64(len(line)), moveT)
		} else if v, ok := strings.CutPrefix(line, ";TIME_ELAPSED:"); ok {
			if t, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				slicer.add(pos, t)
			}
		} else if v, ok := strings.CutPrefix(line, ";TIME:"); ok {
			if t, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				slicer.total = t
			}
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
			case "PRINTDECK_PAUSE":
				for _, w := range f[1:] {
					if v, ok := strings.CutPrefix(strings.ToUpper(w), "ROLL="); ok {
						m.Changes = append(m.Changes, Change{Pos: pos, Roll: v})
					}
				}
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
			case "M73":
				// PrusaSlicer and OrcaSlicer: M73 P<percent> R<minutes left>. The first one has the
				// whole time.
				for _, w := range f[1:] {
					if r, ok := word(w, 'R'); ok {
						if m73Total == 0 {
							m73Total = r * 60
						}
						slicer.add(pos, m73Total-r*60)
						slicer.total = max(slicer.total, m73Total)
					}
				}
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
				if d := math.Sqrt(length*length + (nz-z)*(nz-z)); d > 0 && feed > 0 {
					moveT += d / (feed / 60)
				}
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
	moves.add(pos, moveT)
	moves.total = moveT
	// The marks of the slicer are better than the time of the moves: they know the accelerations.
	if len(slicer.pos) >= 2 {
		slicer.total = max(slicer.total, float64(slicer.val[len(slicer.val)-1]))
		m.TimePos, m.TimeVal, m.TimeTotal, m.TimeSlicer = slicer.pos, slicer.val, float32(slicer.total), true
	} else if moveT > 0 {
		m.TimePos, m.TimeVal, m.TimeTotal = moves.pos, moves.val, float32(moves.total)
	}
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
	// Directions to the key light and to the weaker fill light. The key light is at the left, so
	// that the two walls that face the viewer have different shades.
	keyLight  = unit([3]float64{-0.7, -0.25, 0.7})
	fillLight = unit([3]float64{0.7, 0.3, 0.2})
	// The direction to the viewer: the depth axis of project.
	viewer = [3]float64{-sinA * cosE, -cosA * cosE, sinE}
)

// project returns the screen coordinates of a point, and its depth (larger is nearer to the viewer).
func project(x, y, z float32) (u, v, d float64) {
	fx, fy, fz := float64(x), float64(y), float64(z)
	depth := fx*sinA + fy*cosA
	return fx*cosA - fy*sinA, -(depth*sinE + fz*cosE), -(depth * cosE) + fz*sinE
}

// lineWidth is the width of an extruded line in mm, for the picture.
const lineWidth = 0.45

// supersample is the scale at which Render draws, before it scales the picture down to smooth
// the edges.
const supersample = 2

// Render draws the model into a w x h picture. The part up to file offset pos is in the colour
// printed, with the layer that starts at curStart (the one being printed) in the colour hot and a
// mark at the end. The rest is in the colour ghost. The result has a transparent background.
//
// Each line is a round extrusion of the width of a line, in front of the lines that it hides. A
// key light, a fill light and a highlight shade it across its width, as plastic. A wall is lit at
// its side, which faces the viewer, and the other lines are lit as the top of a layer. The rest of
// the model does not hide the printed part.
func (m *Model) Render(w, h int, pos, curStart int64, printed, hot, ghost color.RGBA) *image.RGBA {
	return m.RenderColors(w, h, pos, curStart, []color.RGBA{printed}, hot, ghost)
}

// RenderColors is Render with a colour for each part of a print with color changes (Changes): the
// first colour before the first change, the second after it, and so on. The last colour is for
// the changes that have no colour of their own.
func (m *Model) RenderColors(w, h int, pos, curStart int64, colors []color.RGBA, hot, ghost color.RGBA) *image.RGBA {
	printed := colors[0]
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	if len(m.Segs) == 0 {
		return out
	}
	minU, minV, maxU, maxV := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for i := range m.Segs {
		s := &m.Segs[i]
		for _, p := range [2][3]float32{{s.X0, s.Y0, s.Z0}, {s.X1, s.Y1, s.Z1}} {
			u, v, _ := project(p[0], p[1], p[2])
			minU, maxU = math.Min(minU, u), math.Max(maxU, u)
			minV, maxV = math.Min(minV, v), math.Max(maxV, v)
		}
	}
	const pad = 8.0
	scale := math.Min((float64(w)-2*pad)/(maxU-minU), (float64(h)-2*pad)/(maxV-minV))
	ox := (float64(w) - (maxU-minU)*scale) / 2
	oy := (float64(h) - (maxV-minV)*scale) / 2
	pt := func(x, y, z float32) (float64, float64, float64) {
		u, v, d := project(x, y, z)
		return ox + (u-minU)*scale, oy + (v-minV)*scale, d
	}
	ss := supersample
	img := image.NewRGBA(image.Rect(0, 0, w*ss, h*ss))
	zbuf := make([]float64, w*ss*h*ss)
	for i := range zbuf {
		zbuf[i] = math.Inf(-1)
	}
	half := math.Max(1, lineWidth*scale*float64(ss)/2)
	last := -1 // the last move that is done: where the nozzle is
	// The printed part first, and then the rest in the pixels that the printed part leaves: the rest
	// does not hide the printed part.
	for pass := 0; pass < 2; pass++ {
		if pass == 1 {
			for i := range zbuf {
				if img.Pix[i*4+3] == 0 {
					zbuf[i] = math.Inf(-1)
				} else {
					zbuf[i] = math.Inf(1)
				}
			}
		}
		for i := range m.Segs {
			s := &m.Segs[i]
			done := s.Pos <= pos
			if (pass == 0) != done {
				continue
			}
			col := ghost
			if done {
				col = printed
				if s.Pos > curStart {
					col = hot
				}
				last = i
			}
			if len(colors) > 1 {
				// The part of the print after the changes before this move.
				k := 0
				for k < len(m.Changes) && m.Changes[k].Pos <= s.Pos {
					k++
				}
				part := colors[min(k, len(colors)-1)]
				if done && col != hot {
					col = part
				} else if !done {
					col = mix(ghost, part, 0.35) // the part to come, in a faint colour of its roll
				}
			}
			x0, y0, d0 := pt(s.X0, s.Y0, s.Z0)
			x1, y1, d1 := pt(s.X1, s.Y1, s.Z1)
			f := float64(ss)
			tube(img, zbuf, x0*f, y0*f, d0, x1*f, y1*f, d1, half, col, s)
		}
	}
	shrink(out, img, ss)
	// The mark of the nozzle, while the print has moves to do.
	if last >= 0 && last < len(m.Segs)-1 {
		sg := &m.Segs[last]
		x, y, _ := pt(sg.X1, sg.Y1, sg.Z1)
		dot(out, int(x), int(y), max(2, min(5, w/90)), color.RGBA{255, 255, 255, 255}) // smaller in a small picture
	}
	return out
}

// tubeSteps is the number of shades across the width of a line.
const tubeSteps = 9

// tube draws a line of the model from (x0, y0) to (x1, y1), with the depths d0 and d1 at its ends,
// half pixels wide at each side. The surface of the line is round across its width: a wall turns
// from its side to the top and the bottom, and a top line from the top to its sides. A pixel is
// drawn only when it is nearer than what zbuf has.
func tube(img *image.RGBA, zbuf []float64, x0, y0, d0, x1, y1, d1, half float64, c color.RGBA, s *Seg) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	// The direction across the line on the screen, and in the model.
	dx, dy := x1-x0, y1-y0
	l := math.Hypot(dx, dy)
	px, py := 0.0, 1.0
	if l > 0 {
		px, py = -dy/l, dx/l
	}
	// A wall is round from its top to its bottom. A top line is flatter: the lines of a top
	// surface touch each other.
	n := segNormal(s)
	var across [3]float64
	curve := 0.45
	if s.Kind == KindOuterWall || s.Kind == KindInnerWall {
		across, curve = [3]float64{0, 0, 1}, 1.0
	} else {
		across = unit([3]float64{float64(s.Y1 - s.Y0), float64(s.X0 - s.X1), 0})
	}
	// Turn across so that it goes the way of (px, py) on the screen.
	au := across[0]*cosA - across[1]*sinA
	av := -((across[0]*sinA+across[1]*cosA)*sinE + across[2]*cosE)
	if au*px+av*py < 0 {
		across = [3]float64{-across[0], -across[1], -across[2]}
	}
	// The shades across the width, from one side to the other.
	var shades [tubeSteps]color.RGBA
	for i := range shades {
		a := (float64(i)/(tubeSteps-1)*2 - 1) * curve // radians from the middle of the line
		sa, ca := math.Sincos(a)
		shades[i] = plastic(c, unit([3]float64{n[0]*ca + across[0]*sa, n[1]*ca + across[1]*sa, n[2]*ca + across[2]*sa}))
	}
	steps := int(math.Ceil(l)) + 1
	for i := 0; i < steps; i++ {
		t := 0.0
		if steps > 1 {
			t = float64(i) / float64(steps-1)
		}
		cx, cy := x0+dx*t, y0+dy*t
		d := d0 + (d1-d0)*t
		for k := -half; k <= half; k += 0.5 {
			x, y := int(cx+px*k), int(cy+py*k)
			if x < 0 || y < 0 || x >= w || y >= h {
				continue
			}
			if j := y*w + x; d > zbuf[j] {
				zbuf[j] = d
				img.SetRGBA(x, y, shades[int(math.Round((k/half+1)/2*(tubeSteps-1)))])
			}
		}
	}
}

// segNormal is the direction that the surface of a line faces. A wall faces out at its side,
// turned to the viewer. The other lines are the top of a layer, and face up.
func segNormal(s *Seg) [3]float64 {
	if s.Kind != KindOuterWall && s.Kind != KindInnerWall {
		return [3]float64{0, 0, 1}
	}
	n := unit([3]float64{float64(s.Y1 - s.Y0), float64(s.X0 - s.X1), 0})
	if n[0]*viewer[0]+n[1]*viewer[1] < 0 {
		n[0], n[1] = -n[0], -n[1]
	}
	return n
}

// plastic is the colour c of a surface that faces n: the key light and the fill light, and a
// highlight of the key light, as on plastic.
func plastic(c color.RGBA, n [3]float64) color.RGBA {
	dot := func(a, b [3]float64) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
	diffuse := 0.36 + 0.6*max(0, dot(n, keyLight)) + 0.22*max(0, dot(n, fillLight))
	spec := 0.35 * math.Pow(max(0, dot(n, halfway)), 28)
	f := func(v uint8) uint8 { return uint8(min(255, float64(v)*diffuse+255*spec)) }
	return color.RGBA{f(c.R), f(c.G), f(c.B), 255}
}

// halfway is the direction between the key light and the viewer, for the highlight.
var halfway = unit([3]float64{keyLight[0] + viewer[0], keyLight[1] + viewer[1], keyLight[2] + viewer[2]})

func unit(v [3]float64) [3]float64 {
	l := math.Sqrt(v[0]*v[0] + v[1]*v[1] + v[2]*v[2])
	if l == 0 {
		return v
	}
	return [3]float64{v[0] / l, v[1] / l, v[2] / l}
}

// shrink scales src down by the whole factor k into dst: each pixel of dst is the mean of k x k
// pixels of src. The colours of image.RGBA are premultiplied, so the edges blend into the
// transparent background.
func shrink(dst, src *image.RGBA, k int) {
	b := dst.Bounds()
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			var sum [4]int
			for j := 0; j < k; j++ {
				o := src.PixOffset(x*k, y*k+j)
				for i := 0; i < k; i++ {
					for c := 0; c < 4; c++ {
						sum[c] += int(src.Pix[o+i*4+c])
					}
				}
			}
			o := dst.PixOffset(x, y)
			for c := 0; c < 4; c++ {
				dst.Pix[o+c] = uint8(sum[c] / (k * k))
			}
		}
	}
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
// Cura's ";MESH:NONMESH" is the travel between objects. It does not mark an object.
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
