package board

import (
	"bytes"
	"errors"
	"fmt"
	"time"
)

// The STK500 version 2 protocol of the Arduino Mega bootloader (stk500boot), which avrdude calls
// "wiring". The boards with an atmega2560 have it: RAMBo, RAMPS, MKS Gen. The bootloader runs for
// about one second after a reset. The deck resets the board with the DTR line.

const (
	stkStart = 0x1b
	stkToken = 0x0e

	cmdSignOn        = 0x01
	cmdLoadAddress   = 0x06
	cmdEnterProgmode = 0x10
	cmdLeaveProgmode = 0x11
	cmdProgramFlash  = 0x13
	cmdReadFlash     = 0x14
	cmdReadSignature = 0x1b

	stkOK = 0x00
)

// stkPort is the serial port of the flash. serialport.Port has these methods.
type stkPort interface {
	Write(b []byte) (int, error)
	ReadFor(d time.Duration) ([]byte, error)
	SetDTR(on bool) error
	SetRTS(on bool) error
}

// Chip is an AVR chip that the flash knows.
type Chip struct {
	Name      string
	Signature [3]byte
	Flash     int // bytes of flash for the program; the bootloader is above
	Page      int // bytes of a flash page
}

// chips are the chips of the AVR firmware of the image.
var chips = []Chip{
	{Name: "atmega2560", Signature: [3]byte{0x1e, 0x98, 0x01}, Flash: 256*1024 - 8*1024, Page: 256},
}

// stk500v2 is one session with the bootloader.
type stk500v2 struct {
	port stkPort
	seq  byte
	buf  []byte // bytes after the last frame
}

// frame makes a message of the protocol.
func stkFrame(seq byte, body []byte) []byte {
	m := []byte{stkStart, seq, byte(len(body) >> 8), byte(len(body)), stkToken}
	m = append(m, body...)
	var sum byte
	for _, b := range m {
		sum ^= b
	}
	return append(m, sum)
}

// parseFrame takes one message from the start of b. It returns the body, the rest of b, and ok
// false when b has no complete message yet. It drops bytes before a start byte, and bad messages.
func parseFrame(b []byte) (seq byte, body, rest []byte, ok bool) {
	for {
		i := bytes.IndexByte(b, stkStart)
		if i < 0 {
			return 0, nil, nil, false
		}
		b = b[i:]
		if len(b) < 6 {
			return 0, nil, b, false
		}
		n := int(b[2])<<8 | int(b[3])
		if b[4] != stkToken || n > 1024 {
			b = b[1:]
			continue
		}
		if len(b) < 6+n {
			return 0, nil, b, false
		}
		var sum byte
		for _, c := range b[:6+n] {
			sum ^= c
		}
		if sum != 0 {
			b = b[1:]
			continue
		}
		return b[1], append([]byte(nil), b[5:5+n]...), b[6+n:], true
	}
}

// call sends a command and waits for its answer, which starts with the command and the status OK.
func (s *stk500v2) call(body []byte, timeout time.Duration) ([]byte, error) {
	s.seq++
	if _, err := s.port.Write(stkFrame(s.seq, body)); err != nil {
		return nil, err
	}
	end := time.Now().Add(timeout)
	for time.Now().Before(end) {
		if seq, ans, rest, ok := parseFrame(s.buf); ok {
			s.buf = rest
			if seq != s.seq || len(ans) < 2 || ans[0] != body[0] {
				continue
			}
			if ans[1] != stkOK {
				return nil, fmt.Errorf("bootloader: command 0x%02x failed (0x%02x)", body[0], ans[1])
			}
			return ans, nil
		}
		b, err := s.port.ReadFor(20 * time.Millisecond)
		if err != nil {
			return nil, err
		}
		s.buf = append(s.buf, b...)
	}
	return nil, errors.New("bootloader: no answer")
}

// resetToBootloader resets the board with the DTR and RTS lines, and signs on to the bootloader in
// the time that it waits. The bootloader misses a message that comes in the first 50 ms or so
// after the reset (a CR-10 V3 board), so the deck waits, and tries again with a longer wait.
func (s *stk500v2) resetToBootloader() error {
	for _, wait := range []time.Duration{200 * time.Millisecond, 400 * time.Millisecond} {
		s.port.SetDTR(false)
		s.port.SetRTS(false)
		time.Sleep(250 * time.Millisecond)
		s.port.SetDTR(true)
		s.port.SetRTS(true)
		time.Sleep(wait)
		s.port.ReadFor(0) // drop the bytes of the program before the reset
		s.buf = nil
		for range 8 {
			if _, err := s.call([]byte{cmdSignOn}, 100*time.Millisecond); err == nil {
				return nil
			}
		}
	}
	return errors.New("the board does not answer. It has no bootloader, or the bootloader is not STK500v2")
}

