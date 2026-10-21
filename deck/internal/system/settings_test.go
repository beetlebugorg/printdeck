package system

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeSettings(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	configDir = filepath.Join(dir, "config")
	klipperConfigDir = filepath.Join(dir, "klipper")
	etcDir = filepath.Join(dir, "etc")
	stateDir = filepath.Join(dir, "state")
	zoneinfoDir = filepath.Join(dir, "zoneinfo")
	wpaConf = filepath.Join(etcDir, "wpa_supplicant-wlan0.conf")
	userProgram = filepath.Join(dir, "no-such-program")
	for _, d := range []string{configDir, etcDir, filepath.Join(zoneinfoDir, "Europe")} {
		os.MkdirAll(d, 0o755)
	}
	os.WriteFile(filepath.Join(zoneinfoDir, "Europe", "Berlin"), []byte("TZif"), 0o644)
	restartWifi = func() error { return nil }
	sethostname = func(string) error { return nil }
	return dir
}

func TestSettings(t *testing.T) {
	fakeSettings(t)
	// A file from a Windows computer, with CR LF line ends.
	txt := "# Printdeck settings\r\nwifi_ssid=Workshop\r\nwifi_password=ThisIsAPassword\r\nwifi_country=DE\r\n" +
		"hostname=deck2\r\ntimezone=Europe/Berlin\r\n"
	os.WriteFile(filepath.Join(configDir, "printdeck.txt"), []byte(txt), 0o644)
	os.WriteFile(filepath.Join(configDir, "printer.cfg"), []byte("[mcu]\r\nserial: /dev/ttyACM0\r\n"), 0o644)
	if err := applySettings(io.Discard); err != nil {
		t.Fatal(err)
	}

	cfg, _ := os.ReadFile(filepath.Join(klipperConfigDir, "printer.cfg"))
	if string(cfg) != "[mcu]\nserial: /dev/ttyACM0\n"+printdeckInclude {
		t.Errorf("printer.cfg:\n%s", cfg)
	}
	if _, err := os.Stat(filepath.Join(configDir, "printer.cfg.imported")); err != nil {
		t.Error("printer.cfg was not renamed")
	}
	conf, _ := os.ReadFile(wpaConf)
	if want, _ := wifiConf("Workshop", "ThisIsAPassword", "DE"); string(conf) != want {
		t.Errorf("wpa config:\n%s", conf)
	}
	if b, _ := os.ReadFile(filepath.Join(etcDir, "hostname")); string(b) != "deck2\n" {
		t.Errorf("hostname %q", b)
	}
	if l, _ := os.Readlink(filepath.Join(etcDir, "localtime")); l != filepath.Join(zoneinfoDir, "Europe/Berlin") {
		t.Errorf("localtime -> %q", l)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "setup-done")); err != nil {
		t.Error("no setup-done")
	}
	// The password is gone from the file, and the other lines stay as they were.
	after, _ := os.ReadFile(filepath.Join(configDir, "printdeck.txt"))
	if want := strings.Replace(txt, "wifi_password=ThisIsAPassword\r", "wifi_password=", 1); string(after) != want {
		t.Errorf("printdeck.txt:\n%q\nwant\n%q", after, want)
	}

	// The next start has no password: the network stays as it is.
	os.WriteFile(wpaConf, []byte("network={\n\tssid=x\n}\n"), 0o600)
	if err := applySettings(io.Discard); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(wpaConf); string(b) != "network={\n\tssid=x\n}\n" {
		t.Errorf("the network was written again with no password:\n%s", b)
	}
}

func TestDeckSettings(t *testing.T) {
	fakeSettings(t)
	os.WriteFile(filepath.Join(configDir, "printdeck.txt"), []byte("display_rotate=90\r\npreheat_bed=70\nremote_addr=:8080\n"), 0o644)
	if err := applySettings(io.Discard); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(stateDir, "settings.env"))
	if string(b) != "DISPLAY_ROTATE=90\nPREHEAT_BED=70\n" {
		t.Errorf("settings.env %q", b)
	}
	// With no settings in the file, settings.env goes.
	os.WriteFile(filepath.Join(configDir, "printdeck.txt"), []byte("hostname=x\n"), 0o644)
	applySettings(io.Discard)
	if _, err := os.Stat(filepath.Join(stateDir, "settings.env")); err == nil {
		t.Error("settings.env stayed")
	}
}

func TestRemoveDisplayCfg(t *testing.T) {
	fakeSettings(t)
	os.MkdirAll(klipperConfigDir, 0o755)
	path := filepath.Join(klipperConfigDir, "printdeck-display.cfg")
	os.WriteFile(path, []byte("[display]\n"), 0o664)
	if err := removeDisplayCfg(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("the old display layout stayed")
	}
	if err := removeDisplayCfg(); err != nil {
		t.Errorf("with no file: %v", err)
	}
}

func TestPlaceholder(t *testing.T) {
	fakeSettings(t)
	if err := placeholderPrinterCfg(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(klipperConfigDir, "printer.cfg"))
	if string(b) != placeholder {
		t.Errorf("placeholder %q", b)
	}
	// An existing printer.cfg stays.
	os.WriteFile(filepath.Join(klipperConfigDir, "printer.cfg"), []byte("[mcu]\n"), 0o664)
	placeholderPrinterCfg()
	if b, _ := os.ReadFile(filepath.Join(klipperConfigDir, "printer.cfg")); string(b) != "[mcu]\n" {
		t.Errorf("printer.cfg changed to %q", b)
	}
}

func TestSettingsNoFile(t *testing.T) {
	fakeSettings(t)
	if err := applySettings(io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wpaConf); err == nil {
		t.Error("a Wi-Fi config with no settings file")
	}
}
