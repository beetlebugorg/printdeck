package ui

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"
	"sync"
	"time"

	"printdeck/internal/gfx"
	lcdscreen "printdeck/internal/lcd"
)

// The start animation, on a screen that is all one blue LCD:
//
//  1. A small printer slides in, in the shape of a LulzBot TAZ Workhorse: the electronics tower
//     with its display and knob at the left, the extrusion at the right, the top and the base
//     bars, the X rods with the toolhead carriage, and the bed.
//  2. The bed heats. The toolhead goes to work: it prints PRINTDECK, small, one row of dots at a
//     time from the bottom up, as a printer prints its layers. Each dot lands hot and cools to the
//     colour of the filament. The filament runs from above the screen into the toolhead, in one
//     arc that swings as the toolhead moves, and gets shorter as the toolhead goes up.
//  3. The toolhead parks and stops. The bed moves forward to present the part: it comes toward the
//     viewer and grows to the dots of the display, and the printer fades back.
//  4. The toolhead comes back alone. It hops to a wipe pad in the corner, wipes, spits out a blob
//     of ooze, hops over, prints the version under the name, and hops away.
//
// A tall screen has the word on two lines, PRINT over DECK, for larger dots.

const (
	introBlank   = 400 * time.Millisecond // the empty display
	introSlide   = 700 * time.Millisecond // the printer slides in
	introHeat    = 900 * time.Millisecond // the bed heats
	introDrop    = 400 * time.Millisecond // the toolhead goes to the first dot
	introRow     = 240 * time.Millisecond // one row of dots
	introCool    = 400 * time.Millisecond // a dot cools to the colour of the filament
	introPark    = 500 * time.Millisecond // the toolhead goes to its park place
	introPresent = 900 * time.Millisecond // the bed comes forward
	introEnter   = 500 * time.Millisecond // the toolhead hops in to the wipe pad
	introWipe    = 800 * time.Millisecond // it wipes its nozzle
	introOoze    = 700 * time.Millisecond // it spits out a blob, which drops
	introGoto    = 400 * time.Millisecond // it hops to the version
	introVerRow  = 110 * time.Millisecond // one row of the dots of the version
	introExit    = 500 * time.Millisecond // it hops away
	introFont    = 8                      // the rows of the 5 x 8 font
	introGap     = 2                      // the empty rows between two lines
	introBed     = 6                      // the rows of the bed: the plate and its legs
)

// IntroLength is the time of the whole animation, on the screen with the most rows (the tall
// one), with a version of up to 8 characters.
var IntroLength = introLayout(image.Rect(0, 0, BaseW, BaseH)).times("v0.00.00").end

// The colours: the display of the day palette, the filament (PLA orange) and its hot colour, the
// heat of the bed, the white of the printer, its dim frame, and the green of the LulzBot parts.
var (
	introBG    = rgb(0x1f4bd1)
	introCell  = rgb(0x2a58e0)
	introInk   = rgb(0xf2f6ff)
	introPLA   = rgb(0xff7a1a)
	introHot   = rgb(0xffe08a)
	introHeat2 = rgb(0xff5a3c)
	introFrame = rgb(0x8fa8ef)
	introGreen = rgb(0xc4e538)
)

// introGeom is the place of the word on a screen: its lines, the size of its dots, the top left
// dot, and the size in dots. The pitch can have a fraction, while the bed comes forward.
type introGeom struct {
	lines      []string
	cols, rows int
	pitch      float64
	x0, y0     float64
	fade       float64 // the printer fades back: 0 is all there, 1 is gone
}

// introLayout is the place of the word at the dots of the display, at the end of the animation.
func introLayout(b image.Rectangle) introGeom {
	lines := []string{"PRINTDECK"}
	if b.Dy() > b.Dx() {
		lines = []string{"PRINT", "DECK"}
	}
	cols := 0 // the 5 x 8 font, with one empty column between the letters
	for _, l := range lines {
		cols = max(cols, len(l)*6-1)
	}
	rows := len(lines)*introFont + (len(lines)-1)*introGap
	pitch := max(4, min(14, (b.Dx()-48)/cols))
	pitch -= pitch % 2 // the print is half as large, on the same grid
	// The word and the bed under it, in the middle of the screen.
	return introGeom{lines: lines, cols: cols, rows: rows, pitch: float64(pitch),
		x0: float64(b.Min.X + (b.Dx()-cols*pitch)/2), y0: float64(b.Min.Y + (b.Dy()-(rows+introBed)*pitch)/2)}
}

