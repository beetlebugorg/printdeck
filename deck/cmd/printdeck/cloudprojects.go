package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"printdeck/internal/cloud"
	"printdeck/internal/printer"
	"printdeck/internal/timelapse"
)

// projectLink is the cloud link of the deck with the projects of the service (ui.CloudProjects):
// the owner code, and the share of a project of the history. A project of the history becomes a
// project of the service at its first share, or at the first share link or end of a print in it.
// The file cloud-projects.json of the state folder keeps the ID of each project at the service and
// the prints that are up, so a share sends only the new prints. The deck writes it only after a
// change at the service.
type projectLink struct {
	*cloud.Link
	k        *printer.Klippy
	stateDir string
	model    string // the brand and the model of the printer, for the prints
	// shareOf returns the ID of the share link of the print r, or "", or nil for none.
	shareOf func(r printer.Record) string

	mu sync.Mutex // one upload of prints at a time

	fmu     sync.Mutex        // the file and the projects that the deck checked
	files   cloudProjectsFile // cloud-projects.json, or nil before the first read
	checked map[string]bool   // the projects that exist at the service, from this deck program
	tried   map[string]time.Time
}

// cloudProjectsFile is cloud-projects.json: the projects of the history at the service.
type cloudProjectsFile map[string]*cloudProject

type cloudProject struct {
	ID     string            `json:"id"`
	Domain string            `json:"domain"`
	Prints map[string]string `json:"prints"` // the key of a print and its ID at the service
	// Format is the printFormat of the prints that are up. A share sends the prints of an older
	// format again.
	Format int `json:"format,omitempty"`
}

// printFormat is the format of the prints that the deck sends: 2 adds the details of the print
// page (the layers, the plan, the filament and the times). 3 sends the pictures of the models
// again: the deck found the model by the name of the file, and now by the title first.
const printFormat = 3

func (p *projectLink) file() string { return filepath.Join(p.stateDir, "cloud-projects.json") }

// load returns cloud-projects.json. The caller holds fmu.
func (p *projectLink) load() cloudProjectsFile {
	if p.files != nil {
		return p.files
	}
	out := cloudProjectsFile{}
	if b, err := os.ReadFile(p.file()); err == nil {
		json.Unmarshal(b, &out)
	}
	for _, cp := range out {
		if cp.Prints == nil {
			cp.Prints = map[string]string{}
		}
	}
	p.files = out
	return out
}

// save writes cloud-projects.json. The caller holds fmu.
func (p *projectLink) save(f cloudProjectsFile) error {
	b, _ := json.MarshalIndent(f, "", " ")
	tmp := p.file() + ".new"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p.file())
}

