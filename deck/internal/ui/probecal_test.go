package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"printdeck/internal/printer"
)

// calMachine is the TAZ with the BLTouch on the printed mount: a probe, z_tilt, the bed mesh, and
// the twist calibration along X and Y.
func calMachine() printer.Machine {
	return printer.Machine{Modules: []string{"probe", "z_tilt", "bed_mesh", "axis_twist_compensation"},
		HasProbe: true, ProbeOffset: [3]float64{68, -17, 3.6}, HasMesh: true,
		MeshMin: [2]float64{20, 15}, MeshMax: [2]float64{265, 265}, MinX: -50, MaxX: 293, MinY: -17, MaxY: 308,
		HasTwist: true, HasTwistY: true, TwistX: []float64{0.048, 0.030, 0.044, -0.034, -0.088},
		TwistY: []float64{-0.007, 0.001, 0.007}}
}

// waitCall waits for a call of the printer that has want in it: the deck sends some commands from
// a goroutine.
func waitCall(t *testing.T, pr *recPrinter, want string) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if slices.ContainsFunc(pr.Calls(), func(c string) bool { return strings.Contains(c, want) }) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no call with %q in %q", want, pr.Calls())
}

// Run all has every step; a calibration of one row has its step between a heat and the end.
func TestCalSteps(t *testing.T) {
	m := calMachine()
	all := calSteps("all", m)
	want := []calStep{calHeat, calZOffset, calSave, calHeat, calTwistX, calTwistY, calSave, calHeat, calMap, calCool}
	if !slices.Equal(all, want) {
		t.Errorf("all: %v, want %v", all, want)
	}
	m.HasTwistY = false
	if got := calSteps("twist", m); !slices.Equal(got, []calStep{calHeat, calTwistX, calSave, calCool}) {
		t.Errorf("twist with no Y: %v", got)
	}
	m.HasTwist = false
	if got := calSteps("all", m); slices.Contains(got, calTwistX) {
		t.Errorf("all with no twist section: %v", got)
	}
}

// The pin goes over the middle of the mesh area: the nozzle is there less the probe offset.
func TestCalCenter(t *testing.T) {
	x, y := calCenter(calMachine())
	if x != 142.5-68 || y != 140+17 {
		t.Errorf("nozzle at %v, %v", x, y)
	}
}

// The Z offset row heats and levels, starts the paper test with the pin over the middle, lowers
// the nozzle 4.5 mm, saves after Accept, and turns the heaters off at the end.
func TestProbeCalZOffset(t *testing.T) {
	now := time.Unix(1800000000, 0)
	st := idle
	st.Homed = ""
	a, pr, _ := newPrintApp(st)
	a.Now = func() time.Time { return now }
	pr.Machine_ = calMachine()
	a.startProbeCal("zoffset", "Z offset")

	a.followProbeCal(pr.State_)
	waitCall(t, pr, "action Z offset: Heating, homing and leveling: M140 S60; M104 S150; G28; TEMPERATURE_WAIT SENSOR=heater_bed MINIMUM=59; TEMPERATURE_WAIT SENSOR=extruder MINIMUM=148; Z_TILT_ADJUST")
	now = now.Add(time.Minute)
	pr.State_.Homed = "xyz"
	pr.State_.LastAction = printer.ActionResult{Name: "Z offset", End: now}
	a.followProbeCal(pr.State_)
	a.followProbeCal(pr.State_)
	waitCall(t, pr, "G1 X74.5 Y157.0 F6000; PROBE_CALIBRATE")

	now = now.Add(5 * time.Second)
	pr.State_.LastAction = printer.ActionResult{Name: "Z offset", End: now}
	pr.State_.ManualProbe, pr.State_.ManualZ = true, 8.6
	a.followProbeCal(pr.State_)
	waitCall(t, pr, "TESTZ Z=-4.500")
	a.followProbeCal(pr.State_) // the screen stays open: no second move
	n := 0
	for _, c := range pr.Calls() {
		if strings.Contains(c, "TESTZ") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d TESTZ calls", n)
	}

	// Accept: the result waits for SAVE_CONFIG.
	pr.State_.ManualProbe, pr.State_.SaveConfigPending = false, true
	a.followProbeCal(pr.State_)
	a.followProbeCal(pr.State_)
	waitCall(t, pr, "gcode SAVE_CONFIG")
	// Klipper restarts, and comes back with no unsaved result.
	pr.State_.SaveConfigPending = false
	now = now.Add(10 * time.Second)
	a.followProbeCal(pr.State_)
	a.followProbeCal(pr.State_)
	waitCall(t, pr, "gcode M104 S0; M140 S0")
	a.followProbeCal(pr.State_)
	if a.probeCal != nil || a.calMsg != "" {
		t.Errorf("calibration %+v, message %q", a.probeCal, a.calMsg)
	}
}

