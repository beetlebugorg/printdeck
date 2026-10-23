package printer

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"slices"
	"strings"
)

// The pressure advance test of Klipper: a square tower with thin walls, printed fast with a low
// acceleration, while TUNING_TOWER raises the pressure advance with the height. The height where
// the corners look best gives the pressure advance: PAStart + PAFactor * height.
const (
	PAStart       = 0.0
	PAFactor      = 0.005 // for a direct drive extruder: 0.15 at the top of the tower
	PATowerHeight = 30.0  // mm
	PATowerFile   = "printdeck-pa-tower.gcode"

	paSide       = 40.0  // mm, the outer side of the tower
	paWallSpeed  = 100.0 // mm/s
	paFirstSpeed = 20.0  // mm/s, the first layer
	paTravel     = 150.0 // mm/s
	paAccel      = 500.0 // mm/s², the acceleration of the test
	paCorner     = 1.0   // mm/s, the square corner velocity of the test
	paRetract    = 1.0   // mm, at each layer change
)

// PATowerValue is the pressure advance of the tower at a height in mm.
func PATowerValue(height float64) float64 {
	return PAStart + PAFactor*height
}

// PATower is the G-code of the pressure advance tower for the machine m, with the nozzle at tool
// and the bed at bed degrees, for the material (for example "PLA"). It starts with the PRINT_START
// macro when the printer has one, with the parameters that the Cura profile of the deck gives it,
// and else heats and homes. At the end it sets the pressure advance and the acceleration of
// printer.cfg again.
func PATower(m Machine, tool, bed int, material string) string {
	nozzle := m.NozzleDiameter
	if nozzle <= 0 {
		nozzle = 0.4
	}
	width := math.Round(nozzle*1.125*100) / 100       // the width of a line
	layer := math.Round(nozzle*0.6/0.05) * 0.05       // a coarse layer: about 60 % of the nozzle
	area := math.Pi * m.Diameter() * m.Diameter() / 4 // of the filament
	cx, cy := (m.MinX+m.MaxX)/2, (m.MinY+m.MaxY)/2    // the middle of the bed
	if m.MaxX <= m.MinX || m.MaxY <= m.MinY {
		cx, cy = 100, 100
	}
	layers := int(math.Round(PATowerHeight / layer))
	macro := func(name string) bool { return slices.Contains(m.Macros, name) }

	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	w(";FLAVOR:Klipper")
	w(";Generated with Printdeck: the pressure advance tower")
	w(";LAYER_COUNT:%d", layers)
	if material != "" {
		w(";PRINTDECK_MATERIAL: %s", material)
	}
	w(";Pressure advance %.3f at the bottom, %.3f mm a layer: read the height of the best corners.", PAStart, PAFactor)
	if macro("PRINT_START") {
		w("PRINT_START BED=%d EXTRUDER=%d WIPE=%d LAYER=%.2f SPEED=%.0f", bed, tool, max(tool-50, 150), layer, paFirstSpeed)
	} else {
		w("M140 S%d", bed)
		w("M104 S%d", tool)
		w("G28")
		w("M190 S%d", bed)
		w("M109 S%d", tool)
	}
	w("G90")
	w("M83")
	w("TUNING_TOWER COMMAND=SET_PRESSURE_ADVANCE PARAMETER=ADVANCE START=%g FACTOR=%g", PAStart, PAFactor)

	// A square loop at an inset from the outer side, from the middle of its back side, so that the
	// seam is not at a corner.
	loop := func(inset, z, speed, flow float64) {
		h := paSide/2 - inset - width/2
		pts := [][2]float64{{cx, cy + h}, {cx + h, cy + h}, {cx + h, cy - h}, {cx - h, cy - h}, {cx - h, cy + h}, {cx, cy + h}}
		w("G1 X%.3f Y%.3f F%.0f", pts[0][0], pts[0][1], paTravel*60)
		w("G1 E%.3f F2100", paRetract)
		for i := 1; i < len(pts); i++ {
			d := math.Hypot(pts[i][0]-pts[i-1][0], pts[i][1]-pts[i-1][1])
			w("G1 X%.3f Y%.3f E%.5f F%.0f", pts[i][0], pts[i][1], d*width*layer/area*flow, speed*60)
		}
		w("G1 E-%.3f F2100", paRetract)
	}
	for i := 0; i < layers; i++ {
		z := layer * float64(i+1)
		w(";LAYER:%d", i)
		w("G1 Z%.3f F600", z)
		if i == 1 {
			// After the first layer: the fan, and the slow corners and acceleration of the test.
			w("M106 S255")
			w("SET_VELOCITY_LIMIT ACCEL=%g SQUARE_CORNER_VELOCITY=%g", paAccel, paCorner)
		}
		speed, flow, walls := paWallSpeed, 1.0, 2
		if i == 0 {
			speed, flow, walls = paFirstSpeed, 1.1, 3
		}
		w(";TYPE:WALL-OUTER")
		loop(0, z, speed, flow)
		for k := 1; k < walls; k++ {
			w(";TYPE:WALL-INNER")
			loop(float64(k)*width, z, speed, flow)
		}
	}
	// The settings of printer.cfg again.
	w("SET_PRESSURE_ADVANCE ADVANCE=%g", m.PressureAdvance)
	if m.MaxAccel > 0 && m.SquareCornerVelocity > 0 {
		w("SET_VELOCITY_LIMIT ACCEL=%g SQUARE_CORNER_VELOCITY=%g", m.MaxAccel, m.SquareCornerVelocity)
	}
	if macro("PRINT_END") {
		w("PRINT_END")
	} else {
		w("M107")
		w("M104 S0")
		w("M140 S0")
		w("G91")
		w("G1 Z10 F600")
		w("G90")
		w("M84")
	}
	return b.String()
}