// OwnerCode asks the service for an owner code.
func (p *projectLink) OwnerCode() (string, time.Time, error) {
	id := p.Identity()
	if id == nil {
		return "", time.Time{}, errors.New("Connect the deck to the service first.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := p.Client.OwnerCode(ctx, id)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("The service did not answer: %v", err)
	}
	return out.URL, out.Expires, nil
}

// printKey is the key of a print of the history: its file and its end.
func printKey(r printer.Record) string { return r.Path + "@" + strconv.FormatInt(r.End.Unix(), 10) }

// ShareProject makes the project with the name at the service, if it is not there, and uploads its
// prints that are not up yet: the record, the picture of the model and the frames of the time
// lapse. It returns the page of the project.
func (p *projectLink) ShareProject(name string, progress func(done, total int)) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	id := p.Identity()
	if id == nil {
		return "", errors.New("Connect the deck to the service first.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cp, err := p.serviceProject(ctx, id, name, true)
	if err != nil {
		return "", err
	}
	recs := printer.OfProject(p.k.History(), name)
	var todo []printer.Record
	p.fmu.Lock()
	for _, r := range recs {
		if cp.Prints[printKey(r)] == "" || cp.Format < printFormat {
			todo = append(todo, r)
		}
	}
	p.fmu.Unlock()
	progress(0, len(todo))
	for i, r := range todo {
		if err := p.upPrint(ctx, id, cp, r); err != nil {
			return "", fmt.Errorf("%s did not go up: %v", r.Name(), err)
		}
		progress(i+1, len(todo))
	}
	p.fmu.Lock()
	defer p.fmu.Unlock()
	if cp.Format < printFormat {
		cp.Format = printFormat
		if err := p.save(p.load()); err != nil {
			return "", err
		}
	}
	return cloud.ProjectPage(id.Domain, cp.ID), nil
}

// serviceProject returns the project with the name at the service, and makes it when it is not
// there. check asks the service if the project still exists: the owner can delete it on the web.
// Without check, the deck asks once for each deck program.
func (p *projectLink) serviceProject(ctx context.Context, id *cloud.Identity, name string, check bool) (*cloudProject, error) {
	p.fmu.Lock()
	defer p.fmu.Unlock()
	f := p.load()
	cp := f[name]
	if cp != nil && cp.Domain != id.Domain {
		cp = nil
	}
	if cp != nil && (check || !p.checked[name]) {
		ok, err := p.Client.ProjectExists(ctx, id, cp.ID)
		if err != nil {
			return nil, fmt.Errorf("The service did not answer: %v", err)
		}
		if !ok {
			cp = nil
		}
	}
	if cp == nil {
		sum, err := p.Client.NewProject(ctx, id, name)
		if err != nil {
			return nil, fmt.Errorf("The service did not make the project: %v", err)
		}
		cp = &cloudProject{ID: sum.ID, Domain: id.Domain, Prints: map[string]string{}, Format: printFormat}
		f[name] = cp
		if err := p.save(f); err != nil {
			return nil, err
		}
	}
	if p.checked == nil {
		p.checked = map[string]bool{}
	}
	p.checked[name] = true
	return cp, nil
}

// upPrint uploads the print r to the project cp, and keeps its ID. The caller holds mu.
func (p *projectLink) upPrint(ctx context.Context, id *cloud.Identity, cp *cloudProject, r printer.Record) error {
	pid, err := p.sendPrint(ctx, id, cp.ID, r)
	if err != nil {
		return err
	}
	p.fmu.Lock()
	defer p.fmu.Unlock()
	cp.Prints[printKey(r)] = pid
	return p.save(p.load())
}

// ShareProjectID returns the ID at the service of the project of the print now, or of the print
// that ended, or "" for none. It makes the project at the service when it is not there. It is the
// Project of the share link.
func (p *projectLink) ShareProjectID(ctx context.Context, name string) (string, error) {
	if name == "" {
		return "", nil
	}
	id := p.Identity()
	if id == nil {
		return "", errors.New("no identity")
	}
	cp, err := p.serviceProject(ctx, id, name, false)
	if err != nil {
		return "", err
	}
	return cp.ID, nil
}

// autoSendAfter is the longest wait for the video of a print before the deck sends the print to
// its project with no time lapse. autoSendWindow is the age of the oldest print that the deck
// sends by itself, and autoSendRetry the time between two tries of a print.
const (
	autoSendAfter  = 10 * time.Minute
	autoSendWindow = 6 * time.Hour
	autoSendRetry  = 15 * time.Minute
)

// watch sends each print of a project to the project at its end, until ctx ends. A print with a
// time lapse waits for its video. It looks at the history each minute: the history is in memory.
func (p *projectLink) watch(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		p.sendNew(ctx, time.Now())
	}
}

// sendNew sends the new prints of the projects that are ready at now.
func (p *projectLink) sendNew(ctx context.Context, now time.Time) {
	id := p.Identity()
	if id == nil {
		return
	}
	for _, r := range p.k.History() {
		if now.Sub(r.End) > autoSendWindow {
			break // the history is newest first
		}
		if r.Project == "" || r.Video == "" && now.Sub(r.End) < autoSendAfter {
			continue
		}
		k := printKey(r)
		p.fmu.Lock()
		cp := p.load()[r.Project]
		sent := cp != nil && cp.Domain == id.Domain && cp.Prints[k] != ""
		wait := now.Sub(p.tried[k]) < autoSendRetry
		if !sent && !wait {
			if p.tried == nil {
				p.tried = map[string]time.Time{}
			}
			p.tried[k] = now
		}
		p.fmu.Unlock()
		if sent || wait {
			continue
		}
		p.sendOne(ctx, id, r)
	}
}

// sendOne sends the print r to its project.
func (p *projectLink) sendOne(ctx context.Context, id *cloud.Identity, r printer.Record) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	cp, err := p.serviceProject(ctx, id, r.Project, false)
	if err == nil {
		err = p.upPrint(ctx, id, cp, r)
	}
	if err != nil {
		log.Printf("projects: %s did not go to %s: %v", r.Name(), r.Project, err)
		p.fmu.Lock()
		delete(p.checked, r.Project) // the owner can have deleted the project
		p.fmu.Unlock()
		return
	}
	log.Printf("projects: %s went to %s", r.Name(), r.Project)
}

