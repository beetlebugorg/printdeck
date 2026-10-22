package lcd

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Status is the status screen of the deck: the heaters and the fan with their icons, and the
// progress of the print or the position. The icons move: heat waves while a heater has power, and
// the blades of the fan at its speed.
type Status struct {
	State    string  `json:"state"`              // for example "Ready" or "Printing"
	Clock    string  `json:"clock,omitempty"`    // for example "2:24PM" or "14:24" (see Clock)
	Message  string  `json:"message,omitempty"`  // the M117 message, or the next step
	Tool     float64 `json:"tool"`               // degrees
	ToolT    float64 `json:"tool_target"`        // degrees, 0 is off
	Bed      float64 `json:"bed"`                // degrees
	BedT     float64 `json:"bed_target"`         // degrees, 0 is off
	ToolPow  float64 `json:"tool_power"`         // 0 to 1, the power of the heater now
	BedPow   float64 `json:"bed_power"`          // 0 to 1
	Fan      float64 `json:"fan"`                // 0 to 1
	Progress float64 `json:"progress"`           // 0 to 1, with Printing
	Printing bool    `json:"printing,omitempty"` // a print runs: a progress bar
	Elapsed  string  `json:"elapsed,omitempty"`  // the time of the print, for example "0:42"
	Left     string  `json:"left,omitempty"`     // the time left of the print, for example "1:23"
	Done     string  `json:"done,omitempty"`     // the end of the print on the clock, for example "3:47PM"
	Position string  `json:"position,omitempty"` // for example "X110 Y110 Z5.00", when no print runs
	Hint     string  `json:"hint,omitempty"`     // for example "Click for menu", when no print runs
}

// Moves reports that the status has a part that moves, so that the module draws its frames.
func (s *Status) Moves() bool {
	return heating(s.Tool, s.ToolT, s.ToolPow) || heating(s.Bed, s.BedT, s.BedPow) || s.Fan > 0 || s.Hint != ""
}

// heating is true while the heater has power, or while it is more than 2 degrees under its target.
func heating(temp, target, power float64) bool { return power > 0 || target > 0 && temp < target-2 }

// The columns of the three gauges: their centers.
const (
	colTool = canvasW/2 - 42
	colBed  = canvasW / 2
	colFan  = canvasW/2 + 42
)

// The layout, from the top: the icons (y 0 to 15), the temperatures in the large font (16 to
// 31), the targets in the small font (32 to 39), a dotted line (42), and two lines of small text
// (45 and 55). It has no boxes: lit pixels are bright, so the screen lights only what it says.
func (s *Status) draw(c *canvas, frame int) {
	small := &font5x8
	centered := func(cx, y int, t string) {
		w := small.w * len(t)
		c.smallText(small, max(0, min(cx-w/2, canvasW-w)), y, t, false)
	}
	// With no target, the row of the targets is empty: the icons and the numbers move down to
	// keep the space even.
	top := 0
	if s.ToolT <= 0 && s.BedT <= 0 {
		top = 4
	}
	big := func(cx int, t string) { c.text(max(0, min(cx-4*len(t), canvasW-8*len(t))), top+16, t) }

	hotendIcon(c, colTool-8, top, s.ToolT > 0, heating(s.Tool, s.ToolT, s.ToolPow), frame)
	bedIcon(c, colBed-8, top, s.BedT > 0, heating(s.Bed, s.BedT, s.BedPow), frame)
	fanIcon(c, colFan-8, top, s.Fan, frame)
	big(colTool, fmt.Sprintf("%.0f\xf8", s.Tool))
	big(colBed, fmt.Sprintf("%.0f\xf8", s.Bed))
	big(colFan, fmt.Sprintf("%.0f%%", s.Fan*100))
	target := func(cx int, t float64) {
		if t <= 0 {
			return
		}
		v := fmt.Sprintf("%.0f\xf8", t)
		w := small.w*len(v) + 8
		x := max(0, cx-w/2)
		// A small arrow: the heater goes to this temperature.
		m := y32 + 4
		c.line(x, m, x+5, m)
		c.line(x+3, m-2, x+5, m)
		c.line(x+3, m+2, x+5, m)
		c.smallText(small, x+8, y32, v, false)
	}
	target(colTool, s.ToolT)
	target(colBed, s.BedT)
	for x := 0; x < canvasW; x += 2 {
		c.set(x, 42, true)
	}

	const line1, line2 = 44, 54
	if s.Printing {
		pct := fmt.Sprintf("%.0f%%", s.Progress*100)
		x1 := canvasW - small.w*4 - 3
		roundBar(c, 0, line1+1, x1, line1+7, s.Progress)
		c.smallText(small, canvasW-small.w*len(pct), line1, pct, false)
		// The end of the print on the clock at the left, when the time left is known; else the
		// time of the print.
		if s.Done != "" {
			c.smallText(small, 0, line2, "done "+s.Done, false)
		} else {
			c.smallText(small, 0, line2, s.Elapsed, false)
		}
		if s.Left != "" {
			l := s.Left + " left"
			c.smallText(small, canvasW-small.w*len(l), line2, l, false)
		}
		return
	}
	// The position, or the message when the position is not known. Then the state and the clock,
	// and every 3 seconds the message or the hint in place of the state.
	state := s.State
	if s.Position != "" {
		centered(canvasW/2, line1, fit(s.Position, 25))
		if alt := s.alternate(frame); alt != "" {
			state = alt
		}
	} else {
		centered(canvasW/2, line1, fit(s.Message, 25))
		if s.Hint != "" && frame/12%2 == 1 {
			state = s.Hint
		}
	}
	c.smallText(small, 0, line2, fit(strings.TrimSpace(state), 19), false)
	if s.Clock != "" {
		c.smallText(small, canvasW-small.w*len(s.Clock), line2, s.Clock, false)
	}
}

