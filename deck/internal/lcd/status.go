package lcd

import (
	"fmt"
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
	// Running is a print between its start and its end macro: the layers print. The screen then
	// shows the progress and the time left in the large font. Paused is a paused print.
	Running bool   `json:"running,omitempty"`
	Paused  bool   `json:"paused,omitempty"`
	Elapsed string `json:"elapsed,omitempty"` // the time of the print, for example "0:42"
	Left    string `json:"left,omitempty"`    // the time left of the print, for example "1:23"
	Done    string `json:"done,omitempty"`    // the end of the print on the clock, for example "3:47PM"
	// Cooled is the time on the clock when the bed after the print is cool enough to remove the
	// part. The line of the end shows it in place of Done.
	Cooled string `json:"cooled,omitempty"`
	// Cooling is the end of a print that waits for the bed to cool: the bar is the cooling
	// (Progress), to CoolTo degrees, and Left is the time that it needs, for example "4 min".
	Cooling bool    `json:"cooling,omitempty"`
	CoolTo  float64 `json:"cool_to,omitempty"`
	// The layer of a print and the number of layers ("17/120"), the time to the next layer
	// ("1:32"), the height of the nozzle ("3.20"), and the name of the print.
	Layer    string `json:"layer,omitempty"`
	Next     string `json:"next,omitempty"`
	Z        string `json:"z,omitempty"`
	File     string `json:"file,omitempty"`
	Position string `json:"position,omitempty"` // for example "X110 Y110 Z5.00", when no print runs
	Hint     string `json:"hint,omitempty"`     // for example "Click for menu", when no print runs
}

// Moves reports that the status has a part that moves, so that the module draws its frames.
func (s *Status) Moves() bool {
	return heating(s.Tool, s.ToolT, s.ToolPow) || heating(s.Bed, s.BedT, s.BedPow) || s.Fan > 0
}

// heating is true while the heater has power, or while it is more than 2 degrees under its target.
func heating(temp, target, power float64) bool { return power > 0 || target > 0 && temp < target-2 }

// The columns of the three gauges: their centers.
const (
	colTool = canvasW/2 - 42
	colBed  = canvasW / 2
	colFan  = canvasW/2 + 42
)

