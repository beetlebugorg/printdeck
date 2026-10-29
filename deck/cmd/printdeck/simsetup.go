package main

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"

	"printdeck/internal/system"
	"printdeck/internal/wifi"
)

// simSetup is the setup host of the simulator in the desktop app: no Wi-Fi, and the address and
// the password of its remote screen, so that Wi-Fi and help shows them as on a deck.
type simSetup struct {
	addr, password string
}

func (s *simSetup) Scan() error                       { return nil }
func (s *simSetup) Networks() ([]wifi.Network, error) { return nil, nil }
func (s *simSetup) Status() (wifi.Status, error) {
	return wifi.Status{State: "COMPLETED", SSID: "This computer", IP: "127.0.0.1"}, nil
}
func (s *simSetup) SetWifi(string, string) error { return errors.New("The simulator has no Wi-Fi.") }
func (s *simSetup) Login() (string, string)      { return "", s.password }
func (s *simSetup) Done()                        {}

// Address is the address of the remote screen of the simulator.
func (s *simSetup) Address() string { return s.addr }

// simPassword returns the password of the remote screen of the simulator in dir: the file
// remote-password, made at the first start.
func simPassword(dir string) string {
	file := filepath.Join(dir, "remote-password")
	if b, err := os.ReadFile(file); err == nil && len(strings.TrimSpace(string(b))) > 0 {
		return strings.TrimSpace(string(b))
	}
	p, err := system.NewPassword()
	if err != nil {
		log.Printf("simulator: no password for the remote screen: %v", err)
		return ""
	}
	if err := os.WriteFile(file, []byte(p+"\n"), 0o600); err != nil {
		log.Printf("simulator: the password of the remote screen: %v", err)
	}
	return p
}
