package system

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ulikunitz/xz"
)

// makeBundle writes a small update bundle with no signature to dir, and returns its path. The
// root file system is the bytes root. With damage, rootfs.erofs has another sum than SHA256SUMS.
func makeBundle(t *testing.T, dir string, root []byte, damage bool) string {
	t.Helper()
	files := map[string][]byte{
		"boot-a.vfat":  bytes.Repeat([]byte("A"), 3000),
		"boot-b.vfat":  bytes.Repeat([]byte("B"), 3000),
		"rootfs.erofs": root,
	}
	var sums strings.Builder
	for _, n := range []string{"boot-a.vfat", "boot-b.vfat", "rootfs.erofs"} {
		h := sha256.Sum256(files[n])
		fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(h[:]), n)
	}
	if damage {
		files["rootfs.erofs"] = append([]byte("X"), root[1:]...)
	}
	var buf bytes.Buffer
	zw, err := xz.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(zw)
	add := func(name string, b []byte) {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(b)), Typeflag: tar.TypeReg})
		tw.Write(b)
	}
	add("SHA256SUMS", []byte(sums.String()))
	for _, n := range []string{"boot-a.vfat", "boot-b.vfat", "rootfs.erofs"} {
		add(n, files[n])
	}
	tw.Close()
	zw.Close()
	p := filepath.Join(dir, fmt.Sprintf("bundle-%d.tar.xz", time.Now().UnixNano()))
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// fakeStage is a system with the slot a running. The mount of a root partition copies the
// version in the partition file ("VERSION=v...") to an os-release file under the mount point.
type fakeStage struct {
	dir  string
	busy bool // a program of NextRoot runs: unmount fails
}

func newFakeStage(t *testing.T) *fakeStage {
	t.Helper()
	f := &fakeStage{dir: fakeSystem(t, "printdeck.slot=a")}
	stagedFile = filepath.Join(f.dir, "run", "printdeck-update", "staged.json")
	NextRoot = filepath.Join(f.dir, "run", "printdeck-next")
	procMounts = filepath.Join(f.dir, "mounts")
	KlippySocket = filepath.Join(f.dir, "no-klippy.sock")
	os.WriteFile(procMounts, []byte("/dev/mmcblk0p4 / erofs ro 0 0\n"), 0o644)
	mountReadOnly = func(dev, dir string) error {
		b, err := os.ReadFile(dev)
		if err != nil {
			return err
		}
		v, ok := strings.CutPrefix(string(b), "VERSION=")
		if !ok {
			return errors.New("not an EROFS file system")
		}
		os.MkdirAll(filepath.Join(dir, "usr", "lib"), 0o755)
		os.WriteFile(filepath.Join(dir, "usr", "lib", "os-release"), []byte(`PRINTDECK_VERSION="`+v+`"`+"\n"), 0o644)
		m, _ := os.ReadFile(procMounts)
		return os.WriteFile(procMounts, append(m, []byte(dev+" "+dir+" erofs ro 0 0\n")...), 0o644)
	}
	unmount = func(dir string) error {
		if f.busy {
			return syscall.EBUSY
		}
		m, _ := os.ReadFile(procMounts)
		var keep []string
		for _, l := range strings.Split(strings.TrimSpace(string(m)), "\n") {
			if fs := strings.Fields(l); len(fs) > 1 && fs[1] == dir {
				continue
			}
			keep = append(keep, l)
		}
		return os.WriteFile(procMounts, []byte(strings.Join(keep, "\n")+"\n"), 0o644)
	}
	gentleRate = 1 << 30
	return f
}

func part(name string) []byte {
	b, _ := os.ReadFile(filepath.Join(partLabelDir, name))
	return b
}

