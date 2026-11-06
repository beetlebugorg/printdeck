// Package platepack is the layout of the objects of a plate. It is a copy of platepack.py of
// printdeck-action, which places the objects of the slice. The plan page uses it to show the
// plate that the slice makes, and to check that the parts fit. The two must give the same places:
// testdata/pack_cases.json has cases from platepack.py.
//
// The outline of an object is its support function on N fixed directions: for each direction,
// the largest distance of the object from its reference point (the middle of its box) in that
// direction. These N lines make a convex polygon around the object. A turn of the object by a
// multiple of 360/N degrees moves the values along the list.
//
// The layout puts the largest object first. For each object and each turn, the free places are
// the places where the object is on the bed and keeps Gap mm from each object on the bed. The
// candidates are the corners of the polygons of the free area. The layout picks the candidate
// that gives the smallest box around all the objects, then the lowest Y, then the lowest X. At
// the end, the box of the objects goes to the middle of the bed. The brim or the skirt (the
// margin) of each object stays on the bed. The brims of two objects can merge.
package platepack

import (
	"math"
	"sort"
)

const (
	N    = 64 // the directions of an outline
	Turn = 4  // the turns are multiples of Turn directions: 22.5 degrees
	eps  = 1e-6
	// Gap is the room between two objects, and Edge the room at the edges of the bed, in mm.
	Gap  = 2.0
	Edge = 0.0
	// MaxPack is the most objects for Pack. A plate with more objects goes in the grid.
	MaxPack = 12
	// GridEdge is the room at the edges of the bed in the grid.
	GridEdge = 10.0
)

type pt struct{ x, y float64 }

var dirs = func() (d [N]pt) {
	for i := range d {
		a := 2 * math.Pi * float64(i) / N
		d[i] = pt{round12(math.Cos(a)), round12(math.Sin(a))}
	}
	return
}()

func round12(v float64) float64 { return math.Round(v*1e12) / 1e12 }

// Outline is the outline of an object: N values, in mm.
type Outline []float64

// Up is v rounded up to 0.01 mm, as the outlines of the plan are.
func Up(v float64) float64 { return math.Ceil(math.Round(v*100*1e6)/1e6) / 100 }

// RectOutline is the outline of a box w x d about its middle.
func RectOutline(w, d float64) Outline {
	h := make(Outline, N)
	for i, u := range dirs {
		h[i] = Up(math.Abs(u.x)*w/2 + math.Abs(u.y)*d/2)
	}
	return h
}

// Turned is the outline h turned by k directions, counterclockwise.
func Turned(h Outline, k int) Outline {
	out := make(Outline, N)
	for i := range out {
		out[i] = h[((i-k)%N+N)%N]
	}
	return out
}

// TurnDegrees is a turn of k directions in degrees.
func TurnDegrees(k int) float64 { return 360 * float64(k) / N }

// Size is the size (w, d) of the box of the outline.
func Size(h Outline) (float64, float64) { return h[0] + h[N/2], h[N/4] + h[3*N/4] }

func corners(h Outline) []pt {
	out := make([]pt, N)
	for i := range out {
		a, b := dirs[i], dirs[(i+1)%N]
		e, f := h[i], h[(i+1)%N]
		det := a.x*b.y - a.y*b.x
		out[i] = pt{(e*b.y - a.y*f) / det, (a.x*f - e*b.x) / det}
	}
	return out
}

// Corners are the corners of the polygon of the outline at the point (x, y), with no two corners
// at the same place, for a drawing.
func Corners(h Outline, x, y float64) [][2]float64 {
	var out [][2]float64
	for _, p := range poly(h, pt{x, y}) {
		out = append(out, [2]float64{p.x, p.y})
	}
	return out
}

func poly(h Outline, at pt) []pt {
	var out []pt
	for _, c := range corners(h) {
		q := pt{at.x + c.x, at.y + c.y}
		if len(out) == 0 || math.Abs(q.x-out[len(out)-1].x) > eps || math.Abs(q.y-out[len(out)-1].y) > eps {
			out = append(out, q)
		}
	}
	for len(out) > 1 && math.Abs(out[0].x-out[len(out)-1].x) <= eps && math.Abs(out[0].y-out[len(out)-1].y) <= eps {
		out = out[:len(out)-1]
	}
	return out
}

func area(h Outline) float64 {
	p := corners(h)
	s := 0.0
	for i := range p {
		j := (i + 1) % N
		s += p[i].x*p[j].y - p[j].x*p[i].y
	}
	return math.Abs(s) / 2
}

func iround(v float64) int64 { return int64(math.Floor(v*1000 + 0.5)) }

func outside(q pt, h Outline) bool {
	m := math.Inf(-1)
	for i, u := range dirs {
		m = math.Max(m, u.x*q.x+u.y*q.y-h[i])
	}
	return m >= -eps
}

