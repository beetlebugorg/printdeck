package main

import (
	"os"
	"path/filepath"
	"strings"

	"printdeck/internal/system"
	"printdeck/internal/wifi"
)

// setupHost is the system side of the setup screen on the Printdeck OS image. The system helper sets
// the Wi-Fi network. Other systems have no setup screen.
type setupHost struct {
	wifi *wifi.Client
	done string // this file says that the setup is finished
}

func newSetupHost(stateDir string) *setupHost {
	if osVersion() == "" {
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
func (h *setupHost) SetWifi(ssid, pass string) error {
	return system.AskHelper(system.HelperRequest{Action: "wifi", SSID: ssid, Password: pass})
}
func (h *setupHost) Done() { os.WriteFile(h.done, []byte("done\n"), 0o644) }

// Login is the password that printdeck-credentials made, and the user with this password in the
// debug image.
func (h *setupHost) Login() (string, string) {
	b, err := os.ReadFile("/var/lib/printdeck/password")
	if err != nil {
		return "", ""
	}
	user := ""
	if _, err := os.Stat("/usr/bin/printdeck-user"); err == nil {
		user = "printdeck"
	}
	return user, strings.TrimSpace(string(b))
}
