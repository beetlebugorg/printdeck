package system

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Paths of the settings. Tests change them.
var (
	klipperConfigDir = "/var/lib/klipper/config"
	etcDir           = "/etc"
	zoneinfoDir      = "/usr/share/zoneinfo"
	sethostname      = func(name string) error { return syscall.Sethostname([]byte(name)) }
)

// printdeckInclude goes at the end of an imported printer.cfg, so that the settings of the deck win
// (for example the upload folder).
const printdeckInclude = "\n# Added by Printdeck: keep this line last.\n[include /usr/share/printdeck/klipper/printdeck.cfg]\n"

// Settings applies the settings on the config partition: a printer.cfg, and the settings in
// printdeck.txt. Then it clears the Wi-Fi password in printdeck.txt. log gets the settings that
// fail.
func Settings(log io.Writer) error {
	if err := placeholderPrinterCfg(); err != nil {
		fmt.Fprintf(log, "printer.cfg: %v\n", err)
	}
	umount, err := MountConfig()
	if err != nil {
		return err
	}
	defer umount()
	err = applySettings(log)
	if derr := removeDisplayCfg(); derr != nil {
		fmt.Fprintf(log, "display layout: %v\n", derr)
	}
	return err
}

func applySettings(log io.Writer) error {
	if err := importPrinterCfg(); err != nil {
		return err
	}

	file := filepath.Join(configDir, "printdeck.txt")
	text, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	get := func(key string) string {
		v := ""
		for _, l := range strings.Split(string(text), "\n") {
			if s, ok := strings.CutPrefix(l, key+"="); ok {
				v = s
			}
		}
		return strings.ReplaceAll(v, "\r", "")
	}

	ssid := get("wifi_ssid")
	if ssid != "" {
		// Apply the network again only when a password is in the file, or for an open network at
		// the first boot. Otherwise each boot writes the same network again with no password.
		pass := get("wifi_password")
		conf, _ := os.ReadFile(wpaConf)
		if pass != "" || !hasLinePrefix(conf, "network=") {
			if err := SetWifi(ssid, pass, get("wifi_country")); err != nil {
				fmt.Fprintf(log, "Wi-Fi: not applied: %v\n", err)
			}
		}
	}

	// In the debug image, a key here lets the user "printdeck" log in with SSH.
	if key := get("ssh_key"); key != "" {
		if _, err := os.Stat(userProgram); err == nil {
			if err := run(userProgram, loginName, "", key); err != nil {
				fmt.Fprintf(log, "SSH key: not applied: %v\n", err)
			}
		}
	}

	// The touch screen links Spotify with a QR code. It needs the client ID of a Spotify app.
	if id := get("spotify_client_id"); id != "" {
		os.MkdirAll(stateDir, 0o700)
		if err := writeFile(filepath.Join(stateDir, "spotify.env"), []byte("SPOTIFY_CLIENT_ID="+id+"\n"), 0o644); err != nil {
			fmt.Fprintf(log, "Spotify: not applied: %v\n", err)
		}
	}

	if host := get("hostname"); host != "" {
		cur, _ := os.ReadFile(filepath.Join(etcDir, "hostname"))
		if host != strings.TrimSpace(string(cur)) {
			if err := writeFile(filepath.Join(etcDir, "hostname"), []byte(host+"\n"), 0o644); err != nil {
				fmt.Fprintf(log, "hostname: not applied: %v\n", err)
			} else {
				sethostname(host)
			}
		}
	}

	if tz := get("timezone"); tz != "" && !strings.Contains(tz, "..") {
		zone := filepath.Join(zoneinfoDir, tz)
		if fi, err := os.Stat(zone); err == nil && fi.Mode().IsRegular() {
			lt := filepath.Join(etcDir, "localtime")
			os.Remove(lt + ".new")
			if err := os.Symlink(zone, lt+".new"); err == nil {
				os.Rename(lt+".new", lt)
			}
		}
	}

	// The settings of the deck (deck.env) in lower case, for example display_rotate=270. The deck
	// service reads settings.env after deck.env, so these win. Only the names in deckSettings count.
	var env strings.Builder
	for _, name := range deckSettings {
		if v := get(strings.ToLower(name)); v != "" {
			fmt.Fprintf(&env, "%s=%s\n", name, v)
		}
	}
	envFile := filepath.Join(stateDir, "settings.env")
	if env.Len() > 0 {
		os.MkdirAll(stateDir, 0o700)
		if err := writeFile(envFile, []byte(env.String()), 0o644); err != nil {
			fmt.Fprintf(log, "deck settings: not applied: %v\n", err)
		}
	} else {
		os.Remove(envFile)
	}

	// With the Wi-Fi set here, the touch screen does not open its setup screen.
	if ssid != "" {
		os.MkdirAll(stateDir, 0o700)
		os.WriteFile(filepath.Join(stateDir, "setup-done"), nil, 0o644)
	}

	// The file is on a FAT partition that any computer can read. Do not keep the password in it.
	lines := strings.Split(string(text), "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "wifi_password=") {
			lines[i] = "wifi_password="
		}
	}
	if cleared := strings.Join(lines, "\n"); cleared != string(text) {
		return os.WriteFile(file, []byte(cleared), 0o644)
	}
	return nil
}

