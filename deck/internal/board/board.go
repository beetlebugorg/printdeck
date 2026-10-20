// Package board finds the control board of the printer on the USB serial ports, and reads the
// identity of its Klipper firmware: the MCU type and the firmware version.
package board

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"printdeck/internal/klipper/reactor"
	"printdeck/internal/klipper/serialhdl"
)

// Baud is the serial speed of a board with a USB to serial chip, such as a RAMBo or a board with a
// CH340. Klipper's firmware uses 250000 by default. A board with native USB ignores it.
const Baud = 250000

// Info is what a probe found on a port.
type Info struct {
	Device string // the stable path, for example /dev/serial/by-id/usb-...
	Name   string // the name of the USB device, from the path

	Klipper       bool   // the board answered the Klipper protocol
	MCU           string // for example "atmega2560" or "stm32f103xe"
	Version       string // the firmware version, for example "v0.13.0-777-g7bc4d0946-..."
	BuildVersions string // the compiler versions of the firmware build
	Err           string // why the probe failed
}

// Ports returns the USB serial devices, by their stable paths in /dev/serial/by-id when the
// system has them, else by /dev/ttyACM* and /dev/ttyUSB*.
func Ports() []string {
	ports, _ := filepath.Glob("/dev/serial/by-id/*")
	if len(ports) == 0 {
		a, _ := filepath.Glob("/dev/ttyACM*")
		u, _ := filepath.Glob("/dev/ttyUSB*")
		ports = append(a, u...)
	}
	sort.Strings(ports)
	return ports
}

// Name returns a readable name for a port: the USB vendor and product of a by-id path.
func Name(device string) string {
	n := filepath.Base(device)
	if !strings.HasPrefix(device, "/dev/serial/by-id/") {
		return n
	}
	n = strings.TrimPrefix(n, "usb-")
	// The interface and the serial number are at the end: "..._75935323230351802211-if00".
	if i := strings.LastIndex(n, "-if"); i > 0 {
		n = n[:i]
	}
	if i := strings.LastIndex(n, "_"); i > 0 {
		n = n[:i]
	}
	return strings.Join(strings.FieldsFunc(n, func(r rune) bool { return r == '_' }), " ")
}

// Probe connects to a port as the Klipper host does, and reads the data dictionary of the
// firmware. It sends only the identify command. Opening the port resets an Arduino style board,
// as it does when the Klipper host connects. The port must have no other user, so call Probe only
// when Klipper does not use the port.
func Probe(device string, baud int) Info {
	info := Info{Device: device, Name: Name(device)}
	if _, err := os.Stat(device); err != nil {
		info.Err = err.Error()
		return info
	}
	r := reactor.New()
	r.RegisterCallback(func(float64) any {
		defer r.End()
		s := serialhdl.New(r, "mcu")
		if err := s.TryUART(device, baud, true); err != nil {
			info.Err = err.Error()
			return nil
		}
		defer s.Disconnect()
		p := s.MsgParser()
		info.Klipper = true
		info.Version, info.BuildVersions = p.Version()
		if mcu, ok := p.Constants()["MCU"]; ok {
			info.MCU = fmt.Sprint(mcu)
		}
		return nil
	}, reactor.Now)
	r.Run()
	return info
}

// SameVersion reports that a firmware version and the version of the host are the same Klipper
// commit. The firmware adds the build date and the build host after the commit, for example
// "v0.13.0-777-g7bc4d0946-20260930_185514-host". The host version is "v0.13.0-777-g7bc4d0946".
func SameVersion(firmware, host string) bool {
	return commit(firmware) != "" && commit(firmware) == commit(host)
}

// commit returns the "vX.Y.Z-N-gHASH" part of a version, or "".
func commit(v string) string {
	parts := strings.Split(v, "-")
	if len(parts) < 3 || !strings.HasPrefix(parts[0], "v") || !strings.HasPrefix(parts[2], "g") {
		return ""
	}
	return strings.Join(parts[:3], "-")
}