// The twist calibration lowers each point to Z 1 after a new z_offset, else to Z 3, and goes on
// to the next step after the last point.
func TestProbeCalTwist(t *testing.T) {
	for _, fresh := range []bool{false, true} {
		now := time.Unix(1800000000, 0)
		st := idle
		st.Homed = "xyz"
		a, pr, _ := newPrintApp(st)
		a.Now = func() time.Time { return now }
		pr.Machine_ = calMachine()
		a.startProbeCal("twist", "Twist")
		fl := a.probeCal
		fl.at, fl.fresh = 1, fresh // the step of the twist along X
		a.followProbeCal(pr.State_)
		waitCall(t, pr, "AXIS_TWIST_COMPENSATION_CALIBRATE AXIS=X SAMPLE_COUNT=5")
		want := "TESTZ Z=-5.000"
		if fresh {
			want = "TESTZ Z=-7.000"
		}
		for i := 0; i < calTwistXPoints; i++ {
			now = now.Add(20 * time.Second)
			pr.State_.ManualProbe, pr.State_.ManualZ = true, 8
			a.followProbeCal(pr.State_)
			now = now.Add(20 * time.Second)
			pr.State_.ManualProbe = false
			a.followProbeCal(pr.State_)
		}
		waitCall(t, pr, want)
		if fl.probes != calTwistXPoints || fl.steps[fl.at] != calTwistX {
			t.Fatalf("fresh %v: %d points, step %v", fresh, fl.probes, fl.steps[fl.at])
		}
		pr.State_.SaveConfigPending = true
		a.followProbeCal(pr.State_)
		if fl.steps[fl.at] != calTwistY {
			t.Errorf("fresh %v: step %v after the last point", fresh, fl.steps[fl.at])
		}
	}
}

// Abort on the paper screen stops the calibration, and the page tells why.
func TestProbeCalAbort(t *testing.T) {
	st := idle
	st.Homed = "xyz"
	a, pr, cv := newPrintApp(st)
	pr.Machine_ = calMachine()
	a.startProbeCal("zoffset", "Z offset")
	a.probeCal.at = 1
	a.followProbeCal(pr.State_)
	pr.State_.ManualProbe = true
	a.followProbeCal(pr.State_)
	tapLabel(t, a, cv, "Abort")
	if a.probeCal != nil || !strings.Contains(a.calMsg, "Abort") {
		t.Fatalf("calibration %+v, message %q", a.probeCal, a.calMsg)
	}
	waitCall(t, pr, "gcode ABORT")
	pr.State_.ManualProbe = false
	if a.topPage() != pageProbeCal {
		t.Errorf("page %v after the stop", a.topPage())
	}
	f := simFrame(t, a, "probecal-stopped")
	if !strings.Contains(strings.ToUpper(f), "STOPPED WITH ABORT") {
		t.Error("the page does not show why it stopped")
	}
}

// The Machine page has the Calibration key with a probe, and the page has a row for each step.
func TestProbeCalPage(t *testing.T) {
	for _, wide := range []bool{false, true} {
		a, pr, cv := newPrintApp(idle)
		pr.Machine_ = calMachine()
		a.Wide = wide
		a.pages = []page{pageMachine}
		tapLabel(t, a, cv, "Calibration")
		if a.topPage() != pageProbeCal {
			t.Fatalf("wide %v: page %v", wide, a.topPage())
		}
		name := "probecal"
		if wide {
			name += "-wide"
		}
		f := simFrame(t, a, name)
		for _, s := range []string{"Run all", "Z offset", "3.600 mm", "Twist", "X 0.14 mm, Y 0.01 mm", "Bed map"} {
			if !strings.Contains(strings.ToUpper(f), strings.ToUpper(s)) {
				t.Errorf("%s: no %q", name, s)
			}
		}
		tapLabel(t, a, cv, "Run all")
		if a.confirm == nil || !strings.Contains(a.confirm.question, "25 minutes") {
			t.Fatalf("%s: Run all asks %+v", name, a.confirm)
		}
	}
	// No probe: no key.
	a, pr, _ := newPrintApp(idle)
	pr.Machine_ = printer.Machine{Modules: []string{"bed_mesh"}}
	a.pages = []page{pageMachine}
	if f := simFrame(t, a, "machine-no-probe"); strings.Contains(strings.ToUpper(f), "CALIBRATION") {
		t.Error("Calibration key with no probe")
	}
}
