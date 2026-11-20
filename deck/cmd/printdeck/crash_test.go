package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// The crash.log of the run before becomes last-crash.log, and /system/crash sends it.
func TestKeepCrashes(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "crash.log"), []byte("panic: test\n"), 0o600)
	keepCrashes(dir)
	defer func() { crashDir = "" }()
	w := httptest.NewRecorder()
	serveCrash(w, httptest.NewRequest("GET", "/system/crash", nil))
	if w.Code != 200 || w.Body.String() != "panic: test\n" {
		t.Errorf("%d %q", w.Code, w.Body.String())
	}
	if fi, err := os.Stat(filepath.Join(dir, "crash.log")); err != nil || fi.Size() != 0 {
		t.Errorf("no new empty crash.log: %v", err)
	}
}
