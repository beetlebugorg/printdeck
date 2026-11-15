package printer

import (
	"fmt"
	"math"
	"os"
	"strings"
)

// The probe offset: a solid square pad, and then PAD_FIND_OFFSET of [printdeck_pad]. The pin finds
// the edges of the pad, and the middle of the edges gives x_offset and y_offset of the probe.
const (
	ProbePadFile   = "printdeck-probe-offset.gcode"
	ProbePadSide   = 20.0 // mm
	ProbePadHeight = 3.0  // mm
)

// ProbePad is the G-code of the probe offset pad for the machine m, with the nozzle at tool and the
// bed at bed degrees, for the material (for example "PLA"). The pad is at the middle of the bed.
func ProbePad(m Machine, tool, bed int, material string) string {
	nozzle := m.NozzleDiameter
	if nozzle <= 0 {
		nozzle = 0.4
	}
	width := math.Round(nozzle*1.125*100) / 100
	layer, layers := probePadLayers(m)
	area := math.Pi * m.Diameter() * m.Diameter() / 4
	cx, cy := towerCenter(m)
	retract := padRetract
	if m.Has("firmware_retraction") && m.RetractLength > 0 {
		retract = m.RetractLength
	}

	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	w(";FLAVOR:Klipper")
	w(";Generated with Printdeck: the probe offset pad")
	w(";LAYER_COUNT:%d", layers)
	if material != "" {
		w(";PRINTDECK_MATERIAL: %s", material)
	}
	printStart(&b, m, tool, bed, layer, padFirst)
	w("G90")
	w("M83")
	w("G1 E-%.3f F1800", retract)
	x, y := 0.0, 0.0
	to := func(nx, ny float64) { w("G1 X%.3f Y%.3f F%.0f", nx, ny, padTravel*60); x, y = nx, ny }
	line := func(nx, ny, speed, flow float64) {
		w("G1 X%.3f Y%.3f E%.5f F%.0f", nx, ny, math.Hypot(nx-x, ny-y)*width*layer/area*flow, speed*60)
		x, y = nx, ny
	}
	for i := 0; i < layers; i++ {
		w(";LAYER:%d", i)
		w("G1 Z%.3f F600", layer*float64(i+1))
		if i == 1 {
			w("M106 S255")
		}
		speed, flow := padSpeed, 1.0
		if i == 0 {
			speed = padFirst // full flow: the pad is solid, and more plastic has no room
		}
		// Two walls, for sharp edges, then solid lines along X or Y.
		w(";TYPE:WALL-OUTER")
		for k := 1; k >= 0; k-- {
			h := ProbePadSide/2 - float64(k)*width - width/2
			to(cx-h, cy-h)
			if k == 1 {
				w("G1 E%.3f F1800", retract)
			}
			line(cx+h, cy-h, speed, flow)
			line(cx+h, cy+h, speed, flow)
			line(cx-h, cy+h, speed, flow)
			line(cx-h, cy-h, speed, flow)
		}
		w(";TYPE:SKIN")
		h := ProbePadSide/2 - 2*width - width/2
		n := int(math.Floor(2*h/width)) + 1
		for j := 0; j < n; j++ {
			a := -h + float64(j)*2*h/float64(n-1)
			p0, p1 := [2]float64{cx - h, cy + a}, [2]float64{cx + h, cy + a}
			if i%2 == 1 {
				p0, p1 = [2]float64{cx + a, cy - h}, [2]float64{cx + a, cy + h}
			}
			if j%2 == 1 {
				p0, p1 = p1, p0
			}
			if j == 0 {
				to(p0[0], p0[1])
			} else {
				line(p0[0], p0[1], speed, flow)
			}
			line(p1[0], p1[1], speed, flow)
		}
		w("G1 E-%.3f F1800", retract)
	}
	w("M107")
	w("%s", findOffsetCmd(m))
	w("G1 E%.3f F1800", retract)
	printEnd(&b, m)
	return b.String()
}

// probePadLayers is the layer height and the count of layers of the probe offset pad.
func probePadLayers(m Machine) (float64, int) {
	nozzle := m.NozzleDiameter
	if nozzle <= 0 {
		nozzle = 0.4
	}
	layer := math.Round(nozzle*0.5/0.05) * 0.05
	return layer, int(math.Round(ProbePadHeight / layer))
}

// findOffsetCmd is the PAD_FIND_OFFSET command for the pad of ProbePad.
func findOffsetCmd(m Machine) string {
	cx, cy := towerCenter(m)
	layer, layers := probePadLayers(m)
	return fmt.Sprintf("PAD_FIND_OFFSET X=%.3f Y=%.3f SIDE=%g HEIGHT=%.3f", cx, cy, ProbePadSide, layer*float64(layers))
}

// FindProbeOffsetOnPad homes, and finds the offsets on a pad of ProbePad that is still on the
// bed.
func (k *Klippy) FindProbeOffsetOnPad() error {
	return k.script("G28", findOffsetCmd(k.Machine()))
}

// PrintProbePad prints the probe offset pad, and finds the offsets on it.
func (k *Klippy) PrintProbePad(tool, bed int, material string) error {
	m := k.Machine()
	if !m.Has("printdeck_pad") {
		return fmt.Errorf("printer.cfg has no [printdeck_pad]")
	}
	return k.printTest(ProbePadFile, ProbePad(m, tool, bed, material))
}

// FoundProbeOffset reads x_offset and y_offset of the last PAD_FIND_OFFSET. ok is false with no
// result.
func (k *Klippy) FoundProbeOffset() (x, y float64, ok bool, err error) {
	st, err := k.Query(map[string][]string{"printdeck_pad": {"found_x_offset", "found_y_offset"}})
	if err != nil {
		return 0, 0, false, err
	}
	s := st["printdeck_pad"]
	_, ok = s["found_x_offset"].(float64)
	return num(s["found_x_offset"]), num(s["found_y_offset"]), ok, nil
}

// SetProbeXYOffset writes x_offset and y_offset to the probe section of printer.cfg ([bltouch] or
// [probe]), and restarts Klipper to load them.
func (k *Klippy) SetProbeXYOffset(x, y float64) error {
	if k.ConfigPath == "" {
		return fmt.Errorf("no path of printer.cfg")
	}
	data, err := os.ReadFile(k.ConfigPath)
	if err != nil {
		return err
	}
	sec := "probe"
	if k.Machine().Has("bltouch") {
		sec = "bltouch"
	}
	cfg := setConfigOption(string(data), sec, "x_offset", fmt.Sprintf("%.2f", x))
	cfg = setConfigOption(cfg, sec, "y_offset", fmt.Sprintf("%.2f", y))
	if err := os.WriteFile(k.ConfigPath, []byte(cfg), 0o644); err != nil {
		return err
	}
	return k.script("RESTART")
}
