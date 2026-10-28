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
	"time"

	"github.com/ulikunitz/xz"
)

// bundleFiles are the files of an update bundle (os/board/post-image.sh).
var bundleFiles = map[string]bool{"SHA256SUMS": true, "SHA256SUMS.sig": true, "boot-a.vfat": true, "boot-b.vfat": true, "rootfs.erofs": true}

// Paths of the update. Tests change them.
var (
	updateWorkDir = "/data"
	partLabelDir  = "/dev/disk/by-partlabel"
)

// Progress tells the step of an update, and how far it is: done of total bytes, or 0 of 0 for a
// step with no size.
type Progress func(step string, done, total int64)

// The steps of an update, for Progress.
const (
	StepUnpack = "Unpacking"
	StepCheck  = "Checking the signature"
	StepWrite  = "Writing the system"
)

// Update writes an update bundle (printdeck-update.tar.xz) to the other slot. Then Reboot with
// tryboot starts the other slot one time. After the restart, Commit makes the new slot the default
// if the services start. If the new system does not start, the next restart returns to the current
// slot. progress gets the steps, or it is nil.
func Update(bundle string, progress Progress) (slot string, err error) {
	return update(bundle, progress, false)
}

// update is Update. With gentle, it writes the partitions slowly (writeDevice).
func update(bundle string, progress Progress, gentle bool) (slot string, err error) {
	if progress == nil {
		progress = func(string, int64, int64) {}
	}
	cur, err := Slot()
	if err != nil {
		return "", err
	}
	slot = otherSlot(cur)

	// The bundle is not unpacked on the disk, because the disk is an SD card. The first read takes
	// SHA256SUMS and its signature. The second read writes the files of the slot to its partitions,
	// and checks the sum of each file. With a bad sum, Update fails and the Pi does not start the
	// slot.
	work, err := os.MkdirTemp(updateWorkDir, "update.")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	progress(StepUnpack, 0, 0)
	if err := extractSums(bundle, work); err != nil {
		return "", err
	}
	progress(StepCheck, 0, 0)
	if err := checkSignature(work); err != nil {
		return "", err
	}
	sums, err := readSums(work)
	if err != nil {
		return "", err
	}
	devs := map[string]string{
		"boot-" + slot + ".vfat": filepath.Join(partLabelDir, "printdeck-boot-"+slot),
		"rootfs.erofs":           filepath.Join(partLabelDir, "printdeck-root-"+slot),
	}
	if err := checkUnmounted([]string{devs["boot-"+slot+".vfat"], devs["rootfs.erofs"]}); err != nil {
		return "", err
	}
	progress(StepWrite, 0, 0)
	err = readBundle(bundle, func(done, total int64) { progress(StepWrite, done, total) }, func(name string, r io.Reader) error {
		if name == "SHA256SUMS" || name == "SHA256SUMS.sig" {
			return nil
		}
		h := sha256.New()
		if dev, ok := devs[name]; ok {
			if err := writeDevice(io.TeeReader(r, h), dev, gentle); err != nil {
				return err
			}
			delete(devs, name)
		} else if _, err := io.Copy(h, r); err != nil {
			return fmt.Errorf("The bundle is damaged: %w", err)
		}
		if hex.EncodeToString(h.Sum(nil)) != sums[name] {
			return fmt.Errorf("The bundle is damaged: %s", name)
		}
		delete(sums, name)
		return nil
	})
	if err != nil {
		return "", err
	}
	for name := range devs {
		return "", fmt.Errorf("The bundle is damaged: it has no %s", name)
	}
	for name := range sums {
		return "", fmt.Errorf("The bundle is damaged: it has no %s", name)
	}
	return slot, nil
}

// VerifyBundle checks an update bundle as Update does, and writes nothing: the signature of a
// Printdeck release, and the checksums of its files. A bundle with no signature fails, also on
// a debug image.
func VerifyBundle(bundle string) error {
	work, err := os.MkdirTemp("", "printdeck-verify.")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	if err := extractBundle(bundle, work); err != nil {
		return err
	}
	unsigned := allowUnsigned
	allowUnsigned = func() bool { return false }
	err = checkSignature(work)
	allowUnsigned = unsigned
	if err != nil {
		return err
	}
	return checkSums(work)
}

// RebootTryboot restarts into the other slot one time. The reboot argument "0 tryboot" makes the
// firmware start the tryboot partition one time. systemd 257 accepts it only with
// --reboot-argument.
func RebootTryboot() error {
	return run("systemctl", "reboot", "--reboot-argument=0 tryboot")
}

// extractBundle unpacks the files of a bundle into dir. It takes only the files of a bundle.
func extractBundle(bundle, dir string) error { return extractBundleProgress(bundle, dir, nil) }

// extractBundleProgress is extractBundle, and tells the bytes of the bundle that it read.
func extractBundleProgress(bundle, dir string, progress func(done, total int64)) error {
	return readBundle(bundle, progress, func(name string, r io.Reader) error {
		out, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		_, err = io.Copy(out, r)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return fmt.Errorf("The bundle is damaged: %w", err)
		}
		return nil
	})
}

// errStop ends readBundle early with no error.
var errStop = errors.New("stop")

