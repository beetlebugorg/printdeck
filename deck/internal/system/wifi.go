package system

import (
	"crypto/pbkdf2"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
)

// wpaConf is the config of wpa_supplicant for wlan0.
var wpaConf = "/etc/wpa_supplicant/wpa_supplicant-wlan0.conf"

// psk turns a Wi-Fi password into the 256-bit key, as wpa_passphrase does: PBKDF2 with SHA-1, 4096
// rounds, and the SSID as the salt. The password has 8 to 63 characters from space to "~".
func psk(ssid, pass string) (string, error) {
	if len(pass) < 8 || len(pass) > 63 {
		return "", errors.New("The password must have 8 to 63 characters.")
	}
	for i := 0; i < len(pass); i++ {
		if pass[i] < 32 || pass[i] > 126 {
			return "", errors.New("The password has a character that Wi-Fi does not allow.")
		}
	}
	key, err := pbkdf2.Key(sha1.New, pass, []byte(ssid), 4096, 32)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(key), nil
}

// wifiConf is the text of the wpa_supplicant config for one network. An empty password is for an
// open network. The SSID is in hex, so that any character in it is safe. The file has the key, not
// the password.
func wifiConf(ssid, pass, country string) (string, error) {
	security := "key_mgmt=NONE"
	if pass != "" {
		k, err := psk(ssid, pass)
		if err != nil {
			return "", err
		}
		security = "psk=" + k
	}
	return fmt.Sprintf("ctrl_interface=DIR=/run/wpa_supplicant GROUP=deck\nupdate_config=1\ncountry=%s\n\nnetwork={\n\tssid=%s\n\t%s\n}\n",
		country, hex.EncodeToString([]byte(ssid)), security), nil
}

// SetWifi sets the Wi-Fi network, and restarts wpa_supplicant. An empty password is for an open
// network. country is the two-letter country code (default US).
func SetWifi(ssid, pass, country string) error {
	if ssid == "" {
		return errors.New("The network has no name.")
	}
	if country == "" {
		country = "US"
	}
	conf, err := wifiConf(ssid, pass, country)
	if err != nil {
		return err
	}
	if err := writeFile(wpaConf, []byte(conf), 0o600); err != nil {
		return err
	}
	return restartWifi()
}

// restartWifi restarts wpa_supplicant. --no-block: Settings calls SetWifi before wpa_supplicant
// starts. Tests replace it.
var restartWifi = func() error {
	return run("systemctl", "--no-block", "restart", "wpa_supplicant@wlan0.service")
}
