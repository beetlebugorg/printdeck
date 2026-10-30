package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"printdeck/internal/chitu"
	"printdeck/internal/printer"
	"printdeck/internal/system"
	"printdeck/internal/wifi"
)

// setupHost is the system side of the setup screen on the Printdeck OS image. The system helper sets
// the Wi-Fi network. Other systems have no setup screen.
type setupHost struct {
	wifi     *wifi.Client
	done     string // this file says that the setup is finished
	stateDir string
	// restart ends the deck program, so that systemd starts it again with a new printer. The
	// setup sets restartAfter when it writes a new printer.
	restart      func()
	restartAfter bool
}

func newSetupHost(stateDir string) *setupHost {
	if osVersion() == "" {
		return nil
	}
	return &setupHost{wifi: wifi.New(), done: filepath.Join(stateDir, "setup-done"), stateDir: stateDir}
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
func (h *setupHost) Done() {
	os.WriteFile(h.done, []byte("done\n"), 0o644)
	if h.restartAfter && h.restart != nil {
		log.Print("setup: a new printer: the deck starts again")
		go func() {
			time.Sleep(time.Second) // the screen closes the setup first
			h.restart()
		}()
	}
}

// FindChiTu asks the networks of the deck for ChiTu boards (chitu.Discover).
func (h *setupHost) FindChiTu() ([]chitu.Info, error) {
	return chitu.Discover(context.Background(), 4*time.Second)
}

// CheckChiTu asks the board at addr for its board and its state, with queries that only read.
func (h *setupHost) CheckChiTu(addr string) (chitu.Info, chitu.Status, error) {
	return checkChiTu(addr)
}

// checkChiTu asks the board at addr ("host" or "host:port") for M99999 and M4000.
func checkChiTu(addr string) (chitu.Info, chitu.Status, error) {
	if !strings.Contains(addr, ":") {
		addr += ":" + strconv.Itoa(chitu.Port)
	}
	c := &chitu.Client{Addr: addr}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	in, err := c.Info(ctx)
	if err != nil {
		return chitu.Info{}, chitu.Status{}, err
	}
	st, err := c.Status(ctx)
	return in, st, err
}

// Printer is printer.json of the state folder.
func (h *setupHost) Printer() printer.Setting {
	s, _ := printer.LoadSetting(h.stateDir)
	return s
}

// SetPrinter writes printer.json. The deck starts again after Done.
func (h *setupHost) SetPrinter(s printer.Setting) error {
	if err := printer.SaveSetting(h.stateDir, s); err != nil {
		return err
	}
	h.restartAfter = true
	return nil
}

// Name is the name of the deck that slicers show. SetName changes it with the system helper.
func (h *setupHost) Name() string { return system.DeckName() }
func (h *setupHost) SetName(name string) error {
	return system.AskHelper(system.HelperRequest{Action: "name", Name: name})
}

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
