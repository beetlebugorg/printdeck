package printer

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"strings"
)

// The ringing test of Klipper (docs/Resonance_Compensation.md): a square tower with one thin
// wall, printed fast with no input shaper and no pressure advance, while TUNING_TOWER raises the
// acceleration with the height. After each corner the head or the bed rings, and the wall shows
// ripples. The distance between the ripples gives the ringing frequency of the axis:
//
//	frequency = RingSpeed × ripples / distance
//
// Each wall but the back one has a V notch, as the ringing tower of Klipper
// (docs/prints/ringing_tower.stl): the head jogs in and out, and the ripples start at the notch.
// Count them from the notch. A notch in a wall along Y jogs the head in X, so X rings on the left
// and the right walls; a notch in a wall along X jogs it in Y, so Y rings on the front wall. The
// layers start in the middle of the back wall, so it has no notch.
const (
	RingTowerFile   = "printdeck-ringing-tower.gcode"
	RingCheckFile   = "printdeck-ringing-check.gcode"
	RingSpeed       = 80.0   // mm/s, the speed of the wall
	RingAccelStart  = 1000.0 // mm/s², at the bottom of the tower
	RingAccelStep   = 500.0  // mm/s² more for each band
	RingBand        = 5.0    // mm, the height of a band
	RingTowerHeight = 40.0   // mm: 8 bands, up to 4500 mm/s²

	ringSide       = 60.0  // mm, the outer side of the tower
	ringNotchAt    = 12.0  // mm from the corner where the wall starts to the notch
	ringNotchWidth = 6.0   // mm, the V notch along the wall
	ringNotchDepth = 3.0   // mm, into the tower
	ringFirstSpeed = 20.0  // mm/s, the first layer
	ringTravel     = 150.0 // mm/s
	ringRetract    = 1.0   // mm, at each layer change
)

// RingAccel is the acceleration of the tower at a height in mm.
func RingAccel(height float64) float64 {
	return RingAccelStart + RingAccelStep*math.Floor(height/RingBand)
}

// RingFrequency is the ringing frequency in Hz from a count of ripples over a distance in mm.
// It is 0 for no ripples or no distance.
func RingFrequency(ripples int, distance float64) float64 {
	if ripples <= 0 || distance <= 0 {
		return 0
	}
	return RingSpeed * float64(ripples) / distance
}

// The tests of the shaper frequency, with the input shaper on at a fixed acceleration:
//
//	SweepTower  one tower in bands of SweepBand, a frequency of SweepFreqs in each band. The
//	            smoothest band of an axis gives its frequency (SweepFreqAt).
//	PairTowers  two towers next to each other, each with its own frequencies: the left tower a
//	            lower pair, the right tower a higher pair. The smoother tower of an axis moves its
//	            frequency, and the next pair is closer.
//
// The deck sets the shaper with SET_INPUT_SHAPER in the G-code: before a band, or before each
// tower in each layer.
const (
	SweepFile       = "printdeck-shaper-sweep.gcode"
	PairFile        = "printdeck-shaper-pair.gcode"
	SweepBand       = 2.5   // mm
	SweepStart      = 20.0  // Hz, the bottom band
	SweepStep       = 3.0   // Hz more for each band
	SweepBands      = 10    // 20 to 47 Hz: 25 mm
	PairHeight      = 8.0   // mm
	ShaperTestAccel = 4500. // mm/s², the acceleration of the sweep and the pair: the top of the ringing tower

	pairGap = 10.0 // mm between the two towers
)

// SweepFreqs are the frequencies of the bands of the sweep tower, from the bottom.
func SweepFreqs() []float64 {
	out := make([]float64, SweepBands)
	for i := range out {
		out[i] = SweepStart + SweepStep*float64(i)
	}
	return out
}

// SweepFreqAt is the frequency of the sweep tower at a height in mm.
func SweepFreqAt(height float64) float64 {
	i := int(math.Floor(height / SweepBand))
	i = min(max(i, 0), SweepBands-1)
	return SweepStart + SweepStep*float64(i)
}

