package printer

import (
	"math"
	"strings"
	"testing"
)

func TestDriftGcode(t *testing.T) {
	g := DriftGcode(60, 150)
	for _, want := range []string{"G28\n", "TEMPERATURE_WAIT SENSOR=heater_bed MAXIMUM=30\n", "M140 S60\n",
		"TEMPERATURE_WAIT SENSOR=heater_bed MINIMUM=59\n", "BED_SETTLE SAMPLE=1 RESET=1\n", "G4 P60000\n", "M140 S0\n"} {
		if !strings.Contains(g, want) {
			t.Errorf("no %q", want)
		}
	}
	if n := strings.Count(g, "BED_SETTLE SAMPLE=1"); n != DriftMinutes+1 {
		t.Errorf("%d probes", n)
	}
}

func TestDriftSummary(t *testing.T) {
	var rs [][2]float64
	for s := 0.0; s <= 1200; s += 60 {
		rs = append(rs, [2]float64{s, 0.1 * (1 - math.Exp(-s/300))})
	}
	got := DriftSummary(rs)
	for _, want := range []string{"21 probes over 20 min", "moved +0.098 mm", "2 min +0.033", "20 min +0.098", "after 11 min"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in %q", want, got)
		}
	}
}
