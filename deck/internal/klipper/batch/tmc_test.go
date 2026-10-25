package batch

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"printdeck/internal/printer"
)

// The registers of a TMC2130 of the LulzBot Mini 2 example config, after SET_TMC_CURRENT, are
// the registers of the real Klipper host: its DUMP_TMC lines for the same commands.
func TestTMC2130Registers(t *testing.T) {
	text, err := printer.ExampleConfig("printer-lulzbot-mini2-2018.cfg", "")
	if err != nil {
		t.Fatal(err)
	}
	text, _, _ = strings.Cut(text, "\n# Added by Printdeck")
	dir := t.TempDir()
	cfg := filepath.Join(dir, "printer.cfg")
	gcode := filepath.Join(dir, "in.gcode")
	if err := os.WriteFile(cfg, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "G28\nSET_TMC_CURRENT STEPPER=stepper_x CURRENT=0.8\nDUMP_TMC STEPPER=stepper_x\n"
	if err := os.WriteFile(gcode, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	log.SetOutput(&out)
	defer log.SetOutput(os.Stderr)
	result, msg := Run(cfg, dictFile, gcode, filepath.Join(dir, "out.serial"))
	if result != "exit" {
		t.Fatalf("run result %q: %s", result, msg)
	}
	want := []string{
		"IHOLD_IRUN: 00081b1b ihold=27 irun=27 iholddelay=8",
		"MSLUT0:     aaaab554 mslut0=2863314260",
		"MSLUT4:     fbffffff mslut4=4227858431",
		"MSLUTSEL:   ffff8056 w0=2 w1=1 w2=1 w3=1 x1=128 x2=255 x3=255",
		"MSLUTSTART: 00f70000 start_sin90=247",
		"TPWMTHRS:   000fffff tpwmthrs=1048575",
		"COOLCONF:   00040000 sgt=4",
		"PWMCONF:    00050480 pwm_ampl=128 pwm_grad=4 pwm_freq=1 pwm_autoscale=1",
		"DRV_STATUS: 00000000 cs_actual=0(Reset?)",
	}
	for _, w := range want {
		if !strings.Contains(out.String(), w) {
			t.Errorf("missing %q", w)
		}
	}
}