// scaled is the geometry with dots of the size pitch, around the same middle.
func (g introGeom) scaled(pitch float64) introGeom {
	cx := g.x0 + float64(g.cols)*g.pitch/2
	cy := g.y0 + float64(g.rows+introBed)*g.pitch/2
	g.x0, g.y0 = cx-float64(g.cols)*pitch/2, cy-float64(g.rows+introBed)*pitch/2
	g.pitch = pitch
	return g
}

// between is the geometry between g (f 0) and h (f 1).
func (g introGeom) between(h introGeom, f float64) introGeom {
	l := func(a, b float64) float64 { return a + (b-a)*f }
	g.pitch, g.x0, g.y0 = l(g.pitch, h.pitch), l(g.x0, h.x0), l(g.y0, h.y0)
	return g
}

// introTimes are the starts of the parts of the animation.
type introTimes struct {
	slide, heat, drop, print, park, present, enter, wipe, ooze, goTo, version, exit, end time.Duration
}

func (g introGeom) times(version string) introTimes {
	var t introTimes
	t.slide = introBlank
	t.heat = t.slide + introSlide
	t.drop = t.heat + introHeat
	t.print = t.drop + introDrop
	t.park = t.print + time.Duration(g.rows)*introRow
	t.present = t.park + introPark
	t.enter = t.present + introPresent
	t.wipe = t.enter + introEnter
	t.ooze = t.wipe + introWipe
	t.goTo = t.ooze + introOoze
	t.version = t.goTo + introGoto
	t.exit = t.version
	if version != "" {
		t.exit += introFont * introVerRow
	}
	t.end = t.exit + introExit
	return t
}

// cell is the rectangle of the dot at column x and row y of the word.
func (g introGeom) cell(x, y int) image.Rectangle {
	px, py := g.x0+float64(x)*g.pitch, g.y0+float64(y)*g.pitch
	gap := max(1, math.Round(g.pitch/7))
	return image.Rect(int(math.Round(px+gap/2)), int(math.Round(py+gap/2)),
		int(math.Round(px+g.pitch-gap/2)), int(math.Round(py+g.pitch-gap/2)))
}

// lit reports the dots of the word. Each line is in the middle, on the grid of the dots.
func (g introGeom) lit(x, y int) bool {
	line, row := y/(introFont+introGap), y%(introFont+introGap)
	if row >= introFont || line >= len(g.lines) {
		return false
	}
	return glyphDot(g.lines[line], x-(g.cols-(len(g.lines[line])*6-1))/2, row)
}

// glyphDot reports the dot at column x and row y of a text in the 5 x 8 font.
func glyphDot(s string, x, y int) bool {
	ch, col := x/6, x%6
	if x < 0 || col == 5 || ch >= len(s) {
		return false
	}
	return lcdscreen.Glyph5x8(s[ch])[y]&(0x80>>col) != 0
}

// printedAt is the time when the toolhead puts down the dot at column x and row y, from the
// start of the print. The bottom row comes first, and the toolhead goes back and forth.
func printedAt(cols, rows, x, y int, row time.Duration) time.Duration {
	layer := rows - 1 - y
	f := float64(x) / float64(max(1, cols-1))
	if layer%2 == 1 {
		f = 1 - f
	}
	return time.Duration(layer)*row + time.Duration(f*float64(row))
}

// printingAt is the place of the dot that the toolhead prints at the time t from the start of a
// print of cols x rows dots: the column (with a fraction) and the row.
func printingAt(cols, rows int, t, row time.Duration) (x float64, y int) {
	layer := min(rows-1, int(t/row))
	f := min(1, float64(t-time.Duration(layer)*row)/float64(row))
	if layer%2 == 1 {
		f = 1 - f
	}
	return f * float64(cols-1), rows - 1 - layer
}

// The empty display of a screen size, drawn once.
var introGrid struct {
	sync.Mutex
	size image.Point
	img  *image.RGBA
}

