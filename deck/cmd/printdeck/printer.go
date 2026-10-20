package main

import (
	"context"

	"printdeck/internal/config"
	"printdeck/internal/gcodestore"
	"printdeck/internal/octoprint"
	"printdeck/internal/printer"
	"printdeck/internal/ui"
)

// printerBackend is the printer of the deck, and the loop that follows it.
type printerBackend interface {
	ui.Printer
	Run(ctx context.Context, notify func())
}

// newPrinter returns the Klipper backend when KLIPPY_SOCKET is set, and the OctoPrint client otherwise.
func newPrinter(cfg config.Config) printerBackend {
	if cfg.KlippySocket != "" {
		k := printer.NewKlippy(cfg.KlippySocket, &gcodestore.Store{Dir: cfg.GcodeDir})
		k.ModelDir = cfg.ModelDir
		return k
	}
	op := octoprint.New(cfg.OctoPrintURL, cfg.OctoPrintKey)
	op.ModelDir = cfg.ModelDir
	return op
}
