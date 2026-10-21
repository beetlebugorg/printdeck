package board

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"printdeck/internal/klipper/serialport"
)

// FirmwareDir has the Klipper firmware of the image: klipper-CHIP.bin for each chip, and the file
// version (package klipper-firmware of the OS).
var FirmwareDir = "/usr/share/printdeck/firmware"

// Firmware returns the firmware of the image for a chip, for example "atmega2560".
func Firmware(chip string) ([]byte, error) {
	b, err := os.ReadFile(filepath.Join(FirmwareDir, "klipper-"+chip+".bin"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("Printdeck has no Klipper firmware for the " + chip)
	}
	return b, err
}

// CanFlash says if the image has firmware for a chip. An empty chip (a board with no Klipper
// firmware yet, so the chip is not known) is true when the image has any firmware.
func CanFlash(chip string) bool {
	if chip == "" {
		m, _ := filepath.Glob(filepath.Join(FirmwareDir, "klipper-*.bin"))
		return len(m) > 0
	}
	_, err := os.Stat(filepath.Join(FirmwareDir, "klipper-"+chip+".bin"))
	return err == nil
}

// FirmwareVersion is the Klipper version of the firmware of the image.
func FirmwareVersion() string {
	b, _ := os.ReadFile(filepath.Join(FirmwareDir, "version"))
	return strings.TrimSpace(string(b))
}

// Flash writes the Klipper firmware of the image to the board on device, through the bootloader of
// the board. The Klipper host must not use the port.
func Flash(device string, progress FlashProgress) error {
	p, err := serialport.Open(device, 115200, false)
	if err != nil {
		return err
	}
	defer p.Close()
	return FlashAVR(p, Firmware, progress)
}
