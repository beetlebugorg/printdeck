package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"printdeck/internal/bambu"
	"strings"
	"time"

	"printdeck/internal/config"
	"printdeck/internal/printer"
	"printdeck/internal/system"
	"printdeck/internal/ui"
)

// The printers of a deck (specs/many-printers-on-one-deck.md): printers.json in the state folder
// of the deck. A deck with no file has one printer, as before. The first printer keeps the
// folders of the deck: the state folder, klipper.service and /var/lib/klipper/config. Each other
// printer has the folder printers/<id> in the state folder, and the Klipper host
// klipper@<id>.service with /var/lib/klipper/printers/<id>.

// deckPrinter is one printer of printers.json.
type deckPrinter struct {
	ID    string `json:"id"` // "" for the first printer
	Name  string `json:"name"`
	Label string `json:"label"`
	Color int    `json:"color"` // an index of ui.PrinterColors
}

func printersFile(stateDir string) string { return filepath.Join(stateDir, "printers.json") }

// loadDeckPrinters returns the printers of the deck. The first one has the ID "". A deck with no
// printers.json, or a file that does not read, has one printer.
func loadDeckPrinters(stateDir string) []deckPrinter {
	one := []deckPrinter{{Name: system.DeckName(), Label: ui.LabelFrom(system.DeckName())}}
	b, err := os.ReadFile(printersFile(stateDir))
	if err != nil {
		return one
	}
	var ps []deckPrinter
	if json.Unmarshal(b, &ps) != nil || len(ps) == 0 || ps[0].ID != "" {
		return one
	}
	seen := map[string]bool{"": true}
	out := ps[:1]
	for _, p := range ps[1:] {
		if system.ValidPrinterID(p.ID) && !seen[p.ID] {
			seen[p.ID] = true
			out = append(out, p)
		}
	}
	for i := range out {
		if out[i].Label == "" {
			out[i].Label = ui.LabelFrom(out[i].Name)
		}
		if out[i].Color < 0 || out[i].Color >= len(ui.PrinterColors) {
			out[i].Color = i % len(ui.PrinterColors)
		}
	}
	return out
}

// saveDeckPrinters writes printers.json.
func saveDeckPrinters(stateDir string, ps []deckPrinter) error {
	if len(ps) == 0 || ps[0].ID != "" {
		return errors.New("the first printer has no ID")
	}
	b, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return err
	}
	tmp := printersFile(stateDir) + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, printersFile(stateDir))
}

// config returns the settings of the deck for the printer p: its state folder, and the folders,
// the socket and the log of its Klipper host.
func (p deckPrinter) config(base config.Config) config.Config {
	if p.ID == "" {
		return base
	}
	c := base
	c.StateDir = filepath.Join(base.StateDir, "printers", p.ID)
	kd := system.PrinterKlipperDir(p.ID)
	c.KlipperConfig = filepath.Join(kd, "printer.cfg")
	c.GcodeDir = filepath.Join(kd, "gcodes")
	c.KlippySocket = filepath.Join("/run/klipper", p.ID, "klippy.sock")
	c.KlippyLog = filepath.Join("/run/klipper", p.ID, "klippy.log")
	// The main slicer key of the deck is the key of the first printer. Another printer has only
	// the keys of its pairings, so each key belongs to one printer.
	c.OctoPrintKey = ""
	return c
}

// identity is how the screens show the printer p.
func (p deckPrinter) identity() ui.Identity {
	id := p.ID
	if id == "" {
		id = "first"
	}
	return ui.Identity{ID: id, Name: p.Name, Label: p.Label, Color: ui.PrinterColors[p.Color%len(ui.PrinterColors)]}
}

// deckApps returns the App of the printer with the ID of its identity, or nil, for the remote
// screens of a deck with many printers. It is nil for a deck with one printer.
var deckApps func(id string) *ui.App

// deckState is the state of the printers of the deck for the updater: the state of a printer that
// prints, or else of the first printer. The deck restarts into an update only when no printer
// prints.
type deckState struct {
	printerBackend
	all []printerBackend
}

func (d deckState) State() printer.State {
	st := d.printerBackend.State()
	if st.Busy() {
		return st
	}
	for _, p := range d.all {
		if s := p.State(); s.Busy() {
			return s
		}
	}
	return st
}

// printerAdmin changes printers.json for Settings, Printers (ui.PrinterAdmin). A change starts
// to work after a restart of the deck.
type printerAdmin struct {
	base    config.Config
	running []deckPrinter // the printers that the deck runs now
	restart func()
	// helper asks the root helper for an action; tests replace it.
	helper func(system.HelperRequest) error
}

// FindBambu listens for the notices of the Bambu Lab printers on the network for a few seconds.
func (pa *printerAdmin) FindBambu() ([]ui.BambuFound, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	found, err := bambu.Discover(ctx, fmt.Sprintf(":%d", bambu.DiscoverPort))
	var out []ui.BambuFound
	for _, f := range found {
		out = append(out, ui.BambuFound{Addr: f.Addr, Serial: f.Serial, Name: f.Name})
	}
	return out, err
}

