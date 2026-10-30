package main

import (
	"testing"

	"printdeck/internal/config"
	"printdeck/internal/printer"
)

// newPrinter follows printer.json: the Klipper backend with no file, the ChiTu backend for a ChiTu
// printer.
func TestNewPrinterKind(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{StateDir: dir, KlippySocket: dir + "/none.sock", GcodeDir: dir}
	if _, ok := newPrinter(cfg).(*printer.Klippy); !ok {
		t.Error("no printer.json: not the Klipper backend")
	}
	if err := printer.SaveSetting(dir, printer.Setting{Kind: printer.KindChiTu, Addr: "192.168.76.58", Name: "Phenom"}); err != nil {
		t.Fatal(err)
	}
	c, ok := newPrinter(cfg).(*printer.ChiTu)
	if !ok || c.Client.Addr != "192.168.76.58:3000" || c.Name != "Phenom" {
		t.Errorf("a ChiTu printer: %T %+v", newPrinter(cfg), c)
	}
}
