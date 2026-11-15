package ui

import (
	"fmt"
	"math"
)

// The Height check row of the Calibration page: a test tower with a small pad next to it. After
// each layer the probe measures the pad ([printdeck_pad]). Print it once to measure only, and
// once with the correction, then compare the results.

// askHeightCheck shows the choices of the Height check row.
func (a *App) askHeightCheck() {
	pr := a.Printer
	m := a.loaded()
	start := func(correct bool) func() {
		return func() { a.do(func() error { return pr.PrintPadTower(m.Tool, m.Bed, m.Name, correct) }) }
	}
	a.confirm = &confirmBox{question: "Height check: a 20 mm tower and a pad. The probe taps the pad after each layer.",
		choices: []menuItem{
			{"Print, measure only", fmt.Sprintf("At %d° · about 40 minutes · the Z offset stays", m.Tool), dim, start(false)},
			{"Print, measure and correct", "The deck corrects the Z offset after each layer", dim, start(true)},
			{"Last results", "The error of each check of the last print", dim, a.showPadChecks},
		}}
}

// showPadChecks shows the results of the last height check.
func (a *App) showPadChecks() {
	pr := a.Printer
	a.do(func() error {
		checks, err := pr.PadChecks()
		if err != nil {
			return err
		}
		a.post(func() { a.confirm = &confirmBox{question: padSummary(checks), choices: []menuItem{}} })
		return nil
	})
}

// padSummary is the text of the results: the error that the probe reads, and the error of the
// height of the print (the error plus the correction of the layer).
func padSummary(checks [][3]float64) string {
	if len(checks) == 0 {
		return "No results. Print the height check first."
	}
	stats := func(v func(c [3]float64) float64) (mean, lo, hi float64) {
		lo, hi = math.Inf(1), math.Inf(-1)
		for _, c := range checks {
			x := v(c)
			mean += x / float64(len(checks))
			lo, hi = math.Min(lo, x), math.Max(hi, x)
		}
		return
	}
	em, elo, ehi := stats(func(c [3]float64) float64 { return c[1] })
	pm, plo, phi := stats(func(c [3]float64) float64 { return c[1] + c[2] })
	last := checks[len(checks)-1]
	return fmt.Sprintf("%d checks to Z %.1f mm. Probe error: mean %+.3f mm, from %+.3f to %+.3f. "+
		"Height of the print: mean %+.3f mm, from %+.3f to %+.3f, last %+.3f. Last correction %+.3f mm.",
		len(checks), last[0], em, elo, ehi, pm, plo, phi, last[1]+last[2], last[2])
}