// towerPlan is a test print of one or more notched towers with one wall.
type towerPlan struct {
	title   string
	notes   []string     // comment lines at the top
	centers [][2]float64 // the middle of each tower
	height  float64      // mm
	setup   []string     // after the first layer: the test settings
	// before returns the commands before a tower in a layer (from the second layer), or nil.
	before func(layer int, z float64, tower int) []string
	// shaper is true when the test changes the input shaper: the end sets it again.
	shaper bool
}

// towerCenter is the middle of the bed: the middle of the mesh area, or of the travel.
func towerCenter(m Machine) (float64, float64) {
	switch {
	case m.HasMesh:
		return (m.MeshMin[0] + m.MeshMax[0]) / 2, (m.MeshMin[1] + m.MeshMax[1]) / 2
	case m.MaxX > m.MinX && m.MaxY > m.MinY:
		return (m.MinX + m.MaxX) / 2, (m.MinY + m.MaxY) / 2
	}
	return 100, 100
}

// towerGcode is the G-code of a tower plan for the machine m, with the nozzle at tool and the bed
// at bed degrees, for the material (for example "PLA"). It starts with the PRINT_START macro when
// the printer has one, and else heats and homes. At the end it sets the acceleration, the
// pressure advance and the input shaper of printer.cfg again.
func towerGcode(m Machine, tool, bed int, material string, p towerPlan) string {
	nozzle := m.NozzleDiameter
	if nozzle <= 0 {
		nozzle = 0.4
	}
	width := math.Round(nozzle*1.1*100) / 100         // one wall: about 0.55 mm for a 0.5 mm nozzle
	layer := math.Round(nozzle*0.4/0.05) * 0.05       // 0.2 mm for a 0.5 mm nozzle: the flow stays low at 80 mm/s
	area := math.Pi * m.Diameter() * m.Diameter() / 4 // of the filament
	layers := int(math.Round(p.height / layer))

	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	w(";FLAVOR:Klipper")
	w(";Generated with Printdeck: %s", p.title)
	w(";LAYER_COUNT:%d", layers)
	if material != "" {
		w(";PRINTDECK_MATERIAL: %s", material)
	}
	for _, n := range p.notes {
		w(";%s", n)
	}
	printStart(&b, m, tool, bed, layer, ringFirstSpeed)
	w("G90")
	w("M83")

	// A square loop at an inset from the outer side, from the middle of its back side, so that the
	// seam is not at a corner. The right, the front and the left walls have a V notch.
	loop := func(cx, cy, inset, speed, flow float64, hop bool, z float64) {
		h := ringSide/2 - inset - width/2
		corners := [][2]float64{{cx, cy + h}, {cx + h, cy + h}, {cx + h, cy - h}, {cx - h, cy - h}, {cx - h, cy + h}, {cx, cy + h}}
		pts := [][2]float64{corners[0], corners[1]}
		for i := 1; i < len(corners)-1; i++ {
			p, q := corners[i], corners[i+1]
			if i < 4 {
				// The notch: in toward the middle of the tower, and out again.
				l := math.Hypot(q[0]-p[0], q[1]-p[1])
				u := [2]float64{(q[0] - p[0]) / l, (q[1] - p[1]) / l}
				n := [2]float64{u[1], -u[0]} // the loop goes clockwise: the right of the way is in
				at := func(along, in float64) [2]float64 {
					return [2]float64{p[0] + u[0]*along + n[0]*in, p[1] + u[1]*along + n[1]*in}
				}
				pts = append(pts, at(ringNotchAt, 0), at(ringNotchAt+ringNotchWidth/2, ringNotchDepth), at(ringNotchAt+ringNotchWidth, 0))
			}
			pts = append(pts, q)
		}
		if hop {
			// To the next tower: up over the walls.
			w("G1 Z%.3f F600", z+1)
		}
		w("G1 X%.3f Y%.3f F%.0f", pts[0][0], pts[0][1], ringTravel*60)
		if hop {
			w("G1 Z%.3f F600", z)
		}
		w("G1 E%.3f F2100", ringRetract)
		for i := 1; i < len(pts); i++ {
			d := math.Hypot(pts[i][0]-pts[i-1][0], pts[i][1]-pts[i-1][1])
			w("G1 X%.3f Y%.3f E%.5f F%.0f", pts[i][0], pts[i][1], d*width*layer/area*flow, speed*60)
		}
		w("G1 E-%.3f F2100", ringRetract)
	}
	for i := 0; i < layers; i++ {
		z := layer * float64(i+1)
		w(";LAYER:%d", i)
		w("G1 Z%.3f F600", z)
		if i == 1 {
			// After the first layer: the fan, and the test.
			w("M106 S255")
			for _, l := range p.setup {
				w("%s", l)
			}
		}
		for t, c := range p.centers {
			if i > 0 && p.before != nil {
				for _, l := range p.before(i, z, t) {
					w("%s", l)
				}
			}
			if i == 0 {
				// Three walls at the bottom, so that the tower stays on the bed.
				for k := 0; k < 3; k++ {
					w(";TYPE:WALL-OUTER")
					loop(c[0], c[1], float64(k)*width, ringFirstSpeed, 1.1, t > 0 && k == 0, z)
				}
				continue
			}
			w(";TYPE:WALL-OUTER")
			loop(c[0], c[1], 0, RingSpeed, 1.0, t > 0, z)
		}
	}
	// The settings of printer.cfg again.
	w("SET_PRESSURE_ADVANCE ADVANCE=%g", m.PressureAdvance)
	if m.HasShaper && p.shaper {
		w("SET_INPUT_SHAPER SHAPER_TYPE=%s SHAPER_FREQ_X=%g SHAPER_FREQ_Y=%g", shaperType(m), m.ShaperFreqX, m.ShaperFreqY)
	}
	if m.MaxAccel > 0 {
		w("SET_VELOCITY_LIMIT ACCEL=%g MINIMUM_CRUISE_RATIO=%g", m.MaxAccel, m.MinCruiseRatio)
	}
	printEnd(&b, m)
	return b.String()
}

