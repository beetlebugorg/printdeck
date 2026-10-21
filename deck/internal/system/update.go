package system

import (
	"archive/tar"
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ulikunitz/xz"
)

// bundleFiles are the files of an update bundle (os/board/post-image.sh).
var bundleFiles = map[string]bool{"SHA256SUMS": true, "boot-a.vfat": true, "boot-b.vfat": true, "rootfs.erofs": true}

// Paths of the update. Tests change them.
var (
	updateWorkDir = "/data"
	partLabelDir  = "/dev/disk/by-partlabel"
)

// Update writes an update bundle (printdeck-update.tar.xz) to the other slot. Then Reboot with
// tryboot starts the other slot one time. After the restart, Commit makes the new slot the default
// if the services start. If the new system does not start, the next restart returns to the current
// slot. log gets the steps.
func Update(bundle string, log io.Writer) (slot string, err error) {
	cur, err := Slot()
	if err != nil {
		return "", err
	}
	slot = otherSlot(cur)

	work, err := os.MkdirTemp(updateWorkDir, "update.")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	if err := extractBundle(bundle, work); err != nil {
		return "", err
	}
	if err := checkSums(work); err != nil {
		return "", err
	}

	fmt.Fprintf(log, "Writing slot %s.\n", slot)
	if err := copyToDevice(filepath.Join(work, "boot-"+slot+".vfat"), filepath.Join(partLabelDir, "printdeck-boot-"+slot)); err != nil {
		return "", err
	}
	if err := copyToDevice(filepath.Join(work, "rootfs.erofs"), filepath.Join(partLabelDir, "printdeck-root-"+slot)); err != nil {
		return "", err
	}
	return slot, nil
}

// RebootTryboot restarts into the other slot one time. The reboot argument "0 tryboot" makes the
// firmware start the tryboot partition one time. systemd 257 accepts it only with
// --reboot-argument.
func RebootTryboot() error {
	return run("systemctl", "reboot", "--reboot-argument=0 tryboot")
}

// extractBundle unpacks the files of a bundle into dir. It takes only the files of a bundle.
func extractBundle(bundle, dir string) error {
	f, err := os.Open(bundle)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := xz.NewReader(bufio.NewReaderSize(f, 1<<20))
	if err != nil {
		return fmt.Errorf("The bundle is damaged: %w", err)
	}
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("The bundle is damaged: %w", err)
		}
		name := strings.TrimPrefix(h.Name, "./")
		if h.Typeflag != tar.TypeReg || !bundleFiles[name] {
			return fmt.Errorf("The bundle has an unknown file: %s", h.Name)
		}
		out, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		_, err = io.Copy(out, tr)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return fmt.Errorf("The bundle is damaged: %w", err)
		}
	}
}

// checkSums checks the files in SHA256SUMS (sha256sum -c). Each file of the bundle must be in it.
func checkSums(dir string) error {
	b, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		return errors.New("The bundle is damaged: it has no SHA256SUMS")
	}
	listed := map[string]bool{}
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		sum, name, ok := strings.Cut(l, "  ")
		if !ok || !bundleFiles[name] || name == "SHA256SUMS" {
			return fmt.Errorf("The bundle is damaged: bad line in SHA256SUMS: %q", l)
		}
		got, err := fileSum(filepath.Join(dir, name))
		if err != nil || got != sum {
			return fmt.Errorf("The bundle is damaged: %s", name)
		}
		listed[name] = true
	}
	for name := range bundleFiles {
		if name != "SHA256SUMS" && !listed[name] {
			return fmt.Errorf("The bundle is damaged: SHA256SUMS has no %s", name)
		}
	}
	return nil
}

func fileSum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// copyToDevice writes a file to a partition and waits until the data is on the disk (dd
// conv=fsync).
func copyToDevice(src, dev string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dev, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if _, err := io.CopyBuffer(out, in, make([]byte, 4<<20)); err != nil {
		out.Close()
		return fmt.Errorf("writing %s: %w", dev, err)
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return fmt.Errorf("writing %s: %w", dev, err)
	}
	return out.Close()
}
