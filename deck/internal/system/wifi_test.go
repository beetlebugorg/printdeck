package system

import (
	"os/exec"
	"strings"
	"testing"
)

func TestPSK(t *testing.T) {
	for _, c := range []struct{ ssid, pass, want string }{
		// The test vectors of IEEE 802.11i, as wpa_passphrase prints them.
		{"IEEE", "password", "f42c6fc52df0ebef9ebb4b90b38a5f902e83fe1b135a70e23aed762e9710a12e"},
		{"ThisIsASSID", "ThisIsAPassword", "0dc0d6eb90555ed6419756b9a15ec3e3209b63df707dd508d14581f8982721af"},
	} {
		if got, err := psk(c.ssid, c.pass); got != c.want || err != nil {
			t.Errorf("psk(%q, %q) = %s, %v", c.ssid, c.pass, got, err)
		}
	}
	for _, bad := range []string{"short", strings.Repeat("x", 64), "tab\tin it!"} {
		if _, err := psk("x", bad); err == nil {
			t.Errorf("psk accepted %q", bad)
		}
	}
}

// The config is the text that printdeck-wifi wrote.
func TestWifiConf(t *testing.T) {
	got, err := wifiConf("Extremely", "ThisIsAPassword", "US")
	if err != nil {
		t.Fatal(err)
	}
	k, _ := psk("Extremely", "ThisIsAPassword")
	want := "ctrl_interface=DIR=/run/wpa_supplicant GROUP=deck\nupdate_config=1\ncountry=US\n\nnetwork={\n\tssid=45787472656d656c79\n\tpsk=" + k + "\n}\n"
	if got != want {
		t.Errorf("wifiConf =\n%s\nwant\n%s", got, want)
	}
	open, _ := wifiConf("Guest", "", "DE")
	if !strings.Contains(open, "\tkey_mgmt=NONE\n") || !strings.Contains(open, "country=DE\n") {
		t.Errorf("open network:\n%s", open)
	}
	// wpa_passphrase gives the same key.
	if out, err := exec.Command("wpa_passphrase", "Extremely", "ThisIsAPassword").Output(); err == nil {
		if !strings.Contains(string(out), "\tpsk="+k+"\n") {
			t.Errorf("wpa_passphrase gives another key:\n%s", out)
		}
	}
}
