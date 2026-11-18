package system

import (
	"errors"
	"path/filepath"
	"regexp"
)

// The printers of a deck with more than one (specs/many-printers-on-one-deck.md). The first
// printer uses klipper.service and /var/lib/klipper/config. Each other printer has the ID of its
// folder: klipper@<id>.service runs its Klipper host with /var/lib/klipper/printers/<id>, and
// the socket and the log in /run/klipper/<id>.

// printerID is the form of the ID of a printer: it is a part of a unit name and of a path.
var printerID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,15}$`)

// ValidPrinterID reports that id can be the ID of a printer that is not the first one.
func ValidPrinterID(id string) bool { return printerID.MatchString(id) && id != "config" }

// KlipperUnit returns the systemd unit of the Klipper host of the printer id: klipper.service for
// "" (the first printer), and klipper@<id>.service for another printer.
func KlipperUnit(id string) (string, error) {
	if id == "" {
		return "klipper.service", nil
	}
	if !ValidPrinterID(id) {
		return "", errors.New("the printer ID is not valid")
	}
	return "klipper@" + id + ".service", nil
}

// PrinterKlipperDir returns the folder of the Klipper host of the printer id: its printer.cfg and
// its gcodes folder.
func PrinterKlipperDir(id string) string {
	if id == "" {
		return klipperConfigDir
	}
	return filepath.Join(filepath.Dir(klipperConfigDir), "printers", id)
}

// addPrinter starts the Klipper host of the printer id, and turns it on at each start. The unit
// makes the folder of the printer (StateDirectory), and writes a placeholder printer.cfg as the
// user printer when there is none: the helper runs as root, and writes no file in the folders of
// the user printer. The deck then shows the printer setup for it.
func addPrinter(id string) error {
	unit, err := KlipperUnit(id)
	if err != nil || id == "" {
		return errors.New("the printer ID is not valid")
	}
	return run("systemctl", "enable", "--now", unit)
}

// removePrinter stops the Klipper host of the printer id, and turns it off. Its folder stays: its
// printer.cfg and its files come back if the printer is added again.
func removePrinter(id string) error {
	unit, err := KlipperUnit(id)
	if err != nil || id == "" {
		return errors.New("the printer ID is not valid")
	}
	return run("systemctl", "disable", "--now", unit)
}
