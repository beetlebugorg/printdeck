package printer

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"math"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"printdeck/internal/bambu"
	"printdeck/internal/gcodemeta"
)

// The files of a Bambu Lab printer: the sliced 3MF files of Bambu Studio and OrcaSlicer in the
// store of the deck. A print sends the 3MF to the card of the printer (FTPS), and starts the plate
// with the trays of the AMS that match the filaments of the plate.

// bambuFile is what the deck read of a 3MF of the store, by its path and its time.
type bambuFile struct {
	mod    time.Time
	plates []bambu.Plate
	pic    image.Image
	err    error
}

// threeMF returns the plates and the picture of the 3MF path of the store, read once for each
// version of the file.
func (b *Bambu) threeMF(p string) (bambuFile, error) {
	if b.Store == nil {
		return bambuFile{}, ErrNotSupported
	}
	fi, err := b.Store.Stat(p)
	if err != nil {
		return bambuFile{}, err
	}
	b.filesMu.Lock()
	if f, ok := b.files3MF[p]; ok && f.mod.Equal(fi.ModTime()) {
		b.filesMu.Unlock()
		return f, f.err
	}
	b.filesMu.Unlock()
	ra, size, closeFile, err := b.openFile(p)
	if err != nil {
		return bambuFile{}, err
	}
	defer closeFile()
	f := bambuFile{mod: fi.ModTime()}
	f.plates, f.err = bambu.Plates(ra, size)
	if f.err == nil {
		f.pic = bambu.PlatePicture(ra, size, f.plates[0].Index)
	}
	b.filesMu.Lock()
	if b.files3MF == nil {
		b.files3MF = map[string]bambuFile{}
	}
	b.files3MF[p] = f
	b.filesMu.Unlock()
	return f, f.err
}

// openFile opens the 3MF p of the store for the zip reader: the store opens it in its folder
// (os.OpenInRoot), so p cannot leave the folder. The zip reader reads only the parts that it
// needs.
func (b *Bambu) openFile(p string) (io.ReaderAt, int64, func(), error) {
	rc, err := b.Store.Open(p)
	if err != nil {
		return nil, 0, nil, err
	}
	f, ok := rc.(*os.File)
	if !ok {
		rc.Close()
		return nil, 0, nil, errors.New("the store gives no file")
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, nil, err
	}
	return f, fi.Size(), func() { f.Close() }, nil
}

// Files returns the 3MF files of the store, newest first.
func (b *Bambu) Files() []File {
	if b.Store == nil {
		return nil
	}
	return b.Store.Files()
}

// LoadFiles reads the list of the store again. Files reads the store each time.
func (b *Bambu) LoadFiles() {}

// FileMeta returns what the slicer wrote in the 3MF about its first plate: the time, the grams,
// the material, the color and the picture of the plate.
func (b *Bambu) FileMeta(p string) gcodemeta.Meta {
	f, err := b.threeMF(p)
	if err != nil || len(f.plates) == 0 {
		return gcodemeta.Meta{}
	}
	pl := f.plates[0]
	m := gcodemeta.Meta{PrintTime: time.Duration(pl.Seconds) * time.Second, FilamentG: pl.Grams, Thumbnail: f.pic}
	var types []string
	for _, fl := range pl.Filaments {
		m.FilamentMM += fl.Meters * 1000
		if !containsFold(types, fl.Type) {
			types = append(types, fl.Type)
		}
	}
	m.Material = strings.Join(types, " + ")
	if len(pl.Filaments) == 1 {
		m.Color = strings.ToLower(pl.Filaments[0].Color)
	}
	return m
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

// FilePicture returns the picture of the first plate of a 3MF, or nil.
func (b *Bambu) FilePicture(f File, _ string) image.Image {
	bf, err := b.threeMF(f.Path)
	if err != nil {
		return nil
	}
	return bf.pic
}

// DeleteFile removes a 3MF from the store of the deck. The card of the printer keeps its copy.
func (b *Bambu) DeleteFile(p string) error {
	if b.Store == nil {
		return ErrNotSupported
	}
	if st := b.State(); st.Busy() && b.sentPath() == p {
		return errors.New("This file prints now.")
	}
	return b.Store.Delete(p)
}

// sentPath returns the path in the store of the last file that the deck started.
func (b *Bambu) sentPath() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sent.path
}