// y32 is the top of the targets.
const y32 = 32

// roundBar draws a progress bar with round ends from x0, y0 to x1, y1, filled to v (0 to 1).
func roundBar(c *canvas, x0, y0, x1, y1 int, v float64) {
	c.line(x0+2, y0, x1-3, y0)
	c.line(x0+2, y1-1, x1-3, y1-1)
	c.line(x0, y0+2, x0, y1-3)
	c.line(x1-1, y0+2, x1-1, y1-3)
	c.set(x0+1, y0+1, true)
	c.set(x1-2, y0+1, true)
	c.set(x0+1, y1-2, true)
	c.set(x1-2, y1-2, true)
	w := int(float64(x1-x0-4) * min(max(v, 0), 1))
	c.fill(x0+2, y0+2, x0+2+w, y1-2, true)
}

// alternate is the text that takes the place of the state on the bottom line for 3 of each 6
// seconds (at 4 frames a second), or "".
func (s *Status) alternate(frame int) string {
	if frame/12%2 == 0 {
		return ""
	}
	if s.Message != "" && frame/24%2 == 0 || s.Hint == "" {
		return s.Message
	}
	return s.Hint
}

// The icons are 16 x 16 pixels. '#' is a lit pixel.
var (
	hotendArt = []string{
		"......####......",
		"......####......",
		"..############..",
		"..#..........#..",
		"..############..",
		"..#..........#..",
		"..############..",
		"................",
		"...##########...",
		"...#........#...",
		"...#........#...",
		"...##########...",
		".....######.....",
		"......####......",
		".......##.......",
		"................",
	}
	bedArt = []string{
		"................",
		"................",
		"................",
		"................",
		"................",
		"................",
		"................",
		"................",
		"################",
		"#..............#",
		"#..............#",
		"################",
		".##..........##.",
		".##..........##.",
		".##..........##.",
		"................",
	}
)

func drawArt(c *canvas, x, y int, art []string) {
	for dy, row := range art {
		for dx, ch := range row {
			if ch == '#' {
				c.set(x+dx, y+dy, true)
			}
		}
	}
}

// hotendIcon draws the hotend. Its heater block is lit while the heater is on, and waves rise at
// its sides while it heats.
func hotendIcon(c *canvas, x, y int, on, heats bool, frame int) {
	drawArt(c, x, y, hotendArt)
	if on {
		c.fill(x+4, y+9, x+12, y+11, true) // the heater block is lit
		c.fill(x+6, y+12, x+10, y+13, true)
	}
	if heats {
		for _, wx := range []int{x, x + 15} {
			for dy := 0; dy < 8; dy++ {
				if (dy+frame)%4 != 3 {
					c.set(wx+wave(1, float64(dy+frame)*1.2), y+12-dy, true)
				}
			}
		}
	}
}

// bedIcon draws the bed. The plate is lit while the heater is on, and three waves rise from it
// while it heats.
func bedIcon(c *canvas, x, y int, on, heats bool, frame int) {
	drawArt(c, x, y, bedArt)
	if on {
		c.fill(x+1, y+9, x+15, y+11, true)
	}
	for i := 0; i < 3; i++ {
		wx := x + 3 + i*5
		if !heats {
			// Short marks: the heat lines at rest.
			c.line(wx, y+4, wx, y+6)
			continue
		}
		for dy := 0; dy < 7; dy++ {
			c.set(wx+wave(1, float64(dy+frame*2+i*2)/1.5), y+6-dy, true)
		}
	}
}

// fanIcon draws a fan in a frame with round corners, with three filled blades that turn at the
// speed of the fan.
func fanIcon(c *canvas, x, y int, speed float64, frame int) {
	// The frame.
	c.line(x+2, y, x+13, y)
	c.line(x+2, y+15, x+13, y+15)
	c.line(x, y+2, x, y+13)
	c.line(x+15, y+2, x+15, y+13)
	c.set(x+1, y+1, true)
	c.set(x+14, y+1, true)
	c.set(x+1, y+14, true)
	c.set(x+14, y+14, true)
	// The blades: the parts of the circle in three sectors that bend with the radius.
	// The blades turn against their curve, as the blades of a real fan.
	turn := -float64(frame) * speed * 0.7
	const cx, cy = 7.5, 7.5
	for py := 1; py < 15; py++ {
		for px := 1; px < 15; px++ {
			dx, dy := float64(px)-cx, float64(py)-cy
			r := math.Hypot(dx, dy)
			if r > 6.6 {
				continue
			}
			if r < 1.6 {
				c.set(x+px, y+py, true) // the hub
				continue
			}
			a := math.Atan2(dy, dx) - turn - r*0.28
			sector := math.Mod(a, 2*math.Pi/3)
			if sector < 0 {
				sector += 2 * math.Pi / 3
			}
			if sector < 0.95 {
				c.set(x+px, y+py, true)
			}
		}
	}
}

// Clock is the time for the display: "2:24PM" on a 12-hour clock, or "14:24" on a 24-hour clock.
func Clock(t time.Time, h24 bool) string {
	if h24 {
		return t.Format("15:04")
	}
	return t.Format("3:04PM")
}

// IconDots returns the dots of an icon of the status screen, 16 x 16 and the top row first:
// "hotend" or "bed". lit draws the heater on.
func IconDots(name string, lit bool) [16][16]bool {
	var c canvas
	switch name {
	case "hotend":
		hotendIcon(&c, 0, 0, lit, false, 0)
	case "bed":
		bedIcon(&c, 0, 0, lit, false, 0)
	}
	var out [16][16]bool
	for y := range 16 {
		for x := range 16 {
			out[y][x] = c.get(x, y)
		}
	}
	return out
}