func introBackground(b image.Rectangle, g introGeom) *image.RGBA {
	introGrid.Lock()
	defer introGrid.Unlock()
	if introGrid.img != nil && introGrid.size == b.Size() {
		return introGrid.img
	}
	p := int(g.pitch)
	c := gfx.New(b.Dx(), b.Dy())
	c.Fill(c.Bounds(), introBG)
	// The dots of the whole screen, on the grid of the word at its end.
	ox, oy := (int(g.x0)-b.Min.X)%p, (int(g.y0)-b.Min.Y)%p
	for y := oy - p; y < b.Dy(); y += p {
		for x := ox - p; x < b.Dx(); x += p {
			c.Fill(image.Rect(x+1, y+1, x+p-1, y+p-1), introCell)
		}
	}
	introGrid.size, introGrid.img = b.Size(), c.RGBA
	return c.RGBA
}

// introScene is the state of the animation at one time.
type introScene struct {
	b              image.Rectangle
	big, small     introGeom // the word at its end, and the word on the bed of the printer
	pl             whPlace   // the printer
	dx             float64   // the slide of the printer
	tm             introTimes
	version        string
	tipX, tipY     float64 // the tip of the nozzle
	dot            float64 // the size of the small dots of the toolhead
	shown, hot     bool
	onGantry       bool    // the toolhead is on the printer; else it is alone (the hiccup)
	wipeShown      float64 // the wipe pad: 0 gone, 1 there
	oozeX, oozeY   float64 // the blob of ooze
	oozeR          float64 // its size in small dots; 0 is no blob
	verX, verY     float64 // the top left dot of the version
	verDot         float64 // the size of the dots of the version
	verCols        int
	presented, pre float64 // the bed comes forward (0 to 1); pre: the printer fades
}

// scene works out the places of the animation at the time t.
func introSceneAt(b image.Rectangle, t time.Duration, version string) introScene {
	version = strings.ToUpper(version)
	s := introScene{b: b, big: introLayout(b), version: version}
	// The word on the bed of the printer: as wide as the bed, less a dot at each end.
	s.pl = workhorsePlace(b, s.big.cols)
	p := (whBedRight - whBedLeft) * s.pl.s / float64(s.big.cols+2)
	s.small = s.big
	s.small.pitch = p
	s.small.x0 = s.pl.ox + whBedLeft*s.pl.s + p
	s.small.y0 = s.pl.oy + whBedTop*s.pl.s - float64(s.big.rows)*p
	s.tm = s.small.times(version)
	tm := s.tm
	// The printer slides in from the right.
	if t < tm.heat {
		f := easeOut(max(0, float64(t-tm.slide)) / float64(introSlide))
		s.dx = (1 - f) * float64(b.Dx())
		s.small.x0 += s.dx
	}
	if t > tm.present {
		s.presented = easeInOut(min(1, float64(t-tm.present)/float64(introPresent)))
	}
	s.small.fade = s.presented

	// The version: under the bed, in the middle, in the small dots of the toolhead alone.
	s.verDot = max(2, math.Round(s.big.pitch*0.4))
	s.verCols = max(0, len(version)*6-1)
	s.verX = float64(b.Min.X) + (float64(b.Dx())-float64(s.verCols)*s.verDot)/2
	s.verY = s.big.y0 + float64(s.big.rows+introBed+1)*s.big.pitch

	g := s.small
	park := [2]float64{g.x0 + g.pitch/2, g.y0 - 6*g.pitch}
	s.dot, s.onGantry, s.shown = s.pl.dot, true, t >= tm.slide
	switch {
	case t < tm.drop:
		s.tipX, s.tipY = park[0], park[1]
	case t < tm.print:
		f := easeInOut(float64(t-tm.drop) / float64(introDrop))
		s.tipX = park[0]
		s.tipY = park[1] + f*(g.y0+float64(g.rows-1)*g.pitch-park[1])
	case t < tm.park:
		x, y := printingAt(g.cols, g.rows, t-tm.print, introRow)
		s.tipX, s.tipY, s.hot = g.x0+g.pitch/2+x*g.pitch, g.y0+float64(y)*g.pitch, true
	case t < tm.enter:
		x, y := printingAt(g.cols, g.rows, tm.park-tm.print, introRow)
		ex, ey := g.x0+g.pitch/2+x*g.pitch, g.y0+float64(y)*g.pitch
		f := easeInOut(min(1, float64(t-tm.park)/float64(introPark)))
		s.tipX, s.tipY = ex+(park[0]-ex)*f, ey+(park[1]-ey)*f
	default:
		s.hiccup(t)
	}
	return s
}

