package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
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
// project of the service at its first share. The file cloud-projects.json of the state folder
// keeps the ID of each project at the service and the prints that are up, so a share sends only
// the new prints. The deck writes it only at a share.
type projectLink struct {
	*cloud.Link
	k        *printer.Klippy
	stateDir string
	model    string // the brand and the model of the printer, for the prints

	mu sync.Mutex // one share at a time
}

// cloudProjectsFile is cloud-projects.json: the projects of the history at the service.
type cloudProjectsFile map[string]*cloudProject

type cloudProject struct {
	ID     string            `json:"id"`
	Domain string            `json:"domain"`
	Prints map[string]string `json:"prints"` // the key of a print and its ID at the service
}

func (p *projectLink) file() string { return filepath.Join(p.stateDir, "cloud-projects.json") }

func (p *projectLink) load() cloudProjectsFile {
	out := cloudProjectsFile{}
	if b, err := os.ReadFile(p.file()); err == nil {
		json.Unmarshal(b, &out)
	}
	return out
}

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
	f := p.load()
	cp := f[name]
	if cp != nil && cp.Domain == id.Domain {
		// The project can be gone at the service: the owner deleted it.
		ok, err := p.Client.ProjectExists(ctx, id, cp.ID)
		if err != nil {
			return "", fmt.Errorf("The service did not answer: %v", err)
		}
		if !ok {
			cp = nil
		}
	} else {
		cp = nil
	}
	if cp == nil {
		sum, err := p.Client.NewProject(ctx, id, name)
		if err != nil {
			return "", fmt.Errorf("The service did not make the project: %v", err)
		}
		cp = &cloudProject{ID: sum.ID, Domain: id.Domain, Prints: map[string]string{}}
		f[name] = cp
		if err := p.save(f); err != nil {
			return "", err
		}
	}
	recs := printer.OfProject(p.k.History(), name)
	var todo []printer.Record
	for _, r := range recs {
		if cp.Prints[printKey(r)] == "" {
			todo = append(todo, r)
		}
	}
	progress(0, len(todo))
	for i, r := range todo {
		pid, err := p.sendPrint(ctx, id, cp.ID, r)
		if err != nil {
			return "", fmt.Errorf("%s did not go up: %v", r.Name(), err)
		}
		cp.Prints[printKey(r)] = pid
		if err := p.save(f); err != nil {
			return "", err
		}
		progress(i+1, len(todo))
	}
	return cloud.ProjectPage(id.Domain, cp.ID), nil
}

// maxProjectFrames is the most frames of a time lapse in a project. A longer video sends every
// n-th frame.
const maxProjectFrames = 1000

// sendPrint uploads one print of the history to the project: the record, the picture of the
// model, and the frames of the time lapse as the video has them (no encode).
func (p *projectLink) sendPrint(ctx context.Context, id *cloud.Identity, project string, r printer.Record) (string, error) {
	up := cloud.PrintUpload{FPS: 24, Model: p.recordPicture(r), Request: cloud.PrintRequest{Name: r.Name(), File: r.File,
		Result: r.Result, Ended: r.End.UTC(), Seconds: int(r.Duration.Seconds()), Material: r.Material, Grams: r.Grams,
		Roll: r.Spool, Printer: p.model, Note: r.Note, Key: printKey(r)}}
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
