package printer

import (
	"fmt"
	"strings"
)

// The shake test: the head shakes along X, then the bed along Y, at one frequency after the other,
// as TEST_RESONANCES of Klipper. A phone records the sound, or the acceleration on the bed. At a
// resonance the printer gets louder: the loudest step of an axis gives its frequency.
//
// The steps follow a fixed time table, so that the analysis finds each step in the recording:
//
//	ShakeLead   quiet, after the home: start the recording now
//	X steps     ShakeStart to ShakeEnd Hz, ShakeHold seconds each
//	ShakeGap    quiet
//	Y steps     the same
//
// Each move goes out or back in half a period: the speed rises from 0 and falls to 0 again. The
// acceleration is ShakeAccelPerHz × the frequency, as accel_per_hz of Klipper, so the top speed
// stays at ShakeAccelPerHz / 4 mm/s at each frequency. The input shaper is off during the test.
const (
	ShakeFile       = "printdeck-shake-test.gcode"
	ShakeStart      = 16.0   // Hz, the first step
	ShakeEnd        = 90.0   // Hz, the last step
	ShakeStep       = 2.0    // Hz more for each step
	ShakeHold       = 2.0    // s at each step
	ShakeLead       = 5.0    // s quiet before the X steps
	ShakeGap        = 3.0    // s quiet between X and Y
	ShakeAccelPerHz = 60.0   // mm/s² for each Hz: 5400 mm/s² at 90 Hz
	shakeZ          = 20.0   // mm, the nozzle over the bed
	shakeFeed       = 1800.0 // mm/min: over the top speed (15 mm/s), so the moves have no cruise
)

// ShakeFreqs are the frequencies of the steps, in order.
func ShakeFreqs() []float64 {
	var out []float64
	for f := ShakeStart; f <= ShakeEnd+1e-9; f += ShakeStep {
		out = append(out, f)
	}
	return out
}

// ShakeTest is the G-code of the shake test for the machine m: home, go to the middle of the bed,
// wait ShakeLead seconds, shake X, wait ShakeGap seconds, shake Y. At the end it sets the
// acceleration and the input shaper of printer.cfg again.
func ShakeTest(m Machine) string {
	cx, cy := towerCenter(m)
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	w(";FLAVOR:Klipper")
	w(";Generated with Printdeck: the shake test")
	w(";%.0f s quiet, X %.0f to %.0f Hz in %.0f Hz steps of %.0f s, %.0f s quiet, Y the same.",
		ShakeLead, ShakeStart, ShakeEnd, ShakeStep, ShakeHold, ShakeGap)
	w("M107")
	w("G90")
	w("G28")
	w("G1 Z%.3f F600", shakeZ)
	w("G1 X%.3f Y%.3f F%.0f", cx, cy, ringTravel*60)
	if m.HasShaper {
		w("SET_INPUT_SHAPER SHAPER_FREQ_X=0 SHAPER_FREQ_Y=0")
	}
	w("M400")
	w("M117 Shake test: start the recording")
	w("G4 P%.0f", ShakeLead*1000)
	for _, axis := range []string{"X", "Y"} {
		c := cx
		if axis == "Y" {
			c = cy
			w("M400")
			w("M117 Shake test: Y next")
			w("G4 P%.0f", ShakeGap*1000)
		}
		for _, f := range ShakeFreqs() {
			a := ShakeAccelPerHz * f
			// Half a period for each move, with the speed up for half of it and down again.
			d := a / (16 * f * f)
			w("M117 Shake %s %.0f Hz", axis, f)
			w("SET_VELOCITY_LIMIT ACCEL=%g MINIMUM_CRUISE_RATIO=0", a)
			n := int(ShakeHold*2*f + 0.5)
			n += n % 2 // an even count ends at the middle
			for k := 0; k < n; k++ {
				p := c
				if k%2 == 0 {
					p = c + d
				}
				w("G1 %s%.4f F%.0f", axis, p, shakeFeed)
			}
		}
	}
	w("M400")
	w("M117 Shake test: done")
	if m.HasShaper {
		w("SET_INPUT_SHAPER SHAPER_TYPE=%s SHAPER_FREQ_X=%g SHAPER_FREQ_Y=%g", shaperType(m), m.ShaperFreqX, m.ShaperFreqY)
	}
	if m.MaxAccel > 0 {
		w("SET_VELOCITY_LIMIT ACCEL=%g MINIMUM_CRUISE_RATIO=%g", m.MaxAccel, m.MinCruiseRatio)
	}
	return b.String()
}

// PrintShakeTest runs the shake test.
func (k *Klippy) PrintShakeTest() error {
	return k.printTest(ShakeFile, ShakeTest(k.Machine()))
}
