package configfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SAVE_CONFIG writes the changes in the #*# block, comments out the fields of the file that the
// block overrides, and keeps a backup. A second read of the file gives the new values.
func TestSaveConfig(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "printer.cfg")
	orig := "[stepper_z]\nposition_endstop: 1.0\nstep_pin: PA1\n"
	os.WriteFile(name, []byte(orig), 0o644)
	pc := NewPrinterConfig(nil)
	if _, err := pc.ReadMainConfig(name); err != nil {
		t.Fatal(err)
	}
	pc.Set("stepper_z", "position_endstop", "0.425")
	if !pc.Status(0)["save_config_pending"].(bool) {
		t.Error("no pending save")
	}
	restart, err := pc.SaveConfig(name)
	if err != nil || !restart {
		t.Fatalf("restart %v, error %v", restart, err)
	}
	b, _ := os.ReadFile(name)
	got := string(b)
	for _, want := range []string{"#position_endstop: 1.0", "step_pin: PA1", autosaveHeader[1:],
		"#*# [stepper_z]\n#*# position_endstop = 0.425\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("file has no %q:\n%s", want, got)
		}
	}
	fc, _, err := LoadMainConfig(name)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := fc.Get("stepper_z", "position_endstop"); v != "0.425" {
		t.Errorf("position_endstop = %q", v)
	}
	backups, _ := filepath.Glob(filepath.Join(dir, "printer-*.cfg"))
	if len(backups) != 1 {
		t.Errorf("backups %v", backups)
	}
}
