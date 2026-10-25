// Package system has the system programs of Printdeck OS: the first program at boot, the update
// of the other slot, the settings of the config partition, the Wi-Fi network and the login. They
// were shell scripts. In Go they need no shell in the image.
package system

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Paths of the system. Tests change them.
var (
	procCmdline = "/proc/cmdline"
	sysBlock    = "/sys/class/block"
	tryboot     = "/proc/device-tree/chosen/bootloader/tryboot"
)

// Slot returns the slot of the running system, "a" or "b", from printdeck.slot on the kernel
// command line.
func Slot() (string, error) {
	b, err := os.ReadFile(procCmdline)
	if err != nil {
		return "", err
	}
	for _, f := range strings.Fields(string(b)) {
		if v, ok := strings.CutPrefix(f, "printdeck.slot="); ok && (v == "a" || v == "b") {
			return v, nil
		}
	}
	return "", errors.New("The kernel command line has no printdeck.slot.")
}

// otherSlot returns the slot that an update writes.
func otherSlot(s string) string {
	if s == "a" {
		return "b"
	}
	return "a"
}

// Tryboot reports that the firmware started this system one time with tryboot (see Update).
func Tryboot() bool {
	b, err := os.ReadFile(tryboot)
	return err == nil && bytes.Equal(b, []byte{0, 0, 0, 1})
}

// Partition returns the device name, for example "sda6", of the partition with a GPT partition
// name.
func Partition(name string) (string, error) {
	events, _ := filepath.Glob(filepath.Join(sysBlock, "*", "uevent"))
	for _, u := range events {
		b, err := os.ReadFile(u)
		if err != nil {
			continue
		}
		var dev string
		found := false
		for _, l := range strings.Split(string(b), "\n") {
			if l == "PARTNAME="+name {
				found = true
			}
			if v, ok := strings.CutPrefix(l, "DEVNAME="); ok {
				dev = v
			}
		}
		if found && dev != "" {
			return dev, nil
		}
	}
	return "", fmt.Errorf("No %s partition", name)
}

// run runs a program, and returns its output in the error when it fails.
func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("%s: %s", name, msg)
		}
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// writeFile writes a file through a temporary file and a rename, so that a reader sees the old or
// the new file, never a part. It does not write a file that already has data and mode, because
// the disk is an SD card and many of these files are written at each start.
func writeFile(path string, data []byte, mode os.FileMode) error {
	if fi, err := os.Stat(path); err == nil && fi.Mode().Perm() == mode {
		if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
			return nil
		}
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
