package kit

import "image"

// A Screen is the size of the frame that the program draws.
type Screen struct {
	W, H int
}

// Wide reports that the screen is wider than high.
func (s Screen) Wide() bool { return s.W > s.H }

// Bounds is the whole screen.
func (s Screen) Bounds() Rect { return image.Rect(0, 0, s.W, s.H) }

// Split divides the screen into two panes: the side pane and the main pane. On a tall screen the
// side pane is the top, side pixels high. On a wide screen it is the left, side pixels wide. The
// main pane is the rest.
func (s Screen) Split(side int) (sidePane, mainPane Rect) {
	if s.Wide() {
		c := Cols(s.Bounds(), 0, Px(side), Fr(1))
		return c[0], c[1]
	}
	r := Rows(s.Bounds(), 0, Px(side), Fr(1))
	return r[0], r[1]
}