func crossing(p1, p2, p3, p4 pt) (pt, bool) {
	d1x, d1y := p2.x-p1.x, p2.y-p1.y
	d2x, d2y := p4.x-p3.x, p4.y-p3.y
	den := d1x*d2y - d1y*d2x
	if math.Abs(den) < 1e-12 {
		return pt{}, false
	}
	t := ((p3.x-p1.x)*d2y - (p3.y-p1.y)*d2x) / den
	u := ((p3.x-p1.x)*d1y - (p3.y-p1.y)*d1x) / den
	if -eps <= t && t <= 1+eps && -eps <= u && u <= 1+eps {
		return pt{p1.x + t*d1x, p1.y + t*d1y}, true
	}
	return pt{}, false
}

type box struct{ x0, y0, x1, y1 float64 }

func boxOf(h Outline, p pt, margin float64) box {
	return box{p.x - h[N/2] - margin, p.y - h[3*N/4] - margin, p.x + h[0] + margin, p.y + h[N/4] + margin}
}

// Place is the place of an object: the offset of its reference point from the middle of the bed
// in mm, and its turn in directions.
type Place struct {
	X, Y float64
	K    int
}

type key [4]int64

func less(a, b key) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// Pack places the objects with the outlines on a bed w x d: nil for an object that does not
// fit. One object goes to the middle with no turn.
func Pack(outlines []Outline, w, d, margin float64) []*Place {
	out := make([]*Place, len(outlines))
	if len(outlines) == 1 {
		h := outlines[0]
		if h[0]+h[N/2]+2*margin <= w+eps && h[N/4]+h[3*N/4]+2*margin <= d+eps {
			out[0] = &Place{}
		}
		return out
	}
	order := make([]int, len(outlines))
	areas := make([]int64, len(outlines))
	for i := range order {
		order[i] = i
		areas[i] = iround(area(outlines[i]))
	}
	sort.SliceStable(order, func(a, b int) bool { return areas[order[a]] > areas[order[b]] })
	type placed struct {
		h Outline
		p pt
	}
	type nfp struct {
		at pt
		h  Outline
		cs []pt
		bb box
	}
	var on []placed
	for _, i := range order {
		var best *key
		var bestH Outline
		var bestQ pt
		bestK := 0
		seen := map[[N]float64]bool{}
		for k := 0; k < N; k += Turn {
			h := Turned(outlines[i], k)
			var sk [N]float64
			copy(sk[:], h)
			if seen[sk] {
				continue
			}
			seen[sk] = true
			x0, x1 := -w/2+Edge+margin+h[N/2], w/2-Edge-margin-h[0]
			y0, y1 := -d/2+Edge+margin+h[3*N/4], d/2-Edge-margin-h[N/4]
			if x0 > x1+eps || y0 > y1+eps {
				continue
			}
			var nfps []nfp
			for _, a := range on {
				hn := make(Outline, N)
				for j := range hn {
					hn[j] = a.h[j] + h[(j+N/2)%N] + Gap
				}
				cs := poly(hn, a.p)
				bb := box{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
				for _, c := range cs {
					bb.x0, bb.y0, bb.x1, bb.y1 = math.Min(bb.x0, c.x), math.Min(bb.y0, c.y), math.Max(bb.x1, c.x), math.Max(bb.y1, c.y)
				}
				nfps = append(nfps, nfp{a.p, hn, cs, bb})
			}
			rect := []pt{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}
			cands := append([]pt(nil), rect...)
			for _, n := range nfps {
				bb := n.bb
				if bb.x1 < x0-eps || bb.x0 > x1+eps || bb.y1 < y0-eps || bb.y0 > y1+eps {
					continue
				}
				cands = append(cands, n.cs...)
				for j := range n.cs {
					a, b := n.cs[j], n.cs[(j+1)%len(n.cs)]
					for r := 0; r < 4; r++ {
						if q, ok := crossing(a, b, rect[r], rect[(r+1)%4]); ok {
							cands = append(cands, q)
						}
					}
				}
			}
			for m := range nfps {
				for n := m + 1; n < len(nfps); n++ {
					bm, bn := nfps[m].bb, nfps[n].bb
					if bm.x1 < bn.x0 || bn.x1 < bm.x0 || bm.y1 < bn.y0 || bn.y1 < bm.y0 {
						continue
					}
					cm, cn := nfps[m].cs, nfps[n].cs
					for j := range cm {
						a, b := cm[j], cm[(j+1)%len(cm)]
						ex0, ex1 := math.Min(a.x, b.x), math.Max(a.x, b.x)
						ey0, ey1 := math.Min(a.y, b.y), math.Max(a.y, b.y)
						for l := range cn {
							c, e := cn[l], cn[(l+1)%len(cn)]
							if math.Max(c.x, e.x) < ex0 || math.Min(c.x, e.x) > ex1 || math.Max(c.y, e.y) < ey0 || math.Min(c.y, e.y) > ey1 {
								continue
							}
							if q, ok := crossing(a, b, c, e); ok {
								cands = append(cands, q)
							}
						}
					}
				}
			}
			u := box{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
			for _, a := range on {
				b := boxOf(a.h, a.p, margin)
				u = box{math.Min(u.x0, b.x0), math.Min(u.y0, b.y0), math.Max(u.x1, b.x1), math.Max(u.y1, b.y1)}
			}
			type scored struct {
				k key
				q pt
			}
			var sc []scored
			for _, q := range cands {
				if q.x < x0-eps || q.x > x1+eps || q.y < y0-eps || q.y > y1+eps {
					continue
				}
				q = pt{math.Min(math.Max(q.x, x0), x1), math.Min(math.Max(q.y, y0), y1)}
				b := boxOf(h, q, margin)
				size := math.Max(u.x1, b.x1) - math.Min(u.x0, b.x0) + math.Max(u.y1, b.y1) - math.Min(u.y0, b.y0)
				sc = append(sc, scored{key{iround(size), iround(q.y), iround(q.x), int64(k)}, q})
			}
			sort.SliceStable(sc, func(a, b int) bool { return less(sc[a].k, sc[b].k) })
			for _, s := range sc {
				if best != nil && !less(s.k, *best) {
					break
				}
				free := true
				for _, n := range nfps {
					if !outside(pt{s.q.x - n.at.x, s.q.y - n.at.y}, n.h) {
						free = false
						break
					}
				}
				if free {
					kk := s.k
					best, bestH, bestQ, bestK = &kk, h, s.q, k
					break
				}
			}
		}
		if best != nil {
			on = append(on, placed{bestH, bestQ})
			out[i] = &Place{bestQ.x, bestQ.y, bestK}
		}
	}
	if len(on) == 0 {
		return out
	}
	u := box{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, a := range on {
		b := boxOf(a.h, a.p, margin)
		u = box{math.Min(u.x0, b.x0), math.Min(u.y0, b.y0), math.Max(u.x1, b.x1), math.Max(u.y1, b.y1)}
	}
	sx, sy := -(u.x0+u.x1)/2, -(u.y0+u.y1)/2
	for _, p := range out {
		if p != nil {
			p.X, p.Y = float64(iround(p.X+sx))/1000, float64(iround(p.Y+sy))/1000
		}
	}
	return out
}

// Grid places the objects as the earlier layout: in rows, each in a cell of the size of the
// largest object with its margin, GridEdge mm from the edges, and the grid at the middle. It
// returns nil when they do not fit, and the number of objects that fit.
func Grid(outlines []Outline, w, d, margin float64) ([]Place, int) {
	var cw, cd float64
	for _, h := range outlines {
		sw, sd := Size(h)
		cw, cd = math.Max(cw, sw+2*margin), math.Max(cd, sd+2*margin)
	}
	n := len(outlines)
	cols := math.Max(1, math.Floor((w-2*GridEdge)/cw+eps))
	fit := int(cols * math.Max(1, math.Floor((d-2*GridEdge)/cd+eps)))
	rows := math.Ceil(float64(n) / cols)
	if rows*cd > d-2*GridEdge+eps {
		return nil, min(fit, n)
	}
	cols = math.Min(cols, float64(n))
	out := make([]Place, n)
	for i := range out {
		r, c := float64(i/int(cols)), float64(i%int(cols))
		out[i] = Place{float64(iround((c-(cols-1)/2)*cw)) / 1000, float64(iround(((rows-1)/2-r)*cd)) / 1000, 0}
	}
	return out, n
}

// Plate places the objects of a plate: nil when they do not fit, and the number of objects that
// fit. One object goes to the middle. Up to MaxPack objects go in Pack, else in the grid; when
// Pack does not place all the objects, the grid can.
func Plate(outlines []Outline, w, d, margin float64) ([]Place, int) {
	n := len(outlines)
	if n == 0 {
		return []Place{}, 0
	}
	if n > MaxPack {
		return Grid(outlines, w, d, margin)
	}
	p := Pack(outlines, w, d, margin)
	placed := 0
	out := make([]Place, n)
	for i, x := range p {
		if x != nil {
			placed++
			out[i] = *x
		}
	}
	if placed == n {
		return out, n
	}
	if n == 1 {
		return nil, 0
	}
	if g, _ := Grid(outlines, w, d, margin); g != nil {
		return g, n
	}
	_, fit := Grid(outlines, w, d, margin)
	return nil, max(placed, fit)
}
