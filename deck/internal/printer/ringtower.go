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

// RingTower is the G-code of the ringing tower for the machine m, with the nozzle at tool and the
// bed at bed degrees, for the material (for example "PLA"). It starts with the PRINT_START macro
// when the printer has one, and else heats and homes. At the end it sets the acceleration, the
// pressure advance and the input shaper of printer.cfg again.
//
// With check, the input shaper stays on: the tower checks the shaper, and the band where ringing or
// round corners start gives the largest acceleration (RingMaxAccel).
func RingTower(m Machine, tool, bed int, material string, check bool) string {
	nozzle := m.NozzleDiameter
	if nozzle <= 0 {
		nozzle = 0.4
	}
	width := math.Round(nozzle*1.1*100) / 100         // one wall: about 0.55 mm for a 0.5 mm nozzle
	layer := math.Round(nozzle*0.4/0.05) * 0.05       // 0.2 mm for a 0.5 mm nozzle: the flow stays low at 80 mm/s
	area := math.Pi * m.Diameter() * m.Diameter() / 4 // of the filament
	cx, cy := (m.MinX+m.MaxX)/2, (m.MinY+m.MaxY)/2    // the middle of the bed
	if m.HasMesh {
		cx, cy = (m.MeshMin[0]+m.MeshMax[0])/2, (m.MeshMin[1]+m.MeshMax[1])/2
	} else if m.MaxX <= m.MinX || m.MaxY <= m.MinY {
		cx, cy = 100, 100
	}
	layers := int(math.Round(RingTowerHeight / layer))

	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	w(";FLAVOR:Klipper")
	if check {
		w(";Generated with Printdeck: the ringing tower, with the input shaper on")
	} else {
		w(";Generated with Printdeck: the ringing tower")
	}
	w(";LAYER_COUNT:%d", layers)
	if material != "" {
		w(";PRINTDECK_MATERIAL: %s", material)
	}
	w(";Acceleration %.0f at the bottom, %.0f more each %.0f mm. Walls at %.0f mm/s.", RingAccelStart, RingAccelStep, RingBand, RingSpeed)
	printStart(&b, m, tool, bed, layer, ringFirstSpeed)
	w("G90")
	w("M83")

	// A square loop at an inset from the outer side, from the middle of its back side, so that the
	// seam is not at a corner. The right, the front and the left walls have a V notch.
	loop := func(inset, speed, flow float64) {
		h := ringSide/2 - inset - width/2
		corners := [][2]float64{{cx, cy + h}, {cx + h, cy + h}, {cx + h, cy - h}, {cx - h, cy - h}, {cx - h, cy + h}, {cx, cy + h}}
		pts := [][2]float64{corners[0], corners[1]}
		for i := 1; i < len(corners)-1; i++ {
			p, q := corners[i], corners[i+1]
			if i < 4 {
				// The notch: in toward the middle of the tower, and out again.
				l := math.Hypot(q[0]-p[0], q[1]-p[1])
				u := [2]float64{(q[0] - p[0]) / l, (q[1] - p[1]) / l}
				n := [2]float64{-u[1], u[0]} // the loop goes clockwise: the left of the way is out
				n = [2]float64{-n[0], -n[1]}
				at := func(along, in float64) [2]float64 {
					return [2]float64{p[0] + u[0]*along + n[0]*in, p[1] + u[1]*along + n[1]*in}
				}
				pts = append(pts, at(ringNotchAt, 0), at(ringNotchAt+ringNotchWidth/2, ringNotchDepth), at(ringNotchAt+ringNotchWidth, 0))
			}
			pts = append(pts, q)
		}
		w("G1 X%.3f Y%.3f F%.0f", pts[0][0], pts[0][1], ringTravel*60)
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
			// After the first layer: the fan, and the test. No pressure advance and no input
			// shaper, the full acceleration up to the end of each wall, and the acceleration of
			// each band.
			w("M106 S255")
			w("SET_PRESSURE_ADVANCE ADVANCE=0")
			if m.HasShaper && !check {
				w("SET_INPUT_SHAPER SHAPER_FREQ_X=0 SHAPER_FREQ_Y=0")
			}
			w("SET_VELOCITY_LIMIT MINIMUM_CRUISE_RATIO=0")
			w("TUNING_TOWER COMMAND=SET_VELOCITY_LIMIT PARAMETER=ACCEL START=%g STEP_DELTA=%g STEP_HEIGHT=%g",
				RingAccelStart, RingAccelStep, RingBand)
		}
		if i == 0 {
			// Three walls at the bottom, so that the tower stays on the bed.
			for k := 0; k < 3; k++ {
				w(";TYPE:WALL-OUTER")
				loop(float64(k)*width, ringFirstSpeed, 1.1)
			}
			continue
		}
		w(";TYPE:WALL-OUTER")
		loop(0, RingSpeed, 1.0)
	}
	// The settings of printer.cfg again.
	w("SET_PRESSURE_ADVANCE ADVANCE=%g", m.PressureAdvance)
	if m.HasShaper && !check {
		w("SET_INPUT_SHAPER SHAPER_FREQ_X=%g SHAPER_FREQ_Y=%g", m.ShaperFreqX, m.ShaperFreqY)
	}
	if m.MaxAccel > 0 {
		w("SET_VELOCITY_LIMIT ACCEL=%g MINIMUM_CRUISE_RATIO=%g", m.MaxAccel, m.MinCruiseRatio)
	}
	printEnd(&b, m)
	return b.String()
}

// RingMaxAccel is the largest acceleration from the check tower: the acceleration of the band under
// the height in mm where ringing or round corners start. It is 0 in the first band.
func RingMaxAccel(height float64) float64 {
	if height < RingBand {
		return 0
	}
	return RingAccel(height) - RingAccelStep
}

// PrintRingTower writes the ringing tower to the file store and prints it. With check, the input
// shaper stays on (RingTower).
func (k *Klippy) PrintRingTower(tool, bed int, material string, check bool) error {
	file := RingTowerFile
	if check {
		file = RingCheckFile
	}
	path, err := k.store.FullPath(file)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(RingTower(k.Machine(), tool, bed, material, check)), 0o644); err != nil {
		return err
	}
	k.LoadFiles()
	return k.script(fmt.Sprintf("SDCARD_PRINT_FILE FILENAME=%q", file))
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
