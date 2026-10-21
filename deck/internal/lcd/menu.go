package lcd

import "fmt"

// Menu is a menu of the deck: a carousel of large icons (the home menu), or a list of rows with
// icons, a frame around the selected row, and a scrollbar.
type Menu struct {
	Title    string     `json:"title"`
	Items    []MenuItem `json:"items"`
	Selected int        `json:"selected"`
	Carousel bool       `json:"carousel,omitempty"`
	// Notes are lines under the title, for example the step of a filament change.
	Notes []string `json:"notes,omitempty"`
}

// MenuItem is a key of a menu: its label and the name of its icon (icons.go).
type MenuItem struct {
	Label string `json:"label"`
	Icon  string `json:"icon,omitempty"`
}

func (m *Menu) draw(c *canvas, frame int) {
	if len(m.Items) == 0 {
		return
	}
	sel := max(0, min(m.Selected, len(m.Items)-1))
	f := &font5x8
	// The header: the title, the place in the menu, and a line.
	c.smallText(f, 0, 0, fit(m.Title, 19), false)
	pos := fmt.Sprintf("%d/%d", sel+1, len(m.Items))
	c.smallText(f, canvasW-f.w*len(pos), 0, pos, false)
	c.line(0, 9, canvasW-1, 9)
	if m.Carousel {
		m.drawCarousel(c, sel, frame)
		return
	}
	y := 11
	for _, n := range m.Notes {
		c.smallText(f, 0, y, fit(n, 25), false)
		y += 9
	}
	m.drawList(c, sel, y)
}

// drawCarousel draws the selected item as a large icon with its label, between the small icons
// of the items before and after it.
func (m *Menu) drawCarousel(c *canvas, sel, frame int) {
	it := m.Items[sel]
	// The large icon, 24 x 24, in a frame with round corners.
	const bx, by = canvasW/2 - 16, 12
	roundFrame(c, bx, by, bx+32, by+30)
	c.bigIcon(it.Icon, bx+4, by+3)
	// The neighbours have the same icons, with no frame.
	if sel > 0 {
		c.bigIcon(m.Items[sel-1].Icon, 12, by+3)
		arrow(c, 4, by+15, -1)
	}
	if sel < len(m.Items)-1 {
		c.bigIcon(m.Items[sel+1].Icon, canvasW-36, by+3)
		arrow(c, canvasW-5, by+15, 1)
	}
	// The label in the large font, centered under the icon.
	l := fit(it.Label, 16)
	c.text((canvasW-8*len(l))/2, 46, l)
	_ = frame
}

// drawList draws the items as rows from y down, with a frame around the selected row and a
// scrollbar when the list is longer than the screen.
func (m *Menu) drawList(c *canvas, sel, y int) {
	const rowH = 10
	f := &font5x8
	n := (canvasH - y) / rowH
	top := max(0, min(sel-n+2, len(m.Items)-n))
	if sel < top {
		top = sel
	}
	long := len(m.Items) > n
	right := canvasW
	if long {
		right = canvasW - 4
	}
	for i := top; i < min(top+n, len(m.Items)); i++ {
		ry := y + (i-top)*rowH
		it := m.Items[i]
		c.icon(it.Icon, 3, ry+1, 1)
		c.smallText(f, 14, ry+1, fit(it.Label, (right-16)/f.w), false)
		if i == sel {
			roundFrame(c, 0, ry, right, ry+rowH)
		}
	}
	if long {
		// The scrollbar: a dotted track and a solid thumb.
		h := canvasH - y
		for ty := y; ty < canvasH; ty += 2 {
			c.set(canvasW-2, ty, true)
		}
		th := max(4, h*n/len(m.Items))
		t0 := y + (h-th)*top/max(1, len(m.Items)-n)
		c.fill(canvasW-3, t0, canvasW, t0+th, true)
	}
}

// roundFrame draws the edge of a rectangle with round corners; x1 and y1 are not part of it.
func roundFrame(c *canvas, x0, y0, x1, y1 int) {
	c.line(x0+2, y0, x1-3, y0)
	c.line(x0+2, y1-1, x1-3, y1-1)
	c.line(x0, y0+2, x0, y1-3)
	c.line(x1-1, y0+2, x1-1, y1-3)
	c.set(x0+1, y0+1, true)
	c.set(x1-2, y0+1, true)
	c.set(x0+1, y1-2, true)
	c.set(x1-2, y1-2, true)
}

// arrow draws a small arrow head that points left (dir -1) or right (1), with its tip at x, y.
func arrow(c *canvas, x, y, dir int) {
	for i := 0; i < 4; i++ {
		c.line(x-dir*i, y-i, x-dir*i, y+i)
	}
}
