package main

import (
	"context"
	"log"
	"path/filepath"

	"printdeck/internal/config"
	"printdeck/internal/gcodestore"
	"printdeck/internal/printer"
	"printdeck/internal/system"
	"printdeck/internal/ui"
)

// printerBackend is the printer of the deck, and the loop that follows it.
type printerBackend interface {
	ui.Printer
	Run(ctx context.Context, notify func())
}

// The backend of a ChiTu resin printer has each method of a backend.
var _ printerBackend = (*printer.ChiTu)(nil)

// newPrinter returns the backend of printer.json in the state folder: a ChiTu resin printer on the
// network, or the Klipper backend, which follows the Klipper host on its API socket.
func newPrinter(cfg config.Config) printerBackend {
	hist := &printer.History{Path: filepath.Join(cfg.StateDir, "history.json")}
	s, err := printer.LoadSetting(cfg.StateDir)
	if err != nil {
		log.Printf("printer: %v: the deck drives Klipper", err)
	}
	if s.Kind == printer.KindChiTu {
		return printer.NewChiTu(s.Addr, s.Name, hist)
	}
	k := printer.NewKlippy(cfg.KlippySocket, &gcodestore.Store{Dir: cfg.GcodeDir})
	k.ModelDir = cfg.ModelDir
	k.ConfigPath = cfg.KlipperConfig
	k.Hist = hist
	k.Spools = &printer.Spools{Path: filepath.Join(cfg.StateDir, "spools.json")}
	k.View.Rolls = k.Spools
	k.RestartService = func() error { return system.AskHelper(system.HelperRequest{Action: "restart-klipper"}) }
	k.StopService = func() error { return system.AskHelper(system.HelperRequest{Action: "stop-klipper"}) }
	return k
}
