package printdeck_display

import "strings"

// Screen is what the deck shows on the display: up to 4 rows of text, or a title, an animation
// and a caption. The deck sends it with printdeck/screen.
type Screen struct {
	Rows []Row `json:"rows,omitempty"`
	// Anim is a full screen animation: "work" (an action), "heat", "in" (load filament), "out"
	// (unload filament), or "". With an animation, Title is the top row and Caption the bottom
	// row, and the rows are not used.
	Anim    string `json:"anim,omitempty"`
	Title   string `json:"title,omitempty"`
	Caption string `json:"caption,omitempty"`
}

// Row is a row of 16 characters: text at the left and at the right, a selection bar (Selected),
// or a progress bar (Bar from 0 to 1; a negative Bar is no bar).
type Row struct {
	Left     string  `json:"left,omitempty"`
	Right    string  `json:"right,omitempty"`
	Selected bool    `json:"selected,omitempty"`
	Bar      float64 `json:"bar,omitempty"`
	HasBar   bool    `json:"has_bar,omitempty"`
}

// cols is the number of characters in a row.
const cols = 16

// draw draws the screen at the time frame (4 frames a second).
func (s *Screen) draw(c *canvas, frame int) {
	if s.Anim != "" {
		s.drawAnim(c, frame)
		return
	}
	for i, r := range s.Rows {
		if i >= 4 {
			break
		}
		r.draw(c, i*16)
	}
}

func (r Row) draw(c *canvas, y int) {
	left, right := fit(r.Left, cols), fit(r.Right, cols)
	if len(left)+len(right) > cols {
		left = left[:max(0, cols-len(right)-1)]
	}
	if r.HasBar {
		x0 := 0
		if left != "" {
			x0 = (len(left) + 1) * 8
		}
		c.frame(x0, y+4, canvasW, y+12)
		w := int(float64(canvasW-x0-4) * min(max(r.Bar, 0), 1))
		c.fill(x0+2, y+6, x0+2+w, y+10, true)
		c.text(0, y, left)
		return
	}
	// The selection is a ">" and a thin frame: a lit pixel is bright on these displays, so the
	// screens light few of them.
	x := 0
	if r.Selected {
		c.text(0, y, ">")
		c.frame(0, y, canvasW, y+16)
		x = 8
		left = left[:min(len(left), cols-1-len(right))]
	}
	c.text(x, y, left)
	c.text(canvasW-8*len(right), y, right)
}

// fit makes text safe for the font: printable ASCII, and the degree sign of the font.
func fit(s string, n int) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '°':
			b.WriteByte(0xf8)
		case r >= 32 && r < 127:
			b.WriteRune(r)
		}
		if b.Len() >= n {
			break
		}
	}
	return b.String()
}

// drawAnim draws the title at the top, the caption at the bottom, and the animation between them,
// in the rows from y 16 to 48.
func (s *Screen) drawAnim(c *canvas, frame int) {
	t := fit(s.Title, cols)
	c.text((canvasW-8*len(t))/2, 0, t)
	c.line(0, 15, canvasW-1, 15)
	cap := fit(s.Caption, cols)
	c.text((canvasW-8*len(cap))/2, 48, cap)
	switch s.Anim {
	case "work":
		// A nozzle goes to and fro over the bed, and probes it now and then.
		const bedY = 44
		c.line(4, bedY, canvasW-5, bedY)
		span := canvasW - 24
		p := (frame * 4) % (2 * span)
		if p > span {
			p = 2*span - p
		}
		x := 12 + p
		down := frame%8 >= 6 // the probe goes down for 2 of 8 frames
		tip := 34
		if down {
			tip = bedY - 1
		}
		c.frame(x-6, 18, x+7, 24) // the heater block
		c.line(x-4, 24, x, 28)    // the nozzle
		c.line(x+4, 24, x, 28)
		c.line(x, 29, x, tip)
		if down {
			c.line(x-3, bedY-2, x+3, bedY-2)
		}
	case "heat":
		// Heat waves rise from the bed.
		c.line(8, 44, canvasW-9, 44)
		for i := 0; i < 6; i++ {
			x0 := 18 + i*19
			for y := 18; y < 42; y++ {
				c.set(x0+wave(3, float64(y+frame*3)/3.5), y, true)
			}
		}
	case "in", "out":
		// The filament moves into the extruder, or out of it.
		c.line(0, 30, canvasW-27, 30) // the filament
		c.line(0, 33, canvasW-27, 33)
		c.frame(canvasW-26, 20, canvasW-6, 44) // the extruder
		dir := 1
		if s.Anim == "out" {
			dir = -1
		}
		for k := 0; k < 4; k++ {
			x := ((frame*dir*6+k*26)%104 + 104) % 104
			for i := 0; i < 6; i++ {
				if dir > 0 {
					c.set(x+i, 26+i, true)
					c.set(x+i, 37-i, true)
				} else {
					c.set(x+5-i, 26+i, true)
					c.set(x+5-i, 37-i, true)
				}
			}
		}
	}
}