// paLine is a pressure_advance line of printer.cfg.
var paLine = regexp.MustCompile(`(?m)^pressure_advance\s*[:=].*$`)

// setConfigPA sets pressure_advance in the [extruder] section of the config text cfg. It changes
// the line of the section when it has one, and else adds a section [extruder] with the line before
// the block of SAVE_CONFIG, where Klipper adds it to the section of the same name.
func setConfigPA(cfg string, v float64) string {
	line := fmt.Sprintf("pressure_advance: %g", v)
	lines := strings.Split(cfg, "\n")
	in := false
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "#*#") {
			break
		}
		if strings.HasPrefix(t, "[") {
			in = t == "[extruder]"
			continue
		}
		if in && paLine.MatchString(l) {
			lines[i] = line
			return strings.Join(lines, "\n")
		}
	}
	add := "\n[extruder]\n" + line + "\n"
	if i := strings.Index(cfg, "#*# <"); i >= 0 {
		return strings.TrimRight(cfg[:i], "\n") + "\n" + add + "\n" + cfg[i:]
	}
	return strings.TrimRight(cfg, "\n") + "\n" + add
}

// PrintPATower writes the pressure advance tower to the file store and prints it.
func (k *Klippy) PrintPATower(tool, bed int, material string) error {
	path, err := k.store.FullPath(PATowerFile)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(PATower(k.Machine(), tool, bed, material)), 0o644); err != nil {
		return err
	}
	k.LoadFiles()
	return k.script(fmt.Sprintf("SDCARD_PRINT_FILE FILENAME=%q", PATowerFile))
}

// SetPressureAdvance sets the pressure advance of the extruder now, and writes it to printer.cfg.
func (k *Klippy) SetPressureAdvance(v float64) error {
	if err := k.script(fmt.Sprintf("SET_PRESSURE_ADVANCE ADVANCE=%g", v)); err != nil {
		return err
	}
	if k.ConfigPath == "" {
		return nil
	}
	b, err := os.ReadFile(k.ConfigPath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(k.ConfigPath, []byte(setConfigPA(string(b), v)), 0o644); err != nil {
		return err
	}
	k.mu.Lock()
	k.settings.PressureAdvance = v
	k.mu.Unlock()
	return nil
}
