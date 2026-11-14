package printer

import (
	"fmt"
	"math"
	"os"
	"strings"
)

// The retraction test: two thin square towers, apart on X. In each layer the head prints one
// tower, retracts with G10, travels to the other tower, and unretracts with G11. A retraction that
// is too short leaves strings between the towers. The tower is in bands, and the retraction
// length goes up in each band (SET_RETRACTION). The lowest band with no strings gives the length:
// RetractAt(height).
//
// The test needs [firmware_retraction]. The deck changes the length only when the filament is not
// retracted: SET_RETRACTION clears the retracted state, so a G11 after it does not push the
// filament back.
const (
	RetractTowerFile = "printdeck-retraction-tower.gcode"
	RetractStart     = 0.25 // mm, the bottom band
	RetractStep      = 0.25 // mm more for each band
	RetractBand      = 3.0  // mm, the height of a band
	RetractBands     = 10   // 0.25 to 2.5 mm: 30 mm
	RetractSpeed     = 15.0 // mm/s, retract_speed and unretract_speed of a new [firmware_retraction]

	retSide       = 15.0  // mm, the outer side of a tower
	retApart      = 50.0  // mm between the middles of the towers
	retWallSpeed  = 40.0  // mm/s
	retFirstSpeed = 20.0  // mm/s, the first layer
	retTravel     = 150.0 // mm/s, the travel between the towers
)

// RetractAt is the retraction length of the tower at a height in mm.
func RetractAt(height float64) float64 {
	i := int(math.Floor(height / RetractBand))
	i = min(max(i, 0), RetractBands-1)
	return RetractStart + RetractStep*float64(i)
}

// RetractTower is the G-code of the retraction tower for the machine m, with the nozzle at tool
// and the bed at bed degrees, for the material (for example "PLA"). At the end it sets the
// retraction length of printer.cfg again.
func RetractTower(m Machine, tool, bed int, material string) string {
	nozzle := m.NozzleDiameter
	if nozzle <= 0 {
		nozzle = 0.4
	}
	width := math.Round(nozzle*1.125*100) / 100       // the width of a line
	layer := math.Round(nozzle*0.5/0.05) * 0.05       // 0.25 mm for a 0.5 mm nozzle
	area := math.Pi * m.Diameter() * m.Diameter() / 4 // of the filament
	cx, cy := towerCenter(m)
	towers := [2][2]float64{{cx - retApart/2, cy}, {cx + retApart/2, cy}}
	height := RetractBand * RetractBands
	layers := int(math.Round(height / layer))

	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	w(";FLAVOR:Klipper")
	w(";Generated with Printdeck: the retraction tower")
	w(";LAYER_COUNT:%d", layers)
	if material != "" {
		w(";PRINTDECK_MATERIAL: %s", material)
	}
	w(";Retraction %.2f mm at the bottom, %.2f mm more each %.0f mm: read the height of the lowest band with no strings.",
		RetractStart, RetractStep, RetractBand)
	printStart(&b, m, tool, bed, layer, retFirstSpeed)
	w("G90")
	w("M83")

	// A square loop at an inset from the outer side. It starts and ends at the middle of the side
	// that faces the other tower, so the travel is straight across the gap.
	loop := func(t int, inset, speed, flow float64, unretract bool) {
		c := towers[t]
		h := retSide/2 - inset - width/2
		s := 1.0 // the side that faces the other tower: right for tower 0, left for tower 1
		if t == 1 {
			s = -1
		}
		pts := [][2]float64{{c[0] + s*h, c[1]}, {c[0] + s*h, c[1] + h}, {c[0] - s*h, c[1] + h},
			{c[0] - s*h, c[1] - h}, {c[0] + s*h, c[1] - h}, {c[0] + s*h, c[1]}}
		w("G1 X%.3f Y%.3f F%.0f", pts[0][0], pts[0][1], retTravel*60)
		if unretract {
			w("G11")
		}
		for i := 1; i < len(pts); i++ {
			d := math.Hypot(pts[i][0]-pts[i-1][0], pts[i][1]-pts[i-1][1])
			w("G1 X%.3f Y%.3f E%.5f F%.0f", pts[i][0], pts[i][1], d*width*layer/area*flow, speed*60)
		}
	}
	// The walls of a tower, from the inner wall to the outer wall: the outer wall ends next to the
	// gap. With unretract, G11 follows the travel to the first wall.
	walls := func(t, n int, speed, flow float64, unretract bool) {
		for k := n - 1; k >= 0; k-- {
			if k == 0 {
				w(";TYPE:WALL-OUTER")
			} else {
				w(";TYPE:WALL-INNER")
			}
			loop(t, float64(k)*width, speed, flow, unretract && k == n-1)
		}
	}
	band := -1
	for i := 0; i < layers; i++ {
		z := layer * float64(i+1)
		w(";LAYER:%d", i)
		w("G1 Z%.3f F600", z)
		if i == 1 {
			w("M106 S255")
		}
		// A new band: the filament is not retracted here.
		if n := int(math.Floor((z - 0.001) / RetractBand)); n != band && n < RetractBands {
			band = n
			w("SET_RETRACTION RETRACT_LENGTH=%.2f", RetractStart+RetractStep*float64(n))
		}
		speed, flow, n := retWallSpeed, 1.0, 2
		if i == 0 {
			speed, flow, n = retFirstSpeed, 1.1, 3
		}
		walls(0, n, speed, flow, false)
		w("G10")
		walls(1, n, speed, flow, true) // the first move is the travel across the gap
		w("G10")
		// Back to the first tower, so the layer change is over it and not retracted.
		h := retSide/2 - width/2
		w("G1 X%.3f Y%.3f F%.0f", towers[0][0]+h, towers[0][1], retTravel*60)
		w("G11")
	}
	// The setting of printer.cfg again.
	w("SET_RETRACTION RETRACT_LENGTH=%g", m.RetractLength)
	printEnd(&b, m)
	return b.String()
}

// PrintRetractTower prints the retraction tower.
func (k *Klippy) PrintRetractTower(tool, bed int, material string) error {
	if !k.Machine().Has("firmware_retraction") {
		return fmt.Errorf("printer.cfg has no [firmware_retraction]: set the retraction first")
	}
	return k.printTest(RetractTowerFile, RetractTower(k.Machine(), tool, bed, material))
}

// SetRetraction writes the retraction length in mm to [firmware_retraction] of printer.cfg. With
// the section, the length changes at once (SET_RETRACTION). Without it, the deck adds the section
// with the speeds of RetractSpeed, and Klipper restarts to load it.
func (k *Klippy) SetRetraction(length float64) error {
	if k.ConfigPath == "" {
		return fmt.Errorf("no path of printer.cfg")
	}
	data, err := os.ReadFile(k.ConfigPath)
	if err != nil {
		return err
	}
	has := k.Machine().Has("firmware_retraction")
	cfg := setConfigOption(string(data), "firmware_retraction", "retract_length", fmt.Sprintf("%.2f", length))
	if !has {
		cfg = setConfigOption(cfg, "firmware_retraction", "retract_speed", fmt.Sprintf("%g", RetractSpeed))
		cfg = setConfigOption(cfg, "firmware_retraction", "unretract_speed", fmt.Sprintf("%g", RetractSpeed))
	}
	if err := os.WriteFile(k.ConfigPath, []byte(cfg), 0o644); err != nil {
		return err
	}
	if has {
		k.mu.Lock()
		k.settings.RetractLength = length
		k.mu.Unlock()
		return k.script(fmt.Sprintf("SET_RETRACTION RETRACT_LENGTH=%.2f", length))
	}
	return k.script("RESTART")
}