func shaperType(m Machine) string {
	if m.ShaperType == "" {
		return "mzv"
	}
	return m.ShaperType
}

// RingTower is the G-code of the ringing tower: the shaper off and no pressure advance, while
// TUNING_TOWER raises the acceleration with the height.
//
// With check, the input shaper stays on: the tower checks the shaper, and the band where ringing or
// round corners start gives the largest acceleration (RingMaxAccel).
func RingTower(m Machine, tool, bed int, material string, check bool) string {
	cx, cy := towerCenter(m)
	p := towerPlan{title: "the ringing tower", centers: [][2]float64{{cx, cy}}, height: RingTowerHeight,
		notes: []string{fmt.Sprintf("Acceleration %.0f at the bottom, %.0f more each %.0f mm. Walls at %.0f mm/s.",
			RingAccelStart, RingAccelStep, RingBand, RingSpeed)}}
	if check {
		p.title = "the ringing tower, with the input shaper on"
	}
	// No pressure advance and no input shaper, the full acceleration up to the end of each wall,
	// and the acceleration of each band.
	p.setup = append(p.setup, "SET_PRESSURE_ADVANCE ADVANCE=0")
	if m.HasShaper && !check {
		p.setup = append(p.setup, "SET_INPUT_SHAPER SHAPER_FREQ_X=0 SHAPER_FREQ_Y=0")
		p.shaper = true
	}
	p.setup = append(p.setup, "SET_VELOCITY_LIMIT MINIMUM_CRUISE_RATIO=0",
		fmt.Sprintf("TUNING_TOWER COMMAND=SET_VELOCITY_LIMIT PARAMETER=ACCEL START=%g STEP_DELTA=%g STEP_HEIGHT=%g",
			RingAccelStart, RingAccelStep, RingBand))
	return towerGcode(m, tool, bed, material, p)
}

// testSetup is the setup of the sweep and the pair: no pressure advance, the MZV shaper, and the
// fixed acceleration to the end of each wall.
func testSetup() []string {
	return []string{"SET_PRESSURE_ADVANCE ADVANCE=0", "SET_INPUT_SHAPER SHAPER_TYPE=mzv",
		fmt.Sprintf("SET_VELOCITY_LIMIT ACCEL=%g MINIMUM_CRUISE_RATIO=0", ShaperTestAccel)}
}

