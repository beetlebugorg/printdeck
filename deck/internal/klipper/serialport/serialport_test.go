package serialport

import (
	"bytes"
	"fmt"
	"os"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// openPty opens a pseudo-terminal: the master side for the test, and the path of the other side
// for Open.
func openPty(t *testing.T) (*os.File, string) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminals: %v", err)
	}
	t.Cleanup(func() { m.Close() })
	var unlock int32
	var n uint32
	rc, _ := m.SyscallConn()
	rc.Control(func(fd uintptr) {
		ioctl(fd, syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock)))
		ioctl(fd, syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n)))
	})
	return m, fmt.Sprintf("/dev/pts/%d", n)
}

func TestOpenRawAndBaud(t *testing.T) {
	m, path := openPty(t)
	p, err := Open(path, 250000, true)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	var tio *termios2
	p.control(func(fd uintptr) error {
		tio, err = getTermios(fd)
		return err
	})
	if tio.Ispeed != 250000 || tio.Ospeed != 250000 || tio.Cflag&cbaud != bother {
		t.Errorf("speed %d/%d cflag %#o, want 250000 with BOTHER", tio.Ispeed, tio.Ospeed, tio.Cflag)
	}
	if tio.Lflag&(syscall.ICANON|syscall.ECHO) != 0 || tio.Oflag&syscall.OPOST != 0 {
		t.Errorf("not raw: lflag %#o oflag %#o", tio.Lflag, tio.Oflag)
	}

	// Bytes go both ways unchanged, also the bytes that a terminal would change.
	data := []byte{0x7e, 0x0d, 0x0a, 0x03, 0x11, 0x13, 0xff}
	if _, err := m.Write(data); err != nil {
		t.Fatal(err)
	}
	got, err := p.ReadFor(100 * time.Millisecond)
	if err != nil || !bytes.Equal(got, data) {
		t.Errorf("read % x %v, want % x", got, err, data)
	}
	if _, err := p.Write(data); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	m.SetReadDeadline(time.Now().Add(time.Second))
	n, err := m.Read(buf)
	if err != nil || !bytes.Equal(buf[:n], data) {
		t.Errorf("write: the other side read % x %v", buf[:n], err)
	}
}

func TestOpenIsExclusive(t *testing.T) {
	_, path := openPty(t)
	p, err := Open(path, 115200, false)
	if err != nil {
		t.Fatal(err)
	}
	if p2, err := Open(path, 115200, false); err == nil {
		p2.Close()
		t.Error("a second open of the port worked")
	}
	// After the close, the port opens again.
	p.Close()
	p3, err := Open(path, 115200, false)
	if err != nil {
		t.Fatalf("open after close: %v", err)
	}
	p3.Close()
}

func TestSetBaudAndHUPCL(t *testing.T) {
	_, path := openPty(t)
	p, err := Open(path, 250000, true)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err := p.SetBaud(2400); err != nil {
		t.Fatal(err)
	}
	if err := p.ClearHUPCL(); err != nil {
		t.Fatal(err)
	}
	p.control(func(fd uintptr) error {
		tio, _ := getTermios(fd)
		if tio.Ospeed != 2400 || tio.Cflag&syscall.HUPCL != 0 {
			t.Errorf("speed %d, cflag %#o", tio.Ospeed, tio.Cflag)
		}
		return nil
	})
}
