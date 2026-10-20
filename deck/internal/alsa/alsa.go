// Package alsa plays sound and sets the mixer controls of a sound card through the ALSA kernel
// interface (/dev/snd), in pure Go. It covers what Printdeck needs: interleaved playback on a
// hardware device ("hw:CARD,DEV") and the integer controls (the volume of the amp). It has none of
// alsa-lib's plugins.
//
// The structures and the ioctl numbers are those of the kernel header sound/asound.h for 64-bit
// Linux. tools/alsa-abi prints them from the real header, and the test compares them.
package alsa

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// The ioctl numbers (sound/asound.h).
const (
	ioctlPVersion    = 0x80044100
	ioctlHWRefine    = 0xc2604110
	ioctlHWParams    = 0xc2604111
	ioctlSWParams    = 0xc0884113
	ioctlDelay       = 0x80084121
	ioctlPrepare     = 0x00004140
	ioctlDrop        = 0x00004143
	ioctlWriteIFrame = 0x40184150
	ioctlElemInfo    = 0xc1105511
	ioctlElemRead    = 0xc4c85512
	ioctlElemWrite   = 0xc4c85513
)

func ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	for {
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg))
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return errno
		}
		return nil
	}
}

// ParseDevice reads a device name: "hw:CARD,DEV", "hw:CARD" or "plughw:..." (as "hw"). CARD is a
// number or the id of the card (for example "sndrpihifiberry").
func ParseDevice(name string) (card, device int, err error) {
	rest, ok := strings.CutPrefix(name, "hw:")
	if !ok {
		rest, ok = strings.CutPrefix(name, "plughw:")
	}
	if !ok {
		return 0, 0, fmt.Errorf("alsa: unsupported device %q: only hw:CARD,DEV", name)
	}
	cardName, devName, _ := strings.Cut(rest, ",")
	if devName != "" {
		if device, err = strconv.Atoi(devName); err != nil {
			return 0, 0, fmt.Errorf("alsa: bad device in %q", name)
		}
	}
	card, err = CardIndex(cardName)
	return card, device, err
}

// CardIndex returns the index of a card from its number or its id.
func CardIndex(card string) (int, error) {
	if n, err := strconv.Atoi(card); err == nil {
		return n, nil
	}
	// /proc/asound/ID is a link to cardN.
	target, err := os.Readlink(filepath.Join("/proc/asound", card))
	if err != nil {
		return 0, fmt.Errorf("alsa: no card %q", card)
	}
	n, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(target), "card"))
	if err != nil {
		return 0, fmt.Errorf("alsa: no card %q", card)
	}
	return n, nil
}

// openDevice opens a device file. The open does not wait for a busy device.
func openDevice(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("alsa: open %s: %w", path, err)
	}
	// Writes and reads wait (as alsa-lib does after the open).
	if err := syscall.SetNonblock(fd, false); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

// ErrUnderrun is a write after the buffer ran empty (EPIPE).
var ErrUnderrun = errors.New("alsa: underrun")
