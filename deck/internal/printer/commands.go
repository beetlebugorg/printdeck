package printer

import "fmt"

// The G-code of the deck's controls. All backends send the same lines.

// HomeGcode homes the axes ("x", "y", "z"); none means all.
func HomeGcode(axes ...string) string {
	g := "G28"
	for _, a := range axes {
		g += " " + upper(a)
	}
	return g
}

// JogGcode moves the head relative to where it is, in mm, at speed mm/min, and goes back to absolute
// moves. It returns nil when there is nothing to move.
func JogGcode(x, y, z float64, speed int) []string {
	move := "G1"
	for _, ax := range []struct {
		name string
		mm   float64
	}{{"X", x}, {"Y", y}, {"Z", z}} {
		if ax.mm != 0 {
			move += fmt.Sprintf(" %s%.3f", ax.name, ax.mm)
		}
	}
	if move == "G1" {
		return nil
	}
	return []string{"G91", fmt.Sprintf("%s F%d", move, speed), "G90"}
}

// ExtrudeGcode pushes filament (or pulls it back when mm is negative) at speed mm/min. The extruder is
// put back into absolute mode afterwards, as the slicer expects.
func ExtrudeGcode(mm float64, speed int) []string {
	return []string{"M83", fmt.Sprintf("G1 E%.2f F%d", mm, speed), "M82"}
}

// FanGcode sets the part cooling fan, in percent.
func FanGcode(percent int) string {
	if percent <= 0 {
		return "M107"
	}
	return fmt.Sprintf("M106 S%d", min(255, percent*255/100))
}

// SpeedGcode and FlowGcode change a running print, in percent (100 is as sliced).
func SpeedGcode(p int) string { return fmt.Sprintf("M220 S%d", p) }
func FlowGcode(p int) string  { return fmt.Sprintf("M221 S%d", p) }

// BabyStepGcode moves the nozzle up (positive) or down during a print, in mm. Klipper keeps it as a
// G-code offset.
func BabyStepGcode(mm float64) string {
	return fmt.Sprintf("SET_GCODE_OFFSET Z_ADJUST=%.3f MOVE=1", mm)
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b)
}