func (pa *printerAdmin) List() []ui.PrinterEntry {
	var out []ui.PrinterEntry
	for _, p := range loadDeckPrinters(pa.base.StateDir) {
		e := ui.PrinterEntry{ID: p.ID, Name: p.Name, Label: p.Label, Color: p.Color, Kind: "Klipper"}
		if s, err := printer.LoadSetting(p.config(pa.base).StateDir); err == nil {
			switch s.Kind {
			case printer.KindChiTu:
				e.Kind, e.Addr = "Resin", s.Addr
			case printer.KindBambu:
				e.Kind, e.Addr = "Bambu Lab", s.Addr
			}
		}
		out = append(out, e)
	}
	return out
}

// newPrinterID returns a new ID from the name: its letters and digits in lower case, and a number
// when the ID is in use.
func newPrinterID(name string, ps []deckPrinter) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	base := b.String()
	if base == "" || base[0] < 'a' || base[0] > 'z' {
		base = "p" + base
	}
	if len(base) > 12 {
		base = base[:12]
	}
	used := map[string]bool{}
	for _, p := range ps {
		used[p.ID] = true
	}
	id := base
	for n := 2; used[id] || !system.ValidPrinterID(id); n++ {
		id = fmt.Sprintf("%s%d", base, n)
	}
	return id
}

// add adds the printer with the name to printers.json, with a free colour, and returns it.
func (pa *printerAdmin) add(name string, kind func(p deckPrinter) error) error {
	ps := loadDeckPrinters(pa.base.StateDir)
	if len(ps) >= ui.MaxPrinters {
		return fmt.Errorf("a deck drives at most %d printers", ui.MaxPrinters)
	}
	used := map[int]bool{}
	for _, p := range ps {
		used[p.Color] = true
	}
	p := deckPrinter{ID: newPrinterID(name, ps), Name: name, Label: ui.LabelFrom(name)}
	for used[p.Color] && p.Color < len(ui.PrinterColors)-1 {
		p.Color++
	}
	if err := os.MkdirAll(p.config(pa.base).StateDir, 0o700); err != nil {
		return err
	}
	if err := kind(p); err != nil {
		return err
	}
	return saveDeckPrinters(pa.base.StateDir, append(ps, p))
}

// AddKlipper adds a Klipper printer: the helper starts its Klipper host, and the deck shows its
// printer setup after the restart.
func (pa *printerAdmin) AddKlipper(name string) error {
	return pa.add(name, func(p deckPrinter) error {
		return pa.helper(system.HelperRequest{Action: "add-printer", Printer: p.ID})
	})
}

// AddResin adds a resin printer with a ChiTu board at the address.
func (pa *printerAdmin) AddResin(name, addr string) error {
	return pa.add(name, func(p deckPrinter) error {
		return printer.SaveSetting(p.config(pa.base).StateDir, printer.Setting{Kind: printer.KindChiTu, Addr: addr, Name: name})
	})
}

// AddBambu adds a Bambu Lab printer at the address, with its serial number and its access code.
// The deck learns the certificate of the printer at the first connection.
func (pa *printerAdmin) AddBambu(name, addr, serial, code string) error {
	return pa.add(name, func(p deckPrinter) error {
		return printer.SaveSetting(p.config(pa.base).StateDir, printer.Setting{Kind: printer.KindBambu,
			Addr: addr, Serial: serial, Code: code, Name: name})
	})
}

func (pa *printerAdmin) Update(e ui.PrinterEntry) error {
	ps := loadDeckPrinters(pa.base.StateDir)
	for i := range ps {
		if ps[i].ID == e.ID {
			ps[i].Name, ps[i].Color = e.Name, e.Color
			ps[i].Label = e.Label
			if ps[i].Label == "" {
				ps[i].Label = ui.LabelFrom(e.Name)
			}
			return saveDeckPrinters(pa.base.StateDir, ps)
		}
	}
	return errors.New("no such printer")
}

// Remove removes a printer that is not the first one. Its Klipper host stops; its folders stay.
func (pa *printerAdmin) Remove(id string) error {
	if id == "" {
		return errors.New("the first printer stays")
	}
	ps := loadDeckPrinters(pa.base.StateDir)
	for i, p := range ps {
		if p.ID != id {
			continue
		}
		// A printer on the network has no Klipper host.
		if s, err := printer.LoadSetting(p.config(pa.base).StateDir); err != nil || s.Kind == printer.KindKlipper {
			if err := pa.helper(system.HelperRequest{Action: "remove-printer", Printer: id}); err != nil {
				return err
			}
		}
		return saveDeckPrinters(pa.base.StateDir, append(ps[:i:i], ps[i+1:]...))
	}
	return errors.New("no such printer")
}

func (pa *printerAdmin) Changed() bool {
	now := loadDeckPrinters(pa.base.StateDir)
	if len(now) != len(pa.running) {
		return true
	}
	for i := range now {
		if now[i] != pa.running[i] {
			return true
		}
	}
	return false
}

func (pa *printerAdmin) Restart() { pa.restart() }

// deckIdentities returns how the screens show each printer of the deck.
func deckIdentities(ps []deckPrinter, backends []printerBackend) []ui.Identity {
	ids := make([]ui.Identity, len(ps))
	for i, p := range ps {
		ids[i] = p.identity()
		_, ids[i].Resin = backends[i].(*printer.ChiTu)
	}
	return ids
}

// deckStateDir is the state folder of the deck, for the parts of the deck (the paired phones) in
// the code of a printer, or "" before run sets it.
var deckStateDir string

// deckDir returns the state folder of the deck, or the state folder of cfg when there is none.
func deckDir(cfg config.Config) string {
	if deckStateDir != "" {
		return deckStateDir
	}
	return cfg.StateDir
}
