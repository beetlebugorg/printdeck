package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"printdeck/internal/wifi"
)

// setupHost is the system side of the setup screen on the Printdeck OS image. The image has the
// scripts printdeck-wifi and printdeck-user. Other systems, like Docker, have no setup screen.
type setupHost struct {
	wifi *wifi.Client
	done string // this file says that the setup is finished
}

func newSetupHost(stateDir string) *setupHost {
	if _, err := exec.LookPath("printdeck-user"); err != nil {
		return nil
	}
	return &setupHost{wifi: wifi.New(), done: filepath.Join(stateDir, "setup-done")}
}

// Needed says if the setup screen opens at the start.
func (h *setupHost) Needed() bool {
	_, err := os.Stat(h.done)
	return err != nil
}

func (h *setupHost) Scan() error                       { return h.wifi.Scan() }
func (h *setupHost) Networks() ([]wifi.Network, error) { return h.wifi.Networks() }
func (h *setupHost) Status() (wifi.Status, error)      { return h.wifi.Status() }
func (h *setupHost) SetWifi(ssid, pass string) error   { return runScript("printdeck-wifi", ssid, pass) }
func (h *setupHost) Done()                             { os.WriteFile(h.done, []byte("done\n"), 0o644) }

// Login is the user that printdeck-credentials made, and its password.
func (h *setupHost) Login() (string, string) {
	b, err := os.ReadFile("/var/lib/printdeck/password")
	if err != nil {
		return "", ""
	}
	return "printdeck", strings.TrimSpace(string(b))
}

// runScript starts a command and returns its error message as the error.
func runScript(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return errors.New(msg)
		}
		return err
	}
	return nil
}
