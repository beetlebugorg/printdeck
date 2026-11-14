package ui

import (
	"fmt"
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

// With a NOZZLE_WIPE macro, both cube flows heat, wipe the nozzle, and cool to calToolTemp before
// the touch.
func TestProbeCalCubeWipe(t *testing.T) {
	now := time.Unix(1800000000, 0)
	a, pr, _ := newPrintApp(idle)
	a.Now = func() time.Time { return now }
	m := calMachine()
	m.Modules = append(m.Modules, "printdeck_cube", "printdeck_backlash")
	m.Macros = append(m.Macros, "NOZZLE_WIPE")
	pr.Machine_ = m
	for _, kind := range []string{"cube", "backlash"} {
		if s := calSteps(kind, m); s[0] != calWipe {
			t.Errorf("%s: steps %v", kind, s)
		}
	}
	m.Macros = nil
	if s := calSteps("backlash", m); s[0] != calCubeBacklash {
		t.Errorf("no macro: steps %v", s)
	}
	a.startProbeCal("backlash", "Backlash")
	a.followProbeCal(pr.State_)
	w := calWipeTemp(a.loaded().Tool)
	waitCall(t, pr, fmt.Sprintf("action Backlash: Heating and wiping the nozzle: M104 S%d; G28; TEMPERATURE_WAIT SENSOR=extruder MINIMUM=%d; NOZZLE_WIPE COUNT=3; M104 S150; M106 S255; TEMPERATURE_WAIT SENSOR=extruder MAXIMUM=155; M107", w, w-2))
	// The end of the wipe starts the measure.
	now = now.Add(time.Minute)
	pr.State_.LastAction = printer.ActionResult{Name: "Backlash", End: now}
	pr.State_.Homed = "xyz"
	a.followProbeCal(pr.State_)
	a.followProbeCal(pr.State_)
	waitCall(t, pr, "action Backlash: Home, then touch the cube: CUBE_MEASURE")
	if calWipeTemp(210) != 170 || calWipeTemp(240) != 200 {
		t.Errorf("wipe temperatures %d %d", calWipeTemp(210), calWipeTemp(240))
	}
}
