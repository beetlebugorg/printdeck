package ui

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

func (r *recPrinter) UseSim(id string) error { return r.rec("sim " + id) }

// simFrame draws a frame and returns its recording. With PRINTDECK_SIM_SHOTS set to a folder, it
// also writes the frame there as name.png.
func simFrame(t *testing.T, a *App, name string) string {
	t.Helper()
	c, rec := gfx.NewRecording(a.Screen().W, a.Screen().H)
	a.Draw(c)
	if dir := os.Getenv("PRINTDECK_SIM_SHOTS"); dir != "" {
		img := gfx.New(a.Screen().W, a.Screen().H)
		a.Draw(img)
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		png.Encode(f, img.RGBA)
		f.Close()
	}
	return string(rec.JSON())
}

// Each screen shows the band of the simulated printer while one is in use, and no band with the
// real printer.
func TestSimBand(t *testing.T) {
	printing := printer.State{Reachable: true, Text: "Printing", Connected: true, Printing: true,
		File: "benchy.gcode", Layer: 12, Layers: 27, Tool: 210, ToolTarget: 210, Bed: 60, BedTarget: 60}
	broken := printer.State{Err: "Klipper is not ready", ErrDetail: "mcu 'mcu': Unable to connect"}
	for _, wide := range []bool{false, true} {
		for _, tc := range []struct {
			name  string
			st    printer.State
			pages []page
		}{
			{"idle", idle, nil},
			{"printing", printing, nil},
			{"machine", idle, []page{pageMachine}},
			{"simulator", idle, []page{pageMachine, pageSimulator}},
			{"error", broken, nil},
		} {
			for _, sim := range []string{"", "Creality CR-10 V3"} {
				st := tc.st
				st.Simulated = sim
				a := New()
				a.Printer = &DemoPrinter{View: -1, State_: st}
				a.Wide = wide
				a.pages = tc.pages
				name := tc.name
				if wide {
					name += "-wide"
				}
				if sim != "" {
					name += "-sim"
				}
				f := simFrame(t, a, name)
				has := strings.Contains(f, "SIMULATED PRINTER ·")
				if has != (sim != "") {
					t.Errorf("%s: band %v, want %v", name, has, sim != "")
				}
				if sim != "" && !strings.Contains(f, "CREALITY CR-10 V3") {
					t.Errorf("%s: no name of the simulated printer in the band", name)
				}
			}
		}
	}
}

// The Machine page opens the simulated printers. A tap on a model switches to it after a
// question, and Real printer switches back.
func TestSimulatorSwitch(t *testing.T) {
	a, pr, cv := newPrintApp(idle)
	a.pages = []page{pageMachine}
	tapLabel(t, a, cv, "Simulated printer")
	if a.topPage() != pageSimulator {
		t.Fatalf("page %v, want the simulated printers", a.topPage())
	}
	tapLabel(t, a, cv, "Simulated Creality CR-10 V3")
	if a.confirm == nil {
		t.Fatal("no confirmation")
	}
	tapLabel(t, a, cv, "Simulate")
	if c := pr.wait(1); len(c) != 1 || c[0] != "sim cr10-v3" {
		t.Errorf("calls %v", c)
	}

	pr.State_.Simulated = "Creality CR-10 V3"
	tapLabel(t, a, cv, "Real printer")
	tapLabel(t, a, cv, "Go back")
	if c := pr.wait(2); len(c) != 2 || c[1] != "sim " {
		t.Errorf("calls %v", c)
	}
}

// During a print, the rows of the simulated printers wait for its end.
func TestSimulatorWaitsForPrint(t *testing.T) {
	st := idle
	st.Printing = true
	a, pr, cv := newPrintApp(st)
	a.pages = []page{pageMachine, pageSimulator}
	a.Draw(cv)
	if r, ok := a.KeyRect("Simulated Creality CR-10 V3"); ok {
		p := r.Min.Add(r.Size().Div(2))
		tapAt(a, cv, p.X, p.Y)
	}
	if a.confirm != nil || len(pr.Calls()) != 0 {
		t.Errorf("a switch during a print: confirm %v, calls %v", a.confirm != nil, pr.Calls())
	}
}

// The setup of a new printer has the key of the simulated printers.
func TestSetupOffersSimulator(t *testing.T) {
	for _, wide := range []bool{false, true} {
		a := New()
		a.Wide = wide
		a.Printer = &DemoPrinter{View: -1}
		a.OpenPrintScreen("setup")
		name := "setup"
		if wide {
			name += "-wide"
		}
		simFrame(t, a, name)
		if _, ok := a.KeyRect("Simulated printer"); !ok {
			t.Errorf("wide %v: no Simulated printer key", wide)
		}
	}
}

// The Klipper error screen of the real printer has the key of the simulated printers, also after
// a shutdown of the board.
func TestKlipperErrorOffersSimulator(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for name, detail := range map[string]string{
			"klipper-error":    "mcu 'mcu': Unable to connect",
			"klipper-shutdown": "MCU 'mcu' shutdown: ADC out of range",
		} {
			a := New()
			a.Wide = wide
			a.Printer = &DemoPrinter{View: -1, State_: printer.State{Err: "Klipper is not ready", ErrDetail: detail}}
			if wide {
				name += "-wide"
			}
			simFrame(t, a, name)
			tapLabel(t, a, gfx.New(a.Screen().W, a.Screen().H), "Simulated printer")
			if a.topPage() != pageSimulator {
				t.Errorf("%s: the key opens page %v, want the simulated printers", name, a.topPage())
			}
		}
	}
}
