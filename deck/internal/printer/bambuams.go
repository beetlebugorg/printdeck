package printer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"sort"
	"strconv"
	"time"

	"printdeck/internal/bambu"
)

// The AMS of a Bambu Lab printer: the trays and their spools, and a roll of the deck for each tray.
// The AMS knows the filament left only on a spool with an RFID tag of Bambu Lab. For the other
// spools the deck counts it: each tray has a roll of the deck (LinkTray), and at the end of a print
// the deck reads the grams of each filament from the 3MF of the print on the card of the printer,
// and subtracts them from the roll of the tray that fed it.

// AMSTray is one tray of an AMS, for the screens.
type AMSTray struct {
	Slot     int // Unit*4 + Index, the number of the tray in the reports of the printer
	Unit     int // the AMS, from 0
	Index    int // the tray in the AMS, from 0
	Loaded   bool
	Material string // "PLA"
	Brand    string // "Bambu PLA Basic", or ""
	Color    string // "#rrggbb", or "" with no color
	// Remain is the percent left that the printer knows (a spool with an RFID tag), or -1.
	Remain int
	Active bool   // the tray feeds the nozzle now
	Roll   string // the ID of the roll of the deck in the tray, or ""
}

// AMSUnit is one AMS.
type AMSUnit struct {
	Index       int
	Temp        float64 // °C
	HumidityPct float64 // relative humidity in percent
	Humidity    int     // the level of the printer, 1 (dry) to 5 (wet)
}

// AMS is the AMS units of the printer and their trays.
type AMS struct {
	Units []AMSUnit
	Trays []AMSTray
}

// AMS returns the AMS units and trays of the last report, with the roll of each tray.
func (b *Bambu) AMS() AMS {
	b.mu.Lock()
	r, has := b.last, b.has
	b.mu.Unlock()
	var out AMS
	if !has {
		return out
	}
	links := b.trayLinks()
	active := r.ActiveSlot()
	for _, u := range r.AMS.Units {
		ui, _ := strconv.Atoi(u.ID)
		out.Units = append(out.Units, AMSUnit{Index: ui, Temp: float64(u.Temp),
			HumidityPct: float64(u.HumidityPct), Humidity: u.Humidity.Int()})
		for _, t := range u.Trays {
			ti, _ := strconv.Atoi(t.ID)
			slot := ui*4 + ti
			tr := AMSTray{Slot: slot, Unit: ui, Index: ti, Loaded: r.TrayLoaded(slot), Material: t.Type,
				Brand: t.Brand, Color: t.ColorHex(), Remain: -1, Active: slot == active, Roll: links[slot]}
			if t.Known() {
				tr.Remain = t.Remain.Int()
			}
			out.Trays = append(out.Trays, tr)
		}
	}
	sort.Slice(out.Trays, func(i, j int) bool { return out.Trays[i].Slot < out.Trays[j].Slot })
	return out
}

// Loaded returns the trays of the AMS for the status panel, with the part left from the roll of the
// deck in each tray, or from the printer for a spool with an RFID tag.
func (b *Bambu) Loaded() []Loaded {
	var out []Loaded
	for _, t := range b.AMS().Trays {
		l := Loaded{Color: t.Color, Material: t.Material, Fraction: -1, Active: t.Active, Empty: !t.Loaded}
		if t.Roll != "" && b.Spools != nil {
			if sp, ok := b.Spools.Get(t.Roll); ok {
				l.Fraction = sp.Fraction()
				if sp.Color != "" {
					l.Color = sp.Color // the roll knows the color when the printer does not
				}
			}
		}
		if t.Remain >= 0 {
			l.Fraction = float64(t.Remain) / 100
		}
		out = append(out, l)
	}
	return out
}

// trayLinks returns the roll of each tray, from TrayPath.
func (b *Bambu) trayLinks() map[int]string {
	b.trayMu.Lock()
	defer b.trayMu.Unlock()
	if b.trays != nil {
		return b.trays
	}
	b.trays = map[int]string{}
	if b.TrayPath != "" {
		if data, err := os.ReadFile(b.TrayPath); err == nil {
			var m map[string]string
			if json.Unmarshal(data, &m) == nil {
				for k, v := range m {
					if n, err := strconv.Atoi(k); err == nil {
						b.trays[n] = v
					}
				}
			}
		}
	}
	return b.trays
}

// LinkTray puts the roll of the deck with the ID in the tray slot. "" takes the roll out. A roll
// is in one tray at most.
func (b *Bambu) LinkTray(slot int, roll string) error {
	links := b.trayLinks()
	b.trayMu.Lock()
	defer b.trayMu.Unlock()
	for s, id := range links {
		if id == roll && roll != "" {
			delete(links, s)
		}
	}
	if roll == "" {
		delete(links, slot)
	} else {
		links[slot] = roll
	}
	if b.TrayPath == "" {
		return nil
	}
	m := map[string]string{}
	for k, v := range links {
		m[strconv.Itoa(k)] = v
	}
	data, _ := json.Marshal(m)
	tmp := b.TrayPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, b.TrayPath)
}

