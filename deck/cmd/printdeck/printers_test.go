package main

import (
	"os"
	"path/filepath"
	"testing"

	"printdeck/internal/camera"
	"printdeck/internal/config"
	"printdeck/internal/printer"
	"printdeck/internal/system"
)

// A deck with no printers.json has one printer with the folders of the deck. Each added printer
// gets its own folder, Klipper host and colour, and Changed reports the difference from the
// printers that run.
func TestPrinterAdmin(t *testing.T) {
	dir := t.TempDir()
	base := config.Config{StateDir: dir, KlippySocket: "/run/klipper/klippy.sock", KlipperConfig: "/var/lib/klipper/config/printer.cfg", OctoPrintKey: "MAINKEY"}
	running := loadDeckPrinters(dir)
	if len(running) != 1 || running[0].ID != "" || running[0].config(base) != base {
		t.Fatalf("one printer: %+v", running)
	}
	var asked []system.HelperRequest
	pa := &printerAdmin{base: base, running: running, restart: func() {},
		helper: func(r system.HelperRequest) error { asked = append(asked, r); return nil }}
	if pa.Changed() {
		t.Fatal("changed before a change")
	}
	if err := pa.AddKlipper("Mini 2"); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || asked[0].Action != "add-printer" || asked[0].Printer != "mini2" {
		t.Fatalf("helper: %+v", asked)
	}
	if err := pa.AddResin("Phenom", "192.168.76.58"); err != nil {
		t.Fatal(err)
	}
	if err := pa.AddKlipper("Mini 2"); err != nil {
		t.Fatal(err)
	}
	list := pa.List()
	if len(list) != 4 || list[1].ID != "mini2" || list[2].Kind != "Resin" || list[2].Addr != "192.168.76.58" || list[3].ID != "mini22" {
		t.Fatalf("list: %+v", list)
	}
	colors := map[int]bool{}
	for _, p := range list {
		if colors[p.Color] {
			t.Fatalf("two printers with the colour %d", p.Color)
		}
		colors[p.Color] = true
	}
	if err := pa.AddKlipper("Fifth"); err == nil {
		t.Fatal("a fifth printer")
	}
	if !pa.Changed() {
		t.Fatal("no change after an add")
	}
	ps := loadDeckPrinters(dir)
	c := ps[1].config(base)
	if c.StateDir != filepath.Join(dir, "printers", "mini2") || c.KlippySocket != "/run/klipper/mini2/klippy.sock" ||
		c.KlipperConfig != "/var/lib/klipper/printers/mini2/printer.cfg" || c.GcodeDir != "/var/lib/klipper/printers/mini2/gcodes" ||
		c.OctoPrintKey != "" {
		t.Fatalf("config: %+v", c)
	}
	if s, err := printer.LoadSetting(ps[2].config(base).StateDir); err != nil || s.Kind != printer.KindChiTu {
		t.Fatalf("the resin setting: %+v %v", s, err)
	}
	// A resin printer has no Klipper host to stop.
	asked = nil
	if err := pa.Remove("phenom"); err != nil || len(asked) != 0 {
		t.Fatalf("remove resin: %v %+v", err, asked)
	}
	if err := pa.Remove("mini22"); err != nil || len(asked) != 1 || asked[0].Action != "remove-printer" {
		t.Fatalf("remove klipper: %v %+v", err, asked)
	}
	if err := pa.Remove(""); err == nil {
		t.Fatal("the first printer was removed")
	}
	if n := len(loadDeckPrinters(dir)); n != 2 {
		t.Fatalf("%d printers after the removes", n)
	}
	// A bad file gives one printer, and a bad ID in the file is left out.
	os.WriteFile(printersFile(dir), []byte(`[{"id":"","name":"TAZ"},{"id":"../x","name":"Bad"},{"id":"ok","name":"OK"}]`), 0o644)
	if ps := loadDeckPrinters(dir); len(ps) != 2 || ps[1].ID != "ok" {
		t.Fatalf("bad id: %+v", ps)
	}
	os.WriteFile(printersFile(dir), []byte(`[{"id":"x","name":"No first"}]`), 0o644)
	if ps := loadDeckPrinters(dir); len(ps) != 1 || ps[0].ID != "" {
		t.Fatalf("no first printer: %+v", ps)
	}
}

func TestNewPrinterID(t *testing.T) {
	ps := []deckPrinter{{ID: ""}, {ID: "mini"}}
	for name, want := range map[string]string{"Mini": "mini2", "TAZ Pro": "tazpro", "3D": "p3d", "!!!": "p", "A very long printer name": "averylongpri", "Config": "config2"} {
		if got := newPrinterID(name, ps); got != want {
			t.Errorf("%q: %q, want %q", name, got, want)
		}
	}
}

// Each camera belongs to one printer: a camera that the file does not name is a camera of the
// first printer. A move writes the file, and a move to the first printer removes the entry.
func TestCameraPrinters(t *testing.T) {
	dir := t.TempDir()
	if err := setCameraPrinter(dir, "usb-cam-a", "mini2"); err != nil {
		t.Fatal(err)
	}
	m := cameraPrinters(dir)
	first, mini := keepCameras(m, ""), keepCameras(m, "mini2")
	a := camera.Device{ID: "usb-cam-a"}
	b := camera.Device{ID: "usb-cam-b"}
	alias := camera.Device{ID: "pci-path-1", Alias: "usb-cam-a"}
	if first(a) || !mini(a) || !first(b) || mini(b) || first(alias) || !mini(alias) {
		t.Fatal("wrong printer of a camera")
	}
	if err := setCameraPrinter(dir, "usb-cam-a", ""); err != nil {
		t.Fatal(err)
	}
	if len(cameraPrinters(dir)) != 0 {
		t.Fatal("the entry stays after a move to the first printer")
	}
}
