package printer

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"strings"

	"printdeck/internal/camera"
)

// The target of the camera calibration with a print (camera.KindTowers): 5 square towers, solid,
// with a cap on each top (camera.TowerPlaces). The deck writes the G-code itself. The towers have
// no overhang, so they print with no supports.
const (
	TowersFile = "printdeck-camera-towers.gcode"

	towersLayer      = 0.2   // mm, each layer
	towersFirstSpeed = 15.0  // mm/s, the first layer
	towersSpeed      = 30.0  // mm/s, the other layers
	towersTravel     = 120.0 // mm/s
	towersZSpeed     = 10.0  // mm/s
	towersHop        = 0.4   // mm, the lift of the travel from a tower to the next
	towersRetract    = 1.0   // mm of filament, at each travel
	towersLift       = 30.0  // mm over the towers at the end
	// towersMoveTime is the time of the speed changes of a move, in seconds: the moves are short,
	// so the accelerations are a large part of the time. On the simulated TAZ Workhorse the towers
	// take 26 s a layer, and the moves at their speeds take 16 s.
	towersMoveTime = 0.07
)

// Towers is the G-code of the calibration towers, and its estimates.
type Towers struct {
	Gcode string
	// Places are the axes of the towers (X and Y), in the order of the marks.
	Places [][2]float64
	Layers int
	// Seconds is the time of the moves, with no heating.
	Seconds float64
	// Filament is the length of the filament, in mm.
	Filament float64
}

