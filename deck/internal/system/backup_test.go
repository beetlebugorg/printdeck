package system

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"printdeck/internal/backup"
)

func put(t *testing.T, p, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestBackupAndRestore(t *testing.T) {
	old := [3]string{stateDir, KlipperConfigDir, GcodeDir}
	defer SetDataDirs(old[0], old[1], old[2])
	root := t.TempDir()
	state, config, gcodes := filepath.Join(root, "printdeck"), filepath.Join(root, "config"), filepath.Join(root, "gcodes")
	SetDataDirs(state, config, gcodes)
	put(t, filepath.Join(state, "spools.json"), "rolls")
	put(t, filepath.Join(state, "api.env"), "OCTOPRINT_API_KEY=k\n")
	put(t, filepath.Join(state, "push.json"), "phones")
	put(t, filepath.Join(state, "password"), "old words\n")
	put(t, filepath.Join(state, "settings.env"), "HOST=a\n")
	put(t, filepath.Join(state, "logs", "x.log"), "log")
	put(t, filepath.Join(config, "printer.cfg"), "[printer]\n")
	put(t, filepath.Join(gcodes, "benchy.gcode"), "G28\n")

	if err := MakeBackup("short", false, nil); err == nil {
		t.Error("a backup password of 5 characters was accepted")
	}
	if err := MakeBackup("a long password", false, nil); err != nil {
		t.Fatal(err)
	}
	made := get(t, BackupFile())

	// A new deck: its own password, and other data.
	put(t, filepath.Join(state, "spools.json"), "new")
	put(t, filepath.Join(state, "password"), "new words\n")
	put(t, filepath.Join(config, "printer.cfg"), "[other]\n")
	os.Remove(filepath.Join(state, "api.env"))
	os.Remove(filepath.Join(gcodes, "benchy.gcode"))

	// A wrong password changes no file, and removes the upload.
	put(t, RestoreFile(), made)
	if err := RestoreBackup("a wrong password", nil); !errors.Is(err, backup.ErrPassword) {
		t.Fatalf("wrong password: %v", err)
	}
	if get(t, filepath.Join(state, "spools.json")) != "new" {
		t.Fatal("a failed restore changed a file")
	}
	if _, err := os.Stat(RestoreFile()); err == nil {
		t.Error("the upload is still there")
	}

	put(t, RestoreFile(), made)
	if err := RestoreBackup("a long password", nil); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]string{
		filepath.Join(state, "spools.json"):  "rolls",
		filepath.Join(state, "api.env"):      "OCTOPRINT_API_KEY=k\n",
		filepath.Join(state, "push.json"):    "phones",
		filepath.Join(state, "password"):     "new words\n",
		filepath.Join(state, "settings.env"): "HOST=a\n",
		filepath.Join(config, "printer.cfg"): "[printer]\n",
	} {
		if got := get(t, p); got != want {
			t.Errorf("%s: %q, want %q", filepath.Base(p), got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(gcodes, "benchy.gcode")); err == nil {
		t.Error("a backup without G-code files restored a G-code file")
	}

	// With the G-code files.
	put(t, filepath.Join(gcodes, "benchy.gcode"), "G28\n")
	if err := MakeBackup("a long password", true, nil); err != nil {
		t.Fatal(err)
	}
	os.Rename(BackupFile(), RestoreFile())
	os.Remove(filepath.Join(gcodes, "benchy.gcode"))
	if err := RestoreBackup("a long password", nil); err != nil {
		t.Fatal(err)
	}
	if get(t, filepath.Join(gcodes, "benchy.gcode")) != "G28\n" {
		t.Error("the G-code file did not come back")
	}
}
