package gcode

// Object data for Klipper's [exclude_object] in Cura files. Cura marks the moves of each object
// with ";MESH:<name>", and the travel between objects with ";MESH:NONMESH". Klipper reads the
// objects from EXCLUDE_OBJECT_DEFINE lines, and the adaptive bed mesh probes only the area
// under them. The deck adds these lines when a file comes onto the deck, the same as
// preprocess_cancellation of Moonraker:
//
//	EXCLUDE_OBJECT_DEFINE NAME=<name> CENTER=<x>,<y> POLYGON=[[x,y],...]
//
// before the first command of the file, and EXCLUDE_OBJECT_START and EXCLUDE_OBJECT_END around
// the moves of each object in each layer. The polygon is the convex hull of the XY points of the
// extruding moves of the object, with at most polygonMax vertices. CENTER is the center of the
// bounding box of the polygon.

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// hullKeep is the count of points of an object above which the points go down to their hull.
// hullMax is the most vertices of a hull during the read: a larger hull loses every second vertex.
// polygonMax is the most vertices of a polygon in the file.
const (
	hullKeep   = 4096
	hullMax    = 1024
	polygonMax = 64
)

type point struct{ x, y float64 }

// object is an object of the file: its name for Klipper and the points of its extruding moves.
type object struct {
	name string
	pts  []point
}

func (o *object) add(p point) {
	o.pts = append(o.pts, p)
	if len(o.pts) > hullKeep {
		o.pts = hull(o.pts)
		for len(o.pts) > hullMax {
			o.pts = halve(o.pts)
		}
	}
}

// Objects are the objects of a Cura file, in the order of their first mark.
type Objects struct {
	list   []*object
	byMesh map[string]*object // by the name after ";MESH:"
}

// ObjectID returns the name of an object for Klipper, from the name of the slicer. As
// preprocess_cancellation does, it replaces each run of characters that are not letters, digits
// or '_' with one '_', and removes '_' at the two ends. Klipper upper-cases the name.
func ObjectID(name string) string {
	var b strings.Builder
	under := false
	for _, r := range name {
		if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			under = false
		} else if !under {
			b.WriteByte('_')
			under = true
		}
	}
	id := strings.Trim(b.String(), "_")
	if id == "" {
		id = "object"
	}
	return id
}

// meshName returns the name of a ";MESH:" line, and false for a line that is not a mesh mark.
func meshName(line string) (string, bool) {
	v, ok := strings.CutPrefix(strings.TrimSpace(line), ";MESH:")
	return strings.TrimSpace(v), ok
}