// SweepCenters are the places of the towers of the sweep: the middle of the bed, or with five the
// middle and the four corners of the area of the bed level test (250 mm on the TAZ), as the bed
// level test. The shaper uses the middle tower; the corners show if the frequency changes over
// the bed.
func SweepCenters(m Machine, five bool) [][2]float64 {
	cx, cy := towerCenter(m)
	if !five {
		return [][2]float64{{cx, cy}}
	}
	// Corners of the mesh area, 5 mm inside, less half a tower.
	dx, dy := 95., 95.
	if m.HasMesh {
		dx = (m.MeshMax[0]-m.MeshMin[0])/2 - ringSide/2 + 5
		dy = (m.MeshMax[1]-m.MeshMin[1])/2 - ringSide/2 + 5
	}
	return [][2]float64{{cx, cy}, {cx - dx, cy - dy}, {cx + dx, cy - dy}, {cx + dx, cy + dy}, {cx - dx, cy + dy}}
}

// SweepTower is the G-code of the sweep tower: the MZV shaper at the frequency of each band
// (SweepFreqs), the same on X and Y. With five, five towers over the bed (SweepCenters). The
// printer needs an [input_shaper] section.
func SweepTower(m Machine, tool, bed int, material string, five bool) string {
	var bands []string
	for i, f := range SweepFreqs() {
		bands = append(bands, fmt.Sprintf("%.1f-%.1f mm: %.0f Hz", float64(i)*SweepBand, float64(i+1)*SweepBand, f))
	}
	last := -1
	p := towerPlan{title: "the input shaper sweep", centers: SweepCenters(m, five), height: SweepBand * SweepBands,
		notes: append([]string{fmt.Sprintf("MZV at %.0f mm/s², walls at %.0f mm/s. The bands:", ShaperTestAccel, RingSpeed)}, bands...),
		setup: testSetup(), shaper: true,
		before: func(layer int, z float64, tower int) []string {
			// The band of the layer: the height of its bottom.
			i := int(math.Floor((z - 0.001) / SweepBand))
			i = min(max(i, 0), SweepBands-1)
			if tower > 0 || i == last {
				return nil
			}
			last = i
			f := SweepStart + SweepStep*float64(i)
			return []string{fmt.Sprintf("SET_INPUT_SHAPER SHAPER_FREQ_X=%g SHAPER_FREQ_Y=%g", f, f)}
		}}
	return towerGcode(m, tool, bed, material, p)
}

// PairTowers is the G-code of the pair: two towers next to each other, the left one with the
// shaper at left (X, Y), the right one at right (X, Y). The printer needs an [input_shaper]
// section. The deck swaps the side of the higher frequency each round: the place on the bed
// changes the frequency a little (about 0.7 % 35 mm off the middle).
func PairTowers(m Machine, tool, bed int, material string, low, high [2]float64) string {
	cx, cy := towerCenter(m)
	d := (ringSide + pairGap) / 2
	p := towerPlan{title: "the input shaper pair", centers: [][2]float64{{cx - d, cy}, {cx + d, cy}}, height: PairHeight,
		notes: []string{fmt.Sprintf("MZV at %.0f mm/s², walls at %.0f mm/s.", ShaperTestAccel, RingSpeed),
			fmt.Sprintf("Left tower: X %.2f Hz, Y %.2f Hz. Right tower: X %.2f Hz, Y %.2f Hz.", low[0], low[1], high[0], high[1])},
		setup: testSetup(), shaper: true,
		before: func(layer int, z float64, tower int) []string {
			f := low
			if tower == 1 {
				f = high
			}
			return []string{fmt.Sprintf("SET_INPUT_SHAPER SHAPER_FREQ_X=%.2f SHAPER_FREQ_Y=%.2f", f[0], f[1])}
		}}
	return towerGcode(m, tool, bed, material, p)
}

// RingMaxAccel is the largest acceleration from the check tower: the acceleration of the band under
// the height in mm where ringing or round corners start. It is 0 in the first band.
func RingMaxAccel(height float64) float64 {
	if height < RingBand {
		return 0
	}
	return RingAccel(height) - RingAccelStep
}

// printTest writes the G-code of a test to the file store and prints it.
func (k *Klippy) printTest(file, gcode string) error {
	path, err := k.store.FullPath(file)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(gcode), 0o644); err != nil {
		return err
	}
	k.LoadFiles()
	return k.script(fmt.Sprintf("SDCARD_PRINT_FILE FILENAME=%q", file))
}

