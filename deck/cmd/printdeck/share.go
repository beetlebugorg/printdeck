package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"printdeck/internal/cloud"
	"printdeck/internal/printer"
	"printdeck/internal/timelapse"
	"printdeck/internal/ui"
)

// The share link of a print (the Share key of the print and done pages). The sharer uploads the
// status of the print, the plan of the layers, the picture of the model, and the time lapse
// frames. It runs only after a tap on Make the link.

// shareLink is the ui.ShareLink of the deck.
type shareLink struct {
	link   *cloud.Link
	sharer *cloud.Sharer
	pr     printerBackend
	lapse  *timelapse.Runner // or nil
	name   string            // the name of the printer on the page: the host name
	model  string            // the model of the printer, from printer.cfg

	mu      sync.Mutex
	started time.Time // the start of the print of the share
	ended   time.Time // the end of the print of the share, when it ended
	// waiting is the start of a time with no print and no finished print: the deck has no
	// Klipper, for example at the start of a new deck program during the print.
	waiting time.Time

	// The clock of the layers: the layer now, its start, whether the deck saw it start, whether
	// it had a pause, and the measured seconds of the layers that are done.
	layer    int
	layerAt  time.Time
	known    bool
	paused   bool
	measured map[int]float64
}

// newShareLink returns the share link of the deck, with the camera hub and the time lapse runner
// of the app (or nil for none).
func newShareLink(app *ui.App, link *cloud.Link, pr printerBackend, configPath string) *shareLink {
	// The name of the printer on the page: the host name, or "a Printdeck" for the default host
	// name. A simulated printer has the name of its model (status).
	name := hostName()
	if strings.EqualFold(name, "printdeck") {
		name = "a Printdeck"
	}
	l := &shareLink{link: link, pr: pr, name: name, model: printer.ConfigModel(configPath)}
	if r, ok := app.TimeLapse.(*timelapse.Runner); ok {
		l.lapse = r
	}
	s := &cloud.Sharer{Link: link, Status: l.status, LapseFPS: 12, OnChange: app.Redraw}
	if l.lapse != nil {
		l.lapse.OnLapseFrame = s.AddLapseFrame
	}
	// The picture of the model on the build plate of the printer: the whole part, with the part
	// that is printed in color, so the page shows what the print makes. The version is the file
	// position of the picture, so a new layer or move gives a new upload.
	pic, _ := pr.(interface {
		SharePicture(st printer.State, b printer.Bed) image.Image
	})
	s.Model = func() ([]byte, uint64) {
		if pic == nil {
			return nil, 0
		}
		st := pr.State()
		img := pic.SharePicture(st, printer.BedOf(pr.Machine(), l.model))
		if img == nil {
			return nil, 0
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
			return nil, 0
		}
		return buf.Bytes(), uint64(st.FilePos)
	}
	s.Plan = pr.LayerTimes
	l.sharer = s
	return l
}

func (l *shareLink) Ready() bool             { return l.link.Identity() != nil }
func (l *shareLink) State() cloud.ShareState { return l.sharer.State() }
func (l *shareLink) Stop()                   { l.sharer.Stop() }
func (l *shareLink) SetReady(ready bool)     { l.sharer.SetReady(ready, "") }

// Start shares the print now, or the print that ended, with the time lapse frames so far.
func (l *shareLink) Start() {
	st := l.pr.State()
	job := st.Name()
	var frames [][]byte
	now := time.Now()
	l.mu.Lock()
	l.started, l.ended = now.Add(-st.Elapsed), time.Time{}
	l.measured, l.known = nil, false
	if !st.Busy() {
		job = st.FinishedName()
		l.started, l.ended = now.Add(-st.FinishedTime), now
		if recs := l.pr.History(); len(recs) > 0 {
			r := recs[len(recs)-1]
			l.started, l.ended = r.End.Add(-r.Duration), r.End
			frames = l.videoFrames(r.Video)
		}
	} else if l.lapse != nil {
		frames = l.lapse.LapseFrames()
	}
	l.mu.Unlock()
	l.sharer.Start(job, l.name, frames)
}

// videoFrames returns the frames of the time lapse video name, or nil.
func (l *shareLink) videoFrames(name string) [][]byte {
	if l.lapse == nil || name == "" {
		return nil
	}
	p, ok := l.lapse.VideoPath(name)
	if !ok {
		return nil
	}
	m, err := timelapse.OpenMovie(p)
	if err != nil {
		log.Printf("share: the time lapse %s did not open: %v", name, err)
		return nil
	}
	defer m.Close()
	var out [][]byte
	for i := range m.Len() {
		b, err := m.JPEG(i)
		if err != nil {
			break
		}
		out = append(out, b)
	}
	return out
}

