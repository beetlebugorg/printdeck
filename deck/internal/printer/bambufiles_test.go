package printer

import (
	"archive/zip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"printdeck/internal/bambu"
)

// dirStore is a Store of the files of a folder, for the tests.
type dirStore string

func (d dirStore) Open(p string) (io.ReadCloser, error) { return os.Open(filepath.Join(string(d), p)) }
func (d dirStore) Models() []File                       { return nil }
func (d dirStore) Files() []File {
	es, _ := os.ReadDir(string(d))
	var out []File
	for _, e := range es {
		out = append(out, File{Path: e.Name(), Name: e.Name()})
	}
	return out
}
func (d dirStore) Stat(p string) (fs.FileInfo, error) { return os.Stat(filepath.Join(string(d), p)) }
func (d dirStore) FullPath(p string) (string, error)  { return filepath.Join(string(d), p), nil }
func (d dirStore) Delete(p string) error              { return os.Remove(filepath.Join(string(d), p)) }

func write3MF(t *testing.T, path, sliceInfo string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, _ := z.Create("Metadata/slice_info.config")
	w.Write([]byte(sliceInfo))
	z.Close()
	f.Close()
}

func TestBambuFiles(t *testing.T) {
	dir := t.TempDir()
	b := NewBambu("192.168.1.50", "S", "C", "", "X2D", nil)
	b.Spools = &Spools{Path: filepath.Join(dir, "spools.json")}
	b.TrayPath = filepath.Join(dir, "ams-trays.json")
	files := filepath.Join(dir, "files")
	os.Mkdir(files, 0o755)
	b.Store = dirStore(files)
	b.update(amsReport("IDLE", "255"), time.Now())
	// Tray 2 has no color on the printer: its roll is white.
	b.AddTrayRoll(1, 1000, "#ffffff")

	write3MF(t, filepath.Join(files, "two.gcode.3mf"), `<config><plate><metadata key="index" value="1"/>`+
		`<metadata key="prediction" value="3600"/><metadata key="weight" value="20"/>`+
		`<filament id="1" type="PLA" color="#F0F0F0" used_m="2" used_g="6"/>`+
		`<filament id="2" type="PLA" color="#EE2222" used_m="4" used_g="14"/></plate></config>`)
	m := b.FileMeta("two.gcode.3mf")
	if m.PrintTime != time.Hour || m.FilamentG != 20 || m.FilamentMM != 6000 || m.Material != "PLA" {
		t.Errorf("meta %+v", m)
	}
	f, err := b.threeMF("two.gcode.3mf")
	if err != nil {
		t.Fatal(err)
	}
	got := b.MatchTrays(f.plates[0])
	if len(got) != 2 || got[0].Slot != 1 || got[1].Slot != 3 {
		t.Errorf("match %+v", got)
	}
	// A material that no tray has: no tray.
	pl := bambu.Plate{Index: 1, Filaments: []bambu.Filament{{ID: 1, Type: "PETG", Color: "#000000"}}}
	if got := b.MatchTrays(pl); got[0].Slot != -1 {
		t.Errorf("petg %+v", got)
	}
	// Two filaments of one color take two trays while two trays have the material.
	pl = bambu.Plate{Index: 1, Filaments: []bambu.Filament{{ID: 1, Type: "PLA", Color: "#101010"}, {ID: 2, Type: "PLA", Color: "#101010"}}}
	if got := b.MatchTrays(pl); got[0].Slot != 2 || got[1].Slot == 2 {
		t.Errorf("same color %+v", got)
	}
	if len(b.Files()) != 1 {
		t.Errorf("files %v", b.Files())
	}
}
