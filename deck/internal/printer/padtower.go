package printer

import (
	"fmt"
	"math"
	"strings"
)

// The height check: a square tower, and a small solid pad next to it. After each layer, the probe
// measures the top of the pad (PAD_CHECK of [printdeck_pad]). With correct, the deck also changes
// the Z offset for the next layers from the error. Print the tower once without and once with the
// correction, and compare the results and the height of the two towers.
const (
	PadTowerFile        = "printdeck-height-check.gcode"
	PadTowerCorrectFile = "printdeck-height-check-correct.gcode"
	PadTowerHeight      = 20.0 // mm

	padTowerSide = 20.0  // mm, the outer side of the tower
	padSide      = 16.0  // mm, the side of the pad: the pin can be some mm from its place
	padApart     = 34.0  // mm between the middles of the tower and the pad: 7 mm between the brims
	padWalls     = 3     // the walls of the tower
	padSpeed     = 40.0  // mm/s
	padFirst     = 20.0  // mm/s, the first layer
	padTravel    = 150.0 // mm/s
	padRetract   = 1.0   // mm, with no [firmware_retraction]
	padBrim      = 10    // the loops of the brim of the pad: the taps of the pin must not move it
	towerBrim    = 6     // the loops of the brim of the tower
)

// PadTower is the G-code of the height check for the machine m, with the nozzle at tool and the
// bed at bed degrees, for the material (for example "PLA"). With correct, PAD_CHECK corrects the
// Z offset. The pad is on the side of the probe pin, so the nozzle stays near the tower while the
// pin is over the pad.
func PadTower(m Machine, tool, bed int, material string, correct bool) string {
	nozzle := m.NozzleDiameter
	if nozzle <= 0 {
		nozzle = 0.4
	}
	width := math.Round(nozzle*1.125*100) / 100
	layer := math.Round(nozzle*0.5/0.05) * 0.05
	area := math.Pi * m.Diameter() * m.Diameter() / 4
	cx, cy := towerCenter(m)
	side := 1.0
	if m.ProbeOffset[0] < 0 {
		side = -1
	}
	tower := [2]float64{cx - side*padApart/2, cy}
	pad := [2]float64{cx + side*padApart/2, cy}
	layers := int(math.Round(PadTowerHeight / layer))
	retract := padRetract
	if m.Has("firmware_retraction") && m.RetractLength > 0 {
		retract = m.RetractLength
	}
	corr := 0
	if correct {
		corr = 1
	}

	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	w(";FLAVOR:Klipper")
	w(";Generated with Printdeck: the height check")
	w(";LAYER_COUNT:%d", layers)
	if material != "" {
		w(";PRINTDECK_MATERIAL: %s", material)
	}
	if correct {
		w(";The probe measures the pad after each layer, and the deck corrects the Z offset.")
	} else {
		w(";The probe measures the pad after each layer. The Z offset stays.")
	}
	printStart(&b, m, tool, bed, layer, padFirst)
	w("PAD_RESET")
	w("G90")
	w("M83")
	w("G1 E-%.3f F1800", retract) // each unretract below has a retract before it
	e := func(d, flow float64) float64 { return d * width * layer / area * flow }
	x, y := 0.0, 0.0
	to := func(nx, ny float64) { w("G1 X%.3f Y%.3f F%.0f", nx, ny, padTravel*60); x, y = nx, ny }
	line := func(nx, ny, speed, flow float64) {
		w("G1 X%.3f Y%.3f E%.5f F%.0f", nx, ny, e(math.Hypot(nx-x, ny-y), flow), speed*60)
		x, y = nx, ny
	}
	for i := 0; i < layers; i++ {
		z := layer * float64(i+1)
		w(";LAYER:%d", i)
		w("G1 Z%.3f F600", z)
		if i == 1 {
			w("M106 S255")
		}
		speed, flow := padSpeed, 1.0
		if i == 0 {
			speed, flow = padFirst, 1.1
		}
		// squares prints loops around c, each with the half side of hs, from the first. The travel
		// to the first loop has the unretract after it, and the loops next to each other go on
		// with no retract.
		squares := func(c [2]float64, hs []float64) {
			for k, h := range hs {
				to(c[0]-h, c[1]-h)
				if k == 0 {
					w("G1 E%.3f F1800", retract)
				}
				line(c[0]+h, c[1]-h, speed, flow)
				line(c[0]+h, c[1]+h, speed, flow)
				line(c[0]-h, c[1]+h, speed, flow)
				line(c[0]-h, c[1]-h, speed, flow)
			}
		}
		// brim is the half sides of the loops of a brim around a square of side side, from the
		// outer loop in to the part. It is only on the first layer.
		brim := func(side float64, loops int) []float64 {
			var hs []float64
			for k := loops; i == 0 && k >= 1; k-- {
				hs = append(hs, side/2-width/2+float64(k)*width)
			}
			return hs
		}
		// The tower: the brim, then the walls from the inner wall to the outer wall.
		hs := brim(padTowerSide, towerBrim)
		for k := padWalls - 1; k >= 0; k-- {
			hs = append(hs, padTowerSide/2-float64(k)*width-width/2)
		}
		w(";TYPE:WALL-OUTER")
		squares(tower, hs)
		w("G1 E-%.3f F1800", retract)
		// The brim of the pad: wide, so the taps of the pin do not move the pad.
		pb := brim(padSide, padBrim)
		if len(pb) > 0 {
			w(";TYPE:SKIRT")
			squares(pad, pb)
			w("G1 E-%.3f F1800", retract)
		}
		// The pad: solid lines, along X on even layers and along Y on odd layers.
		w(";TYPE:SKIN")
		h := padSide/2 - width/2
		n := int(math.Floor(2*h/width)) + 1
		for j := 0; j < n; j++ {
			a := -h + float64(j)*2*h/float64(n-1)
			p0, p1 := [2]float64{pad[0] - h, pad[1] + a}, [2]float64{pad[0] + h, pad[1] + a}
			if i%2 == 1 {
				p0, p1 = [2]float64{pad[0] + a, pad[1] - h}, [2]float64{pad[0] + a, pad[1] + h}
			}
			if j%2 == 1 {
				p0, p1 = p1, p0
			}
			if j == 0 {
				to(p0[0], p0[1])
				w("G1 E%.3f F1800", retract)
			} else {
				line(p0[0], p0[1], speed, flow)
			}
			line(p1[0], p1[1], speed, flow)
		}
		w("G1 E-%.3f F1800", retract)
		// The check: the nozzle over the middle of the pad.
		to(pad[0], pad[1])
		w("PAD_CHECK X=%.3f Y=%.3f CORRECT=%d", pad[0], pad[1], corr)
	}
	w("G1 E%.3f F1800", retract)
	w("PAD_RESET RESULTS=0") // the correction goes, and the results stay for the deck
	printEnd(&b, m)
	return b.String()
}

// PrintPadTower prints the height check. With correct, the deck corrects the Z offset during the
// print.
func (k *Klippy) PrintPadTower(tool, bed int, material string, correct bool) error {
	m := k.Machine()
	if !m.Has("printdeck_pad") {
		return fmt.Errorf("printer.cfg has no [printdeck_pad]")
	}
	file := PadTowerFile
	if correct {
		file = PadTowerCorrectFile
	}
	return k.printTest(file, PadTower(m, tool, bed, material, correct))
}

// PadChecks reads the results of the last height check: for each check the height of the nozzle,
// the error, and the correction of the layer, in mm.
func (k *Klippy) PadChecks() ([][3]float64, error) {
	st, err := k.Query(map[string][]string{"printdeck_pad": {"checks"}})
	if err != nil {
		return nil, err
	}
	list, _ := st["printdeck_pad"]["checks"].([]any)
	var out [][3]float64
	for _, c := range list {
		v, _ := c.([]any)
		if len(v) == 3 {
			out = append(out, [3]float64{num(v[0]), num(v[1]), num(v[2])})
		}
	}
	return out, nil
}
