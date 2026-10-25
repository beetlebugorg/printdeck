package printer

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realCfg is a printer.cfg of a real printer, with the bytes that a rewrite can change: no newline
// at the end, CRLF line ends, and a SAVE_CONFIG block.
const realCfg = "[mcu]\r\nserial: /dev/serial/by-id/usb-UltiMachine_RAMBo-if00\r\n\r\n[printer]\nkinematics: cartesian\n" +
	"\n[include /usr/share/printdeck/klipper/printdeck.cfg]\n\n#*# <---------------------- SAVE_CONFIG ---------------------->\n#*# [extruder]\n#*# pid_kp = 21.5"

// A switch to each simulated printer and back gives the real printer.cfg byte for byte, with its
// mode. A switch from one simulated printer to another keeps the real file.
func TestSimRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "printer.cfg")
	if err := os.WriteFile(path, []byte(realCfg), 0o640); err != nil {
		t.Fatal(err)
	}
	if SimulatedName(path) != "" {
		t.Fatal("the real printer.cfg is a simulated printer")
	}
	for _, m := range SimModels() {
		if err := UseSim(path, m.ID); err != nil {
			t.Fatal(err)
		}
		if got := SimulatedName(path); got != m.Name {
			t.Errorf("%s: name %q", m.ID, got)
		}
		if MCUSerial(path) != "sim" {
			t.Errorf("%s: serial %q", m.ID, MCUSerial(path))
		}
		b, _ := os.ReadFile(path)
		if !strings.HasSuffix(string(b), "[include /usr/share/printdeck/klipper/printdeck.cfg]\n") {
			t.Errorf("%s: no include of the Printdeck sections at the end", m.ID)
		}
	}
	if b, _ := os.ReadFile(path + ".real"); string(b) != realCfg {
		t.Fatal("printer.cfg.real is not the real printer.cfg")
	}
	if err := LeaveSim(path); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, []byte(realCfg)) {
		t.Errorf("the restored printer.cfg differs:\n%q", b)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	if _, err := os.Stat(path + ".real"); !os.IsNotExist(err) {
		t.Error("printer.cfg.real is still there")
	}
	if err := LeaveSim(path); err == nil {
		t.Error("LeaveSim with the real printer.cfg: no error")
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, []byte(realCfg)) {
		t.Error("LeaveSim changed the real printer.cfg")
	}
}

// After a new real config (Choose printer) during a simulation, the next switch keeps the new
// config, and the old printer.cfg.real goes.
func TestSimNewRealConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "printer.cfg")
	os.WriteFile(path, []byte(realCfg), 0o644)
	if err := UseSim(path, "cr10-v3"); err != nil {
		t.Fatal(err)
	}
	newer := "[mcu]\nserial: /dev/ttyUSB0\n"
	os.WriteFile(path, []byte(newer), 0o644)
	if err := UseSim(path, "taz-workhorse"); err != nil {
		t.Fatal(err)
	}
	if err := LeaveSim(path); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != newer {
		t.Errorf("printer.cfg %q, want the newer config", b)
	}
}

// With no printer.cfg, the switch back removes the file of the simulated printer. The include is
// the include of the deck.
func TestSimNoConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "printer.cfg")
	if err := UseSim(path, "taz-workhorse"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), deckInclude) {
		t.Error("no include of the deck")
	}
	if err := LeaveSim(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("printer.cfg is still there")
	}
}

// The include of the desktop app stays as it is.
func TestSimAppInclude(t *testing.T) {
	path := filepath.Join(t.TempDir(), "printer.cfg")
	os.WriteFile(path, []byte("# my printer\n[include printdeck.cfg]\n"), 0o644)
	if err := UseSim(path, "cr10-v3"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.HasSuffix(string(b), "\n[include printdeck.cfg]\n") || strings.Contains(string(b), deckInclude) {
		t.Errorf("the include of the app is not at the end:\n%s", b[len(b)-120:])
	}
	if err := UseSim(path, "nope"); err == nil {
		t.Error("an unknown simulated printer: no error")
	}
}

// Each simulated printer has its marker line, the simulated board, and only the thermistor that
// the board simulates.
func TestSimConfigs(t *testing.T) {
	for _, m := range SimModels() {
		text, err := SimConfig(m.ID)
		if err != nil {
			t.Fatal(err)
		}
		if first, _, _ := strings.Cut(text, "\n"); first != SimMarker+m.ID {
			t.Errorf("%s: first line %q", m.ID, first)
		}
		if !strings.Contains(text, "\nserial: sim\n") {
			t.Errorf("%s: no serial: sim", m.ID)
		}
		for _, l := range strings.Split(text, "\n") {
			if v, ok := strings.CutPrefix(l, "sensor_type:"); ok && !strings.HasPrefix(strings.TrimSpace(v), "Generic 3950") {
				t.Errorf("%s: %s", m.ID, l)
			}
		}
	}
}
