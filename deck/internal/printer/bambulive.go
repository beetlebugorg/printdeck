package printer

import (
	"bytes"
	"io"
	"log"
	"path"
	"strings"

	"printdeck/internal/bambu"
)

// The live filament of a Bambu Lab print. The printer reports no filament use: for a spool with no
// RFID tag of Bambu Lab, remain is -1. So at the start of a print the deck reads the G-code of
// the plate from the 3MF, and counts the filament of each filament number up to each layer and
// percent mark of the file (bambu.PlateUse). The layer and the percent of each report then give
// the filament that the print used from each tray, with the purge lines and the flushes. The deck
// shows those grams on the rolls during the print (Spools.SetLive), and subtracts them at the end
// (countUse).

// plateFile reads the 3MF of the print p: the copy in the store of a file that the deck sent, or
// the file on the card of the printer. The X2D reports the G-code in the 3MF as its file
// ("/data/Metadata/plate_1.gcode"), so the name of the task with ".gcode.3mf" is the name on the
// card. It calls fn with the file.
func (b *Bambu) plateFile(p bambuPrint, fn func(r io.ReaderAt, size int64) error) error {
	b.mu.Lock()
	sent := b.sent
	b.mu.Unlock()
	stored := p.stored
	if stored == "" && sent.path != "" && path.Base(p.file) == sent.name {
		stored = sent.path
	}
	if stored != "" && b.Store != nil {
		ra, size, closeFile, err := b.openFile(stored)
		if err != nil {
			return err
		}
		defer closeFile()
		return fn(ra, size)
	}
	file := p.file
	if !strings.HasSuffix(strings.ToLower(file), ".3mf") && p.subtask != "" {
		file = "/" + p.subtask + ".gcode.3mf"
	}
	data, err := fetch3MF(b.Client, file)
	if err != nil {
		return err
	}
	return fn(bytes.NewReader(data), int64(len(data)))
}

// readPlate returns the filaments of the plate of the print p, and the count of its G-code. A
// file with no G-code count still gives its filaments: use is then nil.
func (b *Bambu) readPlate(p bambuPrint) (fils []bambu.Filament, use *bambu.PlateUse, err error) {
	err = b.plateFile(p, func(r io.ReaderAt, size int64) error {
		var err error
		if fils, err = bambu.PlateFilaments(r, size, p.plate); err != nil {
			return err
		}
		if u, err := bambu.PlateUseOf(r, size, p.plate); err == nil {
			use = &u
		} else {
			log.Printf("bambu: no G-code count: %s: %v", p.file, err)
		}
		return nil
	})
	return fils, use, err
}

// loadUse reads the plate of the print p, for the live grams of its rolls. key is the print in the
// history: loadUse keeps the count only while key is the print now.
func (b *Bambu) loadUse(key string, p bambuPrint) {
	fils, use, err := b.readPlate(p)
	if err != nil {
		log.Printf("bambu: no live filament count: %s: %v", p.file, err)
		return
	}
	b.mu.Lock()
	if b.path == key {
		b.cur.fils, b.cur.use = fils, use
	}
	b.mu.Unlock()
	b.refreshLive()
	if b.notify != nil {
		b.notify()
	}
}

// slotOf returns the tray that fed the filament f of the print p, or -1. n is the count of the
// filaments of the plate.
func (p bambuPrint) slotOf(f bambu.Filament, n int) int {
	if i := f.ID - 1; i >= 0 && i < len(p.mapping) && p.mapping[i] >= 0 {
		return p.mapping[i]
	}
	// A print with one filament and one tray: that tray.
	if n == 1 && len(p.used) == 1 {
		for s := range p.used {
			return s
		}
	}
	return -1
}

// share returns the part of the filament f that the print p used, 0 to 1: from the G-code count
// at the last layer and percent, or else from the percent.
func (p bambuPrint) share(f bambu.Filament) float64 {
	if p.use != nil {
		if at := p.use.At(p.layer, int(p.percent)); f.ID >= 1 && f.ID <= len(at) {
			return at[f.ID-1]
		}
		return 0
	}
	return max(0, min(1, p.percent/100))
}

// liveGrams returns the grams that the print p used from each roll, by the ID of the roll, or nil
// before the deck has the filaments of the print.
func (p bambuPrint) liveGrams(links map[int]string) map[string]float64 {
	if p.use == nil {
		return nil
	}
	out := map[string]float64{}
	for _, f := range p.fils {
		if roll := links[p.slotOf(f, len(p.fils))]; roll != "" {
			out[roll] += f.Grams * p.share(f)
		}
	}
	return out
}

// refreshLive sets the live grams of the rolls from the print now. After the end of a print, the
// grams stay until countUse has subtracted them.
func (b *Bambu) refreshLive() {
	if b.Spools == nil {
		return
	}
	b.mu.Lock()
	p, printing, counting := b.cur, b.path != "" && !b.system, b.counting
	b.mu.Unlock()
	switch {
	case printing:
		b.Spools.SetLive(p.liveGrams(b.trayLinks()))
	case !counting:
		b.Spools.SetLive(nil)
	}
}