// extractSums unpacks only SHA256SUMS and SHA256SUMS.sig into dir. The build puts them first in
// the bundle, so it does not read the rest.
func extractSums(bundle, dir string) error {
	found := 0
	err := readBundle(bundle, nil, func(name string, r io.Reader) error {
		if name != "SHA256SUMS" && name != "SHA256SUMS.sig" {
			return errStop
		}
		b, err := io.ReadAll(r)
		if err != nil {
			return fmt.Errorf("The bundle is damaged: %w", err)
		}
		found++
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			return err
		}
		if found == 2 {
			return errStop
		}
		return nil
	})
	if err == errStop {
		err = nil
	}
	return err
}

// readBundle calls file for each file of a bundle, with its name and its data. It takes only the
// files of a bundle. progress, if not nil, gets the bytes of the bundle that it read.
func readBundle(bundle string, progress func(done, total int64), file func(name string, r io.Reader) error) error {
	f, err := os.Open(bundle)
	if err != nil {
		return err
	}
	defer f.Close()
	var in io.Reader = f
	if progress != nil {
		if fi, err := f.Stat(); err == nil {
			in = &countReader{r: f, total: fi.Size(), progress: progress}
		}
	}
	zr, err := xz.NewReader(bufio.NewReaderSize(in, 1<<20))
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
		if err := file(name, tr); err != nil {
			return err
		}
	}
}

// readSums reads SHA256SUMS in dir: the sum of each file. Each file of the bundle must be in it.
func readSums(dir string) (map[string]string, error) {
	b, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		return nil, errors.New("The bundle is damaged: it has no SHA256SUMS")
	}
	sums := map[string]string{}
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		sum, name, ok := strings.Cut(l, "  ")
		if !ok || !bundleFiles[name] || name == "SHA256SUMS" || name == "SHA256SUMS.sig" {
			return nil, fmt.Errorf("The bundle is damaged: bad line in SHA256SUMS: %q", l)
		}
		sums[name] = sum
	}
	for name := range bundleFiles {
		if _, ok := sums[name]; !ok && name != "SHA256SUMS" && name != "SHA256SUMS.sig" {
			return nil, fmt.Errorf("The bundle is damaged: SHA256SUMS has no %s", name)
		}
	}
	return sums, nil
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
		if !ok || !bundleFiles[name] || name == "SHA256SUMS" || name == "SHA256SUMS.sig" {
			return fmt.Errorf("The bundle is damaged: bad line in SHA256SUMS: %q", l)
		}
		got, err := fileSum(filepath.Join(dir, name))
		if err != nil || got != sum {
			return fmt.Errorf("The bundle is damaged: %s", name)
		}
		listed[name] = true
	}
	for name := range bundleFiles {
		if name != "SHA256SUMS" && name != "SHA256SUMS.sig" && !listed[name] {
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

// The pace of a gentle write: at most gentleRate bytes a second, with a sync after each
// gentleChunk bytes. Tests change gentleRate.
var gentleRate int64 = 4 << 20

const gentleChunk = 1 << 20

// writeDevice writes r to a partition and waits until the data is on the disk (dd conv=fsync).
//
// With gentle, it writes for a print that runs: the Klipper host reads the G-code file from the
// same SD card. The kernel writes the dirty pages of a fast write in large bursts, and a read of
// the Klipper host can then wait for the card. A gentle write syncs each gentleChunk bytes, so a
// burst is short, and it keeps to gentleRate bytes a second.
func writeDevice(r io.Reader, dev string, gentle bool) error {
	out, err := os.OpenFile(dev, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if gentle {
		err = writePaced(out, r, gentleRate, time.Now())
	} else {
		_, err = io.CopyBuffer(out, r, make([]byte, 4<<20))
	}
	if err != nil {
		out.Close()
		return fmt.Errorf("writing %s: %w", dev, err)
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return fmt.Errorf("writing %s: %w", dev, err)
	}
	return out.Close()
}

// writePaced copies r to out in chunks of gentleChunk bytes, with a sync after each chunk, and
// sleeps to keep to rate bytes a second from start.
func writePaced(out *os.File, r io.Reader, rate int64, start time.Time) error {
	buf := make([]byte, gentleChunk)
	var n int64
	for {
		k, rerr := io.ReadFull(r, buf)
		if k > 0 {
			if _, err := out.Write(buf[:k]); err != nil {
				return err
			}
			if err := out.Sync(); err != nil {
				return err
			}
			n += int64(k)
			if wait := time.Duration(n*int64(time.Second)/rate) - time.Since(start); wait > 0 {
				time.Sleep(wait)
			}
		}
		switch {
		case rerr == io.EOF || rerr == io.ErrUnexpectedEOF:
			return nil
		case rerr != nil:
			return rerr
		}
	}
}

// countReader tells progress the bytes that it read: the sum and the total, or with delta the
// bytes of each read.
type countReader struct {
	r        io.Reader
	n, total int64
	delta    bool
	progress func(done, total int64)
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.n += int64(n)
		if c.delta {
			c.progress(int64(n), 0)
		} else {
			c.progress(c.n, c.total)
		}
	}
	return n, err
}