// shareWaitLimit is the longest time that a share waits for the printer. After it the share ends
// as stopped.
const shareWaitLimit = 10 * time.Minute

// status is the state of the print for the share page.
func (l *shareLink) status() cloud.ShareStatus {
	st := l.pr.State()
	l.mu.Lock()
	defer l.mu.Unlock()
	s := cloud.ShareStatus{Printer: l.name, PrinterModel: l.model, Activity: st.Activity, Layer: st.Layer, Layers: st.Layers,
		Tool: st.Tool, ToolTarget: st.ToolTarget, Bed: st.Bed, BedTarget: st.BedTarget,
		TimeLeft: -1, ReadyLeft: -1, Started: l.started.UTC()}
	// With no print and no finished print, the deck does not know the print: Klipper is not
	// connected yet, or it has an error. The share waits and does not end, because the print can
	// still run. A wait longer than shareWaitLimit ends the share as stopped.
	if !st.Busy() && st.Finished == "" {
		if l.waiting.IsZero() {
			l.waiting = time.Now()
		}
		if time.Since(l.waiting) < shareWaitLimit {
			s.State = "waiting"
			return s
		}
		st.Finished = "error"
	} else {
		l.waiting = time.Time{}
	}
	if st.HasZ {
		s.Height = st.Z
	}

	if st.Busy() {
		l.clockLayers(st)
		if st.HasNow {
			s.Move, s.Speed = moveName(st.NowKind), st.NowSpeed*float64(max(1, st.SpeedPct))/100
			if st.HasLive {
				s.Speed = st.LiveSpeed
			}
		}
		if st.HasFan {
			s.Fan = int(st.Fan + 0.5)
		}
		s.SpeedPct, s.FlowPct = st.SpeedPct, st.FlowPct
		if l.known && l.layer == st.Layer {
			s.LayerStarted = l.layerAt.UTC()
		}
		if n := st.Layers; n > 0 && len(l.measured) > 0 {
			s.LayerTimes = make([]float64, min(n, st.Layer))
			for k, t := range l.measured {
				if k >= 1 && k <= len(s.LayerTimes) {
					s.LayerTimes[k-1] = math.Round(t*10) / 10
				}
			}
		}
	}
	if st.Simulated != "" {
		s.Printer, s.PrinterModel = "the simulated "+st.Simulated, ""
	}
	// The file of the print: the material and the filament that the slicer wrote, and the
	// height of the model.
	path := st.FilePath
	if !st.Busy() {
		path = st.FinishedPath
	}
	meta := l.pr.FileMeta(path)
	density := printer.Density("")
	f := &cloud.ShareFilament{Name: meta.MaterialName, Type: meta.Material, Color: meta.Color,
		Diameter: l.pr.Machine().Diameter(), LayerHeight: meta.LayerHeight,
		NozzleTemp: meta.NozzleTemp, BedTemp: meta.BedTemp}
	// The roll in the printer: its material and name, and its density for the grams.
	if store := l.pr.SpoolStore(); store != nil {
		if sp, ok := store.In(); ok {
			s.Material = strings.TrimSpace(sp.Material + ", " + sp.Name)
			s.Material = strings.Trim(s.Material, ", ")
			density = store.Density(sp)
			used := printer.GramsAt(st.FilamentUsed, l.pr.Machine().Diameter(), density)
			s.Roll = &cloud.ShareRoll{Name: strings.TrimSpace(sp.Number() + " " + sp.Label()), Color: sp.Color,
				Left: math.Round(max(0, sp.Left()-used)), Full: math.Round(sp.Capacity())}
			// The roll is the filament in the printer, so it comes before the file.
			f.Name, f.RollSize = sp.Name, sp.Capacity()
			if sp.Material != "" {
				f.Type = sp.Material
			}
			if sp.Color != "" {
				f.Color = sp.Color
			}
			if m, ok := store.SlicerMaterial(sp.CuraMaterial); sp.CuraMaterial != "" && ok {
				f.Brand = m.Brand
				if m.Name != "" {
					f.Name = m.Name
				}
			}
		}
	}
	if s.Material == "" {
		s.Material = meta.MaterialName
		if s.Material == "" {
			s.Material = meta.Material
		}
		if meta.Material != "" {
			density = printer.Density(meta.Material)
		}
	}
	if f.Name != "" || f.Type != "" || f.Color != "" {
		s.Filament = f
	}
	s.GramsTotal = meta.FilamentG
	if s.GramsTotal == 0 && meta.FilamentMM > 0 {
		s.GramsTotal = printer.GramsAt(meta.FilamentMM, l.pr.Machine().Diameter(), density)
	}
	grams := func(mm float64) float64 {
		return printer.GramsAt(mm, l.pr.Machine().Diameter(), density)
	}
	switch {
	case st.Busy():
		s.Job = st.Name()
		s.Progress = st.Completion / 100
		s.Grams = grams(st.FilamentUsed)
		switch {
		case st.Paused:
			s.State = "paused"
		case st.Preparing, strings.HasPrefix(st.Activity, "Heating"):
			s.State = "heating"
		default:
			s.State = "printing"
		}
		if st.HasTimeLeft {
			s.TimeLeft = int(st.TimeLeft.Seconds())
		}
		if st.HasReleaseLeft {
			s.ReadyLeft = int(st.ReleaseLeft.Seconds())
		}
	default:
		s.Job = st.FinishedName()
		s.Grams = grams(st.FinishedFilament)
		s.State = map[string]string{"complete": "done", "cancelled": "cancelled", "error": "error"}[st.Finished]
		if s.State == "" {
			s.State = "done"
		}
		if s.State == "done" {
			s.Progress = 1
		}
		if l.ended.IsZero() {
			l.ended = time.Now()
		}
		s.Ended = l.ended.UTC()
		s.ReadyLeft = 0
		if st.HasBedCoolLeft && st.BedCoolTo == printer.ReleaseBed {
			s.ReadyLeft = int(st.BedCoolLeft.Seconds())
		}
	}
	return s
}

