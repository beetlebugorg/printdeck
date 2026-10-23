package main

import (
	"context"
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

// newPrinter returns the Klipper backend: the deck follows the Klipper host on its API socket.
func newPrinter(cfg config.Config) printerBackend {
	k := printer.NewKlippy(cfg.KlippySocket, &gcodestore.Store{Dir: cfg.GcodeDir})
	k.ModelDir = cfg.ModelDir
	k.ConfigPath = cfg.KlipperConfig
	k.Hist = &printer.History{Path: filepath.Join(cfg.StateDir, "history.json")}
	k.Spools = &printer.Spools{Path: filepath.Join(cfg.StateDir, "spools.json")}
	k.View.Rolls = k.Spools
	k.RestartService = func() error { return system.AskHelper(system.HelperRequest{Action: "restart-klipper"}) }
	k.StopService = func() error { return system.AskHelper(system.HelperRequest{Action: "stop-klipper"}) }
	return k
}
