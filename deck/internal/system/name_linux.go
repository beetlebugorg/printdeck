package system

import (
	"os"
	"path/filepath"
	"strings"
)

// Paths of the name. Tests change them.
var (
	dnssdDefault = "/usr/lib/systemd/dnssd/printdeck.dnssd"
	dnssdRun     = "/run/systemd/dnssd/printdeck.dnssd"
	mountConfig  = MountConfig
	reloadDNSSD  = func() error { return run("systemctl", "reload-or-restart", "systemd-resolved.service") }
)

// DeckName returns the name of the deck that slicers show: the Name of the service in /run, or
// else of the service of the image, or "Printdeck".
func DeckName() string {
	for _, f := range []string{dnssdRun, dnssdDefault} {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, l := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(l, "Name="); ok && v != "" {
				return strings.ReplaceAll(v, "%%", "%")
			}
		}
	}
	return "Printdeck"
}

// SetName gives the deck a new name: the service for the slicers, the host name, and name= and
// hostname= in printdeck.txt for the next boots. It runs in the helper (root).
func SetName(name string) error {
	name, err := CheckName(name)
	if err != nil {
		return err
	}
	host := HostLabel(name)
	if err := writeFile(filepath.Join(etcDir, "hostname"), []byte(host+"\n"), 0o644); err != nil {
		return err
	}
	if err := sethostname(host); err != nil {
		return err
	}
	if err := writeService(name); err != nil {
		return err
	}
	umount, err := mountConfig()
	if err != nil {
		return err
	}
	defer umount()
	file := filepath.Join(configDir, "printdeck.txt")
	text, _ := os.ReadFile(file)
	text = []byte(setKey(setKey(string(text), "hostname", host), "name", name))
	return os.WriteFile(file, text, 0o644)
}

// writeService writes the service for the slicers with the name in /run, and has systemd-resolved
// read it.
func writeService(name string) error {
	b, err := os.ReadFile(dnssdDefault)
	if err != nil {
		return err
	}
	var lines []string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "Name=") {
			l = "Name=" + strings.ReplaceAll(name, "%", "%%") // % starts a specifier of systemd
		}
		lines = append(lines, l)
	}
	if err := os.MkdirAll(filepath.Dir(dnssdRun), 0o755); err != nil {
		return err
	}
	if err := writeFile(dnssdRun, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return err
	}
	return reloadDNSSD()
}
