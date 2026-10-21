// Package lcd draws the screens of the 128 x 64 display of a printer: rows of text, menus with
// icons, the status screen and animations. The module printdeck_display of the Klipper host shows
// them on the display, and the deck shows the status screen on the touch screen too.
//
//	Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package lcd

import (
	"math"
	"strings"
)

// Screen is what the deck shows on the display: up to 4 rows of text, or a title, an animation
// and a caption. The deck sends it with printdeck/screen.
type Screen struct {
	Rows []Row `json:"rows,omitempty"`
	// Anim is a full screen animation: "work" (an action), "heat", "in" (load filament), "out"
	// (unload filament), or "". With an animation, Title is the top row and Caption the bottom
	// row, and the rows are not used.
	// Anim is an action: "probe", "pid", "flash", "in", "out", or "work" for any other action.
	// Title is its name, Caption the time at the top right, Footer the line at the bottom, and
	// Progress the part that is done (for "flash").
	Anim     string  `json:"anim,omitempty"`
	Title    string  `json:"title,omitempty"`
	Caption  string  `json:"caption,omitempty"`
	Footer   string  `json:"footer,omitempty"`
	Progress float64 `json:"progress,omitempty"`
	// Font is the font of the rows: "5x8" (25 characters, 8 rows), "6x10" (21 characters, 6
	// rows), or "" for the 8 x 16 font of Klipper (16 characters, 4 rows).
	Font string `json:"font,omitempty"`
	// Status is the status screen. With a status, the rows are not used.
	Status *Status `json:"status,omitempty"`
	// Menu is a menu with icons. With a menu, the rows are not used.
	Menu *Menu `json:"menu,omitempty"`
}

// Moves reports that the screen has frames: an animation, or a status with a part that moves.
func (s *Screen) Moves() bool { return s.Anim != "" || s.Status != nil && s.Status.Moves() }

// Row is a row of text: text at the left and at the right, or a progress bar (Bar from 0 to 1,
// with HasBar). An Item is a key of a menu: it has room for the cursor, which shows at the
// Selected item. A Bold row is a heading.
type Row struct {
	Left     string  `json:"left,omitempty"`
	Right    string  `json:"right,omitempty"`
	Item     bool    `json:"item,omitempty"`
	Selected bool    `json:"selected,omitempty"`
	Bold     bool    `json:"bold,omitempty"`
	Bar      float64 `json:"bar,omitempty"`
	HasBar   bool    `json:"has_bar,omitempty"`
}

// cols is the number of characters in a row.
const cols = 16

// Bits returns a frame of the screen (4 frames a second): 64 rows of 16 bytes, the left pixel in
// the top bit, as the graphics of an ST7920.
func (s *Screen) Bits(frame int) [64][16]byte {
	var c canvas
	s.draw(&c, frame)
	return c
}

// draw draws the screen at the time frame (4 frames a second).
func (s *Screen) draw(c *canvas, frame int) {
	if s.Status != nil {
		s.Status.draw(c, frame)
		return
	}
	if s.Menu != nil {
		s.Menu.draw(c, frame)
		return
	}
	if s.Anim != "" {
		s.drawAnim(c, frame)
		return
	}
	if f := fonts[s.Font]; f != nil {
		for i, r := range s.Rows {
			if (i+1)*f.h > canvasH {
				break
			}
			r.drawSmall(c, f, i*f.h)
		}
		return
	}
	for i, r := range s.Rows {
		if i >= 4 {
			break
		}
		r.draw(c, i*16)
	}
}

// fonts are the small fonts of the rows.
var fonts = map[string]*bitmapFont{"5x8": &font5x8, "6x10": &font6x10}