// ReadObjects reads the objects of a Cura file. It returns nil for a file that needs no object
// data: a file with EXCLUDE_OBJECT_DEFINE lines, or with no ";MESH:" objects.
func ReadObjects(r io.Reader) (*Objects, error) {
	br := bufio.NewReaderSize(r, 1<<16)
	o := &Objects{byMesh: map[string]*object{}}
	byID := map[string]*object{}
	var cur, at *object // the object of the moves, and the object of the move to the position
	var x, y, e float64
	relXYZ, relE := false, false
	for {
		line, err := br.ReadString('\n')
		if name, ok := meshName(line); ok {
			cur = nil
			if name != "NONMESH" && name != "" {
				cur = o.byMesh[name]
				if cur == nil {
					id := ObjectID(name)
					if cur = byID[id]; cur == nil {
						cur = &object{name: id}
						byID[id] = cur
						o.list = append(o.list, cur)
					}
					o.byMesh[name] = cur
				}
			}
		} else if strings.HasPrefix(strings.TrimSpace(line), "EXCLUDE_OBJECT_DEFINE") {
			return nil, nil
		}
		if i := strings.IndexByte(line, ';'); i >= 0 {
			line = line[:i]
		}
		if f := strings.Fields(line); len(f) > 0 {
			switch strings.ToUpper(f[0]) {
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
			case "G0", "G1", "G00", "G01", "G2", "G3", "G02", "G03":
				nx, ny, ne := x, y, e
				for _, w := range f[1:] {
					if v, ok := word(w, 'X'); ok {
						nx = pick(relXYZ, x, v)
					} else if v, ok := word(w, 'Y'); ok {
						ny = pick(relXYZ, y, v)
					} else if v, ok := word(w, 'E'); ok {
						ne = pick(relE, e, v)
					}
				}
				if cur != nil && ne > e && (nx != x || ny != y) {
					// The start of the move is a point of the object only if a move of the
					// object went there. The first move after a mark can start at another object.
					if at == cur {
						cur.add(point{x, y})
					}
					cur.add(point{nx, ny})
				}
				x, y, e, at = nx, ny, ne, cur
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	if len(o.list) == 0 {
		return nil, nil
	}
	for _, ob := range o.list {
		ob.pts = simplify(hull(ob.pts), polygonMax)
	}
	return o, nil
}

// Count returns the number of objects.
func (o *Objects) Count() int { return len(o.list) }

// define returns the EXCLUDE_OBJECT_DEFINE line of an object.
func (ob *object) define() string {
	if len(ob.pts) == 0 {
		return "EXCLUDE_OBJECT_DEFINE NAME=" + ob.name + "\n"
	}
	minX, minY, maxX, maxY := ob.pts[0].x, ob.pts[0].y, ob.pts[0].x, ob.pts[0].y
	var poly strings.Builder
	for i, p := range ob.pts {
		minX, maxX = min(minX, p.x), max(maxX, p.x)
		minY, maxY = min(minY, p.y), max(maxY, p.y)
		if i > 0 {
			poly.WriteByte(',')
		}
		fmt.Fprintf(&poly, "[%.3f,%.3f]", p.x, p.y)
	}
	return fmt.Sprintf("EXCLUDE_OBJECT_DEFINE NAME=%s CENTER=%.3f,%.3f POLYGON=[%s]\n",
		ob.name, (minX+maxX)/2, (minY+maxY)/2, poly.String())
}

// Write copies a file from r to w, with the object data. The define lines go before the first
// command of the file. EXCLUDE_OBJECT_START goes after each ";MESH:" line of an object, and
// EXCLUDE_OBJECT_END goes before the next ";MESH:", ";LAYER:" or ";TIME_ELAPSED:" line, and at
// the end of the file. Thus each START has an END in the same layer.
func (o *Objects) Write(r io.Reader, w io.Writer) error {
	br := bufio.NewReaderSize(r, 1<<16)
	bw := bufio.NewWriterSize(w, 1<<16)
	defined := false
	cur := ""
	endLine := true // the last line written ends with a newline
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			t := strings.TrimSpace(line)
			name, mesh := meshName(line)
			if !defined && (mesh || (t != "" && !strings.HasPrefix(t, ";"))) {
				for _, ob := range o.list {
					bw.WriteString(ob.define())
				}
				defined = true
			}
			if cur != "" && (mesh || strings.HasPrefix(t, ";LAYER:") || strings.HasPrefix(t, ";TIME_ELAPSED:")) {
				bw.WriteString("EXCLUDE_OBJECT_END NAME=" + cur + "\n")
				cur = ""
			}
			bw.WriteString(line)
			endLine = strings.HasSuffix(line, "\n")
			if ob := o.byMesh[name]; mesh && ob != nil {
				if !endLine {
					bw.WriteString("\n")
				}
				bw.WriteString("EXCLUDE_OBJECT_START NAME=" + ob.name + "\n")
				cur, endLine = ob.name, true
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	if cur != "" {
		if !endLine {
			bw.WriteString("\n")
		}
		bw.WriteString("EXCLUDE_OBJECT_END NAME=" + cur + "\n")
	}
	return bw.Flush()
}

// IsFile returns whether a name has the extension of a G-code file.
func IsFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".gcode", ".g", ".gco":
		return true
	}
	return false
}

// AddObjects adds the object data to the Cura file at path, if the file needs it. It writes the
// new file to a temporary file in the same folder, and then renames it over the file. The caller
// checks that the file is a G-code file (IsFile).
func AddObjects(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	o, err := ReadObjects(f)
	if err != nil || o == nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(path), fmt.Sprintf(".objects-%d", rand.Uint64()))
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer os.Remove(tmp) // after the rename this does nothing
	err = o.Write(f, out)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// hull returns the convex hull of points, counterclockwise (Andrew's monotone chain).
func hull(pts []point) []point {
	if len(pts) < 3 {
		return dedupe(pts)
	}
	sort.Slice(pts, func(i, j int) bool {
		if pts[i].x != pts[j].x {
			return pts[i].x < pts[j].x
		}
		return pts[i].y < pts[j].y
	})
	cross := func(o, a, b point) float64 { return (a.x-o.x)*(b.y-o.y) - (a.y-o.y)*(b.x-o.x) }
	h := make([]point, 0, 2*len(pts))
	for _, p := range pts {
		for len(h) >= 2 && cross(h[len(h)-2], h[len(h)-1], p) <= 0 {
			h = h[:len(h)-1]
		}
		h = append(h, p)
	}
	lower := len(h) + 1
	for i := len(pts) - 2; i >= 0; i-- {
		p := pts[i]
		for len(h) >= lower && cross(h[len(h)-2], h[len(h)-1], p) <= 0 {
			h = h[:len(h)-1]
		}
		h = append(h, p)
	}
	return h[:len(h)-1]
}

// halve returns every second vertex of a convex polygon. The result is a convex polygon inside it.
func halve(pts []point) []point {
	out := pts[:0]
	for i := 0; i < len(pts); i += 2 {
		out = append(out, pts[i])
	}
	return out
}

// simplify removes vertices of a convex polygon until it has n vertices. It removes the vertex
// with the smallest triangle to its two neighbors first (Visvalingam), so the polygon loses the
// least area.
func simplify(pts []point, n int) []point {
	area := func(a, b, c point) float64 {
		return math.Abs((b.x-a.x)*(c.y-a.y) - (b.y-a.y)*(c.x-a.x))
	}
	for len(pts) > n {
		k, least := 0, math.Inf(1)
		for i := range pts {
			a := area(pts[(i+len(pts)-1)%len(pts)], pts[i], pts[(i+1)%len(pts)])
			if a < least {
				k, least = i, a
			}
		}
		pts = append(pts[:k], pts[k+1:]...)
	}
	return pts
}

func dedupe(pts []point) []point {
	if len(pts) == 2 && pts[0] == pts[1] {
		return pts[:1]
	}
	return pts
}