// chip reads the signature of the chip.
func (s *stk500v2) chip() (Chip, error) {
	var sig [3]byte
	for i := range sig {
		ans, err := s.call([]byte{cmdReadSignature, 4, 0x30, 0, byte(i), 0}, time.Second)
		if err != nil {
			return Chip{}, err
		}
		if len(ans) < 3 {
			return Chip{}, errors.New("bootloader: short signature answer")
		}
		sig[i] = ans[2]
	}
	for _, c := range chips {
		if c.Signature == sig {
			return c, nil
		}
	}
	return Chip{}, fmt.Errorf("unknown chip, signature %02x %02x %02x", sig[0], sig[1], sig[2])
}

func (s *stk500v2) loadAddress(byteAddr int) error {
	w := byteAddr / 2
	_, err := s.call([]byte{cmdLoadAddress, byte(w>>24) | 0x80, byte(w >> 16), byte(w >> 8), byte(w)}, time.Second)
	return err
}

// FlashProgress tells how far a flash is: the step and the bytes done of all bytes.
type FlashProgress func(step string, done, total int)

// flash writes the program to the chip page by page, then reads it back and compares.
func (s *stk500v2) flash(c Chip, prog []byte, progress FlashProgress) error {
	if len(prog) > c.Flash {
		return fmt.Errorf("the firmware has %d bytes, and the %s has room for %d", len(prog), c.Name, c.Flash)
	}
	data := append([]byte(nil), prog...)
	for len(data)%c.Page != 0 {
		data = append(data, 0xff)
	}
	if _, err := s.call([]byte{cmdEnterProgmode, 200, 100, 25, 32, 0, 0x53, 3, 0xac, 0x53, 0, 0}, time.Second); err != nil {
		return err
	}
	for a := 0; a < len(data); a += c.Page {
		if err := s.loadAddress(a); err != nil {
			return err
		}
		cmd := []byte{cmdProgramFlash, byte(c.Page >> 8), byte(c.Page), 0xc1, 10, 0x40, 0x4c, 0x20, 0, 0}
		if _, err := s.call(append(cmd, data[a:a+c.Page]...), 2*time.Second); err != nil {
			return err
		}
		progress("Writing", a+c.Page, 2*len(data))
	}
	for a := 0; a < len(data); a += c.Page {
		if err := s.loadAddress(a); err != nil {
			return err
		}
		ans, err := s.call([]byte{cmdReadFlash, byte(c.Page >> 8), byte(c.Page), 0x20}, 2*time.Second)
		if err != nil {
			return err
		}
		if len(ans) < 2+c.Page || !bytes.Equal(ans[2:2+c.Page], data[a:a+c.Page]) {
			return fmt.Errorf("the check after the write failed at address 0x%05x", a)
		}
		progress("Checking", len(data)+a+c.Page, 2*len(data))
	}
	_, err := s.call([]byte{cmdLeaveProgmode, 1, 1}, time.Second)
	return err
}

// FlashAVR resets the board on the port, reads its chip, and writes the program of firmware(chip)
// with the STK500v2 bootloader. The program starts after the write.
func FlashAVR(port stkPort, firmware func(chip string) ([]byte, error), progress FlashProgress) error {
	s := &stk500v2{port: port}
	progress("Starting the bootloader", 0, 1)
	if err := s.resetToBootloader(); err != nil {
		return err
	}
	c, err := s.chip()
	if err == nil {
		var prog []byte
		if prog, err = firmware(c.Name); err == nil {
			return s.flash(c, prog, progress)
		}
	}
	s.call([]byte{cmdLeaveProgmode, 1, 1}, time.Second)
	return err
}

// CheckAVR resets the board on the port into its STK500v2 bootloader, reads its chip, and leaves
// the bootloader. It writes nothing.
func CheckAVR(port stkPort) (Chip, error) {
	s := &stk500v2{port: port}
	if err := s.resetToBootloader(); err != nil {
		return Chip{}, err
	}
	c, err := s.chip()
	s.call([]byte{cmdLeaveProgmode, 1, 1}, time.Second)
	return c, err
}
