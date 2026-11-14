package ui

import (
	"strings"
	"testing"
	"time"
)

// The step screen shows the steps of a calibration while it runs, between two actions, and at
// its end until OK.
func TestCalStepScreen(t *testing.T) {
	now := time.Unix(1800000000, 0)
	a, pr, cv := newPrintApp(idle)
	a.Now = func() time.Time { return now }
	m := calMachine()
	m.Modules = append(m.Modules, "printdeck_cube", "printdeck_backlash")
	m.Macros = append(m.Macros, "NOZZLE_WIPE")
	pr.Machine_ = m
	a.pages = []page{pageMachine, pageProbeCal}
	a.startProbeCal("cube", "Z offset")
	a.probeCal.at, a.probeCal.sent = 1, true
	pr.State_.Action, pr.State_.ActionStart = "Z offset: Heating, homing and leveling", now.Add(-40*time.Second)
	pr.State_.Bed, pr.State_.BedTarget = 41, 60
	f := simFrame(t, a, "cal-steps-running")
	for _, want := range []string{"Wipe the nozzle", "Done", "Heat and level", "Bed 41° of 60°", "Cool down", "STOP"} {
		if !strings.Contains(f, want) {
			t.Errorf("running: no %q", want)
		}
	}
	// Between two actions the screen stays.
	pr.State_.Action = ""
	if f := simFrame(t, a, "cal-steps-between"); !strings.Contains(f, "Heat and level") {
		t.Error("the screen closed between two steps")
	}
	// The end: all steps done, until OK.
	fl := a.probeCal
	fl.at = len(fl.steps) - 1
	a.calNext(fl)
	if f := simFrame(t, a, "cal-steps-done"); strings.Count(f, "Done") < len(fl.steps) {
		t.Error("the steps are not done")
	}
	tapLabel(t, a, cv, "OK")
	if a.calEnded != nil || a.topPage() != pageProbeCal {
		t.Errorf("after OK: %v, page %v", a.calEnded, a.topPage())
	}
	// A failed step shows the reason.
	a.startProbeCal("backlash", "Backlash")
	a.probeCal.at = 1
	a.calFail(a.probeCal, "No contact on the top face of the cube.")
	if f := simFrame(t, a, "cal-steps-failed"); !strings.Contains(f, "No contact on the top face") || !strings.Contains(f, "Stopped") {
		t.Error("no reason")
	}
}