// hiccup is the toolhead alone at the end: it hops to the wipe pad, wipes, spits out a blob of
// ooze, hops to the version, prints it, and hops away.
func (s *introScene) hiccup(t time.Duration) {
	tm, b := s.tm, s.b
	s.onGantry = false
	s.dot = max(2, math.Round(s.big.pitch*0.4))
	d := s.dot
	pad := s.padPlace()
	over := [2]float64{float64(pad.Min.X) + 7*d, float64(pad.Min.Y) - d} // the tip over the pad
	start := [2]float64{over[0], over[1]}
	if s.version != "" {
		start = [2]float64{s.verX + s.verDot/2, s.verY + float64(introFont-1)*s.verDot}
	}
	away := [2]float64{float64(b.Min.X) - 20*d, float64(b.Min.Y) + float64(b.Dy())/3}
	s.wipeShown = 1
	switch {
	case t < tm.wipe: // hop in from the right
		f := min(1, float64(t-tm.enter)/float64(introEnter))
		from := [2]float64{float64(b.Max.X) + 20*d, over[1] - 10*d}
		s.tipX, s.tipY = hop(from, over, f, 12*d)
		s.wipeShown = f
	case t < tm.ooze: // wipe: quick strokes over the pad
		f := float64(t-tm.wipe) / float64(introWipe)
		s.tipX = over[0] + math.Sin(f*math.Pi*6)*4*d
		s.tipY = over[1]
	case t < tm.goTo: // spit out a blob, which drops on the pad
		f := float64(t-tm.ooze) / float64(introOoze)
		s.tipX, s.tipY = over[0], over[1]-3*d
		s.oozeX = over[0]
		switch {
		case f < 0.5: // it grows under the nozzle
			s.oozeR, s.oozeY, s.hot = 1+f*4, s.tipY+d+f*4*d, true
		default: // it drops on the pad
			k := (f - 0.5) * 2
			s.oozeR, s.oozeY = 3, s.tipY+3*d+k*k*(over[1]+d-s.tipY-3*d)
		}
	case t < tm.version: // hop to the version
		f := float64(t-tm.goTo) / float64(introGoto)
		s.tipX, s.tipY = hop(over, start, f, 8*d)
		s.wipeShown = 1 - f
		s.oozeR, s.oozeX, s.oozeY = 3, over[0], over[1]+d
	case t < tm.exit: // print the version
		x, y := printingAt(s.verCols, introFont, t-tm.version, introVerRow)
		s.tipX, s.tipY = s.verX+s.verDot/2+x*s.verDot, s.verY+float64(y)*s.verDot
		s.hot, s.wipeShown = true, 0
	case t < tm.end: // hop away
		f := float64(t-tm.exit) / float64(introExit)
		from := start
		if s.version != "" {
			from = [2]float64{s.verX + s.verDot/2, s.verY}
		}
		s.tipX, s.tipY = hop(from, away, f, 10*d)
		s.wipeShown = 0
	default:
		s.shown, s.wipeShown = false, 0
	}
	if t > tm.version {
		s.wipeShown = 0
	}
}

// padPlace is the wipe pad, at the bottom right corner of the screen.
func (s *introScene) padPlace() image.Rectangle {
	d := int(max(2, math.Round(s.big.pitch*0.4)))
	return image.Rect(s.b.Max.X-24-14*d, s.b.Max.Y-24-4*d, s.b.Max.X-24, s.b.Max.Y-24)
}

// hop is a jump from a to b at f (0 to 1), with its top h over the higher end.
func hop(a, b [2]float64, f, h float64) (x, y float64) {
	f = max(0, min(1, f))
	x = a[0] + (b[0]-a[0])*easeInOut(f)
	y = a[1] + (b[1]-a[1])*f - 4*h*f*(1-f)
	return x, y
}

