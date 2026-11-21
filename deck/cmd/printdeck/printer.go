package main

import (
	"context"
	"log"
	"path/filepath"

	"printdeck/internal/bambu"
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

// The backend of a Bambu Lab printer has each method of a backend.
var _ printerBackend = (*printer.Bambu)(nil)

// newPrinter returns the backend of the printer id of the deck ("" for the first printer), from
// printer.json in its state folder: a ChiTu resin printer or a Bambu printer on the network, or the
// Klipper backend, which follows the Klipper host on its API socket.
func newPrinter(cfg config.Config, id string) printerBackend {
	hist := &printer.History{Path: filepath.Join(cfg.StateDir, "history.json")}
	s, err := printer.LoadSetting(cfg.StateDir)
	if err != nil {
		log.Printf("printer: %v: the deck drives Klipper", err)
	}
	if s.Kind == printer.KindChiTu {
		c := printer.NewChiTu(s.Addr, s.Name, hist)
		c.PrintStore = filepath.Join(cfg.StateDir, "resin-print.json")
		return c
	}
	if s.Kind == printer.KindBambu {
		b := printer.NewBambu(s.Addr, s.Serial, s.Code, s.Pin, s.Name, hist)
		b.Spools = &printer.Spools{Path: filepath.Join(cfg.StateDir, "spools.json")}
		b.HeatLog = &printer.HeatLog{Path: filepath.Join(cfg.StateDir, "heat-times.json")}
		b.TrayPath = filepath.Join(cfg.StateDir, "ams-trays.json")
		b.HMS = &bambu.HMSFile{Path: filepath.Join(cfg.StateDir, "hms-texts.json")}
		// The 3MF files that slicers send to the deck for this printer.
		b.Store = &gcodestore.Store{Dir: cfg.GcodeDir, ThreeMF: true}
		b.SavePin = func(pin string) {
			s.Pin = pin
			if err := printer.SaveSetting(cfg.StateDir, s); err != nil {
				log.Printf("printer: keep the certificate of %s: %v", s.Name, err)
			}
		}
		return b
	}
	k := printer.NewKlippy(cfg.KlippySocket, &gcodestore.Store{Dir: cfg.GcodeDir})
	k.ModelDir = cfg.ModelDir
	k.ConfigPath = cfg.KlipperConfig
	k.Hist = hist
	// The cool-downs of the bed end at the release temperature of the Klipper estimates.
	k.HeatLog = &printer.HeatLog{Path: filepath.Join(cfg.StateDir, "heat-times.json"), Release: printer.ReleaseBed}
	hist.Stamp = k.View.Stamp
	k.Spools = &printer.Spools{Path: filepath.Join(cfg.StateDir, "spools.json")}
	k.View.Rolls = k.Spools
	// The picture of a print from the print view of zslice and the GPU, when the deck has them.
	if live := startZsliceView(cfg.GcodeDir); live != nil {
		k.View.Live = live
	}
	k.Mesh = printer.NewMeshStore(filepath.Join(cfg.StateDir, "bed-mesh.json"))
	// The Klipper host of the printer: klipper.service for the first printer, klipper@<id> for
	// another printer of the deck.
	k.RestartService = func() error { return system.AskHelper(system.HelperRequest{Action: "restart-klipper", Printer: id}) }
	k.StopService = func() error { return system.AskHelper(system.HelperRequest{Action: "stop-klipper", Printer: id}) }
	return k
}
