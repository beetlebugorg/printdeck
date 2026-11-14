package ui

import (
	"strings"
	"testing"
	"time"

	"printdeck/internal/printer"
)

// waitConfirm runs the posted functions of the App until it shows a question.
func waitConfirm(t *testing.T, a *App) {
	t.Helper()
	for i := 0; i < 200 && a.confirm == nil; i++ {
		select {
		case f := <-a.posted:
			f()
		case <-time.After(10 * time.Millisecond):
		}
	}
	if a.confirm == nil {
		t.Fatal("no question")
	}
}

// The cube row of the Z offset heats and levels, runs CUBE_PROBE_OFFSET as an action, asks to
// save the result, then saves and turns the heaters off.
func TestProbeCalCube(t *testing.T) {
	now := time.Unix(1800000000, 0)
	a, pr, _ := newPrintApp(idle)
	a.Now = func() time.Time { return now }
	m := calMachine()
	m.Modules = append(m.Modules, "printdeck_cube")
	pr.Machine_ = m
	a.startProbeCal("cube", "Z offset")

	a.followProbeCal(pr.State_)
	waitCall(t, pr, "action Z offset: Heating, homing and leveling")
	now = now.Add(time.Minute)
	pr.State_.LastAction = printer.ActionResult{Name: "Z offset", End: now}
	a.followProbeCal(pr.State_)
	a.followProbeCal(pr.State_)
	waitCall(t, pr, "action Z offset: Nozzle and probe on the cube: CUBE_PROBE_OFFSET")

	now = now.Add(time.Minute)
	pr.State_.LastAction = printer.ActionResult{Name: "Z offset", End: now}
	a.followProbeCal(pr.State_)
	waitConfirm(t, a)
	if q := a.confirm.question; !strings.Contains(q, "z_offset 3.142 mm (now 3.600, -0.458)") {
		t.Fatalf("question %q", q)
	}
	a.followProbeCal(pr.State_) // the question stays: no second action
	a.confirm.choices[0].tap()  // Save
	waitCall(t, pr, "gcode CUBE_SAVE_PROBE_OFFSET")
	for i := 0; i < 100 && a.probeCal.steps[a.probeCal.at] != calSave; i++ {
		select {
		case f := <-a.posted:
			f()
		case <-time.After(10 * time.Millisecond):
		}
	}
	pr.State_.SaveConfigPending = true
	a.followProbeCal(pr.State_)
	waitCall(t, pr, "gcode SAVE_CONFIG")
	pr.State_.SaveConfigPending = false
	now = now.Add(10 * time.Second)
	a.followProbeCal(pr.State_)
	a.followProbeCal(pr.State_)
	waitCall(t, pr, "gcode M104 S0; M140 S0")
}

// The LCD shows a deck action: its name, the time, and the step.
func TestStatusLinesAction(t *testing.T) {
	now := time.Unix(1800000000, 0)
	st := idle
	st.Action, st.ActionStart = "Z offset: Nozzle and probe on the cube", now.Add(-83*time.Second)
	l1, l2 := StatusLines(st, "PLA", now, true)
	if l1 != "Z OFFSET    1:23" || l2 != "NOZZLE AND PROBE" {
		t.Errorf("%q %q", l1, l2)
	}
}