// DrawIntro draws the start animation at the time t from its start, on the whole canvas. At 0 it
// is the empty display, the picture of the kernel logo and of the splash screen. version is
// printed at the end; it can be empty.
func DrawIntro(c *gfx.Canvas, t time.Duration, version string) {
	b := c.Bounds()
	s := introSceneAt(b, t, version)
	if c.Recording() == nil {
		draw.Draw(c.RGBA, b, introBackground(b, s.big), image.Point{}, draw.Src)
	} else {
		c.Image(b, introBackground(b, s.big))
	}
	if t < s.tm.slide {
		return
	}
	g := s.small
	part := g.between(s.big, s.presented)

	if s.presented < 1 {
		s.pl.drawPrinter(c, s.dx, s.presented)
	}
	part.drawBed(c, t-s.tm.heat, s.presented)
	for y := range part.rows {
		for x := range part.cols {
			if !part.lit(x, y) {
				continue
			}
			at := s.tm.print + printedAt(g.cols, g.rows, x, y, introRow)
			if t >= at {
				c.Fill(part.cell(x, y), coolDot(t-at))
			}
		}
	}
	// The version, printed by the toolhead alone.
	for y := range introFont {
		for x := range s.verCols {
			if !glyphDot(s.version, x, y) {
				continue
			}
			at := s.tm.version + printedAt(s.verCols, introFont, x, y, introVerRow)
			if t >= at {
				px, py := s.verX+float64(x)*s.verDot, s.verY+float64(y)*s.verDot
				c.Fill(image.Rect(int(px), int(py), int(px+s.verDot)-1, int(py+s.verDot)-1), coolDot(t-at))
			}
		}
	}
	if s.wipeShown > 0 {
		s.drawPad(c)
	}
	if s.oozeR > 0 {
		r := s.oozeR * s.dot
		c.RoundRect(image.Rect(int(s.oozeX-r/2), int(s.oozeY-r/2), int(s.oozeX+r/2), int(s.oozeY+r/2)), int(r/2), introPLA)
	}
	if !s.shown {
		return
	}
	tool := g // the toolhead on the printer fades with it
	tool.pitch = s.dot / 0.4
	if !s.onGantry {
		tool = s.big
		tool.fade = 0
	}
	if s.onGantry {
		s.pl.drawAxis(c, s.tipX, s.tipY, s.dx, s.presented)
	}
	tool.drawFilament(c, b, t, s)
	tool.drawToolhead(c, b, s.tipX, s.tipY, s.hot, !s.onGantry)
}

// coolDot is the colour of a printed dot at the age a: hot at first, then the colour of the
// filament.
func coolDot(a time.Duration) color.RGBA {
	return mixRGB(introPLA, introHot, 1-min(1, float64(a)/float64(introCool)))
}

// ink is a colour of the printer, as it fades back.
func (g introGeom) ink(col color.RGBA) color.RGBA { return mixRGB(col, introBG, g.fade) }

// dot is the size of the small dots of the printer: the toolhead, the gantry and the filament.
func (g introGeom) dot() float64 { return max(2, math.Round(g.pitch*0.4)) }

// smallDot fills the small dot at x, y, on the grid of the small dots, so that it looks like the
// display.
func (g introGeom) smallDot(c *gfx.Canvas, b image.Rectangle, x, y float64, col color.RGBA) {
	d := g.dot()
	px, py := math.Floor(x/d)*d, math.Floor(y/d)*d
	if py+d < float64(b.Min.Y) || py > float64(b.Max.Y) {
		return
	}
	c.Fill(image.Rect(int(px), int(py), int(px+d), int(py+d)), g.ink(col))
}

// drawToolhead draws the hotend of the status screen in small dots, with the tip of its nozzle at
// x, y. While it prints, its heater block glows. alone adds the green carriage on top, for the
// toolhead without the gantry.
func (g introGeom) drawToolhead(c *gfx.Canvas, b image.Rectangle, x, y float64, hot, alone bool) {
	d := g.dot()
	off, on := lcdscreen.IconDots("hotend", false), lcdscreen.IconDots("hotend", true)
	ox, oy := x-8*d, y-15*d // the tip is the bottom of row 14, between columns 7 and 8
	for row := range 16 {
		for col := range 16 {
			if !on[row][col] {
				continue
			}
			col0 := introInk
			if !off[row][col] {
				if !hot {
					continue
				}
				col0 = introHot // the lit heater block
			}
			g.smallDot(c, b, ox+float64(col)*d+d/2, oy+float64(row)*d+d/2, col0)
		}
	}
	if alone {
		g.drawCarriage(c, b, x, y-15*d-3*d)
	}
}

