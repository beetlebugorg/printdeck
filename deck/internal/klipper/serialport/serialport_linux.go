package serialport

import (
	"errors"
	"os"
	"syscall"
	"time"
	"unsafe"
)

// Linux ioctls and flags that the syscall package does not have. They are the same on amd64 and
// arm64.
const (
	tcgets2 = 0x802c542a
	tcsets2 = 0x402c542b
	tcflsh  = 0x540b
	tcoflsh = 1
	cbaud   = 0o010017
	bother  = 0o010000
	crtscts = 0x80000000
)

// termios2 is struct termios2 of the Linux kernel.
type termios2 struct {
	Iflag, Oflag, Cflag, Lflag uint32
	Line                       uint8
	Cc                         [19]uint8
	Ispeed, Ospeed             uint32
}

// Port is an open serial port.
type Port struct {
	f *os.File
}

// Open opens a serial port at a baud rate, with an exclusive lock on it until it closes. rts sets
// the RTS line, on a port that has one.
func Open(path string, baud int, rts bool) (*Port, error) {
	f, err := os.OpenFile(path, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}
	p := &Port{f: f}
	err = p.control(func(fd uintptr) error {
		// An advisory lock, as pyserial's exclusive=True: it ends when the port closes, and it
		// keeps out a Klipper host that opens the same port.
		if err := syscall.Flock(int(fd), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			return err
		}
		t, err := getTermios(fd)
		if err != nil {
			return err
		}
		// Raw mode (cfmakeraw), 8N1, with the receiver on and the modem lines ignored.
		t.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR |
			syscall.IGNCR | syscall.ICRNL | syscall.IXON | syscall.IXOFF | syscall.IXANY
		t.Oflag &^= syscall.OPOST
		t.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
		t.Cflag &^= syscall.CSIZE | syscall.PARENB | syscall.CSTOPB | crtscts
		t.Cflag |= syscall.CS8 | syscall.CREAD | syscall.CLOCAL
		t.Cc[syscall.VMIN], t.Cc[syscall.VTIME] = 1, 0
		setBaud(t, baud)
		return setTermios(fd, t)
	})
	if err == nil {
		// A pseudo-terminal has no modem lines (ENOTTY). Klipper opens those with no RTS too.
		if err = p.SetRTS(rts); errors.Is(err, syscall.ENOTTY) {
			err = nil
		}
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return p, nil
}

func ioctl(fd, req, arg uintptr) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg); e != 0 {
		return e
	}
	return nil
}

func getTermios(fd uintptr) (*termios2, error) {
	t := &termios2{}
	return t, ioctl(fd, tcgets2, uintptr(unsafe.Pointer(t)))
}

func setTermios(fd uintptr, t *termios2) error {
	return ioctl(fd, tcsets2, uintptr(unsafe.Pointer(t)))
}

func setBaud(t *termios2, baud int) {
	t.Cflag &^= cbaud
	t.Cflag |= bother
	t.Ispeed, t.Ospeed = uint32(baud), uint32(baud)
}

// control runs f with the file descriptor, and keeps the file in the runtime poller.
func (p *Port) control(f func(fd uintptr) error) error {
	rc, err := p.f.SyscallConn()
	if err != nil {
		return err
	}
	var ferr error
	if err := rc.Control(func(fd uintptr) { ferr = f(fd) }); err != nil {
		return err
	}
	return ferr
}

func (p *Port) Read(b []byte) (int, error)  { return p.f.Read(b) }
func (p *Port) Write(b []byte) (int, error) { return p.f.Write(b) }
func (p *Port) Close() error                { return p.f.Close() }

// SetBaud changes the baud rate.
func (p *Port) SetBaud(baud int) error {
	return p.control(func(fd uintptr) error {
		t, err := getTermios(fd)
		if err != nil {
			return err
		}
		setBaud(t, baud)
		return setTermios(fd, t)
	})
}

func (p *Port) setLine(line uintptr, on bool) error {
	req := uintptr(syscall.TIOCMBIC)
	if on {
		req = syscall.TIOCMBIS
	}
	bits := uint32(line)
	return p.control(func(fd uintptr) error { return ioctl(fd, req, uintptr(unsafe.Pointer(&bits))) })
}

// SetDTR sets the DTR line. On an Arduino style board, a change of DTR resets the board.
func (p *Port) SetDTR(on bool) error { return p.setLine(syscall.TIOCM_DTR, on) }

// SetRTS sets the RTS line.
func (p *Port) SetRTS(on bool) error { return p.setLine(syscall.TIOCM_RTS, on) }

// ClearHUPCL keeps DTR on when the port closes, so the close does not reset the board.
func (p *Port) ClearHUPCL() error {
	return p.control(func(fd uintptr) error {
		t, err := getTermios(fd)
		if err != nil {
			return err
		}
		t.Cflag &^= syscall.HUPCL
		return setTermios(fd, t)
	})
}

// FlushOutput drops the bytes that are not sent yet (tcflush TCOFLUSH).
func (p *Port) FlushOutput() error {
	return p.control(func(fd uintptr) error { return ioctl(fd, tcflsh, tcoflsh) })
}

// ReadFor reads what arrives within d, as the reads of pyserial with a timeout of 0 after a pause.
func (p *Port) ReadFor(d time.Duration) ([]byte, error) {
	var out []byte
	buf := make([]byte, 4096)
	end := time.Now().Add(d)
	for {
		if err := p.f.SetReadDeadline(end); err != nil {
			return out, err
		}
		n, err := p.f.Read(buf)
		out = append(out, buf[:n]...)
		if errors.Is(err, os.ErrDeadlineExceeded) {
			p.f.SetReadDeadline(time.Time{})
			return out, nil
		}
		if err != nil {
			return out, err
		}
	}
}