// maxProjectFrames is the most frames of a time lapse in a project. A longer video sends every
// n-th frame.
const maxProjectFrames = 1000

// sendPrint uploads one print of the history to the project: the record, the picture of the
// model, and the frames of the time lapse as the video has them (no encode).
func (p *projectLink) sendPrint(ctx context.Context, id *cloud.Identity, project string, r printer.Record) (string, error) {
	up := cloud.PrintUpload{FPS: 24, Model: p.recordPicture(r), Request: p.printRequest(r)}
	if p.shareOf != nil {
		up.Request.Share = p.shareOf(r)
	}
	if l, ok := p.k.View.RecordPlan(r); ok {
		up.Plan = l.Plan
		for i, t := range up.Plan {
			up.Plan[i] = math.Round(t*10) / 10
		}
		up.Request.Layers, up.Request.Layer, up.Request.Height = l.Layers, l.Layer, math.Round(l.Height*100)/100
	}
	if r.Video != "" {
		if m, err := timelapse.OpenMovie(filepath.Join(videoDir(p.stateDir), r.Video)); err == nil {
			defer m.Close()
			step := max(1, (m.Len()+maxProjectFrames-1)/maxProjectFrames)
			up.Frames, up.FPS = (m.Len()+step-1)/step, max(1, m.FPS/step)
			up.Frame = func(i int) ([]byte, error) { return m.JPEG(i * step) }
		}
	}
	return p.Client.SendPrint(ctx, id, project, up)
}

// printRequest is the record of the print r for its page: the record of the history, and the
// facts of its file and its roll.
func (p *projectLink) printRequest(r printer.Record) cloud.PrintRequest {
	in := cloud.PrintRequest{Name: r.Name(), File: r.File, Result: r.Result, Ended: r.End.UTC(),
		Seconds: int(r.Duration.Seconds()), Material: r.Material, Grams: r.Grams, Roll: r.Spool, Printer: p.model,
		Note: r.Note, Key: printKey(r), Started: r.End.Add(-r.Duration).UTC()}
	prep := 0.0
	for _, ph := range r.Phases {
		prep += ph.Seconds
	}
	in.Prep = int(prep)
	meta := p.k.FileMeta(r.Path)
	in.Slicer, in.Estimate = meta.Slicer, int(meta.PrintTime.Seconds())
	diameter := p.k.Machine().Diameter()
	density := printer.Density(meta.Material)
	f := &cloud.ShareFilament{Name: meta.MaterialName, Type: meta.Material, Color: meta.Color, Diameter: diameter,
		LayerHeight: meta.LayerHeight, NozzleTemp: meta.NozzleTemp, BedTemp: meta.BedTemp}
	if store := p.k.SpoolStore(); store != nil && r.SpoolID != "" {
		if sp, ok := store.Get(r.SpoolID); ok {
			rollFilament(f, store, sp)
			density = store.Density(sp)
		}
	}
	if f.Name != "" || f.Type != "" || f.Color != "" {
		in.Filament = f
	}
	in.GramsTotal = meta.FilamentG
	if in.GramsTotal == 0 && meta.FilamentMM > 0 {
		in.GramsTotal = printer.GramsAt(meta.FilamentMM, diameter, density)
	}
	in.GramsTotal = math.Round(in.GramsTotal*10) / 10
	return in
}

// recordPicture returns the picture of the model of a finished print as a JPEG, or nil when the
// file is gone. The view draws it in the background: the deck waits up to 30 s.
func (p *projectLink) recordPicture(r printer.Record) []byte {
	for range 60 {
		img, loading := p.k.View.RecordPicture(r)
		if !loading {
			if img == nil {
				return nil
			}
			var buf bytes.Buffer
			if jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}) != nil {
				return nil
			}
			return buf.Bytes()
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil
}
