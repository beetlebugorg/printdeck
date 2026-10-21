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

// The start animation, on a screen that is all one blue dot-matrix LCD. All the parts are lit
// dots of the one grid of the display, as on a real LCD:
//
//  1. The bed lights up from its middle, and heats: it glows red, and heat rises from it.
//  2. The toolhead comes down from above. It prints PRINTDECK on the bed, one row of dots at a
//     time from the bottom up, as a printer prints its layers. Each new dot is hot and cools to the
//     colour of the filament. The filament comes from above the screen into the toolhead, in one
//     arc that swings a little when the toolhead moves.
//  3. The toolhead hops to the wipe pad, wipes, and spits out a blob of ooze. Then it prints the
//     version above the word in a small font, and hops away. The toolhead stays above the
//     finished word, so the word stays clear.
//
// A tall screen has the word on two lines, PRINT over DECK, for larger dots.

const (
	introBlank  = 400 * time.Millisecond // the empty display
	introLight  = 400 * time.Millisecond // the bed lights up
	introHeat   = 900 * time.Millisecond // the bed heats
	introDrop   = 600 * time.Millisecond // the toolhead comes down to the first dot
	introRow    = 240 * time.Millisecond // one row of dots
	introCool   = 400 * time.Millisecond // a dot cools to the colour of the filament
	introToPad  = 500 * time.Millisecond // the toolhead hops to the wipe pad
	introWipe   = 700 * time.Millisecond // it wipes its nozzle
	introOoze   = 700 * time.Millisecond // it spits out a blob, which drops
	introToVer  = 450 * time.Millisecond // it hops to the version
	introVerRow = 180 * time.Millisecond // one row of the dots of the version
	introExit   = 500 * time.Millisecond // it hops away
	introHold   = 400 * time.Millisecond // the end, before the first screen
	introFont   = 8                      // the rows of the 5 x 8 font
	introGap    = 2                      // the empty rows between two lines
	introBed    = 3                      // the rows of the bed: the plate and its legs
	introVerGap = 2                      // the empty rows between the bed and the version
	introRows   = introBed + introVerGap + tinyRows
)

// IntroLength is the time of the whole animation on a screen of the size b.
func IntroLength(b image.Rectangle) time.Duration {
	return introLayout(b).times().end
}

// The colours: the display of the day palette, the filament (PLA orange) and its hot colour, the
// heat of the bed, and the white of the toolhead and the bed.
var (
	introBG    = rgb(0x1f4bd1)
	introCell  = rgb(0x2a58e0)
	introInk   = rgb(0xf2f6ff)
	introPLA   = rgb(0xff7a1a)
	introHot   = rgb(0xffe08a)
	introHeat2 = rgb(0xff5a3c)
	introFrame = rgb(0x8fa8ef)
)

// introGeom is the grid of the display: the size of its dots, and the screen place of the dot
// 0, 0. The dot 0, 0 is the top left dot of the word. The word has cols x rows dots. The bed is
// under it, and the version is above it.
type introGeom struct {
	b          image.Rectangle
	lines      []string
	cols, rows int
	pitch      int
	x0, y0     int
}

// introLayout puts the word, the bed and the version in the middle of the screen, with the
// largest dots that fit.
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
	// The bed is one dot wider than the word at each end.
	pitch := max(4, min(14, (b.Dx()-48)/(cols+2), (b.Dy()-48)/(rows+introRows)))
	// The version, the word and the bed in the middle of the screen. The toolhead prints the top
	// row of the version from above it, so the toolhead stays on the screen if it can.
	h := (rows + introRows) * pitch
	top := (b.Dy() - h) / 2
	if above := len(toolheadSprite) + 1; top < above*pitch {
		top = min(above*pitch, b.Dy()-h-pitch)
	}
	return introGeom{b: b, lines: lines, cols: cols, rows: rows, pitch: pitch,
		x0: b.Min.X + (b.Dx()-cols*pitch)/2, y0: b.Min.Y + top + (tinyRows+introVerGap)*pitch}
}

