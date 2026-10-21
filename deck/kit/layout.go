package kit

import "image"

// Rect is a rectangle of the screen, in pixels.
type Rect = image.Rectangle

// Size is the size of one part of a row or a column: a number of pixels (Px), or a share of the
// free space (Fr).
type Size struct {
	px int
	fr float64
}

// Px is a fixed size in pixels.
func Px(n int) Size { return Size{px: n} }

// Fr is a share of the free space: the space that the Px sizes and the gaps do not use. Two parts
// of Fr(1) get the same space, and Fr(2) gets twice the space of Fr(1).
func Fr(f float64) Size { return Size{fr: f} }

// Rows divides r from the top to the bottom into one row for each size, with gap pixels between
// the rows.
func Rows(r Rect, gap int, sizes ...Size) []Rect {
	parts := divide(r.Dy(), gap, sizes)
	out := make([]Rect, len(parts))
	for i, p := range parts {
		out[i] = Rect{Min: image.Pt(r.Min.X, r.Min.Y+p[0]), Max: image.Pt(r.Max.X, r.Min.Y+p[1])}
	}
	return out
}

// Cols divides r from the left to the right into one column for each size, with gap pixels
// between the columns.
func Cols(r Rect, gap int, sizes ...Size) []Rect {
	parts := divide(r.Dx(), gap, sizes)
	out := make([]Rect, len(parts))
	for i, p := range parts {
		out[i] = Rect{Min: image.Pt(r.Min.X+p[0], r.Min.Y), Max: image.Pt(r.Min.X+p[1], r.Max.Y)}
	}
	return out
}

// divide gives the start and the end of each part of a length. The Fr parts share the free space.
// The last Fr part gets the pixels that the rounding leaves, so the parts fill the length.
func divide(length, gap int, sizes []Size) [][2]int {
	free := length - gap*max(0, len(sizes)-1)
	var fr float64
	last := -1
	for i, s := range sizes {
		free -= s.px
		fr += s.fr
		if s.fr > 0 {
			last = i
		}
	}
	free = max(0, free)
	out := make([][2]int, len(sizes))
	pos, used := 0, 0
	for i, s := range sizes {
		n := s.px
		if s.fr > 0 {
			share := int(float64(free) * s.fr / fr)
			if i == last {
				share = free - used
			}
			used += share
			n += share
		}
		out[i] = [2]int{pos, pos + n}
		pos += n + gap
	}
	return out
}

// Grid divides r into cols x rows cells of the same size, with gap pixels between them. The cells
// are in reading order: the first row from the left, then the next row.
func Grid(r Rect, cols, rows, gap int) []Rect {
	var out []Rect
	for _, row := range Rows(r, gap, repeat(rows)...) {
		out = append(out, Cols(row, gap, repeat(cols)...)...)
	}
	return out
}

func repeat(n int) []Size {
	s := make([]Size, n)
	for i := range s {
		s[i] = Fr(1)
	}
	return s
}

// Inset makes r smaller by the four margins: left, top, right and bottom.
func Inset(r Rect, left, top, right, bottom int) Rect {
	return Rect{Min: image.Pt(r.Min.X+left, r.Min.Y+top), Max: image.Pt(r.Max.X-right, r.Max.Y-bottom)}
}

// Center returns a rectangle of the size w x h in the middle of r.
func Center(r Rect, w, h int) Rect {
	x, y := r.Min.X+(r.Dx()-w)/2, r.Min.Y+(r.Dy()-h)/2
	return image.Rect(x, y, x+w, y+h)
}

// A Stack gives rectangles one under the other, from the top of an area. Next takes the next
// rectangle, and Rest is the space under the last one.
type Stack struct {
	r   Rect
	y   int
	gap int
}

// NewStack starts a stack at the top of r, with gap pixels between its rectangles.
func NewStack(r Rect, gap int) *Stack { return &Stack{r: r, y: r.Min.Y, gap: gap} }

// Next returns the next rectangle, h pixels high and as wide as the area.
func (s *Stack) Next(h int) Rect {
	out := image.Rect(s.r.Min.X, s.y, s.r.Max.X, s.y+h)
	s.y += h + s.gap
	return out
}

// Skip leaves h pixels free.
func (s *Stack) Skip(h int) { s.y += h }

// Y is the top of the next rectangle.
func (s *Stack) Y() int { return s.y }

// Rest returns the space under the last rectangle, to the bottom of the area.
func (s *Stack) Rest() Rect { return image.Rect(s.r.Min.X, s.y, s.r.Max.X, max(s.y, s.r.Max.Y)) }
