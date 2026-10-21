package printer

import (
	"bufio"
	"errors"
	"os"
	"strings"

	"printdeck/internal/board"
)

// NeedsSetup reports that a printer.cfg has no [mcu] section, and no include of a file that can
// have one. The include of printdeck.cfg, which has only the sections of the deck, does not count.
// The placeholder that the image writes at the first start is such a file.
func NeedsSetup(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		l := strings.ToLower(strings.TrimSpace(s.Text()))
		if !strings.HasPrefix(l, "[") {
			continue
		}
		if l == "[mcu]" || strings.HasPrefix(l, "[mcu ") {
			return false
		}
		if strings.HasPrefix(l, "[include ") && !strings.Contains(l, "/usr/share/printdeck/klipper/printdeck.cfg") {
			return false
		}
	}
	return true
}

// Boards returns what the last probe found, and true while a probe runs.
func (k *Klippy) Boards() ([]board.Info, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.boards, k.probing
}

// ProbeBoards looks for a board with Klipper firmware on the USB serial ports, in the background.
// It probes only while the state asks for the setup: then Klipper does not use a port.
func (k *Klippy) ProbeBoards() {
	k.mu.Lock()
	if k.probing || !k.st.NeedsSetup {
		k.mu.Unlock()
		return
	}
	k.probing = true
	notify := k.notify
	k.mu.Unlock()
	if notify != nil {
		notify()
	}
	go func() {
		var found []board.Info
		for _, p := range board.Ports() {
			found = append(found, board.Probe(p, board.Baud))
		}
		k.mu.Lock()
		k.boards, k.probing = found, false
		k.mu.Unlock()
		if notify != nil {
			notify()
		}
	}()
}

// RestartKlipper restarts the Klipper service, which reads printer.cfg again.
func (k *Klippy) RestartKlipper() error {
	if k.RestartService == nil {
		return errors.New("Klipper cannot be restarted here")
	}
	return k.RestartService()
}

// MCUSerial returns the serial port of [mcu] in a Klipper config file, or "".
func MCUSerial(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	inMCU := false
	for s.Scan() {
		l := strings.TrimSpace(s.Text())
		if strings.HasPrefix(l, "[") {
			inMCU = strings.ToLower(l) == "[mcu]"
			continue
		}
		if k, v, ok := strings.Cut(l, ":"); inMCU && ok && strings.TrimSpace(k) == "serial" {
			v, _, _ = strings.Cut(v, "#")
			return strings.TrimSpace(v)
		}
	}
	return ""
}