// CalibTowers is the G-code of the calibration towers for the machine m, with the nozzle at tool
// and the bed at bed degrees, for the material (for example "PLA"). It starts and ends as the
// pressure advance tower does (printStart, printEnd). The towers are in the print area of the
// travel of X and Y (camera.PrintArea).
func CalibTowers(m Machine, tool, bed int, material string) Towers {
	nozzle := m.NozzleDiameter
	if nozzle <= 0 {
		nozzle = 0.4
	}
	width := math.Round(nozzle*1.125*100) / 100       // the width of a line
	area := math.Pi * m.Diameter() * m.Diameter() / 4 // of the filament
	lo, hi := [2]float64{0, 0}, [2]float64{200, 200}
	if m.MaxX > m.MinX && m.MaxY > m.MinY {
		lo, hi = camera.PrintArea([2]float64{m.MinX, m.MinY}, [2]float64{m.MaxX, m.MaxY})
	}
	places := camera.TowerPlaces(lo, hi)
	body := int(math.Round(camera.TowerHeight / towersLayer))
	layers := body + int(math.Round(camera.TowerCap/towersLayer))

	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	t := Towers{Places: places, Layers: layers}
	var pos [3]float64 // the place of the nozzle, for the estimates
	move := func(x, y, z, e, speed float64) {
		d := math.Sqrt((x-pos[0])*(x-pos[0]) + (y-pos[1])*(y-pos[1]) + (z-pos[2])*(z-pos[2]))
		t.Seconds += d/speed + towersMoveTime
		t.Filament += max(0, e)
		pos = [3]float64{x, y, z}
	}
	// extrude prints a line to (x, y) at the height z.
	extrude := func(x, y, z, speed, flow float64) {
		e := math.Hypot(x-pos[0], y-pos[1]) * width * towersLayer / area * flow
		w("G1 X%.3f Y%.3f E%.5f F%.0f", x, y, e, speed*60)
		move(x, y, z, e, speed)
	}
	// travel goes to (x, y) at the height z: a retract, a lift, the move, and back down.
	travel := func(x, y, z float64) {
		w("G1 E-%.2f F2100", towersRetract)
		w("G1 Z%.3f F%.0f", z+towersHop, towersZSpeed*60)
		move(pos[0], pos[1], z+towersHop, 0, towersZSpeed)
		w("G0 X%.3f Y%.3f F%.0f", x, y, towersTravel*60)
		move(x, y, z+towersHop, 0, towersTravel)
		w("G1 Z%.3f F%.0f", z, towersZSpeed*60)
		move(x, y, z, 0, towersZSpeed)
		w("G1 E%.2f F2100", towersRetract)
	}
	// square prints one layer of a solid square of the side s around (cx, cy) at the height z:
	// two loops, from the middle of the side of the largest Y so the seam is not at a corner, and
	// lines inside them, in X on the even layers and in Y on the odd layers.
	square := func(cx, cy, s, z float64, layer int, speed, flow float64) {
		loops := 0
		for k := range 2 {
			h := s/2 - width/2 - float64(k)*width
			if h < width/2 {
				break
			}
			pts := [][2]float64{{cx, cy + h}, {cx + h, cy + h}, {cx + h, cy - h}, {cx - h, cy - h}, {cx - h, cy + h}, {cx, cy + h}}
			if k == 0 {
				travel(pts[0][0], pts[0][1], z)
				w(";TYPE:WALL-OUTER")
			} else {
				w(";TYPE:WALL-INNER")
				w("G1 X%.3f Y%.3f F%.0f", pts[0][0], pts[0][1], speed*60)
				move(pts[0][0], pts[0][1], z, 0, speed)
			}
			for _, p := range pts[1:] {
				extrude(p[0], p[1], z, speed, flow)
			}
			loops++
		}
		r := s - 2*float64(loops)*width // the side of the inside
		n := int(math.Floor(r/width + 1e-6))
		if n <= 0 {
			return
		}
		w(";TYPE:SKIN")
		half := r/2 - width/2
		for j := range n {
			off := -r/2 + width/2 + float64(j)*width + (r-float64(n)*width)/2
			a, c := [2]float64{-half, off}, [2]float64{half, off}
			if j%2 == 1 {
				a, c = c, a
			}
			if layer%2 == 1 {
				a, c = [2]float64{a[1], a[0]}, [2]float64{c[1], c[0]}
			}
			w("G1 X%.3f Y%.3f F%.0f", cx+a[0], cy+a[1], speed*60)
			move(cx+a[0], cy+a[1], z, 0, speed)
			extrude(cx+c[0], cy+c[1], z, speed, flow)
		}
	}

	w(";FLAVOR:Klipper")
	w(";Generated with Printdeck: the towers of the camera calibration")
	header := b.Len()
	w(";LAYER_COUNT:%d", layers)
	if material != "" {
		w(";PRINTDECK_MATERIAL: %s", material)
	}
	printStart(&b, m, tool, bed, towersLayer, towersFirstSpeed)
	w("G90")
	w("M83")
	pos = [3]float64{places[0][0], places[0][1], towersLift}
	for i := range layers {
		z := towersLayer * float64(i+1)
		w(";LAYER:%d", i)
		if i == 1 {
			w("M106 S255")
		}
		speed, flow := towersSpeed, 1.0
		if i == 0 {
			speed, flow = towersFirstSpeed, 1.05
		}
		side := camera.TowerSide
		if i >= body {
			side = camera.TowerCapSide
		}
		for _, p := range places {
			square(p[0], p[1], side, z, i, speed, flow)
		}
	}
	top := towersLayer*float64(layers) + towersLift
	w("G1 E-%.2f F2100", towersRetract)
	w("G1 Z%.3f F%.0f", top, towersZSpeed*60)
	move(pos[0], pos[1], top, 0, towersZSpeed)
	printEnd(&b, m)

	// The estimates go in the header, as Cura writes them.
	g := b.String()
	meta := fmt.Sprintf(";TIME:%.0f\n;Filament used: %.3fm\n", t.Seconds, t.Filament/1000)
	t.Gcode = g[:header] + meta + g[header:]
	return t
}

// PrintCalibTowers writes the calibration towers to the file store and prints them. The file has
// one name, so a new calibration replaces it, and the deck does not write it again when it is the
// same.
func (k *Klippy) PrintCalibTowers(tool, bed int, material string) error {
	path, err := k.store.FullPath(TowersFile)
	if err != nil {
		return err
	}
	g := []byte(CalibTowers(k.Machine(), tool, bed, material).Gcode)
	if old, err := os.ReadFile(path); err != nil || !bytes.Equal(old, g) {
		if err := os.WriteFile(path, g, 0o644); err != nil {
			return err
		}
		k.LoadFiles()
	}
	return k.script(fmt.Sprintf("SDCARD_PRINT_FILE FILENAME=%q", TowersFile))
}
