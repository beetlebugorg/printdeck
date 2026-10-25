package printer_test

import (
	"bytes"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"printdeck/internal/klipper/batch"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
	"printdeck/internal/klipper/simmcu"
	"printdeck/internal/printer"

	_ "printdeck/internal/klipper/extras/all"
)

// simDict is the data dictionary of the simulated board.
const simDict = "../klipper/simmcu/klipper.dict"

// simConfigFile writes the config of a simulated printer as the deck writes it, with the
// Printdeck sections of the image, and returns its path.
func simConfigFile(t *testing.T, id string) string {
	t.Helper()
	macros, err := filepath.Abs("../../../os/board/rootfs/usr/share/printdeck/klipper/printdeck.cfg")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "printer.cfg")
	if err := os.WriteFile(path, []byte("[include "+macros+"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := printer.UseSim(path, id); err != nil {
		t.Fatal(err)
	}
	return path
}

// homeGcode homes the printer and moves it.
func homeGcode(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "home.gcode")
	if err := os.WriteFile(path, []byte("G28\nG1 X50 Y50 Z10 F3000\nG4 P2000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// Each simulated printer loads in the Klipper host with the dictionary of the simulated board,
// and homes (batch mode, with no board).
func TestSimPrintersLoad(t *testing.T) {
	gcode := homeGcode(t)
	for _, m := range printer.SimModels() {
		cfg := simConfigFile(t, m.ID)
		result, msg := batch.Run(cfg, simDict, gcode, filepath.Join(t.TempDir(), "out.serial"))
		if result != "exit" {
			t.Errorf("%s: run result %q: %s", m.ID, result, msg)
		}
	}
}

// The simulated LulzBot printers run the start and the end G-code of the LulzBot Cura profile:
// PRINT_START heats, wipes, probes and primes, and PRINT_END presents the print (batch mode, with
// no board).
func TestSimLulzBotPrintStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "print.gcode")
	gcode := "PRINT_START BED=60 EXTRUDER=220 WIPE=170 LAYER=0.3 SPEED=20\n" +
		"G1 X60 Y60 Z0.3 F3000\nG1 X80 E1 F1200\nPRINT_END\n"
	if err := os.WriteFile(path, []byte(gcode), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"taz-workhorse", "mini2"} {
		var out bytes.Buffer
		log.SetOutput(&out)
		// With no board, the probe triggers at once, and the tilt puts the prime strip under the bed.
		// The probe of the washers is in TestSimPrintersHome.
		cfg := simConfigFile(t, id)
		f, err := os.OpenFile(cfg, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		f.WriteString("\n[gcode_macro BED_TILT_CALIBRATE]\nrename_existing: BED_TILT_CALIBRATE_BOARD\ngcode:\n    M400\n")
		f.Close()
		result, msg := batch.Run(cfg, simDict, path, filepath.Join(t.TempDir(), "out.serial"))
		log.SetOutput(os.Stderr)
		if result != "exit" {
			t.Errorf("%s: run result %q: %s", id, result, msg)
		}
		for _, bad := range []string{"Unknown command", "Error", "error", "out of range", "below minimum"} {
			if strings.Contains(out.String(), bad) {
				t.Errorf("%s: the log has %q:\n%s", id, bad, out.String())
				break
			}
		}
	}
}

// Each simulated printer homes on the simulated board, in real time: the endstops trigger at
// their places, and the TAZ Workhorse probes its washers. Then the nozzle of the board is at the
// place of the last move. The test runs for about 90 seconds, so it runs only with
// PRINTDECK_SIM_HOME=1.
func TestSimPrintersHome(t *testing.T) {
	if os.Getenv("PRINTDECK_SIM_HOME") == "" {
		t.Skip("homes in real time: set PRINTDECK_SIM_HOME=1")
	}
	log.SetOutput(io.Discard)
	defer log.SetOutput(os.Stderr)
	gcode := homeGcode(t)
	for _, m := range printer.SimModels() {
		t.Run(m.ID, func(t *testing.T) {
			cfg := simConfigFile(t, m.ID)
			f, err := os.Open(gcode)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			start := time.Now()
			done := make(chan [2]string, 1)
			go func() {
				p := host.New(reactor.New(), host.StartArgs{"config_file": cfg, "start_reason": "startup",
					"debuginput": gcode, "gcode_fd": f})
				result := p.Run()
				msg, _ := p.StateMessage()
				done <- [2]string{result, msg}
			}()
			// The place of the nozzle on the board: the last one before the end of the run.
			var place [3]float64
			var placed bool
			var r [2]string
		wait:
			for {
				select {
				case r = <-done:
					break wait
				case <-time.After(100 * time.Millisecond):
					if p, ok := simmcu.Place(); ok {
						place, placed = p, true
					}
				}
				if time.Since(start) > 4*time.Minute {
					t.Fatal("no end after 4 minutes")
				}
			}
			if r[0] != "exit" {
				t.Errorf("run result %q: %s", r[0], r[1])
			}
			t.Logf("homed in %v", time.Since(start).Round(time.Second))
			want := [3]float64{50, 50, 10}
			for i := range want {
				if !placed || math.Abs(place[i]-want[i]) > 0.1 {
					t.Errorf("the nozzle of the board is at %.2f, want %v", place, want)
					break
				}
			}
		})
	}
}
