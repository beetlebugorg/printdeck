package printer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"slices"
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
	Active bool    // the tray feeds the nozzle now
	Roll   string  // the ID of the roll of the deck in the tray, or ""
	Tag    string  // the RFID tag of a spool of Bambu Lab, or ""
	Weight float64 // the grams of a full spool, from its RFID tag, or 0
	// External is true for the external spool (out of the AMS): Slot is then 255 or 254, the ID
	// in the reports, and Unit is -1.
	External bool
	// Feeds is the nozzle that the spool is in now: 0 the nozzle (the main nozzle of a printer
	// with two), 1 the aux nozzle, or -1.
	Feeds int
	// Nozzle is the nozzle that the spool is attached to, loaded or not: 0 the main nozzle, 1 the
	// aux nozzle, or -1 on a printer with one nozzle or when the report does not say.
	Nozzle int
	// Temp is the nozzle temperature of the filament for a load, from the printer, or 0.
	Temp int
	// InfoIdx is the filament code of Bambu Studio for the spool (tray_info_idx), or "".
	InfoIdx string
}

// AMSUnit is one AMS.
type AMSUnit struct {
	// Drying is true while the AMS dries: DryLeft is the time left, and DryTemp, DryHours and
	// DryFilament the drying that runs.
	Drying      bool
	DryLeft     time.Duration
	DryTemp     int
	DryHours    int
	DryFilament string
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
	feeds := map[int]int{} // the nozzle of each tray in a nozzle
	ex := r.Extruders()
	for _, e := range ex {
		if e.Feed >= 0 {
			feeds[e.Feed] = 0
			if e.ID != mainExtruder {
				feeds[e.Feed] = 1
			}
		}
	}
	feed := func(slot int) int {
		if n, ok := feeds[slot]; ok {
			return n
		}
		return -1
	}
	// attached returns the nozzle of a spool on the extruder id. The nozzle that the spool is in
	// now comes first.
	attached := func(slot, id int) int {
		if n, ok := feeds[slot]; ok {
			return n
		}
		switch {
		case len(ex) < 2 || id < 0:
			return -1
		case id == mainExtruder:
			return 0
		}
		return 1
	}
	for _, u := range r.AMS.Units {
		ui, _ := strconv.Atoi(u.ID)
		out.Units = append(out.Units, AMSUnit{Index: ui, Temp: float64(u.Temp),
			HumidityPct: float64(u.HumidityPct), Humidity: u.Humidity.Int(),
			Drying: u.DryTime > 0, DryLeft: time.Duration(u.DryTime) * time.Minute,
			DryTemp: u.DrySetting.Temp.Int(), DryHours: u.DrySetting.Hours.Int(), DryFilament: u.DrySetting.Filament})
		for _, t := range u.Trays {
			ti, _ := strconv.Atoi(t.ID)
			slot := ui*4 + ti
			tr := AMSTray{Slot: slot, Unit: ui, Index: ti, Loaded: r.TrayLoaded(slot), Material: t.Type,
				Brand: t.Brand, Color: t.ColorHex(), Remain: -1, Active: slot == active, Roll: links[slot],
				Tag: t.Tag(), Weight: float64(t.Weight), Feeds: feed(slot), Nozzle: attached(slot, u.Extruder()), Temp: t.TempMax.Int(), InfoIdx: t.InfoIdx}
			if t.Known() {
				tr.Remain = t.Remain.Int()
			}
			out.Trays = append(out.Trays, tr)
		}
	}
	// The external spools: each one that the printer knows a material for or that feeds a nozzle,
	// and on a printer with two nozzles the external spool of the aux nozzle.
	for id, t := range r.ExternalTrays() {
		_, fed := feeds[id]
		if t.Type == "" && !fed && !(len(ex) >= 2 && id == bambu.ExternalMain) {
			continue
		}
		tr := AMSTray{Slot: id, Unit: -1, Index: 0, Loaded: t.Type != "", Material: t.Type, Brand: t.Brand,
			Color: t.ColorHex(), Remain: -1, Active: id == active, Roll: links[id], Tag: t.Tag(),
			Weight: float64(t.Weight), External: true, Feeds: feed(id), Nozzle: attached(id, bambu.ExternalExtruder(id)), Temp: t.TempMax.Int(), InfoIdx: t.InfoIdx}
		if t.Known() {
			tr.Remain = t.Remain.Int()
		}
		out.Trays = append(out.Trays, tr)
	}
	sort.Slice(out.Trays, func(i, j int) bool { return out.Trays[i].Slot < out.Trays[j].Slot })
	return out
}

