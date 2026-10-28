package main

import (
	"bytes"
	"image/jpeg"
	"log"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"printdeck/internal/cloud"
	"printdeck/internal/lcd"
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

	mu      sync.Mutex
	started time.Time // the start of the print of the share
	ended   time.Time // the end of the print of the share, when it ended

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
func newShareLink(app *ui.App, link *cloud.Link, pr printerBackend) *shareLink {
	// The name of the printer on the page: the host name, or "a Printdeck" for the default host
	// name. A simulated printer has the name of its model (status).
	name := hostName()
	if strings.EqualFold(name, "printdeck") {
		name = "a Printdeck"
	}
	l := &shareLink{link: link, pr: pr, name: name}
	if r, ok := app.TimeLapse.(*timelapse.Runner); ok {
		l.lapse = r
	}
	s := &cloud.Sharer{Link: link, Status: l.status, LapseFPS: 12, OnChange: app.Redraw}
	if l.lapse != nil {
		l.lapse.OnLapseFrame = s.AddLapseFrame
	}
	// The picture of the model, from the model view of the print page: the whole part, with the
	// part that is printed in color, so the page shows what the print makes. The version is the
	// file position of the picture, so a new layer or move gives a new upload.
	s.Model = func() ([]byte, uint64) {
		img := pr.Preview()
		if img == nil {
			return nil, 0
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
			return nil, 0
		}
		return buf.Bytes(), uint64(pr.State().FilePos)
	}
	s.Plan = pr.LayerTimes
	// The display of the deck, as the touch screen shows it: the frames of the next 5 s, 4 a
	// second, so the page plays its animations until the next upload.
	s.LCD = func() []byte {
		scr := &lcd.Screen{Status: app.StatusScreen()}
		frame := int(time.Now().UnixMilli() / 250)
		out := make([]byte, 0, lcdFrames*1024)
		for i := range lcdFrames {
			bits := scr.Bits(frame + i)
			for y := range bits {
				out = append(out, bits[y][:]...)
			}
		}
		return out
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

// status is the state of the print for the share page.
func (l *shareLink) status() cloud.ShareStatus {
	st := l.pr.State()
	l.mu.Lock()
	defer l.mu.Unlock()
	s := cloud.ShareStatus{Printer: l.name, Activity: st.Activity, Layer: st.Layer, Layers: st.Layers,
		Tool: st.Tool, ToolTarget: st.ToolTarget, Bed: st.Bed, BedTarget: st.BedTarget,
		TimeLeft: -1, ReadyLeft: -1, Started: l.started.UTC()}
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
		s.Printer = "the simulated " + st.Simulated
	}
	// The file of the print: the material and the filament that the slicer wrote, and the
	// height of the model.
	path := st.FilePath
	if !st.Busy() {
		path = st.FinishedPath
	}
	meta := l.pr.FileMeta(path)
	density := printer.Density("")
	// The roll in the printer: its material and name, and its density for the grams.
	if store := l.pr.SpoolStore(); store != nil {
		if sp, ok := store.In(); ok {
			s.Material = strings.TrimSpace(sp.Material + ", " + sp.Name)
			s.Material = strings.Trim(s.Material, ", ")
			density = store.Density(sp)
			used := printer.GramsAt(st.FilamentUsed, l.pr.Machine().Diameter(), density)
			s.Roll = &cloud.ShareRoll{Name: strings.TrimSpace(sp.Number() + " " + sp.Label()), Color: sp.Color,
				Left: math.Round(max(0, sp.Left()-used)), Full: math.Round(sp.Capacity())}
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

// lcdFrames is the count of frames of the display in each upload: 5 s at 4 frames a second.
const lcdFrames = 20

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
func startShare(stateDir string, app *ui.App, pr printerBackend) {
	link := newCloudLink(stateDir, app.Redraw)
	app.Cloud = link
	app.Share = newShareLink(app, link, pr)
}

// hostName is the host name of the deck, or "Printdeck".
func hostName() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "Printdeck"
	}
	return name
}