// clockLayers follows the layer of st, and measures each layer from its start to the start of the
// next one. A layer with a pause, and the layer that printed at the start of the share, have no
// time. The caller holds l.mu.
func (l *shareLink) clockLayers(st printer.State) {
	now := time.Now()
	if l.measured == nil {
		l.measured, l.layer = map[int]float64{}, st.Layer
		return
	}
	if st.Paused {
		l.paused = true
	}
	if st.Layer != l.layer {
		if st.Layer == l.layer+1 && l.known && !l.paused {
			l.measured[l.layer] = now.Sub(l.layerAt).Seconds()
		}
		l.layer, l.layerAt, l.known, l.paused = st.Layer, now, true, false
	}
}

// moveName is the kind of a move of the G-code for the page, for example "WALL-OUTER" as
// "Outer wall".
func moveName(kind string) string {
	switch strings.ToUpper(kind) {
	case "WALL-OUTER":
		return "Outer wall"
	case "WALL-INNER":
		return "Inner wall"
	case "SKIN":
		return "Top and bottom"
	case "FILL":
		return "Infill"
	case "SUPPORT", "SUPPORT-INTERFACE":
		return "Support"
	case "SKIRT", "BRIM":
		return "Skirt"
	}
	k := strings.ToLower(strings.ReplaceAll(kind, "-", " "))
	if k == "" {
		return ""
	}
	return strings.ToUpper(k[:1]) + k[1:]
}

// startShare sets the cloud link and the share link of the app. The identity of the deck is the
// file cloud.json of the state folder.
func startShare(stateDir, configPath string, app *ui.App, pr printerBackend) {
	link := newCloudLink(stateDir, app.Redraw)
	app.Cloud = link
	l := newShareLink(app, link, pr, configPath)
	app.Share = l
	l.loadHandoff(filepath.Join(handoffDir, shareHandoff))
}

// shareHandoffFile is the handoff file of the share link (handoff.go).
type shareHandoffFile struct {
	Share   cloud.ShareHandoff `json:"share"`
	Started time.Time          `json:"started"`
	Ended   time.Time          `json:"ended"`
}

// saveHandoff stops the uploads of the share, and writes its state to path for the next deck
// program. With no share, it removes path.
func (l *shareLink) saveHandoff(path string) error {
	h, ok := l.sharer.Handoff()
	if !ok {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	l.mu.Lock()
	f := shareHandoffFile{Share: h, Started: l.started, Ended: l.ended}
	l.mu.Unlock()
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// loadHandoff continues the share of the deck program before this one, from the file path of
// saveHandoff, and removes the file.
func (l *shareLink) loadHandoff(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	os.Remove(path)
	var f shareHandoffFile
	if err := json.Unmarshal(b, &f); err != nil {
		log.Printf("share: the handoff file: %v", err)
		return
	}
	l.mu.Lock()
	l.started, l.ended = f.Started, f.Ended
	l.mu.Unlock()
	if l.sharer.Resume(f.Share) {
		log.Printf("share: the share %s continues from the deck program before this one", f.Share.ID)
	} else {
		log.Printf("share: the share %s does not continue", f.Share.ID)
	}
}

// hostName is the host name of the deck, or "Printdeck".
func hostName() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "Printdeck"
	}
	return name
}