// deckSettings are the settings of deck.env that printdeck.txt can change. The paths and the
// address of the web server are not in the list.
var deckSettings = []string{"DISPLAY_ROTATE", "TOUCH_INVERT_X", "TOUCH_INVERT_Y", "TOUCH_SWAP_XY",
	"TOUCH_OFFSET_X", "TOUCH_OFFSET_Y", "TOUCH_LOG", "SPOTIFY_DEVICE_NAME", "VOLUME_FLOOR_DB",
	"VOLUME_CEIL_DB", "MAX_VOLUME", "PREHEAT_TOOL", "PREHEAT_BED", "FILAMENT_UNLOAD_MM",
	"FILAMENT_LOAD_MM", "JOG_INVERT_Y", "NIGHT_FROM", "NIGHT_TO"}

// removeDisplayCfg removes the display layout of older versions. The module printdeck_display of
// the Klipper host draws the display now.
func removeDisplayCfg() error {
	err := os.Remove(filepath.Join(klipperConfigDir, "printdeck-display.cfg"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// placeholder is the printer.cfg of a new system: Klipper starts, and the deck shows the printer
// setup (no [mcu] section).
const placeholder = "# Put the printer configuration above this line.\n[include /usr/share/printdeck/klipper/printdeck.cfg]\n"

// placeholderPrinterCfg writes the placeholder printer.cfg when there is none.
func placeholderPrinterCfg() error {
	path := filepath.Join(klipperConfigDir, "printer.cfg")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(klipperConfigDir, 0o775); err != nil {
		return err
	}
	if err := writeFile(path, []byte(placeholder), 0o664); err != nil {
		return err
	}
	chownPrinter(filepath.Dir(klipperConfigDir))
	return nil
}

// chownPrinter gives a folder and its files to the user printer, which runs Klipper.
func chownPrinter(dir string) {
	u, err := user.Lookup("printer")
	if err != nil {
		return
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	filepath.WalkDir(dir, func(p string, _ fs.DirEntry, err error) error {
		if err == nil {
			os.Lchown(p, uid, gid)
		}
		return nil
	})
}

// importPrinterCfg makes a printer.cfg on the config partition the Klipper configuration. The
// include of the Printdeck sections goes at the end. The file on the partition gets the name
// printer.cfg.imported, so a later SAVE_CONFIG on the Pi stays.
func importPrinterCfg() error {
	src := filepath.Join(configDir, "printer.cfg")
	b, err := os.ReadFile(src)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := os.MkdirAll(klipperConfigDir, 0o775); err != nil {
		return err
	}
	cfg := append(bytes.ReplaceAll(b, []byte("\r"), nil), printdeckInclude...)
	if err := writeFile(filepath.Join(klipperConfigDir, "printer.cfg"), cfg, 0o664); err != nil {
		return err
	}
	chownPrinter(klipperConfigDir)
	return os.Rename(src, src+".imported")
}

// hasLinePrefix reports that a line of a text starts with prefix (grep -q '^prefix').
func hasLinePrefix(text []byte, prefix string) bool {
	for _, l := range strings.Split(string(text), "\n") {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}
