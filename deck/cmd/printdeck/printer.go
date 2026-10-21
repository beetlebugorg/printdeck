package main

import (
	"context"

	"printdeck/internal/config"
	"printdeck/internal/gcodestore"
	"printdeck/internal/printer"
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
	k.RestartService = func() error { return runScript("/usr/bin/systemctl", "restart", "klipper") }
	return k
}