// The layout, from the top: the temperatures and the fan in the large font (y 0 to 15), a small
// icon of each with the target in the small font (16 to 23), a dotted line (25), and four lines
// of small text (27, 36, 45 and 54). It has no boxes: lit pixels are bright, so the screen lights
// only what it says.
func (s *Status) draw(c *canvas, frame int) {
	small := &font5x8
	left := func(y int, t string) { c.smallText(small, 0, y, t, false) }
	// item draws an icon and its text at x on the line y, and returns the x after it. The width
	// of an item is iconW plus the text.
	item := func(x, y int, icon []string, t string) int {
		drawArt(c, x, y, icon)
		return c.smallText(small, x+iconW, y, t, false)
	}
	itemW := func(t string) int { return iconW + small.w*len(t) }
	// items draws l at the left and r at the right of the line y, each with its icon. A missing
	// text has no item. The left text loses its end to fit.
	items := func(y int, li []string, l string, ri []string, r string) {
		x1 := canvasW
		if r != "" {
			x1 -= itemW(r)
			item(x1, y, ri, r)
			x1 -= small.w
		}
		if l != "" {
			n := max(0, (x1-iconW)/small.w)
			item(0, y, li, fit(strings.TrimSpace(l), n))
		}
	}
	const line1, line2, line3, line4 = 27, 36, 45, 54
	if s.Running {
		// The progress and the time left in the large font, and the bar under them.
		c.text(0, 0, fmt.Sprintf("%.0f%%", s.Progress*100))
		if t := s.Left; t != "" {
			x := canvasW - 8*len(t)
			drawArt(c, x-iconW, 5, iconLeft)
			c.text(x, 0, t)
		}
		roundBar(c, 0, 18, canvasW, 25, s.Progress)
		// The end, or the end of the cooling of the bed after the print, and the time of the
		// print.
		switch {
		case s.Cooled != "":
			items(line1, iconCooled, s.Cooled, iconElapsed, s.Elapsed)
		case s.Done != "":
			items(line1, iconDone, s.Done, iconElapsed, s.Elapsed)
		default:
			items(line1, nil, "", iconElapsed, s.Elapsed)
		}
		// The layer and the time to the next layer, and the height.
		if s.Layer != "" {
			x := item(0, line2, iconLayers, s.Layer) + small.w
			if s.Next != "" {
				item(x, line2, iconNext, s.Next)
			}
		}
		items(line2, nil, "", iconZ, s.Z)
		// The heaters and the fan: steady during a print, so in the small font.
		temp := func(t, target float64) string {
			if target > 0 && (t < target-5 || t > target+5) {
				return fmt.Sprintf("%.0f>%.0f", t, target) // away from the target
			}
			return fmt.Sprintf("%.0f\xf8", t)
		}
		tool, bed := temp(s.Tool, s.ToolT), temp(s.Bed, s.BedT)
		// A heater that holds its temperature has power: the icon blinks only under the target.
		smallHotend(c, 0, line3, s.ToolT > 0, heating(s.Tool, s.ToolT, 0), frame)
		x := c.smallText(small, iconW, line3, tool, false) + small.w
		smallBed(c, x, line3, s.BedT > 0, heating(s.Bed, s.BedT, 0), frame)
		x = c.smallText(small, x+iconW, line3, bed, false) + small.w
		smallFan(c, x, line3, s.Fan, frame)
		c.smallText(small, x+iconW, line3, fmt.Sprintf("%.0f%%", s.Fan*100), false)
		// The name of the print, or Paused, and the clock.
		name := s.File
		if s.Paused {
			name = "Paused"
		}
		items(line4, iconFile, name, iconClock, s.Clock)
		return
	}

	big := func(cx int, t string) { c.text(max(0, min(cx-4*len(t), canvasW-8*len(t))), 0, t) }
	big(colTool, fmt.Sprintf("%.0f\xf8", s.Tool))
	big(colBed, fmt.Sprintf("%.0f\xf8", s.Bed))
	big(colFan, fmt.Sprintf("%.0f%%", s.Fan*100))
	// The small icon and the target under each number.
	under := func(cx int, icon func(x int), t string) {
		w := 7
		if t != "" {
			w += 2 + small.w*len(t)
		}
		x := max(0, min(cx-w/2, canvasW-w))
		icon(x)
		c.smallText(small, x+9, 16, t, false)
	}
	target := func(t float64) string {
		if t <= 0 {
			return "off"
		}
		return fmt.Sprintf("%.0f\xf8", t)
	}
	under(colTool, func(x int) { smallHotend(c, x, 17, s.ToolT > 0, heating(s.Tool, s.ToolT, s.ToolPow), frame) }, target(s.ToolT))
	under(colBed, func(x int) { smallBed(c, x, 17, s.BedT > 0, heating(s.Bed, s.BedT, s.BedPow), frame) }, target(s.BedT))
	under(colFan, func(x int) { smallFan(c, x, 17, s.Fan, frame) }, "")
	for x := 0; x < canvasW; x += 2 {
		c.set(x, 25, true)
	}

	bar := func(right string) {
		x1 := canvasW - small.w*4 - 3
		roundBar(c, 0, line1+1, x1, line1+7, s.Progress)
		c.smallText(small, canvasW-small.w*len(right), line1, right, false)
	}
	if s.Printing && s.Cooling {
		bar(fmt.Sprintf("%.0f\xf8", s.CoolTo))
		items(line2, nil, "", iconLeft, s.Left)
		left(line2, "cooling bed")
		items(line4, iconFile, s.File, iconClock, s.Clock)
		return
	}
	if s.Printing {
		bar(fmt.Sprintf("%.0f%%", s.Progress*100))
		// The end of the print on the clock at the left, when the time left is known (the bed
		// cools enough to remove the part at Cooled), else the time of the print; and the time
		// left at the right.
		switch {
		case s.Cooled != "":
			items(line2, iconCooled, s.Cooled, iconLeft, s.Left)
		case s.Done != "":
			items(line2, iconDone, s.Done, iconLeft, s.Left)
		default:
			items(line2, iconElapsed, s.Elapsed, iconLeft, s.Left)
		}
		// The layer and the time to the next layer, or the time of the print; the height.
		x := 0
		if s.Layer != "" {
			x = item(0, line3, iconLayers, s.Layer) + small.w
			if s.Next != "" {
				item(x, line3, iconNext, s.Next)
			}
		} else if s.Done != "" && s.Elapsed != "" {
			item(0, line3, iconElapsed, s.Elapsed)
		}
		items(line3, nil, "", iconZ, s.Z)
		items(line4, iconFile, s.File, iconClock, s.Clock)
		return
	}
	// The position, the message, the hint, and the state with the clock.
	if s.Position != "" {
		c.centered(small, line1, fit(s.Position, lineChars))
		c.centered(small, line2, fit(s.Message, lineChars))
	} else {
		c.centered(small, line1, fit(s.Message, lineChars))
	}
	c.centered(small, line3, fit(s.Hint, lineChars))
	items(line4, nil, "", iconClock, s.Clock)
	n := lineChars
	if s.Clock != "" {
		n = (canvasW - itemW(s.Clock) - small.w) / small.w
	}
	left(line4, fit(strings.TrimSpace(s.State), n))
}