func TestStage(t *testing.T) {
	f := newFakeStage(t)
	if _, ok := ReadStaged(); ok {
		t.Fatal("a staged update with no Stage")
	}
	bundle := makeBundle(t, f.dir, []byte("VERSION=v9.1.0"), false)
	s, err := Stage(bundle, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if s.Slot != "b" || s.Version != "v9.1.0" {
		t.Errorf("Stage = %+v", s)
	}
	got, ok := ReadStaged()
	if !ok || got.Slot != "b" || got.Version != "v9.1.0" {
		t.Errorf("ReadStaged = %+v, %v", got, ok)
	}
	if string(part("printdeck-root-b")) != "VERSION=v9.1.0" || len(part("printdeck-boot-b")) != 3000 {
		t.Error("slot b does not have the bundle")
	}
	if len(part("printdeck-root-a")) != 0 || len(part("printdeck-boot-a")) != 0 {
		t.Error("the running slot was written")
	}
	if !isMountPoint(NextRoot) {
		t.Error("the new root is not mounted")
	}

	// A second update unmounts the first, and replaces it.
	s, err = Stage(makeBundle(t, f.dir, []byte("VERSION=v9.2.0"), false), nil, false)
	if err != nil || s.Version != "v9.2.0" {
		t.Fatalf("second Stage = %+v, %v", s, err)
	}
	if got, ok := ReadStaged(); !ok || got.Version != "v9.2.0" {
		t.Errorf("ReadStaged = %+v, %v", got, ok)
	}
}

// A failed write leaves no record: the deck must not restart into a damaged slot.
func TestStageDamaged(t *testing.T) {
	f := newFakeStage(t)
	if _, err := Stage(makeBundle(t, f.dir, []byte("VERSION=v9.1.0"), false), nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Stage(makeBundle(t, f.dir, []byte("VERSION=v9.3.0"), true), nil, false); err == nil {
		t.Fatal("a damaged bundle gave no error")
	}
	if s, ok := ReadStaged(); ok {
		t.Errorf("a staged update after a failed write: %+v", s)
	}
}

// A root partition that does not mount leaves no record.
func TestStageNoMount(t *testing.T) {
	f := newFakeStage(t)
	if _, err := Stage(makeBundle(t, f.dir, []byte("garbage"), false), nil, false); err == nil {
		t.Fatal("a slot that does not mount gave no error")
	}
	if _, ok := ReadStaged(); ok {
		t.Error("a staged update after a failed mount")
	}
}

// While the deck program of the staged slot runs, a new install fails, and the slot stays.
func TestStageWhileNextRuns(t *testing.T) {
	f := newFakeStage(t)
	if _, err := Stage(makeBundle(t, f.dir, []byte("VERSION=v9.1.0"), false), nil, false); err != nil {
		t.Fatal(err)
	}
	f.busy = true
	_, err := Stage(makeBundle(t, f.dir, []byte("VERSION=v9.2.0"), false), nil, false)
	if err != errNextBusy {
		t.Fatalf("err = %v, want errNextBusy", err)
	}
	if string(part("printdeck-root-b")) != "VERSION=v9.1.0" {
		t.Error("the slot of the running deck program was written")
	}
	if s, ok := ReadStaged(); !ok || s.Version != "v9.1.0" {
		t.Errorf("the staged update is gone: %+v, %v", s, ok)
	}
}

// An update does not write a mounted partition.
func TestUpdateRefusesMounted(t *testing.T) {
	f := newFakeStage(t)
	dev := filepath.Join(partLabelDir, "printdeck-root-b")
	os.WriteFile(procMounts, []byte(dev+" /mnt erofs ro 0 0\n"), 0o644)
	if _, err := Stage(makeBundle(t, f.dir, []byte("VERSION=v9.1.0"), false), nil, false); err == nil {
		t.Fatal("a mounted partition gave no error")
	}
	if len(part("printdeck-root-b")) != 0 {
		t.Error("the mounted partition was written")
	}
}

// A record of the running slot is no staged update: after a restart into the new slot, the
// other slot is the old one.
func TestReadStagedOtherSlot(t *testing.T) {
	newFakeStage(t)
	os.MkdirAll(filepath.Dir(stagedFile), 0o755)
	os.WriteFile(stagedFile, []byte(`{"slot":"a","version":"v1"}`), 0o644)
	if _, ok := ReadStaged(); ok {
		t.Error("a record of the running slot is a staged update")
	}
	os.WriteFile(stagedFile, []byte(`{"slot":"b","version":"v1"}`), 0o644)
	if _, ok := ReadStaged(); !ok {
		t.Error("a record of the other slot is no staged update")
	}
}

func TestRebootStagedNone(t *testing.T) {
	newFakeStage(t)
	if err := RebootStaged(); err == nil {
		t.Error("RebootStaged with no staged update gave no error")
	}
}

func TestWritePaced(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789abcdef"), (3*gentleChunk+100)/16)
	out, err := os.Create(filepath.Join(t.TempDir(), "dev"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	start := time.Now()
	// 3 MiB at 30 MiB a second: 100 ms or more.
	if err := writePaced(out, bytes.NewReader(data), 30<<20, start); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < 90*time.Millisecond {
		t.Errorf("the write took %v, want 100 ms or more", d)
	}
	got, _ := os.ReadFile(out.Name())
	if !bytes.Equal(got, data) {
		t.Errorf("wrote %d bytes, want %d", len(got), len(data))
	}
}

func TestUnescapeMount(t *testing.T) {
	if got := unescapeMount(`/mnt/a\040b`); got != "/mnt/a b" {
		t.Errorf("unescapeMount = %q", got)
	}
}

// realHelperActions are the actions of the helper before a test replaces them.
var realHelperActions = helperActions

// The stage action writes the slot, removes the upload and does not restart. reboot-staged
// restarts only into a staged update.
func TestHelperStage(t *testing.T) {
	f := newFakeStage(t)
	if after, err := realHelperActions(HelperRequest{Action: "reboot-staged"}, nil); err == nil || after != nil {
		t.Fatalf("reboot-staged with no staged update: %v", err)
	}
	UpdateBundle = makeBundle(t, f.dir, []byte("VERSION=v9.1.0"), false)
	after, err := realHelperActions(HelperRequest{Action: "stage"}, func(string, int64, int64) {})
	if err != nil || after != nil {
		t.Fatalf("stage: %v, a restart: %v", err, after != nil)
	}
	if _, err := os.Stat(UpdateBundle); !os.IsNotExist(err) {
		t.Error("the upload is still there")
	}
	if s, ok := ReadStaged(); !ok || s.Version != "v9.1.0" {
		t.Errorf("ReadStaged = %+v, %v", s, ok)
	}
	// Do not call the function: it restarts the computer.
	if after, err := realHelperActions(HelperRequest{Action: "reboot-staged"}, nil); err != nil || after == nil {
		t.Errorf("reboot-staged: %v, a restart: %v", err, after != nil)
	}
}
