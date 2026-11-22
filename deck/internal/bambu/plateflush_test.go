package bambu

import (
	"math"
	"strings"
	"testing"
)

const flushHeader = `; filament_diameter: 1.75,1.75,1.75
; filament_diameter = 1.75,1.75
; filament_type = PLA;PLA;PETG
; filament_map = 1,1,2
; flush_multiplier = 1,1
`

// change is one filament change of a G-code file, with a flush block of the two lengths.
func change(n, from, to int, a, b string) string {
	return "; filament change " + itoa(n) + ": filament " + itoa(from) + " to filament " + itoa(to) + "\n" +
		";VG1 E4 F200\nT" + itoa(to-1) + "\n; VFLUSH_START\n;VG1 E" + a + " F100\n;VG1 E" + b + " F100\nSYNC T5\n; VFLUSH_END\nG1 X1 E1\n"
}

func itoa(n int) string { return string(rune('0' + n)) }

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// The flush of a pair is the median of the block sums of its changes, in mm³. The deretract
// before the block is not in the sum.
func TestFlushFromBlocks(t *testing.T) {
	g := flushHeader + "T0\n" + change(1, 1, 3, "40", "60") + change(2, 3, 1, "10", "10") +
		change(3, 1, 3, "40", "62") + change(4, 3, 1, "10", "10") + change(5, 1, 3, "40", "70")
	u, err := CountUse(strings.NewReader(g))
	if err != nil {
		t.Fatal(err)
	}
	area := math.Pi * 1.75 * 1.75 / 4
	if got := u.Flush[[2]int{1, 3}]; !near(got, 102*area) {
		t.Errorf("1 to 3: %v, want %v", got, 102*area)
	}
	if got := u.Flush[[2]int{3, 1}]; !near(got, 20*area) {
		t.Errorf("3 to 1: %v, want %v", got, 20*area)
	}
	// The comment lines add no filament: 1 mm after each change.
	if u.Total[0] != 2 || u.Total[2] != 3 {
		t.Errorf("Total = %v", u.Total)
	}
}

// A matrix with 8 values for 5 filaments, as in real X2D files, gives no value.
func TestFlushMatrixThatDoesNotFit(t *testing.T) {
	g := "; filament_type = PLA;PLA;PLA;PLA;PLA\n; filament_map = 1,1,1,1,2\n; flush_volumes_matrix = 0,270,120,0,0,270,120,0\nT0\nG1 E1\n"
	u, err := CountUse(strings.NewReader(g))
	if err != nil {
		t.Fatal(err)
	}
	if u.Flush != nil {
		t.Errorf("Flush = %v", u.Flush)
	}
}

// A matrix with one table for each nozzle gives the value of the nozzle of the new filament, with
// its multiplier. A block of the pair has priority.
func TestFlushMatrixFallback(t *testing.T) {
	g := "; filament_diameter: 1.75,1.75\n; filament_type = PLA;PETG\n; filament_map = 1,2\n; flush_multiplier = 1,1.5\n" +
		"; flush_volumes_matrix = 0,100,200,0,0,300,400,0\nT0\nG1 E1\n"
	u, err := CountUse(strings.NewReader(g))
	if err != nil {
		t.Fatal(err)
	}
	// 1 to 2 goes to nozzle 2: 300 × 1.5. 2 to 1 goes to nozzle 1: 200.
	if !near(u.Flush[[2]int{1, 2}], 450) || !near(u.Flush[[2]int{2, 1}], 200) {
		t.Errorf("Flush = %v", u.Flush)
	}
	u, err = CountUse(strings.NewReader(g + change(1, 1, 2, "10", "10")))
	if err != nil {
		t.Fatal(err)
	}
	if want := 20 * math.Pi * 1.75 * 1.75 / 4; !near(u.Flush[[2]int{1, 2}], want) {
		t.Errorf("with a block: %v, want %v", u.Flush[[2]int{1, 2}], want)
	}
}

// A file with no flush gives none, and a block with no diameter gives none.
func TestFlushNone(t *testing.T) {
	for _, g := range []string{"T0\nG1 E1\n", change(1, 1, 2, "10", "10")} {
		u, err := CountUse(strings.NewReader(g))
		if err != nil {
			t.Fatal(err)
		}
		if u.Flush != nil {
			t.Errorf("Flush = %v", u.Flush)
		}
	}
}
