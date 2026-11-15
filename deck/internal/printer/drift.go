package printer

import (
	"fmt"
	"strings"
)

// The drift test: how the bed moves after it gets to its temperature. The bed cools to HeatCold
// first. Then it heats to the test temperature, and the probe measures the place of
// [printdeck_settle] each minute for DriftMinutes (BED_SETTLE SAMPLE=1).
const (
	DriftFile     = "printdeck-bed-drift.gcode"
	DriftMinutes  = 20
	DriftInterval = 60 // s
)

// driftLines are the lines of the drift test at bed °C, with the nozzle at probe °C.
func driftLines(bed, probe int) []string {
	lines := []string{"G28", "M104 S0", "M140 S0",
		fmt.Sprintf("TEMPERATURE_WAIT SENSOR=heater_bed MAXIMUM=%d", HeatCold),
		fmt.Sprintf("M140 S%d", bed), fmt.Sprintf("M104 S%d", probe),
		fmt.Sprintf("TEMPERATURE_WAIT SENSOR=heater_bed MINIMUM=%d", bed-1),
		"BED_SETTLE SAMPLE=1 RESET=1"}
	for i := 0; i < DriftMinutes*60/DriftInterval; i++ {
		lines = append(lines, fmt.Sprintf("G4 P%d", DriftInterval*1000), "BED_SETTLE SAMPLE=1")
	}
	return append(lines, "M140 S0", "M104 S0")
}

// DriftGcode is the G-code file of the drift test.
func DriftGcode(bed, probe int) string {
	var b strings.Builder
	b.WriteString(";FLAVOR:Klipper\n;Generated with Printdeck: the drift test of the bed\n")
	for _, l := range driftLines(bed, probe) {
		b.WriteString(l + "\n")
	}
	return b.String()
}

// DriftTest prints the drift test at bed °C, with the nozzle at 150 °C as for the probe at the
// start of a print.
func (k *Klippy) DriftTest(bed int) error {
	if !k.Machine().Has("printdeck_settle") {
		return fmt.Errorf("printer.cfg has no [printdeck_settle]")
	}
	return k.printTest(DriftFile, DriftGcode(bed, 150))
}

// DriftReadings are the readings of the last drift test or BED_SETTLE: [t, z] with t in seconds.
func (k *Klippy) DriftReadings() ([][2]float64, error) {
	st, err := k.Query(map[string][]string{"printdeck_settle": {"readings"}})
	if err != nil {
		return nil, err
	}
	var out [][2]float64
	rs, _ := st["printdeck_settle"]["readings"].([]any)
	for _, r := range rs {
		if p, ok := r.([]any); ok && len(p) == 2 {
			out = append(out, [2]float64{num(p[0]), num(p[1])})
		}
	}
	return out, nil
}

// DriftSummary is the text of the readings of a drift test: the movement from the first probe at
// some times, and the time to 90 % of the movement.
func DriftSummary(rs [][2]float64) string {
	if len(rs) < 2 {
		return "No results. Run the drift test first."
	}
	z0, last := rs[0][1], rs[len(rs)-1]
	total := last[1] - z0
	var parts []string
	for _, m := range []float64{2, 5, 10, 15, 20} {
		for _, r := range rs {
			if r[0] >= m*60-5 && r[0] <= last[0] {
				parts = append(parts, fmt.Sprintf("%.0f min %+.3f", m, r[1]-z0))
				break
			}
		}
	}
	t90 := last[0]
	for i := len(rs) - 1; i >= 0; i-- {
		d := rs[i][1] - z0
		if (total >= 0 && d < 0.9*total) || (total < 0 && d > 0.9*total) {
			break
		}
		t90 = rs[i][0]
	}
	return fmt.Sprintf("%d probes over %.0f min after the bed got to its temperature. The bed moved %+.3f mm: %s. "+
		"90 %% of the movement after %.0f min.", len(rs), last[0]/60, total, strings.Join(parts, ", "), t90/60)
}