// drawCarriage draws the green carriage of the toolhead, with its top at y.
func (g introGeom) drawCarriage(c *gfx.Canvas, b image.Rectangle, x, y float64) {
	d := g.dot()
	for yy := y - 2*d; yy < y+4*d; yy += d {
		for xx := x - 5*d; xx < x+5*d; xx += d {
			g.smallDot(c, b, xx, yy, introGreen)
		}
	}
}

// drawBed draws the bed under the word, as the bed icon of the status screen: a plate, and legs
// as much as legs (0 to 1). t is the time from the start of the heating: it glows red while it
// heats, with heat waves over it, and then stays lit.
func (g introGeom) drawBed(c *gfx.Canvas, t time.Duration, legs float64) {
	heat := float64(t) / float64(introHeat) // 0 to 1 while it heats, then more
	x0, x1 := -1, g.cols                    // the plate is one dot wider than the word
	top := g.rows
	glow := mixRGB(introInk, introHeat2, max(0, min(1, 1.5-heat)))
	if heat < 0 {
		glow = introInk
	}
	for x := x0; x <= x1; x++ {
		for y := top; y < top+4; y++ {
			c.Fill(g.cell(x, y), glow)
		}
	}
	// The legs of the bed icon come with the part, when the bed comes forward.
	for _, x := range []int{x0 + 1, x1 - 1} {
		for y := top + 4; y < top+6; y++ {
			if legs > 0 {
				c.Fill(g.cell(x, y), mixRGB(introBG, introInk, legs))
			}
		}
	}
	// The heat waves: three wavy lines of dots that rise over the plate while it heats.
	if heat > 0 && heat < 1 {
		for i := range 3 {
			wx := x0 + (x1-x0)*(i+1)/4
			for k := range 4 {
				rise := heat*4 + float64(k)
				y := top - 1 - int(rise)
				x := wx + int(math.Round(math.Sin(rise*1.6+float64(i))))
				if y >= 0 {
					c.Fill(g.cell(x, y), mixRGB(introCell, introHeat2, 1-heat))
				}
			}
		}
	}
}

// The filament: one smooth arc from the guide above the screen into the top of the carriage. It
// goes into the carriage straight from above, and is free at the guide, so it never makes an S.
// The middle of the arc lags behind the toolhead on a damped spring, so the filament swings a
// little when the toolhead moves or turns, and then settles. It gets shorter as the toolhead goes
// up: the spool takes it in.
const (
	filamentStep  = time.Second / 120
	filamentSpace = 0.45 // the control point of the arc, above the toolhead, in parts of the length
	filamentFreq  = 9.0  // the spring of the swing, in radians per second
	filamentDampR = 0.45 // the damping ratio of the swing
	filamentMass  = 0.03 // how much the movement of the toolhead swings the filament
	filamentKick  = 4000 // the largest push, so that a jump of the toolhead does not throw it
)

// filamentSim is the swing of the filament after a number of steps, for one screen. The frames
// come in order, so the next frame goes on from the last one. An earlier time starts again from
// the start, so that each frame is the same on each run.
var filamentSim struct {
	sync.Mutex
	bounds        image.Rectangle
	version       string
	steps         int
	lag, vel      [2]float64 // the swing of the control point, and its speed
	last, lastVel [2]float64 // the inlet of the toolhead at the last step, and its speed
	valid         bool
}

// inlet is the place where the filament goes into the toolhead: the top of the carriage.
func (s introScene) inlet() [2]float64 {
	if s.onGantry {
		return [2]float64{s.tipX, s.tipY - s.pl.inletAbove()}
	}
	return [2]float64{s.tipX, s.tipY - 20*s.dot}
}

