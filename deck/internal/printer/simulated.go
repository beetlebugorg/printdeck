package printer

import (
	"bufio"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The simulated printers: the config of a real printer on the simulated board of the deck
// (serial: sim). The board runs in the Klipper host, so a deck with no printer can heat, home and
// print. sim/make.sh makes the configs.
//
// UseSim moves the real printer.cfg to printer.cfg.real, and writes the config of the simulated
// printer as printer.cfg. LeaveSim moves printer.cfg.real back. The rename keeps the real file
// byte for byte, and the switch writes only the config of the simulated printer.

//go:embed sim/*.cfg
var simConfigs embed.FS

// SimModel is a simulated printer.
type SimModel struct {
	ID   string // the name of its config in sim/, for example "cr10-v3"
	Name string // for example "Creality CR-10 V3"
	Note string // a short description for the list
}

// simModels are the simulated printers, in the order of the list.
var simModels = []SimModel{
	{ID: "taz-workhorse", Name: "LulzBot TAZ Workhorse", Note: "280 x 280 mm bed, probe on washers, bed tilt"},
	{ID: "cr10-v3", Name: "Creality CR-10 V3", Note: "300 x 300 mm bed, endstops, no probe"},
}

// SimModels returns the simulated printers.
func SimModels() []SimModel { return simModels }

// SimModelID returns the ID of the simulated printer with a name (State.Simulated), or "".
func SimModelID(name string) string {
	for _, m := range simModels {
		if m.Name == name {
			return m.ID
		}
	}
	return ""
}

// SimMarker is the first line of the config of a simulated printer, before its ID.
const SimMarker = "# Printdeck simulated printer: "

// SimConfig returns the config of a simulated printer, with no include of the Printdeck sections.
func SimConfig(id string) (string, error) {
	b, err := simConfigs.ReadFile("sim/" + id + ".cfg")
	if err != nil {
		return "", fmt.Errorf("no simulated printer %q", id)
	}
	return string(b), nil
}

// realSuffix is the suffix of the real printer.cfg while the simulated printer is in use.
const realSuffix = ".real"

// deckInclude is the include of the Printdeck sections on the deck.
const deckInclude = "[include /usr/share/printdeck/klipper/printdeck.cfg]"

// SimulatedName returns the name of the simulated printer of a printer.cfg, or "" when the file
// is for a real printer. A config with "serial: sim" and no marker line is "Simulated printer".
func SimulatedName(path string) string {
	if MCUSerial(path) != "sim" {
		return ""
	}
	if id := simID(path); id != "" {
		for _, m := range simModels {
			if m.ID == id {
				return m.Name
			}
		}
	}
	return "Simulated printer"
}

// simID returns the ID of the marker line at the top of a config, or "".
func simID(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	if s.Scan() {
		if id, ok := strings.CutPrefix(s.Text(), SimMarker); ok {
			return strings.TrimSpace(id)
		}
	}
	return ""
}

// includeLine returns the include of the Printdeck sections in a config, or the include of the
// deck when the config has none.
func includeLine(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return deckInclude
	}
	for _, l := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[include ") && strings.HasSuffix(strings.TrimSuffix(t, "]"), "printdeck.cfg") {
			return t
		}
	}
	return deckInclude
}

// UseSim writes the config of a simulated printer as printer.cfg at path. When printer.cfg is for
// a real printer, it first moves it to printer.cfg.real. When printer.cfg is for a simulated
// printer already, printer.cfg.real stays as it is.
func UseSim(path, id string) error {
	text, err := SimConfig(id)
	if err != nil {
		return err
	}
	text += "\n# Added by Printdeck: keep this line last.\n" + includeLine(path) + "\n"
	real := path + realSuffix
	moved := false
	if MCUSerial(path) != "sim" {
		switch _, err := os.Stat(path); {
		case err == nil:
			if err := os.Rename(path, real); err != nil {
				return err
			}
			moved = true
		case errors.Is(err, os.ErrNotExist):
			// No printer.cfg: LeaveSim removes the file of the simulated printer.
			if err := os.Remove(real); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		default:
			return err
		}
	}
	tmp := filepath.Join(filepath.Dir(path), ".printer.cfg.new")
	err = os.WriteFile(tmp, []byte(text), 0o664)
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil && moved {
		// The real printer.cfg goes back to its place.
		os.Rename(real, path)
	}
	return err
}

// LeaveSim puts the real printer.cfg back: it moves printer.cfg.real to printer.cfg. With no
// printer.cfg.real, it removes printer.cfg, as the deck had no printer.cfg before UseSim. It
// returns an error when printer.cfg is for a real printer.
func LeaveSim(path string) error {
	if _, err := os.Stat(path); err == nil && MCUSerial(path) != "sim" {
		return errors.New("printer.cfg is not a simulated printer")
	}
	err := os.Rename(path+realSuffix, path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return err
}
