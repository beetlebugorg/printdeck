package printer

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// The kinds of printer that a deck drives: a printer of the Klipper host on USB, or a ChiTu resin
// printer on the network (package chitu).
const (
	KindKlipper = "klipper"
	KindChiTu   = "chitu"
)

// The processes of a printer.
const (
	ProcessFDM   = "fdm"
	ProcessResin = "resin"
)

// Setting is printer.json of the state folder: the printer that the deck drives. With no file the
// deck drives the Klipper host, as a deck from before the setting.
type Setting struct {
	Kind string `json:"kind"`
	// For a ChiTu printer: the address of the board ("192.168.76.58"), its MAC address to find it
	// again after a change of the address, and the name of the printer.
	Addr string `json:"addr,omitempty"`
	MAC  string `json:"mac,omitempty"`
	Name string `json:"name,omitempty"`
}

// SettingFile is the name of the file of the setting in the state folder.
const SettingFile = "printer.json"

// LoadSetting reads printer.json of the state folder dir. A missing file is the Klipper kind.
func LoadSetting(dir string) (Setting, error) {
	b, err := os.ReadFile(filepath.Join(dir, SettingFile))
	if errors.Is(err, fs.ErrNotExist) {
		return Setting{Kind: KindKlipper}, nil
	}
	if err != nil {
		return Setting{Kind: KindKlipper}, err
	}
	var s Setting
	if err := json.Unmarshal(b, &s); err != nil {
		return Setting{Kind: KindKlipper}, fmt.Errorf("%s: %v", SettingFile, err)
	}
	switch s.Kind {
	case KindKlipper:
	case KindChiTu:
		if s.Addr == "" {
			return Setting{Kind: KindKlipper}, fmt.Errorf("%s: a ChiTu printer with no address", SettingFile)
		}
	default:
		return Setting{Kind: KindKlipper}, fmt.Errorf("%s: no such kind of printer: %q", SettingFile, s.Kind)
	}
	return s, nil
}

// SaveSetting writes printer.json of the state folder dir.
func SaveSetting(dir string, s Setting) error {
	b, _ := json.MarshalIndent(s, "", " ")
	tmp := filepath.Join(dir, SettingFile+".new")
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, SettingFile))
}