// introTimes are the starts of the parts of the animation.
type introTimes struct {
	light, heat, drop, print, toPad, wipe, ooze, toVer, version, exit, hold, end time.Duration
}

func (g introGeom) times() introTimes {
	var t introTimes
	t.light = introBlank
	t.heat = t.light + introLight
	t.drop = t.heat + introHeat
	t.print = t.drop + introDrop
	t.toPad = t.print + time.Duration(g.rows)*introRow
	t.wipe = t.toPad + introToPad
	t.ooze = t.wipe + introWipe
	t.toVer = t.ooze + introOoze
	t.version = t.toVer + introToVer
	t.exit = t.version + tinyRows*introVerRow
	t.hold = t.exit + introExit
	t.end = t.hold + introHold
	return t
}

// cell is the screen rectangle of the dot at column x and row y of the grid.
func (g introGeom) cell(x, y int) image.Rectangle {
	px, py := g.x0+x*g.pitch, g.y0+y*g.pitch
	return image.Rect(px+1, py+1, px+g.pitch-1, py+g.pitch-1)
}

// lit reports the dots of the word. Each line is in the middle of the word.
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

// bedTop is the top row of the plate of the bed.
func (g introGeom) bedTop() int { return g.rows }

// verTop is the top row of the version, above the word.
func (g introGeom) verTop() int { return -(tinyRows + introVerGap) }

// first is the first column and the first row of the grid on the screen.
func (g introGeom) first() (x, y int) {
	return -((g.x0 - g.b.Min.X + g.pitch - 1) / g.pitch), -((g.y0 - g.b.Min.Y + g.pitch - 1) / g.pitch)
}

// last is the last column and the last row of the grid on the screen.
func (g introGeom) last() (x, y int) {
	return (g.b.Max.X-g.x0)/g.pitch - 1, (g.b.Max.Y-g.y0)/g.pitch - 1
}

