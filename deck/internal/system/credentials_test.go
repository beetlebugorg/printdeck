package system

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestCredentials(t *testing.T) {
	dir := t.TempDir()
	stateDir = filepath.Join(dir, "printdeck")
	userProgram = filepath.Join(dir, "no-such-program")
	if err := Credentials(); err != nil {
		t.Fatal(err)
	}
	pw, _ := os.ReadFile(filepath.Join(stateDir, "password"))
	if !regexp.MustCompile(`^[a-z]+-[a-z]+-[a-z]+-[a-z]+\n$`).Match(pw) {
		t.Errorf("password file %q", pw)
	}
	api, _ := os.ReadFile(filepath.Join(stateDir, "api.env"))
	if !regexp.MustCompile(`^OCTOPRINT_API_KEY=[0-9a-f]{32}\n$`).Match(api) {
		t.Errorf("api.env %q", api)
	}
	for _, f := range []string{"password", "api.env"} {
		if fi, _ := os.Stat(filepath.Join(stateDir, f)); fi.Mode().Perm() != 0o600 {
			t.Errorf("%s has mode %v", f, fi.Mode().Perm())
		}
	}
	// A second start keeps both.
	if err := Credentials(); err != nil {
		t.Fatal(err)
	}
	pw2, _ := os.ReadFile(filepath.Join(stateDir, "password"))
	api2, _ := os.ReadFile(filepath.Join(stateDir, "api.env"))
	if string(pw2) != string(pw) || string(api2) != string(api) {
		t.Error("a second start changed the credentials")
	}
	// The debug image makes the login user with the same password.
	userProgram = filepath.Join(dir, "printdeck-user")
	os.WriteFile(userProgram, []byte("#!/bin/sh\necho \"$1 $2\" > "+filepath.Join(dir, "user-args")+"\n"), 0o755)
	os.Remove(filepath.Join(stateDir, "password"))
	if err := Credentials(); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "user-args"))
	pw3, _ := os.ReadFile(filepath.Join(stateDir, "password"))
	if string(args) != "printdeck "+strings.TrimSpace(string(pw3))+"\n" {
		t.Errorf("printdeck-user got %q, password %q", args, pw3)
	}
}
