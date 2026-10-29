//go:build linux

package system

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostLabel(t *testing.T) {
	for in, want := range map[string]string{
		"Workhorse":         "workhorse",
		"Workhorse 2":       "workhorse-2",
		"  Mini (garage)  ": "mini-garage",
		"Café":              "caf",
		"---":               "",
	} {
		if got := HostLabel(in); got != want {
			t.Errorf("HostLabel(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := CheckName("!!"); err == nil {
		t.Error("a name with no letter")
	}
	if n, err := CheckName("  Workhorse "); err != nil || n != "Workhorse" {
		t.Errorf("CheckName = %q, %v", n, err)
	}
}

// SetName sets the host name, the service for the slicers, and the keys of printdeck.txt.
func TestSetName(t *testing.T) {
	dir := t.TempDir()
	oldEtc, oldCfg, oldDef, oldRun, oldMount, oldReload, oldSet := etcDir, configDir, dnssdDefault, dnssdRun, mountConfig, reloadDNSSD, sethostname
	defer func() {
		etcDir, configDir, dnssdDefault, dnssdRun, mountConfig, reloadDNSSD, sethostname = oldEtc, oldCfg, oldDef, oldRun, oldMount, oldReload, oldSet
	}()
	etcDir, configDir = filepath.Join(dir, "etc"), filepath.Join(dir, "config")
	dnssdDefault, dnssdRun = filepath.Join(dir, "usr", "printdeck.dnssd"), filepath.Join(dir, "run", "printdeck.dnssd")
	os.MkdirAll(etcDir, 0o755)
	os.MkdirAll(configDir, 0o755)
	os.MkdirAll(filepath.Dir(dnssdDefault), 0o755)
	os.WriteFile(dnssdDefault, []byte("[Service]\nName=Printdeck\nType=_octoprint._tcp\nPort=80\n"), 0o644)
	os.WriteFile(filepath.Join(configDir, "printdeck.txt"), []byte("wifi_ssid=Home\nhostname=printdeck\ntimezone=UTC\n"), 0o644)
	mountConfig = func() (func(), error) { return func() {}, nil }
	reloads := 0
	reloadDNSSD = func() error { reloads++; return nil }
	host := ""
	sethostname = func(h string) error { host = h; return nil }

	if DeckName() != "Printdeck" {
		t.Errorf("default name %q", DeckName())
	}
	if err := SetName("Workhorse 100%"); err != nil {
		t.Fatal(err)
	}
	if host != "workhorse-100" || reloads != 1 {
		t.Errorf("host %q, %d reloads", host, reloads)
	}
	if b, _ := os.ReadFile(filepath.Join(etcDir, "hostname")); string(b) != "workhorse-100\n" {
		t.Errorf("/etc/hostname %q", b)
	}
	if b, _ := os.ReadFile(dnssdRun); !strings.Contains(string(b), "Name=Workhorse 100%%\n") || !strings.Contains(string(b), "Port=80") {
		t.Errorf("service %q", b)
	}
	if n := DeckName(); n != "Workhorse 100%" {
		t.Errorf("name %q", n)
	}
	b, _ := os.ReadFile(filepath.Join(configDir, "printdeck.txt"))
	if string(b) != "wifi_ssid=Home\nhostname=workhorse-100\ntimezone=UTC\nname=Workhorse 100%\n" {
		t.Errorf("printdeck.txt %q", b)
	}
}
