package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"printdeck/internal/cloud"
	"printdeck/internal/printer"
)

// The printer of the deck at the service, and the projects of the service. Each deck registers its
// printer and reads the list of the projects: the projects of all the printers of the owner. The
// project choice of the deck and the list for the Cura plugin have these projects, so the decks
// share the projects. Each deck keeps its own history. The file cloud-service.json of the state
// folder keeps the last answers, for the time that the service does not answer.

// serviceEvery is the time between two reads of the projects of the service.
const serviceEvery = 10 * time.Minute

// printerKey is the key of the printer of this deck at the service (cloud.PrinterRequest).
const printerKey = "main"

// serviceFile is cloud-service.json.
type serviceFile struct {
	Domain   string                 `json:"domain"`
	Printer  string                 `json:"printer,omitempty"` // the ID of the printer of the deck
	Projects []cloud.ProjectSummary `json:"projects,omitempty"`
	Read     time.Time              `json:"read,omitzero"` // the time of the read of the projects
}

func (p *projectLink) serviceFile() string { return filepath.Join(p.stateDir, "cloud-service.json") }

// service returns cloud-service.json for the domain of the deck now, or an empty file. The caller
// holds smu.
func (p *projectLink) service() serviceFile {
	id := p.Identity()
	if !p.svcLoaded {
		p.svcLoaded = true
		if b, err := os.ReadFile(p.serviceFile()); err == nil {
			json.Unmarshal(b, &p.svc)
		}
	}
	if id == nil || p.svc.Domain != id.Domain {
		return serviceFile{}
	}
	return p.svc
}

// printerID returns the ID of the printer of the deck at the service, or "" before its
// registration.
func (p *projectLink) printerID() string {
	p.smu.Lock()
	defer p.smu.Unlock()
	return p.service().Printer
}

// Projects returns the names of the projects of the service, the newest first.
func (p *projectLink) Projects() []string {
	p.smu.Lock()
	defer p.smu.Unlock()
	var out []string
	for _, s := range p.service().Projects {
		out = append(out, s.Name)
	}
	return out
}

// serviceProjectID returns the ID of the project of the service with the name, or "". A name of the
// same letters in another case matches when no name is the same.
func (p *projectLink) serviceProjectID(name string) string {
	p.smu.Lock()
	defer p.smu.Unlock()
	fold := ""
	for _, s := range p.service().Projects {
		if s.Name == name {
			return s.ID
		}
		if fold == "" && strings.EqualFold(s.Name, name) {
			fold = s.ID
		}
	}
	return fold
}

// printerName is the name of the printer of the deck at the service: the model of the printer, or
// the name of the deck.
func (p *projectLink) printerName() string {
	if p.name != "" {
		return p.name
	}
	if p.model != "" {
		return p.model
	}
	return hostName()
}

// refreshService registers the printer of the deck (each time: the name can change) and reads the
// projects of the service. It keeps the answers in cloud-service.json.
func (p *projectLink) refreshService(ctx context.Context) error {
	id := p.Identity()
	if id == nil {
		return nil
	}
	process := printer.ProcessFDM
	if p.k != nil && p.k.Machine().Process == printer.ProcessResin {
		process = printer.ProcessResin
	}
	pr, err := p.Client.RegisterPrinter(ctx, id, cloud.PrinterRequest{Key: printerKey, Name: p.printerName(), Process: process, Model: p.model})
	if err != nil {
		return err
	}
	list, err := p.Client.Projects(ctx, id)
	if err != nil {
		return err
	}
	p.smu.Lock()
	defer p.smu.Unlock()
	p.svc = serviceFile{Domain: id.Domain, Printer: pr.ID, Projects: list, Read: time.Now().UTC()}
	b, _ := json.MarshalIndent(p.svc, "", " ")
	tmp := p.serviceFile() + ".new"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p.serviceFile())
}

// watchService refreshes the printer and the projects of the service at the start, after a
// connect to another service, and each serviceEvery, until ctx ends.
func (p *projectLink) watchService(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	var last time.Time
	domain := ""
	for {
		if id := p.Identity(); id != nil && (id.Domain != domain || time.Since(last) >= serviceEvery) {
			rctx, cancel := context.WithTimeout(ctx, time.Minute)
			if err := p.refreshService(rctx); err != nil {
				// The next try in 2 minutes.
				log.Printf("cloud: the projects of the service: %v", err)
				domain, last = id.Domain, time.Now().Add(2*time.Minute-serviceEvery)
			} else {
				domain, last = id.Domain, time.Now()
			}
			cancel()
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
