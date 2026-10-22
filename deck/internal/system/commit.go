//go:build linux

package system

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// configDir is the mount point of the config partition (/etc/fstab).
var configDir = "/boot/config"

// MountConfig mounts the config partition, the FAT partition that a computer can read, and returns
// the function that unmounts it.
func MountConfig() (func(), error) {
	dev, err := Partition("printdeck-config")
	if err != nil {
		return nil, err
	}
	if err := syscall.Mount("/dev/"+dev, configDir, "vfat", syscall.MS_NOATIME, "umask=0077"); err != nil {
		return nil, fmt.Errorf("mount %s: %w", configDir, err)
	}
	return func() {
		syscall.Sync()
		syscall.Unmount(configDir, 0)
	}, nil
}

// autoboot is autoboot.txt for a default boot partition and a tryboot partition (2 for slot a,
// 3 for slot b).
func autoboot(def, try int) string {
	return fmt.Sprintf("[all]\ntryboot_a_b=1\nboot_partition=%d\n[tryboot]\nboot_partition=%d\n", def, try)
}

// Commit runs at each boot. After a tryboot into a new slot (see Update), it waits for the
// services. If they start, it makes the new slot the default. If they do not start, it restarts,
// and the Pi starts the old slot again.
func Commit(log io.Writer) error {
	if !Tryboot() {
		return nil
	}
	slot, err := Slot()
	if err != nil {
		return err
	}
	def, other := 2, 3
	if slot == "b" {
		def, other = 3, 2
	}
	for range 60 {
		if exec.Command("systemctl", "is-active", "--quiet", "klipper.service", "printdeck-deck.service").Run() == nil {
			umount, err := MountConfig()
			if err != nil {
				return err
			}
			err = writeFile(filepath.Join(configDir, "autoboot.txt"), []byte(autoboot(def, other)), 0o644)
			umount()
			if err != nil {
				return err
			}
			fmt.Fprintf(log, "Slot %s is now the default.\n", slot)
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	fmt.Fprintf(os.Stderr, "The services did not start in slot %s. Restarting into the old slot.\n", slot)
	return run("systemctl", "reboot")
}