// TrayMatch is the tray of the AMS for a filament of a plate.
type TrayMatch struct {
	Filament bambu.Filament
	Slot     int // the tray, or -1 when no tray has the material
}

// MatchTrays returns the tray of each filament of the plate: a tray with a spool of the same
// material, the nearest color first, each tray once while other trays match.
func (b *Bambu) MatchTrays(pl bambu.Plate) []TrayMatch {
	trays := b.AMS().Trays
	colorOf := func(t AMSTray) string {
		if t.Roll != "" && b.Spools != nil {
			if sp, ok := b.Spools.Get(t.Roll); ok && sp.Color != "" {
				return sp.Color
			}
		}
		return t.Color
	}
	used := map[int]bool{}
	var out []TrayMatch
	for _, f := range pl.Filaments {
		best, bestD := -1, math.Inf(1)
		for _, t := range trays {
			if !t.Loaded || !strings.EqualFold(t.Material, f.Type) {
				continue
			}
			d := colorDistance(colorOf(t), f.Color)
			if used[t.Slot] {
				d += 1e6 // a tray that another filament has is the last choice
			}
			if d < bestD {
				best, bestD = t.Slot, d
			}
		}
		if best >= 0 {
			used[best] = true
		}
		out = append(out, TrayMatch{Filament: f, Slot: best})
	}
	return out
}

// FileTrays returns the tray of each filament of the first plate of the 3MF p of the store.
func (b *Bambu) FileTrays(p string) ([]TrayMatch, error) {
	f, err := b.threeMF(p)
	if err != nil {
		return nil, err
	}
	return b.MatchTrays(f.plates[0]), nil
}

// colorDistance is the distance of two colors "#rrggbb" in RGB. An unknown color is far from each
// color.
func colorDistance(a, b string) float64 {
	ca, ok1 := ParseHex(a)
	cb, ok2 := ParseHex(b)
	if !ok1 || !ok2 {
		return 1000
	}
	dr, dg, db := float64(ca.R)-float64(cb.R), float64(ca.G)-float64(cb.G), float64(ca.B)-float64(cb.B)
	return math.Sqrt(dr*dr + dg*dg + db*db)
}

// bambuSent is the last file that the deck sent and started.
type bambuSent struct {
	path string // the path in the store: the filament count reads it there
	name string // the name on the card
}

// PrintFile sends the 3MF path of the store to the card of the printer and starts its first plate
// with the matching trays of the AMS. The progress and an error show in the panel.
func (b *Bambu) PrintFile(p string) {
	go func() {
		if err := b.sendAndStart(p); err != nil {
			log.Printf("bambu: print %s: %v", p, err)
			b.mu.Lock()
			b.st.Upload.Result, b.st.Upload.Err, b.st.Upload.Ended = "failed", err.Error(), time.Now()
			b.st.Message = "The print did not start: " + err.Error()
			b.mu.Unlock()
		}
	}()
}

