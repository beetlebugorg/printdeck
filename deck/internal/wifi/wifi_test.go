package wifi

import (
	"net"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseResults(t *testing.T) {
	r := "bssid / frequency / signal level / flags / ssid\n" +
		"aa:bb:cc:00:00:01\t2412\t-61\t[WPA2-PSK-CCMP][ESS]\tHome\n" +
		"aa:bb:cc:00:00:02\t5180\t-48\t[WPA2-PSK-CCMP][ESS]\tHome\n" +
		"aa:bb:cc:00:00:03\t2437\t-70\t[ESS]\tCafe \\\"Free\\\"\n" +
		"aa:bb:cc:00:00:04\t2462\t-55\t[WPA2-PSK-CCMP][ESS]\t\n" +
		"aa:bb:cc:00:00:05\t2462\t-80\t[RSN-SAE-CCMP][ESS]\tCaf\\xc3\\xa9\n"
	want := []Network{
		{SSID: "Home", Signal: -48, Secure: true},
		{SSID: `Cafe "Free"`, Signal: -70, Secure: false},
		{SSID: "Café", Signal: -80, Secure: true},
	}
	if got := parseResults(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestParseStatus(t *testing.T) {
	r := "bssid=aa:bb:cc:00:00:02\nfreq=5180\nssid=Home\nid=0\nmode=station\nwpa_state=COMPLETED\nip_address=192.168.76.119\naddress=d8:3a:dd:00:00:00\n"
	s := parseStatus(r)
	if s != (Status{State: "COMPLETED", SSID: "Home", IP: "192.168.76.119"}) || !s.Connected() {
		t.Fatalf("got %+v", s)
	}
	if (Status{State: "COMPLETED"}).Connected() {
		t.Fatal("connected with no address")
	}
}

func TestUnescape(t *testing.T) {
	for in, want := range map[string]string{
		`plain`:      "plain",
		`back\\lash`: `back\lash`,
		`tab\there`:  "tab\there",
		`\x41\x42`:   "AB",
		`end\x4`:     `end\x4`,
		`trailing\`:  `trailing\`,
	} {
		if got := unescape(in); got != want {
			t.Errorf("unescape(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRequest checks the socket exchange against a fake wpa_supplicant.
func TestRequest(t *testing.T) {
	dir := t.TempDir()
	srv, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(dir, "wlan0"), Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	go func() {
		buf := make([]byte, 256)
		n, from, err := srv.ReadFromUnix(buf)
		if err != nil {
			return
		}
		if string(buf[:n]) == "STATUS" {
			srv.WriteToUnix([]byte("wpa_state=SCANNING\n"), from)
		}
	}()
	t.Setenv("TMPDIR", dir)
	c := &Client{Dir: dir, Iface: "wlan0"}
	s, err := c.Status()
	if err != nil {
		t.Fatal(err)
	}
	if s.State != "SCANNING" {
		t.Fatalf("got %+v", s)
	}
	left, _ := filepath.Glob(filepath.Join(dir, "deck-wpa-*"))
	if len(left) != 0 {
		t.Fatalf("the local socket was not removed: %v", left)
	}
}
