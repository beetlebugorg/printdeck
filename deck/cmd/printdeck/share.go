package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"log"
	"math"
	"os"
	"path/filepath"
	"printdeck/internal/chitu"
	"strconv"
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
	link       *cloud.Link
	sharer     *cloud.Sharer
	pr         printerBackend
	lapse      *timelapse.Runner // or nil
	name       string            // the name of the printer on the page: the host name
	model      string            // the model of the printer, from printer.cfg
	loggedPath bool              // the log has the key of the print of a share that went on through a restart

	mu      sync.Mutex
	path    string    // the file of the print of the share
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
	if c, ok := pr.(*printer.ChiTu); ok {
		// A resin printer has its own deck: the page names the printer, "Peopoly Phenom", and
		// not the host name of the deck.
		l.name, l.model = "", c.Model()
	}
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
	if c, ok := pr.(*printer.ChiTu); ok {
		s.Part = c.PartMesh
	}
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
	l.path, l.started, l.ended = st.FilePath, now.Add(-st.Elapsed), time.Time{}
	l.measured, l.known = nil, false
	if !st.Busy() {
		job = st.FinishedName()
		l.path, l.started, l.ended = st.FinishedPath, now.Add(-st.FinishedTime), now
		if recs := l.pr.History(); len(recs) > 0 {
			r := recs[0] // the newest
			l.path, l.started, l.ended = r.Path, r.End.Add(-r.Duration), r.End
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
	if c, ok := l.pr.(*printer.ChiTu); ok {
		// A resin print: no temperatures and no filament. The layer height and the exposure come
		// from the header of the deck's copy of the file.
		s.Process, s.Material = printer.ProcessResin, "Resin"
		s.Tool, s.ToolTarget, s.Bed, s.BedTarget = 0, 0, 0, 0
		if h := c.ResinFile(); h != nil {
			s.ResinLayerHeight = math.Round(float64(h.LayerHeight)*1000) / 1000
			s.ResinExposure = math.Round(float64(h.Exposure)*10) / 10
			s.ResinBottom = math.Round(float64(h.BottomExposure)*10) / 10
			s.ResinBottomN = int(h.BottomLayers)
			p := h.Params
			s.ResinLift, s.ResinLiftSpeed, s.ResinRetractSpeed = p.LiftMM, p.LiftSpeed, p.RetractSpeed
			s.ResinBottomLift, s.ResinBottomSpeed, s.ResinLightOff = p.BottomLiftMM, p.BottomLiftSpeed, p.LightOff
		}
		s.ResinLayerSec = math.Round(c.MeasuredLayer()*10) / 10
		if h := c.ResinFile(); h != nil {
			s.ResinFacts = resinFacts(h, s.ResinLayerSec)
		}
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
			rollFilament(f, store, sp)
		}
	}
	if s.Material == "" && s.Process != printer.ProcessResin {
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

// project returns the project of the print of the share: of the print now, or of its record after
// the end. It is "" for none.
func (l *shareLink) project() string {
	l.mu.Lock()
	path, started := l.path, l.started
	l.mu.Unlock()
	if path == "" {
		return ""
	}
	if st := l.pr.State(); st.Busy() && (st.FilePath == path || l.runningShare(st)) {
		return l.pr.NextProject()
	}
	for _, r := range l.pr.History() {
		if r.Path == path && !r.End.Before(started) {
			return r.Project
		}
	}
	return ""
}

// shareOf returns the ID of the share link of the print r, or "".
func (l *shareLink) shareOf(r printer.Record) string {
	st := l.sharer.State()
	if st.ID != "" {
		if ps := l.pr.State(); ps.Busy() && r.Path == ps.FilePath && l.runningShare(ps) {
			return st.ID
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if st.ID == "" || r.Path != l.path || r.End.Before(l.started) {
		return ""
	}
	return st.ID
}

// runningShare reports that the share is of the print now, st: it has not ended. A share that goes
// on through a restart of a resin deck can have a key of the print from before the restart.
func (l *shareLink) runningShare(st printer.State) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.ended.IsZero() || l.path == "" {
		return false
	}
	if l.path != st.FilePath && !l.loggedPath {
		l.loggedPath = true
		log.Printf("share: the share has the print %s, and the printer prints %s", l.path, st.FilePath)
	}
	return true
}

// startShare sets the cloud link and the share link of the app. The identity of the deck is the
// file cloud.json of the state folder.
func startShare(stateDir, configPath string, app *ui.App, pr printerBackend) {
	link := newCloudLink(stateDir, app.Redraw)
	app.Cloud = link
	l := newShareLink(app, link, pr, configPath)
	{
		// The projects, for each kind of printer.
		pl := &projectLink{Link: link, k: pr, stateDir: stateDir, shareOf: l.shareOf}
		switch b := pr.(type) {
		case *printer.Klippy:
			pl.model = printer.ConfigModel(configPath)
		case *printer.ChiTu:
			pl.name, pl.model = b.Name, b.Model()
		}
		app.Cloud = pl
		l.sharer.Project = func(ctx context.Context) (string, error) { return pl.ShareProjectID(ctx, l.project()) }
		go pl.watch(context.Background())
		go pl.watchLive(context.Background())
		go pl.watchService(context.Background())
	}
	app.Share = l
	l.loadHandoff(filepath.Join(handoffDir, shareHandoff))
	if _, ok := pr.(*printer.ChiTu); ok {
		// A resin print goes on through a restart of the deck (printer.ChiTu.PrintStore), so its
		// share does too: the deck saves the share in the state folder while it runs.
		keep := filepath.Join(stateDir, "share-resume.json")
		go func() {
			// The cloud link connects a moment after the start.
			for i := 0; i < 60 && !l.Ready(); i++ {
				time.Sleep(time.Second)
			}
			if l.sharer.State().ID == "" {
				l.loadHandoff(keep)
			}
			for range time.Tick(30 * time.Second) {
				if err := l.keepShare(keep); err != nil {
					log.Printf("share: %v", err)
				}
			}
		}()
	}
}

// shareHandoffFile is the handoff file of the share link (handoff.go).
type shareHandoffFile struct {
	Share   cloud.ShareHandoff `json:"share"`
	Path    string             `json:"path,omitempty"`
	Started time.Time          `json:"started"`
	Ended   time.Time          `json:"ended"`
}

// saveHandoff stops the uploads of the share, and writes its state to path for the next deck
// program. With no share, it removes path.
func (l *shareLink) saveHandoff(path string) error {
	h, ok := l.sharer.Handoff()
	return l.writeHandoff(path, h, ok)
}

// keepShare writes the state of the share to path, and the uploads go on: a restart of the deck
// during a resin print continues the share from path. With no share, it removes path.
func (l *shareLink) keepShare(path string) error {
	h, ok := l.sharer.Snapshot()
	return l.writeHandoff(path, h, ok)
}

// writeHandoff writes the state h of the share to path, or removes path when ok is false.
func (l *shareLink) writeHandoff(path string, h cloud.ShareHandoff, ok bool) error {
	if !ok {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	l.mu.Lock()
	f := shareHandoffFile{Share: h, Path: l.path, Started: l.started, Ended: l.ended}
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
	l.path, l.started, l.ended = f.Path, f.Started, f.Ended
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

// rollFilament puts the roll sp in the filament f. The roll is the filament in the printer, so it
// comes before the file.
func rollFilament(f *cloud.ShareFilament, store *printer.Spools, sp printer.Spool) {
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

// resinFacts are the facts of the sliced file of a resin print for the share page: all the
// numbers of its header and of its print parameters.
func resinFacts(h *chitu.Sliced, layerSec float64) [][2]string {
	num := func(v float64) string { return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64) }
	var out [][2]string
	add := func(name, value string) {
		if value != "" {
			out = append(out, [2]string{name, value})
		}
	}
	add("Layers", fmt.Sprintf("%d of %s mm", h.Layers, num(float64(h.LayerHeight))))
	add("Height", num(float64(h.Layers)*float64(h.LayerHeight))+" mm")
	add("Exposure", num(float64(h.Exposure))+" s")
	add("Bottom layers", fmt.Sprintf("%d, %s s each", h.BottomLayers, num(float64(h.BottomExposure))))
	p := h.Params
	if p.LiftMM > 0 {
		add("Lift", fmt.Sprintf("%s mm at %s mm/min (%s s)", num(p.LiftMM), num(p.LiftSpeed), num(p.LiftMM/p.LiftSpeed*60)))
		add("Return", fmt.Sprintf("%s mm/min (%s s)", num(p.RetractSpeed), num(p.LiftMM/p.RetractSpeed*60)))
	}
	if p.BottomLiftMM > 0 && p.BottomLiftSpeed > 0 {
		add("Bottom lift", fmt.Sprintf("%s mm at %s mm/min (%s s)", num(p.BottomLiftMM), num(p.BottomLiftSpeed), num(p.BottomLiftMM/p.BottomLiftSpeed*60)))
	}
	add("Light off", fmt.Sprintf("%s s, bottom %s s", num(p.LightOff), num(p.BottomLightOff)))
	if layerSec > 0 {
		add("Layer time", num(layerSec)+" s, as the print measures it")
	}
	if h.PrintSeconds > 0 {
		add("Slicer estimate", fmt.Sprintf("%d h %02d min", h.PrintSeconds/3600, h.PrintSeconds/60%60))
	}
	if p.VolumeML > 0 {
		add("Resin", fmt.Sprintf("%s ml, %s g", num(p.VolumeML), num(p.WeightG)))
	}
	if p.Cost > 0 {
		add("Cost", num(p.Cost))
	}
	add("Screen", fmt.Sprintf("%d x %d pixels, %s x %s mm", h.ResX, h.ResY, num(float64(h.BedX)), num(float64(h.BedY))))
	if h.AntiAlias > 1 {
		add("Anti-aliasing", fmt.Sprintf("%d levels", h.AntiAlias))
	} else {
		add("Anti-aliasing", "off")
	}
	if h.PWM > 0 {
		add("Light power", fmt.Sprintf("%d%%, bottom %d%%", h.PWM*100/255, h.BottomPWM*100/255))
	}
	return out
}
