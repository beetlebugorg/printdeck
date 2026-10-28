package system

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A staged update is an update in the other slot that waits for a restart. Stage writes the slot
// as Update does, and does not restart. The default slot in autoboot.txt stays the running slot:
// only RebootStaged starts the new slot, one time with tryboot, and printdeck-commit then makes it
// the default. After a power loss, the Pi starts the running slot again, and the files in /run are
// gone, so the deck shows no staged update.
//
// Stage also mounts the root file system of the new slot read-only at NextRoot. The deck can then
// run the deck program of the new slot during a print (see the deck's handoff).

// Paths of the staged update. Tests change them.
var (
	// stagedFile has the staged update. Only the helper (root) writes it.
	stagedFile = "/run/printdeck-update/staged.json"
	// NextRoot is the mount point of the root file system of the staged slot.
	NextRoot   = "/run/printdeck-next"
	procMounts = "/proc/self/mounts"
)

// Staged is an update in the other slot that waits for the restart.
type Staged struct {
	Slot    string    `json:"slot"`    // "a" or "b"
	Version string    `json:"version"` // PRINTDECK_VERSION of the new slot, or ""
	Written time.Time `json:"written"`
}

// ReadStaged returns the staged update. It is false with no staged update, and for a file of
// another slot than the other slot of this boot.
func ReadStaged() (Staged, bool) {
	b, err := os.ReadFile(stagedFile)
	if err != nil {
		return Staged{}, false
	}
	var s Staged
	if json.Unmarshal(b, &s) != nil {
		return Staged{}, false
	}
	cur, err := Slot()
	if err != nil || s.Slot != otherSlot(cur) {
		return Staged{}, false
	}
	return s, true
}

// Stage writes an update bundle to the other slot, as Update does, and records the staged update.
// It does not restart. With gentle, the write is slow and has a low priority, for a print that
// runs: see writeDevice.
func Stage(bundle string, progress Progress, gentle bool) (Staged, error) {
	// While the deck program of the staged slot runs, the slot stays as it is, with its record.
	if err := releaseNext(); err != nil {
		return Staged{}, err
	}
	// The old record goes before the write: a failed write leaves no record of a staged update.
	if err := os.Remove(stagedFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Staged{}, err
	}
	slot, err := update(bundle, progress, gentle)
	if err != nil {
		return Staged{}, err
	}
	// The mount of the new root file system is also a check of the slot.
	if err := mountNext(slot); err != nil {
		return Staged{}, fmt.Errorf("The new system does not mount: %w", err)
	}
	s := Staged{Slot: slot, Version: rootVersion(NextRoot), Written: time.Now().UTC()}
	b, _ := json.Marshal(s)
	if err := os.MkdirAll(filepath.Dir(stagedFile), 0o755); err != nil {
		return Staged{}, err
	}
	if err := writeFile(stagedFile, append(b, '\n'), 0o644); err != nil {
		return Staged{}, err
	}
	return s, nil
}

// RebootStaged restarts into the staged update, one time with tryboot. It fails with no staged
// update.
func RebootStaged() error {
	if _, ok := ReadStaged(); !ok {
		return errors.New("No update waits for the restart.")
	}
	return RebootTryboot()
}

// errNextBusy is the error of a new install while the deck program of the staged slot runs.
var errNextBusy = errors.New("The screen program of the installed update runs now. Restart the Printdeck, then install again.")

// releaseNext unmounts NextRoot. It fails while a program of the mount runs.
func releaseNext() error {
	if !isMountPoint(NextRoot) {
		return nil
	}
	if err := unmount(NextRoot); err != nil {
		return errNextBusy
	}
	return nil
}

// mountNext mounts the root partition of slot read-only at NextRoot.
func mountNext(slot string) error {
	if err := os.MkdirAll(NextRoot, 0o755); err != nil {
		return err
	}
	return mountReadOnly(filepath.Join(partLabelDir, "printdeck-root-"+slot), NextRoot)
}

// rootVersion returns PRINTDECK_VERSION of the os-release file under root, or "".
func rootVersion(root string) string {
	f, err := os.Open(filepath.Join(root, "usr/lib/os-release"))
	if err != nil {
		return ""
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if v, ok := strings.CutPrefix(s.Text(), "PRINTDECK_VERSION="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}

// mounts returns the sources and the mount points of the mounts of the system.
func mounts() (sources, targets map[string]bool) {
	sources, targets = map[string]bool{}, map[string]bool{}
	b, err := os.ReadFile(procMounts)
	if err != nil {
		return
	}
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		sources[unescapeMount(f[0])] = true
		targets[unescapeMount(f[1])] = true
	}
	return
}

// unescapeMount undoes the octal escapes of /proc/self/mounts, for example "\040" for a space.
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			var c int
			if _, err := fmt.Sscanf(s[i+1:i+4], "%o", &c); err == nil {
				b.WriteByte(byte(c))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// isMountPoint reports that a file system is mounted at dir.
func isMountPoint(dir string) bool {
	_, targets := mounts()
	return targets[dir]
}

// checkUnmounted fails when one of the partitions devs is mounted: an update must not write a
// mounted file system.
func checkUnmounted(devs []string) error {
	sources, _ := mounts()
	for _, d := range devs {
		real, err := filepath.EvalSymlinks(d)
		if err != nil {
			real = d
		}
		if sources[real] || sources[d] {
			return fmt.Errorf("%s is mounted. Restart the Printdeck, then install again.", filepath.Base(d))
		}
	}
	return nil
}