// sendAndStart sends and starts the file p.
func (b *Bambu) sendAndStart(p string) error {
	if st := b.State(); !st.Connected || st.Busy() {
		return errors.New("the printer is busy or offline")
	}
	f, err := b.threeMF(p)
	if err != nil {
		return fmt.Errorf("the file is not a sliced 3MF: %w", err)
	}
	pl := f.plates[0]
	trays := make([]int, 0, len(pl.Filaments))
	for _, m := range b.MatchTrays(pl) {
		if m.Slot < 0 {
			return fmt.Errorf("no tray of the AMS has %s for filament %d", m.Filament.Type, m.Filament.ID)
		}
		for len(trays) < m.Filament.ID {
			trays = append(trays, -1)
		}
		trays[m.Filament.ID-1] = m.Slot
	}
	rc, err := b.Store.Open(p)
	if err != nil {
		return err
	}
	defer rc.Close()
	fh, ok := rc.(*os.File)
	if !ok {
		return errors.New("the store gives no file")
	}
	fi, err := fh.Stat()
	if err != nil {
		return err
	}
	name := path.Base(p)
	b.mu.Lock()
	b.st.Upload = Upload{Name: name, Size: fi.Size(), Started: time.Now()}
	b.st.Message = ""
	b.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	var last time.Time
	var mu sync.Mutex
	err = b.Client.Upload(ctx, name, fh, func(n int64) {
		mu.Lock()
		defer mu.Unlock()
		if time.Since(last) < 200*time.Millisecond {
			return
		}
		last = time.Now()
		b.mu.Lock()
		b.st.Upload.Done = n
		b.mu.Unlock()
		if b.notify != nil {
			b.notify()
		}
	})
	if err != nil {
		return fmt.Errorf("the upload to the printer: %w", err)
	}
	b.mu.Lock()
	b.st.Upload.Done, b.st.Upload.Result, b.st.Upload.Ended = fi.Size(), "sent", time.Now()
	b.sent = bambuSent{path: p, name: name}
	title := strings.TrimSuffix(strings.TrimSuffix(name, ".3mf"), ".gcode")
	b.mu.Unlock()
	start := bambu.Start{File: name, Name: title, Plate: pl.GcodePath(), Trays: trays,
		BedLevel: true, FlowCali: true, VibrationCali: true, LayerInspect: true}
	return b.start(start)
}

// startCheck is how long the deck waits for a print to start after the printer took the command.
// A test makes it shorter.
var startCheck = 90 * time.Second

// start sends the start of s. The printers take different addresses of the 3MF on the card
// (bambu.StartURLs): the deck tries the next one only when the printer answers with a failure, or
// gives no answer and does not print. It keeps the address that worked. After the start, the deck checks that the print begins: a start
// that the printer refuses, or that does not begin, shows the reason in the panel.
func (b *Bambu) start(s bambu.Start) error {
	urls := bambu.StartURLs(s.File)
	b.mu.Lock()
	if b.startForm > 0 && b.startForm < len(urls) {
		urls[0], urls[b.startForm] = urls[b.startForm], urls[0]
	}
	b.mu.Unlock()
	var reason string
	for _, url := range urls {
		r, answered, err := b.Client.StartPrint(s, url)
		log.Printf("bambu: start %s with %s, trays %v: answer %v %+v, %v", s.File, url, s.Trays, answered, r, err)
		if err != nil {
			return err
		}
		if answered && r.Failed() {
			reason = r.Reason
			if reason == "" {
				reason = fmt.Sprintf("error %d", r.ErrCode)
			}
			continue
		}
		if !answered && !b.State().Busy() {
			// No answer, and no print: the printer did not take this address.
			reason = "no answer from the printer"
			continue
		}
		b.mu.Lock()
		for i, u := range bambu.StartURLs(s.File) {
			if u == url {
				b.startForm = i
			}
		}
		b.mu.Unlock()
		go b.checkStart(s.Name)
		return nil
	}
	return fmt.Errorf("the printer refused the print: %s", reason)
}

// checkStart waits startCheck for the print name to begin, and shows in the panel when it does not:
// with the message of the printer, if it has one.
func (b *Bambu) checkStart(name string) {
	deadline := time.Now().Add(startCheck)
	for time.Now().Before(deadline) {
		if b.State().Busy() {
			return
		}
		time.Sleep(startCheck / 30)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.st.Busy() {
		return
	}
	why := "the printer gave no reason"
	if b.st.Message != "" {
		why = b.st.Message
	}
	b.st.Upload.Result, b.st.Upload.Err, b.st.Upload.Ended = "failed", "it did not begin", time.Now()
	b.st.Message = name + " did not start on the printer: " + why
	log.Printf("bambu: %s did not start in %v: %s", name, startCheck, why)
}