// PrintRingTower prints the ringing tower. With check, the input shaper stays on (RingTower).
func (k *Klippy) PrintRingTower(tool, bed int, material string, check bool) error {
	file := RingTowerFile
	if check {
		file = RingCheckFile
	}
	return k.printTest(file, RingTower(k.Machine(), tool, bed, material, check))
}

// PrintSweepTower prints the sweep tower of the shaper frequency, or with five the five towers.
func (k *Klippy) PrintSweepTower(tool, bed int, material string, five bool) error {
	return k.printTest(SweepFile, SweepTower(k.Machine(), tool, bed, material, five))
}

// PrintPairTowers prints the two towers of the shaper frequency: left at the left, right at the
// right.
func (k *Klippy) PrintPairTowers(tool, bed int, material string, low, high [2]float64) error {
	return k.printTest(PairFile, PairTowers(k.Machine(), tool, bed, material, low, high))
}

// SetMaxAccel sets the largest acceleration now, and writes max_accel of [printer] into printer.cfg.
func (k *Klippy) SetMaxAccel(v float64) error {
	if err := k.script(fmt.Sprintf("SET_VELOCITY_LIMIT ACCEL=%g", v)); err != nil {
		return err
	}
	if k.ConfigPath == "" {
		return nil
	}
	b, err := os.ReadFile(k.ConfigPath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(k.ConfigPath, []byte(setConfigOption(string(b), "printer", "max_accel", fmt.Sprintf("%g", v))), 0o644); err != nil {
		return err
	}
	k.mu.Lock()
	k.settings.MaxAccel = v
	k.mu.Unlock()
	return nil
}

// setConfigOption sets an option of a section in the config text cfg. It changes the line of the
// section when it has one, adds the line at the end of the section, or adds the section before
// the block of SAVE_CONFIG.
func setConfigOption(cfg, section, option, value string) string {
	line := option + ": " + value
	re := regexp.MustCompile(`^` + regexp.QuoteMeta(option) + `\s*[:=]`)
	lines := strings.Split(cfg, "\n")
	in, end := false, -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "#*#") {
			break
		}
		if strings.HasPrefix(t, "[") {
			if in {
				break
			}
			in = t == "["+section+"]"
			if in {
				end = i
			}
			continue
		}
		if in {
			if re.MatchString(l) {
				lines[i] = line
				return strings.Join(lines, "\n")
			}
			if t != "" && !strings.HasPrefix(t, "#") {
				end = i
			}
		}
	}
	if end >= 0 {
		lines = append(lines[:end+1], append([]string{line}, lines[end+1:]...)...)
		return strings.Join(lines, "\n")
	}
	add := "\n[" + section + "]\n" + line + "\n"
	if i := strings.Index(cfg, "#*# <"); i >= 0 {
		return strings.TrimRight(cfg[:i], "\n") + "\n" + add + "\n" + cfg[i:]
	}
	return strings.TrimRight(cfg, "\n") + "\n" + add
}

// SetInputShaper writes the input shaper to printer.cfg: the MZV shaper at the frequencies in Hz.
// With an [input_shaper] section the shaper changes at once (SET_INPUT_SHAPER). Without one,
// Klipper restarts to load it.
func (k *Klippy) SetInputShaper(freqX, freqY float64) error {
	if k.ConfigPath == "" {
		return fmt.Errorf("the deck does not know where printer.cfg is")
	}
	b, err := os.ReadFile(k.ConfigPath)
	if err != nil {
		return err
	}
	cfg := string(b)
	cfg = setConfigOption(cfg, "input_shaper", "shaper_freq_x", fmt.Sprintf("%.1f", freqX))
	cfg = setConfigOption(cfg, "input_shaper", "shaper_freq_y", fmt.Sprintf("%.1f", freqY))
	cfg = setConfigOption(cfg, "input_shaper", "shaper_type", "mzv")
	if err := os.WriteFile(k.ConfigPath, []byte(cfg), 0o644); err != nil {
		return err
	}
	if k.Machine().HasShaper {
		return k.script(fmt.Sprintf("SET_INPUT_SHAPER SHAPER_TYPE=mzv SHAPER_FREQ_X=%.1f SHAPER_FREQ_Y=%.1f", freqX, freqY))
	}
	return k.script("RESTART")
}