// drawSmall draws a row in a small font, with its top at y.
func (r Row) drawSmall(c *canvas, f *bitmapFont, y int) {
	n := canvasW / f.w
	x := 0
	if r.Item {
		if r.Selected {
			c.smallText(f, 0, y, ">", true)
		}
		x, n = f.w+2, n-1
	}
	left, right := fit(r.Left, n), fit(r.Right, n)
	if len(left)+len(right) > n {
		left = left[:max(0, n-len(right)-1)]
	}
	if r.HasBar {
		x0 := x
		if left != "" {
			x0 = x + (len(left)+1)*f.w
		}
		c.frame(x0, y+1, canvasW, y+f.h-1)
		w := int(float64(canvasW-x0-4) * min(max(r.Bar, 0), 1))
		c.fill(x0+2, y+3, x0+2+w, y+f.h-3, true)
		c.smallText(f, x, y, left, r.Bold)
		return
	}
	c.smallText(f, x, y, left, r.Bold)
	c.smallText(f, canvasW-f.w*len(right), y, right, r.Bold)
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

// drawAnim draws an action: a header with the title and the time (Caption), a scene in the middle
// (y 11 to 52), and a line at the bottom (Footer).
func (s *Screen) drawAnim(c *canvas, frame int) {
	f := &font5x8
	c.smallText(f, 0, 0, fit(s.Title, 25-len(s.Caption)-1), false)
	c.smallText(f, canvasW-f.w*len(s.Caption), 0, s.Caption, false)
	c.line(0, 9, canvasW-1, 9)
	foot := fit(s.Footer, 25)
	c.smallText(f, (canvasW-f.w*len(foot))/2, 56, foot, false)
	switch s.Anim {
	case "probe":
		animProbe(c, frame)
	case "pid":
		animPID(c, frame)
	case "flash":
		animFlash(c, frame, s.Progress)
	case "in", "out":
		animFilament(c, frame, s.Anim == "in")
	default:
		animGears(c, frame)
	}
}

// animProbe: the hotend goes to three points of the bed, and goes down to each. A ring shows at
// the touch, and the points that are done are filled.
func animProbe(c *canvas, frame int) {
	const bedY = 47
	points := []int{26, 64, 102}
	c.line(10, bedY, canvasW-11, bedY)
	c.line(10, bedY+1, canvasW-11, bedY+1)
	// 12 frames for each point: 4 to move, 3 down, 2 touch, 3 up.
	step := frame % (12 * len(points))
	pi, ph := step/12, step%12
	from := points[(pi+len(points)-1)%len(points)]
	x := points[pi]
	y := 14
	switch {
	case ph < 4:
		x = from + (points[pi]-from)*(ph+1)/4
	case ph < 7:
		y = 14 + (bedY-31)*(ph-3)/3
	case ph < 9:
		y = bedY - 31 + 2
	default:
		y = bedY - 31 - (bedY-31-14)*(ph-8)/3
	}
	for i, px := range points {
		if i < pi || i == pi && ph >= 7 {
			c.fill(px-2, bedY-4, px+3, bedY-1, true)
		} else {
			c.frame(px-2, bedY-4, px+3, bedY-1)
		}
	}
	drawArt(c, x-8, y, hotendArt)
	if ph == 7 || ph == 8 {
		r := 4 + (ph-7)*3
		for a := 0; a < 16; a++ {
			ang := float64(a) * math.Pi / 8
			c.set(x+int(math.Round(float64(r)*1.6*math.Cos(ang))), bedY-2+int(math.Round(float64(r)*0.5*math.Sin(ang))), true)
		}
	}
}

// animPID: the temperature swings around the target (a dashed line) and scrolls to the left, as
// in a PID tune. The swing gets smaller.
func animPID(c *canvas, frame int) {
	const mid = 31
	for x := 0; x < canvasW; x += 4 {
		c.set(x, mid, true)
		c.set(x+1, mid, true)
	}
	prev := -1
	for x := 0; x < canvasW; x++ {
		t := float64(x+frame*3) / 14
		amp := 6 + 10*math.Exp(-float64((x+frame*3)%180)/90)
		y := mid - int(math.Round(amp*math.Sin(t)))
		if prev >= 0 {
			c.line(x-1, prev, x, y)
		}
		prev = y
	}
	// The head of the curve.
	c.fill(canvasW-3, prev-1, canvasW, prev+2, true)
}

// animFlash: bits flow into a chip, and a bar shows the progress (0 to 1).
func animFlash(c *canvas, frame int, progress float64) {
	const cx, cy = 88, 26
	c.frame(cx-12, cy-11, cx+13, cy+12)
	c.frame(cx-8, cy-7, cx+9, cy+8)
	for i := 0; i < 5; i++ {
		y := cy - 8 + i*4
		c.line(cx-15, y, cx-13, y)
		c.line(cx+13, y, cx+15, y)
	}
	for i := 0; i < 3; i++ {
		y := cy - 6 + i*6
		for k := 0; k < 4; k++ {
			x := 8 + ((frame*4 + k*16 + i*7) % 60)
			if (k+i+frame/2)%2 == 0 {
				c.fill(x, y, x+3, y+3, true)
			} else {
				c.frame(x, y, x+3, y+3)
			}
		}
	}
	w := int(float64(canvasW-24) * min(max(progress, 0), 1))
	c.frame(10, 43, canvasW-10, 50)
	c.fill(12, 45, 12+w, 48, true)
}

// animFilament: the filament goes down into the extruder (in) or up out of it (out), between two
// gears that turn.
func animFilament(c *canvas, frame int, in bool) {
	dir := 1
	if !in {
		dir = -1
	}
	gear(c, 52, 30, 9, float64(frame*dir)*0.35, 8)
	gear(c, 76, 30, 9, -float64(frame*dir)*0.35, 8)
	c.line(62, 11, 62, 52)
	c.line(66, 11, 66, 52)
	for k := 0; k < 5; k++ {
		y := 11 + ((frame*dir*3+k*9)%45+45)%45
		c.line(63, y, 65, y)
	}
	// An arrow at the side, in the direction of the filament, that moves with it.
	ax, ay := 100, 22+((frame*dir*2)%6+6)%6
	c.line(ax, ay, ax, ay+10)
	c.line(ax+1, ay, ax+1, ay+10)
	for i := 0; i < 5; i++ {
		if in {
			c.line(ax-4+i, ay+6+i, ax+5-i, ay+6+i)
		} else {
			c.line(ax-4+i, ay+4-i, ax+5-i, ay+4-i)
		}
	}
}

// animGears: two gears that turn together.
func animGears(c *canvas, frame int) {
	a := float64(frame) * 0.3
	gear(c, 50, 31, 13, a, 10)
	gear(c, 80, 26, 9, -a*13/9+0.3, 7)
}

// gear draws a gear with its center at cx, cy, its radius r, turned by angle a, with n teeth.
func gear(c *canvas, cx, cy, r int, a float64, n int) {
	px, py := -1, -1
	for i := 0; i <= 360; i++ {
		t := float64(i) * math.Pi / 180
		rr := float64(r) - 2
		if math.Mod((t-a)*float64(n)/(2*math.Pi)+100, 1) < 0.5 {
			rr = float64(r)
		}
		x, y := cx+int(math.Round(rr*math.Cos(t))), cy+int(math.Round(rr*math.Sin(t)))
		if px >= 0 {
			c.line(px, py, x, y)
		}
		px, py = x, y
	}
	for i := 0; i < 360; i += 15 { // the hub
		t := float64(i) * math.Pi / 180
		c.set(cx+int(math.Round(2.5*math.Cos(t))), cy+int(math.Round(2.5*math.Sin(t))), true)
	}
}