// AddTrayRoll adds a new roll of the deck with grams of filament for the spool in the tray slot,
// with its material, and puts it in the tray. The color is color ("#rrggbb"), or the color of the
// tray when color is "".
func (b *Bambu) AddTrayRoll(slot int, grams float64, color string) (Spool, error) {
	if b.Spools == nil {
		return Spool{}, ErrNotSupported
	}
	var tr AMSTray
	found := false
	for _, t := range b.AMS().Trays {
		if t.Slot == slot {
			tr, found = t, true
		}
	}
	if !found {
		return Spool{}, fmt.Errorf("the printer has no tray %d", slot+1)
	}
	name := tr.Brand
	if name == "" {
		name = tr.Material
	}
	reel := b.Spools.LastReel()
	sp := b.Spools.AddOut(name, tr.Material, grams+reel, reel, 0, time.Now())
	if grams > 0 {
		b.Spools.SetSize(sp.ID, grams)
	}
	if color == "" {
		color = tr.Color
	}
	if color != "" {
		b.Spools.SetColor(sp.ID, color)
	}
	if err := b.LinkTray(slot, sp.ID); err != nil {
		return sp, err
	}
	sp, _ = b.Spools.Get(sp.ID)
	return sp, nil
}

// bambuPrint is what the deck keeps of the print now for its filament count.
type bambuPrint struct {
	file    string       // gcode_file: the 3MF on the card
	plate   int          // plate_idx
	mapping []int        // the tray of each filament (Report.AMSMapping)
	used    map[int]bool // the trays that fed the nozzle during the print
	percent float64      // the last progress
}

// track keeps the trays of the print now. The caller holds mu.
func (b *Bambu) track(r bambu.Report) {
	p := &b.cur
	if r.File != "" {
		p.file = r.File
	}
	if n := r.PlateIdx.Int(); n > 0 {
		p.plate = n
	}
	if m := r.AMSMapping(); len(m) > 0 {
		p.mapping = m
	}
	if s := r.ActiveSlot(); s >= 0 {
		if p.used == nil {
			p.used = map[int]bool{}
		}
		p.used[s] = true
	}
	p.percent = float64(r.Percent)
}

// fetch3MF reads the 3MF of the print from the card of the printer. It tries the path of the
// report, and its name at the top of the card and in /cache.
var fetch3MF = func(c *bambu.Client, file string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var err error
	for _, p := range []string{file, "/" + path.Base(file), "/cache/" + path.Base(file)} {
		var buf bytes.Buffer
		if err = c.Fetch(ctx, p, &buf, 512<<20); err == nil {
			return buf.Bytes(), nil
		}
	}
	return nil, err
}

// countUse subtracts the filament of the finished print p from the rolls of its trays: all the
// filament of a complete print, or the share of its progress for a print that stopped. It
// returns the grams for each roll.
func (b *Bambu) countUse(p bambuPrint, complete bool) map[string]float64 {
	if b.Spools == nil || p.file == "" {
		return nil
	}
	data, err := fetch3MF(b.Client, p.file)
	if err != nil {
		log.Printf("bambu: no filament count: the 3MF %s: %v", p.file, err)
		return nil
	}
	fils, err := bambu.PlateFilaments(bytes.NewReader(data), int64(len(data)), p.plate)
	if err != nil {
		log.Printf("bambu: no filament count: %s: %v", p.file, err)
		return nil
	}
	share := 1.0
	if !complete {
		share = max(0, min(1, p.percent/100))
	}
	links := b.trayLinks()
	used := map[string]float64{}
	for _, f := range fils {
		slot := -1
		if i := f.ID - 1; i >= 0 && i < len(p.mapping) {
			slot = p.mapping[i]
		}
		if slot < 0 && len(fils) == 1 && len(p.used) == 1 {
			for s := range p.used {
				slot = s
			}
		}
		roll := ""
		if slot >= 0 {
			roll = links[slot]
		}
		if roll == "" {
			log.Printf("bambu: filament %d (%.1f g): no roll in tray %d", f.ID, f.Grams, slot+1)
			continue
		}
		g := f.Grams * share
		if _, _, ok := b.Spools.UseOn(roll, g); ok {
			used[roll] += g
		}
	}
	log.Printf("bambu: filament of %s: %v (mapping %v, trays %v)", p.file, used, p.mapping, p.used)
	return used
}
