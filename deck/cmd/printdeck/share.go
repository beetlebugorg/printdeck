package main

import (
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"printdeck/internal/camera"
	"printdeck/internal/cloud"
	"printdeck/internal/printer"
	"printdeck/internal/timelapse"
	"printdeck/internal/ui"
)

// The share link of a print (the Share key of the print and done pages). The sharer uploads the
// status of the print, the newest frame of the main camera, and the time lapse frames. It runs
// only after a tap on Make the link.

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
	if hub, ok := app.Camera.(*camera.Hub); ok {
		main := hub.Role(camera.RoleMain)
		s.Views = []string{"main"}
		s.Frame = func(string) ([]byte, uint64) {
			if !main.On() {
				return nil, 0
			}
			f := main.Peek() // keeps the camera on while the share runs
			if f == nil {
				return nil, 0
			}
			return f.JPEG(), f.Seq
		}
	}
	if l.lapse != nil {
		l.lapse.OnLapseFrame = s.AddLapseFrame
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
	job := st.File
	var frames [][]byte
	now := time.Now()
	l.mu.Lock()
	l.started, l.ended = now.Add(-st.Elapsed), time.Time{}
	if !st.Busy() {
		job = st.FinishedFile
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
		s.Job = st.File
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
		s.Job = st.FinishedFile
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