// pad is the wipe pad: its left column and its top row. It has 5 x 2 dots. It is at the right end
// of the bed, on the bottom row of the version if there is space next to the version, or else
// above the version.
func (g introGeom) pad(verCols int) (x, y int) {
	x = g.cols - 4
	if x > (g.cols+verCols)/2+1 {
		return x, g.verTop() + tinyRows - 2
	}
	return x, g.verTop() - 4
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

func introBackground(g introGeom) *image.RGBA {
	introGrid.Lock()
	defer introGrid.Unlock()
	b := g.b
	if introGrid.img != nil && introGrid.size == b.Size() {
		return introGrid.img
	}
	c := gfx.New(b.Dx(), b.Dy())
	c.Fill(c.Bounds(), introBG)
	fx, fy := g.first()
	lx, ly := g.last()
	for y := fy; y <= ly+1; y++ {
		for x := fx; x <= lx+1; x++ {
			c.Fill(g.cell(x, y).Sub(b.Min), introCell)
		}
	}
	introGrid.size, introGrid.img = b.Size(), c.RGBA
	return c.RGBA
}

// introScene is the state of the animation at one time.
type introScene struct {
	g        introGeom
	tm       introTimes
	version  string
	verCols  int
	tipX     float64 // the tip of the nozzle, in dots of the grid, with fractions
	tipY     float64
	shown    bool
	hot      bool    // the heater block glows
	pad      float64 // the wipe pad: 0 gone, 1 there
	oozeX    int     // the blob of ooze
	oozeY    float64 // NaN is no blob
	oozeDown bool    // the blob is on the pad
}

// introSceneAt computes the scene at the time t.
func introSceneAt(b image.Rectangle, t time.Duration, version string) introScene {
	g := introLayout(b)
	s := introScene{g: g, tm: g.times(), oozeY: math.NaN()}
	tm := s.tm
	// The version, in as many characters as fit over the width of the bed.
	version = strings.ToUpper(version)
	for len(version) > 0 && len(version)*tinyAdvance-1 > g.cols+2 {
		version = version[:len(version)-1]
	}
	s.version = version
	s.verCols = max(0, len(version)*tinyAdvance-1)

	_, fy := g.first()
	above := float64(fy - 2)                    // the tip of a toolhead above the screen
	start := [2]float64{0, float64(g.rows - 2)} // over the first dot
	px, py := g.pad(s.verCols)
	over := [2]float64{float64(px + 2), float64(py - 1)} // the tip over the pad
	verX := (g.cols - s.verCols) / 2
	verStart := [2]float64{float64(verX), float64(g.verTop() + tinyRows - 2)}
	lx, _ := g.last()
	away := [2]float64{float64(lx + 10), above}

	s.shown = t >= tm.drop && t < tm.hold
	switch {
	case t < tm.print: // come down
		f := easeOut(max(0, float64(t-tm.drop)) / float64(introDrop))
		s.tipX, s.tipY = start[0], above+(start[1]-above)*f
	case t < tm.toPad: // print the word
		x, y := printingAt(g.cols, g.rows, t-tm.print, introRow)
		s.tipX, s.tipY, s.hot = x, float64(y-1), true
	case t < tm.wipe: // hop to the pad
		x, y := printingAt(g.cols, g.rows, tm.toPad-tm.print, introRow)
		f := float64(t-tm.toPad) / float64(introToPad)
		s.tipX, s.tipY = hop([2]float64{x, float64(y - 1)}, over, f, 4)
		s.pad = f
	case t < tm.ooze: // wipe: quick strokes over the pad
		f := float64(t-tm.wipe) / float64(introWipe)
		s.tipX, s.tipY, s.pad = over[0]+math.Round(math.Sin(f*math.Pi*6)*2), over[1], 1
	case t < tm.toVer: // lift, spit out a blob, which drops on the pad
		f := float64(t-tm.ooze) / float64(introOoze)
		s.tipX, s.tipY, s.pad, s.oozeX = over[0], over[1]-2, 1, int(over[0])
		if f < 0.5 {
			s.hot, s.oozeY = true, s.tipY+1
		} else {
			k := (f - 0.5) * 2
			s.oozeY = min(float64(py), s.tipY+1+k*k*(float64(py)-s.tipY-1))
			s.oozeDown = s.oozeY >= float64(py)
		}
	case t < tm.version: // hop to the version
		f := float64(t-tm.toVer) / float64(introToVer)
		s.tipX, s.tipY = hop([2]float64{over[0], over[1] - 2}, verStart, f, 4)
		s.pad = 1 - f
		s.oozeX, s.oozeY, s.oozeDown = int(over[0]), float64(py), true
	case t < tm.exit: // print the version
		x, y := printingAt(s.verCols, tinyRows, t-tm.version, introVerRow)
		s.tipX, s.tipY, s.hot = float64(verX)+x, float64(g.verTop()+y-1), true
	default: // hop away
		f := float64(t-tm.exit) / float64(introExit)
		s.tipX, s.tipY = hop([2]float64{float64(verX), float64(g.verTop() - 1)}, away, f, 6)
	}
	if s.version == "" && t >= tm.toVer {
		s.shown = false
	}
	return s
}

// hop is a jump from a to b at f (0 to 1), with its top h dots over the line between them.
func hop(a, b [2]float64, f, h float64) (x, y float64) {
	f = max(0, min(1, f))
	x = a[0] + (b[0]-a[0])*easeInOut(f)
	y = a[1] + (b[1]-a[1])*f - 4*h*f*(1-f)
	return x, y
}

// DrawIntro draws the start animation at the time t from its start, on the whole canvas. At 0 it
// is the empty display, the picture of the kernel logo and of the splash screen. version is
// printed above the word; it can be empty.
func DrawIntro(c *gfx.Canvas, t time.Duration, version string) {
	b := c.Bounds()
	s := introSceneAt(b, t, version)
	g, tm := s.g, s.tm
	draw.Draw(c.RGBA, b, introBackground(g), image.Point{}, draw.Src)
	if t < tm.light {
		return
	}
	// The filament goes behind the part and the bed, and into the toolhead in front of them.
	if s.shown {
		s.drawFilament(c, t)
	}
	s.drawBed(c, t)
	for y := range g.rows {
		for x := range g.cols {
			if !g.lit(x, y) {
				continue
			}
			if at := tm.print + printedAt(g.cols, g.rows, x, y, introRow); t >= at {
				c.Fill(g.cell(x, y), coolDot(t-at))
			}
		}
	}
	// The version, in the small font.
	verX := (g.cols - s.verCols) / 2
	for y := range tinyRows {
		for x := range s.verCols {
			if !tinyDot(s.version, x, y) {
				continue
			}
			if at := tm.version + printedAt(s.verCols, tinyRows, x, y, introVerRow); t >= at {
				c.Fill(g.cell(verX+x, g.verTop()+y), coolDot(t-at))
			}
		}
	}
	if s.pad > 0 {
		s.drawPad(c)
	}
	if !math.IsNaN(s.oozeY) {
		col := introHot
		if s.oozeDown {
			col = introPLA
		}
		c.Fill(g.cell(s.oozeX, int(math.Round(s.oozeY))), col)
	}
	if s.shown {
		s.drawToolhead(c)
	}
}

// coolDot is the colour of a printed dot at the age a: hot at first, then the colour of the
// filament.
func coolDot(a time.Duration) color.RGBA {
	return mixRGB(introPLA, introHot, 1-min(1, float64(a)/float64(introCool)))
}

// drawBed draws the bed under the word, as the bed icon of the status screen: a plate of two
// rows, one dot wider than the word at each end, and two legs. It lights up from its middle.
// While it heats, it glows red, and heat waves rise from it.
func (s introScene) drawBed(c *gfx.Canvas, t time.Duration) {
	g, tm := s.g, s.tm
	light := min(1, float64(t-tm.light)/float64(introLight))
	heat := max(0, min(1, float64(t-tm.heat)/float64(introHeat)))
	col := mixRGB(introInk, introHeat2, math.Sin(heat*math.Pi))
	mid := float64(g.cols-1) / 2
	reach := light * (mid + 2)
	top := g.bedTop()
	for x := -1; x <= g.cols; x++ {
		if math.Abs(float64(x)-mid) > reach {
			continue
		}
		c.Fill(g.cell(x, top), col)
		c.Fill(g.cell(x, top+1), col)
	}
	if light >= 1 {
		c.Fill(g.cell(0, top+2), introInk)
		c.Fill(g.cell(g.cols-1, top+2), introInk)
	}
	// The heat waves: three wavy lines of dots that rise over the plate while it heats.
	if heat <= 0 || heat >= 1 {
		return
	}
	for i := range 3 {
		wx := (g.cols - 1) * (i + 1) / 4
		for k := range 3 {
			rise := heat*5 + float64(k)
			y := top - 1 - int(rise)
			x := wx + int(math.Round(math.Sin(rise*1.6+float64(i))))
			if y >= 0 {
				c.Fill(g.cell(x, y), mixRGB(introCell, introHeat2, (1-heat)*(1-float64(k)/3)))
			}
		}
	}
}

// drawPad draws the wipe pad: a brush of dots on a base.
func (s introScene) drawPad(c *gfx.Canvas) {
	g := s.g
	px, py := g.pad(s.verCols)
	col := mixRGB(introCell, introFrame, s.pad)
	for x := range 5 {
		if x%2 == 0 {
			c.Fill(g.cell(px+x, py), col)
		}
		c.Fill(g.cell(px+x, py+1), col)
	}
}

// The toolhead: the hotend of the status screen icon, in dots of the grid. # is lit, o is the
// heater block that glows while the toolhead prints, and : is a dark dot that hides what is
// behind. The tip of the nozzle is the last row, column 3.
var toolheadSprite = [...]string{
	"..###..",
	"#######",
	":::::::",
	"#######",
	":::::::",
	".#####.",
	".#ooo#.",
	".#####.",
	"..###..",
	"...#...",
}

const toolheadTipCol = 3

// drawToolhead draws the toolhead with the tip of its nozzle at the dot of the scene.
func (s introScene) drawToolhead(c *gfx.Canvas) {
	g := s.g
	tx, ty := int(math.Round(s.tipX)), int(math.Round(s.tipY))
	for row, line := range toolheadSprite {
		y := ty - (len(toolheadSprite) - 1) + row
		for col, ch := range line {
			x := tx - toolheadTipCol + col
			switch {
			case ch == '#':
				c.Fill(g.cell(x, y), introInk)
			case ch == 'o' && s.hot:
				c.Fill(g.cell(x, y), introHot)
			case ch == 'o' || ch == ':':
				c.Fill(g.cell(x, y), introCell)
			}
		}
	}
}

// The filament: one smooth arc from the guide above the screen into the top of the toolhead. It
// goes into the toolhead straight from above, and is free at the guide, so it never makes an S.
// The middle of the arc lags behind the toolhead on a damped spring, so the filament swings a
// little when the toolhead moves or turns, and then settles. It gets shorter as the toolhead goes
// up: the spool pulls it in.
const (
	filamentStep  = time.Second / 120
	filamentSpace = 0.45 // the control point of the arc, above the toolhead, in parts of the length
	filamentFreq  = 9.0  // the spring of the swing, in radians per second
	filamentDampR = 0.45 // the damping ratio of the swing
	filamentMass  = 0.03 // how much the movement of the toolhead swings the filament
	filamentKick  = 300  // the largest push in dots, so that a jump of the toolhead does not throw it
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

// inlet is the dot where the filament goes into the toolhead: over its top.
func (s introScene) inlet() [2]float64 {
	return [2]float64{s.tipX, s.tipY - float64(len(toolheadSprite))}
}

// filamentAt returns the three points of the arc at the time t, in dots of the grid: the guide,
// the control point and the inlet of the toolhead.
func filamentAt(b image.Rectangle, t time.Duration, version string) [3][2]float64 {
	g := introLayout(b)
	_, fy := g.first()
	guide := [2]float64{float64(g.cols) / 2, float64(fy - 4)}
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

// drawFilament draws the arc of the filament as a line of dots, one dot thick, in the colour of
// the filament.
func (s introScene) drawFilament(c *gfx.Canvas, t time.Duration) {
	g := s.g
	p := filamentAt(g.b, t, s.version)
	// The line ends on the column of the toolhead.
	p[2] = [2]float64{math.Round(p[2][0]), math.Round(p[2][1])}
	length := math.Hypot(p[1][0]-p[0][0], p[1][1]-p[0][1]) + math.Hypot(p[2][0]-p[1][0], p[2][1]-p[1][1])
	n := int(length*4) + 1
	var path []image.Point
	for k := 0; k <= n; k++ {
		f := float64(k) / float64(n)
		// A point of the quadratic Bezier curve.
		x := (1-f)*(1-f)*p[0][0] + 2*(1-f)*f*p[1][0] + f*f*p[2][0]
		y := (1-f)*(1-f)*p[0][1] + 2*(1-f)*f*p[1][1] + f*f*p[2][1]
		d := image.Pt(int(math.Round(x)), int(math.Round(y)))
		if len(path) > 0 && path[len(path)-1] == d {
			continue
		}
		// A dot between two diagonal neighbours makes a corner, where the line is thicker. It
		// goes.
		if len(path) >= 2 {
			a := path[len(path)-2]
			if absInt(a.X-d.X) <= 1 && absInt(a.Y-d.Y) <= 1 {
				path = path[:len(path)-1]
			}
		}
		path = append(path, d)
	}
	for _, d := range path {
		c.Fill(g.cell(d.X, d.Y), introPLA)
	}
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
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
