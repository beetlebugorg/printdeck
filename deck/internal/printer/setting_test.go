package printer

import (
	"os"
	"path/filepath"
	"testing"
)

// A deck with no printer.json drives Klipper. A ChiTu setting goes and comes back, and a bad file
// is the Klipper kind with an error.
func TestSetting(t *testing.T) {
	dir := t.TempDir()
	if s, err := LoadSetting(dir); err != nil || s.Kind != KindKlipper {
		t.Fatalf("no file: %+v %v", s, err)
	}
	want := Setting{Kind: KindChiTu, Addr: "192.168.76.58", MAC: "00:e0:4c:18:00:29", Name: "Phenom"}
	if err := SaveSetting(dir, want); err != nil {
		t.Fatal(err)
	}
	if s, err := LoadSetting(dir); err != nil || s != want {
		t.Fatalf("the setting back: %+v %v", s, err)
	}
	for _, bad := range []string{`{"kind":"chitu"}`, `{"kind":"laser"}`, `{`} {
		os.WriteFile(filepath.Join(dir, SettingFile), []byte(bad), 0o644)
		if s, err := LoadSetting(dir); err == nil || s.Kind != KindKlipper {
			t.Errorf("%s: %+v %v", bad, s, err)
		}
	}
}
