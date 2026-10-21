package board

import (
	"bytes"
	"errors"
	"math/rand/v2"
	"strings"
	"testing"
	"time"
)

// fakeAVR answers like the STK500v2 bootloader of an atmega2560 (stk500boot.c).
type fakeAVR struct {
	sig        [3]byte
	bootloader bool // the board has the bootloader
	flash      []byte
	addr       int
	out        []byte
	left       bool // the bootloader started the program
	noise      bool // the board sends bytes of its program before the bootloader answers
}

func newFakeAVR() *fakeAVR {
	f := &fakeAVR{sig: [3]byte{0x1e, 0x98, 0x01}, bootloader: true, flash: make([]byte, 256*1024)}
	for i := range f.flash {
		f.flash[i] = 0xff
	}
	return f
}

func (f *fakeAVR) SetDTR(bool) error { return nil }
func (f *fakeAVR) SetRTS(bool) error { return nil }

func (f *fakeAVR) ReadFor(time.Duration) ([]byte, error) {
	b := f.out
	f.out = nil
	return b, nil
}

func (f *fakeAVR) Write(b []byte) (int, error) {
	seq, m, _, ok := parseFrame(b)
	if !ok || !f.bootloader {
		return len(b), nil
	}
	ans := []byte{m[0], stkOK}
	switch m[0] {
	case cmdSignOn:
		ans = append(ans, 8)
		ans = append(ans, "AVRISP_2"...)
	case cmdReadSignature:
		ans = append(ans, f.sig[m[4]], stkOK)
	case cmdLoadAddress:
		f.addr = (int(m[1]&0x7f)<<24 | int(m[2])<<16 | int(m[3])<<8 | int(m[4])) * 2
	case cmdProgramFlash:
		n := int(m[1])<<8 | int(m[2])
		copy(f.flash[f.addr:], m[10:10+n])
		f.addr += n
	case cmdReadFlash:
		n := int(m[1])<<8 | int(m[2])
		ans = append(ans, f.flash[f.addr:f.addr+n]...)
		ans = append(ans, stkOK)
		f.addr += n
	case cmdLeaveProgmode:
		f.left = true
	}
	if f.noise {
		f.out = append(f.out, 0x1b, 0x7e, 0x00, 'k', 'l')
	}
	f.out = append(f.out, stkFrame(seq, ans)...)
	return len(b), nil
}

func TestFlashAVR(t *testing.T) {
	f := newFakeAVR()
	f.noise = true
	prog := make([]byte, 52570)
	for i := range prog {
		prog[i] = byte(rand.IntN(256))
	}
	last := 0
	err := FlashAVR(f, fw(prog), func(step string, done, total int) {
		if done < last || done > total {
			t.Errorf("progress %s %d/%d after %d", step, done, total, last)
		}
		last = done
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.flash[:len(prog)], prog) {
		t.Error("the flash differs from the program")
	}
	if f.flash[len(prog)] != 0xff || !f.left {
		t.Errorf("padding %02x, left %v", f.flash[len(prog)], f.left)
	}
}

func TestFlashAVRWrongChip(t *testing.T) {
	f := newFakeAVR()
	f.sig = [3]byte{0x1e, 0x97, 0x05} // atmega1284p
	err := FlashAVR(f, fw([]byte{1, 2}), func(string, int, int) {})
	if err == nil || !strings.Contains(err.Error(), "unknown chip") {
		t.Errorf("error %v", err)
	}
}

func TestFlashAVRNoBootloader(t *testing.T) {
	f := newFakeAVR()
	f.bootloader = false
	err := FlashAVR(f, fw([]byte{1, 2}), func(string, int, int) {})
	if err == nil || !strings.Contains(err.Error(), "no bootloader") {
		t.Errorf("error %v", err)
	}
}

func TestParseFrame(t *testing.T) {
	m := stkFrame(7, []byte{cmdSignOn, stkOK})
	bad := append([]byte(nil), m...)
	bad[len(bad)-1] ^= 1
	in := append(append([]byte("junk"), bad...), m...)
	seq, body, rest, ok := parseFrame(in)
	if !ok || seq != 7 || !bytes.Equal(body, []byte{cmdSignOn, stkOK}) || len(rest) != 0 {
		t.Errorf("seq %d body %x rest %x ok %v", seq, body, rest, ok)
	}
	if _, _, _, ok := parseFrame(m[:len(m)-1]); ok {
		t.Error("a part of a message is a message")
	}
}

// fw is the firmware of the tests: prog for an atmega2560.
func fw(prog []byte) func(string) ([]byte, error) {
	return func(chip string) ([]byte, error) {
		if chip != "atmega2560" {
			return nil, errors.New("no firmware for " + chip)
		}
		return prog, nil
	}
}
