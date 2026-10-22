package system

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The update bundle of the last image build.
const testBundle = "../../../os/out/printdeck-update.tar.xz"

func fakeSystem(t *testing.T, cmdline string) string {
	t.Helper()
	dir := t.TempDir()
	procCmdline = filepath.Join(dir, "cmdline")
	os.WriteFile(procCmdline, []byte(cmdline), 0o644)
	updateWorkDir = dir
	partLabelDir = filepath.Join(dir, "by-partlabel")
	// The bundle of the last build has no signature: sign_test.go tests the signatures.
	allowUnsigned = func() bool { return true }
	os.MkdirAll(partLabelDir, 0o755)
	for _, p := range []string{"printdeck-boot-a", "printdeck-boot-b", "printdeck-root-a", "printdeck-root-b"} {
		os.WriteFile(filepath.Join(partLabelDir, p), nil, 0o644)
	}
	return dir
}

func TestUpdate(t *testing.T) {
	if _, err := os.Stat(testBundle); err != nil {
		t.Skip("no bundle: run os/build.sh")
	}
	dir := fakeSystem(t, "root=PARTLABEL=printdeck-root-a ro printdeck.slot=a quiet\n")
	var steps []string
	slot, err := Update(testBundle, func(step string, done, total int64) {
		if len(steps) == 0 || steps[len(steps)-1] != step {
			steps = append(steps, step)
		}
	})
	if err != nil || slot != "b" {
		t.Fatalf("Update = %q, %v", slot, err)
	}
	if want := []string{StepUnpack, StepCheck, StepWrite}; strings.Join(steps, ",") != strings.Join(want, ",") {
		t.Errorf("steps %v, want %v", steps, want)
	}
	// The partitions of slot b have the files of the bundle, as the script with tar and dd.
	ref := filepath.Join(dir, "ref")
	os.MkdirAll(ref, 0o755)
	if out, err := exec.Command("sh", "-c", "xz -dc "+testBundle+" | tar -xf - -C "+ref).CombinedOutput(); err != nil {
		t.Fatalf("tar: %v %s", err, out)
	}
	for part, file := range map[string]string{"printdeck-boot-b": "boot-b.vfat", "printdeck-root-b": "rootfs.erofs"} {
		got, _ := os.ReadFile(filepath.Join(partLabelDir, part))
		want, _ := os.ReadFile(filepath.Join(ref, file))
		if len(want) == 0 || !bytes.Equal(got, want) {
			t.Errorf("%s: %d bytes, want the %d bytes of %s", part, len(got), len(want), file)
		}
	}
	for _, part := range []string{"printdeck-boot-a", "printdeck-root-a"} {
		if fi, _ := os.Stat(filepath.Join(partLabelDir, part)); fi.Size() != 0 {
			t.Errorf("%s of the running slot was written", part)
		}
	}
	// The work folder is gone.
	if m, _ := filepath.Glob(filepath.Join(dir, "update.*")); len(m) != 0 {
		t.Errorf("work folder left: %v", m)
	}
}

func TestUpdateDamaged(t *testing.T) {
	if _, err := os.Stat(testBundle); err != nil {
		t.Skip("no bundle: run os/build.sh")
	}
	dir := fakeSystem(t, "printdeck.slot=b")
	// A bundle with a changed byte in rootfs.erofs.
	ref := filepath.Join(dir, "ref")
	os.MkdirAll(ref, 0o755)
	script := "xz -dc " + testBundle + " | tar -xf - -C " + ref + " && cd " + ref +
		" && printf 'X' | dd of=rootfs.erofs bs=1 seek=4096 conv=notrunc 2>/dev/null" +
		" && tar -cf - SHA256SUMS boot-a.vfat boot-b.vfat rootfs.erofs | xz -0 > ../bad.tar.xz"
	if out, err := exec.Command("sh", "-c", script).CombinedOutput(); err != nil {
		t.Fatalf("make bad bundle: %v %s", err, out)
	}
	if _, err := Update(filepath.Join(dir, "bad.tar.xz"), nil); err == nil {
		t.Fatal("a damaged bundle was written")
	}
	for _, part := range []string{"printdeck-boot-a", "printdeck-root-a"} {
		if fi, _ := os.Stat(filepath.Join(partLabelDir, part)); fi.Size() != 0 {
			t.Errorf("%s was written from a damaged bundle", part)
		}
	}
}

func TestSlot(t *testing.T) {
	fakeSystem(t, "console=tty1 printdeck.slot=b quiet")
	if s, err := Slot(); s != "b" || err != nil {
		t.Errorf("Slot = %q, %v", s, err)
	}
	fakeSystem(t, "console=tty1 quiet")
	if _, err := Slot(); err == nil {
		t.Error("no slot gave no error")
	}
}
