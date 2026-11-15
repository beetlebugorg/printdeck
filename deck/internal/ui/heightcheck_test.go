package ui

import (
	"strings"
	"testing"
)

func TestPadSummary(t *testing.T) {
	if s := padSummary(nil); !strings.HasPrefix(s, "No results") {
		t.Errorf("no checks: %q", s)
	}
	// Measure only: the print has the error of the probe.
	s := padSummary([][3]float64{{0.25, 0.08, 0}, {0.5, 0.06, 0}, {0.75, 0.07, 0}})
	for _, want := range []string{"3 checks to Z 0.8 mm", "Probe error: mean +0.070 mm, from +0.060 to +0.080",
		"Height of the print: mean +0.070 mm", "last +0.070"} {
		if !strings.Contains(s, want) {
			t.Errorf("no %q in %q", want, s)
		}
	}
	// With the correction: the height of the print is the error plus the correction.
	s = padSummary([][3]float64{{0.25, 0.08, 0}, {0.5, 0.08, -0.04}, {0.75, 0.08, -0.08}})
	if !strings.Contains(s, "last +0.000. Last correction -0.080 mm") {
		t.Errorf("corrected: %q", s)
	}
}