// iconW is the width of a small icon of a line of text with the gap after it.
const iconW = 9

// The small icons of the lines of text, 7 x 7 pixels: the end of the print (a flag), the time
// left (an hourglass), the time of the print (a stopwatch), the end of the cooling (a
// thermometer), the layer (a stack), the next layer (an arrow up), the height (an arrow up and
// down), the name of the print (a page), and the clock.
var (
	iconDone = []string{
		"#.#.#..",
		"##.#.#.",
		"#.#.#..",
		"##.#.#.",
		"#......",
		"#......",
		"#......",
	}
	iconLeft = []string{
		"#######",
		".#...#.",
		"..#.#..",
		"...#...",
		"..#.#..",
		".#.#.#.",
		"#######",
	}
	iconElapsed = []string{
		"..###..",
		"...#...",
		".#####.",
		"#..#..#",
		"#..##.#",
		"#.....#",
		".#####.",
	}
	iconCooled = []string{
		"..#....",
		".#.#...",
		".#.#.##",
		".#.#...",
		".###.##",
		"#####..",
		".###...",
	}
	iconLayers = []string{
		".......",
		"#######",
		".......",
		"#######",
		".......",
		"#######",
		".......",
	}
	iconNext = []string{
		"...#...",
		"..###..",
		".#.#.#.",
		"...#...",
		"...#...",
		".......",
		"#######",
	}
	iconZ = []string{
		"...#...",
		"..###..",
		".#.#.#.",
		"...#...",
		".#.#.#.",
		"..###..",
		"...#...",
	}
	iconFile = []string{
		"####...",
		"#..##..",
		"#...##.",
		"#....#.",
		"#....#.",
		"#....#.",
		"######.",
	}
	iconClock = []string{
		"..###..",
		".#.#.#.",
		"#..#..#",
		"#..##.#",
		"#.....#",
		".#...#.",
		"..###..",
	}
)

// lineChars is the number of characters of the small font in a line.
const lineChars = canvasW / 5

// centered draws t in the font f in the middle of the line y.
func (c *canvas) centered(f *bitmapFont, y int, t string) {
	w := f.w * len(t)
	c.smallText(f, max(0, (canvasW-w)/2), y, t, false)
}

// The small icons of the status screen are 7 x 7 pixels. smallHotend draws the hotend: its
// block is lit while the heater is on, and it blinks while it heats.
func smallHotend(c *canvas, x, y int, on, heats bool, frame int) {
	if heats && frame%4 == 3 {
		return
	}
	drawArt(c, x, y, smallHotendArt)
	if on {
		c.fill(x+2, y+3, x+5, y+4, true)
	}
}

// smallBed draws the bed: its plate is lit while the heater is on, and it blinks while it heats.
func smallBed(c *canvas, x, y int, on, heats bool, frame int) {
	if heats && frame%4 == 3 {
		return
	}
	drawArt(c, x, y, smallBedArt)
	if on {
		c.fill(x+1, y+4, x+6, y+5, true)
	}
}

// smallFan draws the fan. Its blades turn while it runs.
func smallFan(c *canvas, x, y int, speed float64, frame int) {
	art := smallFanArt[0]
	if speed > 0 && frame%2 == 1 {
		art = smallFanArt[1]
	}
	drawArt(c, x, y, art)
}

var (
	smallHotendArt = []string{
		".#####.",
		".#...#.",
		".#####.",
		"#.....#",
		"#######",
		"..###..",
		"...#...",
	}
	smallBedArt = []string{
		".#.#.#.",
		"#.#.#..",
		".......",
		"#######",
		"#.....#",
		"#######",
		".#...#.",
	}
	// The fan: a frame with round corners and four straight blades, as + and as x.
	smallFanArt = [2][]string{{
		".#####.",
		"#..#..#",
		"#..#..#",
		"#######",
		"#..#..#",
		"#..#..#",
		".#####.",
	}, {
		".#####.",
		"##...##",
		"#.#.#.#",
		"#..#..#",
		"#.#.#.#",
		"##...##",
		".#####.",
	}}
)

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