// Loaded returns the trays of the AMS for the status panel, with the part left from the roll of the
// deck in each tray, or from the printer for a spool with an RFID tag.
func (b *Bambu) Loaded() []Loaded {
	var out []Loaded
	for _, t := range b.AMS().Trays {
		l := Loaded{Color: t.Color, Material: t.Material, Fraction: -1, Active: t.Active, Empty: !t.Loaded,
			External: t.External}
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

// trayLinks returns a copy of the roll of each tray, from TrayPath.
func (b *Bambu) trayLinks() map[int]string {
	b.trayMu.Lock()
	defer b.trayMu.Unlock()
	out := map[int]string{}
	for k, v := range b.linksLocked() {
		out[k] = v
	}
	return out
}

// linksLocked returns the roll of each tray, and reads TrayPath once. The caller holds trayMu.
func (b *Bambu) linksLocked() map[int]string {
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
	b.trayMu.Lock()
	defer b.trayMu.Unlock()
	links := b.linksLocked()
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
	return b.saveLinksLocked()
}

// saveLinks writes the roll of each tray to TrayPath.
func (b *Bambu) saveLinks() error {
	b.trayMu.Lock()
	defer b.trayMu.Unlock()
	b.linksLocked()
	return b.saveLinksLocked()
}

// saveLinksLocked writes the roll of each tray. The caller holds trayMu.
func (b *Bambu) saveLinksLocked() error {
	if b.TrayPath == "" {
		return nil
	}
	m := map[string]string{}
	for k, v := range b.trays {
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
	file    string       // gcode_file: the 3MF on the card, or the G-code in it (the X2D)
	subtask string       // subtask_name: the name of the 3MF with no .gcode.3mf
	stored  string       // the copy of the 3MF in the store, for a file that the deck sent, or ""
	plate   int          // plate_idx
	mapping []int        // the tray of each filament (Report.AMSMapping)
	used    map[int]bool // the trays that fed the nozzle during the print
	percent float64      // the last progress
	layer   int          // the last layer
	// fils and use are the filaments of the plate and the count of its G-code (bambulive.go),
	// after loadUse. use is nil before, or for a file with no G-code count.
	fils    []bambu.Filament
	use     *bambu.PlateUse
	loading bool // loadUse runs or ran for this print
}

// track keeps the trays of the print now. The caller holds mu.
func (b *Bambu) track(r bambu.Report) {
	p := &b.cur
	if r.File != "" {
		p.file = r.File
	}
	if r.Subtask != "" {
		p.subtask = r.Subtask
	}
	// A print of a file that the deck sent: its copy in the store (update sets b.path).
	if b.sent.path != "" && b.path == b.sent.path {
		p.stored = b.sent.path
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
	p.layer = r.Layer.Int()
	if p.file != "" && !p.loading && b.Spools != nil && !b.system {
		p.loading = true
		b.useWG.Add(1)
		go func(key string, p bambuPrint) {
			defer b.useWG.Done()
			b.loadUse(key, p)
		}(b.path, *p)
	}
}

// max3MF is the largest 3MF that the deck reads from the card of the printer, in memory.
const max3MF = 128 << 20

// fetch3MF reads the 3MF of the print from the card of the printer. It tries the path of the
// report, and its name at the top of the card and in /cache.
var fetch3MF = func(c *bambu.Client, file string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var err error
	for _, p := range []string{file, "/" + path.Base(file), "/cache/" + path.Base(file)} {
		var buf bytes.Buffer
		if err = c.Fetch(ctx, p, &buf, max3MF); err == nil {
			return buf.Bytes(), nil
		}
	}
	return nil, err
}

// countUse subtracts the filament of the finished print p from the rolls of its trays: all the
// filament of a complete print, or the part that a print that stopped used (bambuPrint.share). It
// returns the grams for each roll. At its end, the rolls have no live grams.
func (b *Bambu) countUse(p bambuPrint, complete bool) map[string]float64 {
	defer b.endCount()
	if b.Spools == nil || p.file == "" {
		return nil
	}
	if p.fils == nil {
		var err error
		if p.fils, p.use, err = b.readPlate(p); err != nil {
			log.Printf("bambu: no filament count: %s: %v", p.file, err)
			return nil
		}
	}
	links := b.trayLinks()
	used := map[string]float64{}
	for _, f := range p.fils {
		slot := p.slotOf(f, len(p.fils))
		roll := links[slot]
		if roll == "" {
			log.Printf("bambu: filament %d (%.1f g): no roll in tray %d", f.ID, f.Grams, slot+1)
			continue
		}
		g := f.Grams
		if !complete {
			g *= p.share(f)
		}
		if _, _, ok := b.Spools.UseOn(roll, g); ok {
			used[roll] += g
		}
	}
	log.Printf("bambu: filament of %s: %v (mapping %v, trays %v)", p.file, used, p.mapping, p.used)
	return used
}

// endCount ends the count of a print: the rolls have the grams of the print in Used now, so their
// live grams go.
func (b *Bambu) endCount() {
	b.mu.Lock()
	b.counting = false
	b.mu.Unlock()
	b.refreshLive()
}

// The change of a spool: the AMS reports a spool in a tray, or no spool. The deck follows each
// change by itself:
//
//   - a spool goes out: the roll of the tray goes out of the tray;
//   - a spool with an RFID tag of Bambu Lab goes in: the roll with that tag goes in the tray, or a
//     new roll with the weight that the printer gives;
//   - another spool goes in: the screens ask which roll it is (TrayQuestion, AnswerTray).
//
// On the first report of a deck with no ams-trays.json, each tray with a spool and no roll asks.

// traySpool is what the deck compares between two reports for a tray.
type traySpool struct {
	loaded bool
	tag    string
}

// watchTrays compares the trays of r with the last report, and handles each change in a
// goroutine. The caller holds mu.
func (b *Bambu) watchTrays(r bambu.Report) {
	now := map[int]traySpool{}
	for _, u := range r.AMS.Units {
		ui, _ := strconv.Atoi(u.ID)
		for _, t := range u.Trays {
			ti, _ := strconv.Atoi(t.ID)
			slot := ui*4 + ti
			now[slot] = traySpool{loaded: r.TrayLoaded(slot), tag: t.Tag()}
		}
	}
	// An external spool has no sensor: it is loaded when the printer has a material for it.
	for id, t := range r.ExternalTrays() {
		now[id] = traySpool{loaded: t.Type != "", tag: t.Tag()}
	}
	prev, first := b.trayPrev, b.trayPrev == nil
	b.trayPrev = now
	if len(now) == 0 {
		return
	}
	var gone, came []int
	for slot, t := range now {
		p, ok := prev[slot]
		switch {
		case first:
			if t.loaded {
				came = append(came, slot)
			}
		case !ok:
		case p.loaded && !t.loaded:
			gone = append(gone, slot)
		case t.loaded && (!p.loaded || p.tag != t.tag):
			came = append(came, slot)
		}
	}
	if len(gone) == 0 && len(came) == 0 {
		return
	}
	sort.Ints(came)
	b.trayWG.Add(1)
	go func() {
		defer b.trayWG.Done()
		b.spoolsChanged(gone, came, first)
	}()
}

// spoolsChanged handles the spools that went out of the trays gone and into the trays came. On
// the first report (first), only a deck with no tray file asks, and only for a tray with no roll.
func (b *Bambu) spoolsChanged(gone, came []int, first bool) {
	if first {
		if _, err := os.Stat(b.TrayPath); b.TrayPath == "" || err == nil {
			// The deck knows the trays from before: a tag can still find its roll.
			var tagged []int
			for _, slot := range came {
				if t, ok := b.tray(slot); ok && t.Tag != "" && t.Roll == "" {
					tagged = append(tagged, slot)
				}
			}
			came = tagged
		}
	}
	for _, slot := range gone {
		if b.trayLinks()[slot] != "" {
			b.LinkTray(slot, "")
			log.Printf("bambu: the spool of tray %d went out", slot+1)
		}
	}
	ask := false
	for _, slot := range came {
		t, ok := b.tray(slot)
		if !ok || b.Spools == nil {
			continue
		}
		if t.Tag != "" {
			b.tagRoll(t)
			continue
		}
		if first && t.Roll != "" {
			continue
		}
		// A new spool with no tag: its old roll goes out, and the screens ask.
		if t.Roll != "" {
			b.LinkTray(slot, "")
		}
		b.trayMu.Lock()
		if !slices.Contains(b.pending, slot) {
			b.pending = append(b.pending, slot)
		}
		b.trayMu.Unlock()
		ask = true
	}
	if b.TrayPath != "" && first {
		if _, err := os.Stat(b.TrayPath); err != nil {
			b.saveLinks() // the next start does not ask again
		}
	}
	if ask && b.OnTrayQuestion != nil {
		b.OnTrayQuestion()
	}
}

// tray returns the tray slot of the last report.
func (b *Bambu) tray(slot int) (AMSTray, bool) {
	for _, t := range b.AMS().Trays {
		if t.Slot == slot {
			return t, true
		}
	}
	return AMSTray{}, false
}

// tagRoll puts the roll with the RFID tag of the spool of t in the tray, or a new roll with the
// weight and the percent left that the printer gives.
func (b *Bambu) tagRoll(t AMSTray) {
	if sp, ok := b.Spools.ByTag(t.Tag); ok {
		if t.Remain >= 0 {
			b.Spools.SetLeft(sp.ID, sp.Capacity()*float64(t.Remain)/100)
		}
		b.LinkTray(t.Slot, sp.ID)
		log.Printf("bambu: tray %d has roll #%s again (RFID)", t.Slot+1, sp.ID)
		return
	}
	full := t.Weight
	if full <= 0 {
		full = DefaultSize
	}
	left := full
	if t.Remain >= 0 {
		left = full * float64(t.Remain) / 100
	}
	sp, err := b.AddTrayRoll(t.Slot, full, "")
	if err != nil {
		log.Printf("bambu: tray %d: %v", t.Slot+1, err)
		return
	}
	b.Spools.SetTag(sp.ID, t.Tag)
	b.Spools.SetLeft(sp.ID, left)
	log.Printf("bambu: tray %d has a new roll #%s (RFID, %.0f g)", t.Slot+1, sp.ID, left)
}

// TrayQuestion returns the first tray with a new spool that the screens ask about. ok is false
// when no tray waits for an answer.
func (b *Bambu) TrayQuestion() (AMSTray, bool) {
	for {
		b.trayMu.Lock()
		if len(b.pending) == 0 {
			b.trayMu.Unlock()
			return AMSTray{}, false
		}
		slot := b.pending[0]
		b.trayMu.Unlock()
		if t, ok := b.tray(slot); ok && t.Loaded && t.Roll == "" {
			return t, true
		}
		b.AnswerTray(slot) // the spool went out, or the tray has a roll now
	}
}

// AnswerTray takes the tray slot out of the questions.
func (b *Bambu) AnswerTray(slot int) {
	b.trayMu.Lock()
	defer b.trayMu.Unlock()
	b.pending = slices.DeleteFunc(b.pending, func(s int) bool { return s == slot })
}

// loadTemp is the nozzle temperature for a load of the filament of t: its temperature from the
// printer, or 220 °C.
func loadTemp(t AMSTray) int {
	if t.Temp > 0 {
		return t.Temp
	}
	return 220
}

// LoadTray loads the filament of the tray slot into its nozzle: the printer unloads the filament
// in the nozzle first, at its temperature. It returns an error during a print.
func (b *Bambu) LoadTray(slot int) error {
	if b.State().Busy() {
		return fmt.Errorf("the printer prints: load the filament after the print")
	}
	t, ok := b.tray(slot)
	if !ok || !t.Loaded {
		return fmt.Errorf("the tray has no spool")
	}
	return b.command("The load of the filament", func() error { return b.Client.ChangeFilament(slot, loadTemp(t)) })
}

// UnloadTray pulls the filament out of the nozzle in use back to the AMS of its tray (or the
// external spool). It returns an error during a print, or with no spool in the nozzle.
func (b *Bambu) UnloadTray() error {
	if b.State().Busy() {
		return fmt.Errorf("the printer prints: unload the filament after the print")
	}
	for _, o := range b.AMS().Trays {
		if o.Active {
			ams := o.Unit
			if o.External {
				ams = 255
			}
			return b.command("The unload of the filament", func() error { return b.Client.UnloadFilament(ams, loadTemp(o)) })
		}
	}
	return fmt.Errorf("no spool feeds the nozzle")
}

// The drying of an AMS 2 Pro: 45 to 65 °C, up to 24 hours.
const (
	minDryTemp  = 45
	maxDryTemp  = 65
	maxDryHours = 24
)

// DryAMS starts the drying of the AMS unit for the filament type, at temp °C for hours hours. It
// returns an error during a print: the printer does not start a drying then.
func (b *Bambu) DryAMS(unit int, filament string, temp, hours int) error {
	if b.State().Busy() {
		return fmt.Errorf("the printer prints: dry the AMS after the print")
	}
	if temp < minDryTemp || temp > maxDryTemp || hours < 1 || hours > maxDryHours {
		return fmt.Errorf("a drying of %d° for %d h is out of range", temp, hours)
	}
	return b.command("The drying of the AMS", func() error { return b.Client.DryAMS(unit, temp, hours, filament) })
}

// StopDryAMS stops the drying of the AMS unit.
func (b *Bambu) StopDryAMS(unit int) error {
	return b.command("The stop of the drying", func() error { return b.Client.StopDryAMS(unit) })
}

// ReadTrayTag asks the AMS to read the RFID tag of the spool in the tray slot again.
func (b *Bambu) ReadTrayTag(slot int) error {
	return b.command("The RFID read", func() error { return b.Client.ReadRFID(slot) })
}
