package kit

import "image"

// A List is a column of rows of the same height in an area, which the finger scrolls. The program
// keeps the scroll position, and gives a pointer to it in Scroll.
type List struct {
	Area   Rect
	N      int  // the number of rows
	RowH   int  // the height of a row, with the gap under it
	Gap    int  // the gap under a row
	Scroll *int // pixels from the top of the first row to the top of the area
}

// MaxScroll is the scroll position that shows the last row at the bottom of the area.
func (l List) MaxScroll() int { return max(0, l.N*l.RowH-l.Area.Dy()) }

// Clamp keeps the scroll position between the first row and MaxScroll, for example after rows go.
func (l List) Clamp() { *l.Scroll = max(0, min(*l.Scroll, l.MaxScroll())) }

// Row is the rectangle of row i at the scroll position. It can be outside the area.
func (l List) Row(i int) Rect {
	y := l.Area.Min.Y + i*l.RowH - *l.Scroll
	return image.Rect(l.Area.Min.X, y, l.Area.Max.X, y+l.RowH-l.Gap)
}

// Shows reports that row i is in the area, or a part of it.
func (l List) Shows(i int) bool { return l.Row(i).Overlaps(l.Area) }

// At returns the row at the point p, or -1.
func (l List) At(p image.Point) int {
	i := (p.Y - l.Area.Min.Y + *l.Scroll) / l.RowH
	if !p.In(l.Area) || i < 0 || i >= l.N {
		return -1
	}
	return i
}

// Reveal scrolls row i into the area.
func (l List) Reveal(i int) {
	y := i * l.RowH
	if y < *l.Scroll {
		*l.Scroll = y
	} else if y+l.RowH > *l.Scroll+l.Area.Dy() {
		*l.Scroll = y + l.RowH - l.Area.Dy()
	}
}

// Hit is the touch area of the list: the finger scrolls it, and a tap on a row calls tap with its
// number.
func (l List) Hit(tap func(i int)) Hit {
	return Hit{R: l.Area,
		Scroll: func(dy int) { *l.Scroll = max(0, min(*l.Scroll+dy, l.MaxScroll())) },
		TapAt: func(p image.Point) {
			if i := l.At(p); i >= 0 {
				tap(i)
			}
		}}
}