// filamentAt returns the three points of the arc at the time t: the guide, the control point and
// the inlet of the carriage.
func filamentAt(b image.Rectangle, t time.Duration, version string) [3][2]float64 {
	guide := [2]float64{float64(b.Min.X + b.Dx()/2), float64(b.Min.Y) - 40}
	inlet := func(t time.Duration) [2]float64 { return introSceneAt(b, t, version).inlet() }
	sim := &filamentSim
	sim.Lock()
	defer sim.Unlock()
	steps := int(t / filamentStep)
	if !sim.valid || sim.bounds != b || sim.version != version || steps < sim.steps {
		sim.bounds, sim.version, sim.steps, sim.valid = b, version, 0, true
		sim.lag, sim.vel, sim.lastVel = [2]float64{}, [2]float64{}, [2]float64{}
		sim.last = inlet(0)
	}
	dt := filamentStep.Seconds()
	for s := sim.steps + 1; s <= steps; s++ {
		in := inlet(time.Duration(s) * filamentStep)
		for k := range 2 {
			v := (in[k] - sim.last[k]) / dt
			acc := max(-filamentKick/filamentMass, min(filamentKick/filamentMass, (v-sim.lastVel[k])/dt))
			// A damped spring, which the movement of the toolhead pushes.
			f := -filamentFreq*filamentFreq*sim.lag[k] - 2*filamentDampR*filamentFreq*sim.vel[k] - acc*filamentMass
			sim.vel[k] += f * dt
			sim.lag[k] += sim.vel[k] * dt
			sim.last[k], sim.lastVel[k] = in[k], v
		}
	}
	sim.steps = max(sim.steps, steps)
	in := inlet(t)
	space := math.Hypot(in[0]-guide[0], in[1]-guide[1]) * filamentSpace
	return [3][2]float64{guide, {in[0] + sim.lag[0], in[1] - space + sim.lag[1]}, in}
}

// drawFilament draws the arc of the filament in small dots, in the colour of the filament.
func (g introGeom) drawFilament(c *gfx.Canvas, b image.Rectangle, t time.Duration, s introScene) {
	p := filamentAt(b, t, s.version)
	length := math.Hypot(p[1][0]-p[0][0], p[1][1]-p[0][1]) + math.Hypot(p[2][0]-p[1][0], p[2][1]-p[1][1])
	n := int(length/g.dot()) + 1
	for k := 0; k <= n; k++ {
		f := float64(k) / float64(n)
		// A point of the quadratic Bezier curve.
		x := (1-f)*(1-f)*p[0][0] + 2*(1-f)*f*p[1][0] + f*f*p[2][0]
		y := (1-f)*(1-f)*p[0][1] + 2*(1-f)*f*p[1][1] + f*f*p[2][1]
		g.smallDot(c, b, x, y, introPLA)
	}
}

// drawPad draws the wipe pad: a brush of small dots, in the corner.
func (s introScene) drawPad(c *gfx.Canvas) {
	r := s.padPlace()
	d := int(s.dot)
	col := mixRGB(introBG, introFrame, s.wipeShown)
	for y := r.Min.Y; y < r.Max.Y; y += d {
		for x := r.Min.X; x < r.Max.X; x += d {
			if (x/d+y/d)%2 == 0 || y >= r.Max.Y-2*d {
				c.Fill(image.Rect(x, y, x+d-1, y+d-1), col)
			}
		}
	}
}

// mixRGB mixes two colours: a at f 0, c at f 1.
func mixRGB(a, c color.RGBA, f float64) color.RGBA {
	m := func(x, y uint8) uint8 { return uint8(math.Round(float64(x) + (float64(y)-float64(x))*f)) }
	return color.RGBA{m(a.R, c.R), m(a.G, c.G), m(a.B, c.B), 255}
}

func easeOut(f float64) float64   { return 1 - (1-f)*(1-f) }
func easeInOut(f float64) float64 { return f * f * (3 - 2*f) }

// Crossfade draws the mix of two frames of the same size in dst: from at f 0, to at f 1. The start
// animation goes into the first screen of the deck with it.
func Crossfade(dst, from, to *gfx.Canvas, f float64) {
	a := uint32(math.Round(max(0, min(1, f)) * 256))
	for i := 0; i+3 < len(dst.Pix) && i+3 < len(from.Pix) && i+3 < len(to.Pix); i += 4 {
		for k := range 3 {
			dst.Pix[i+k] = uint8((uint32(from.Pix[i+k])*(256-a) + uint32(to.Pix[i+k])*a) >> 8)
		}
		dst.Pix[i+3] = 255
	}
}
