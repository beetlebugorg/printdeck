package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"printdeck/internal/cloud"
)

// Approved plates go to the queue of a printer. The owner sends an approved plate to a printer of
// the same process (POST .../builds/<build>/plates/<n>/send). The service keeps an index entry in
// printers/<id>-plates/. The deck of the printer reads its plates (GET
// /api/v1/printers/<id>/plates), downloads each file, adds it to its queue, and says so (POST
// /api/v1/printers/<id>/plates/taken). The queue never starts a print: a press on the deck does.

func sentKey(printer, project, build string, n int) string {
	return "printers/" + printer + "-plates/" + project + "." + build + "." + strconv.Itoa(n) + ".json"
}

type sentEntry struct {
	Project string `json:"project"`
	Build   string `json:"build"`
	Plate   int    `json:"plate"`
}

// sendPlate sends the approved plate n of the build to the printer of in.
func (s *Server) sendPlate(ctx context.Context, p *Project, w *who, id string, n int, in cloud.PlateSend) (any, error) {
	if err := ownerOnly(w); err != nil {
		return nil, err
	}
	b, err := s.loadBuild(ctx, p.ID, id)
	if err != nil {
		return nil, err
	}
	if n < 0 || n >= len(b.Plates) {
		return nil, fail(http.StatusGone, "no such plate")
	}
	pl := &b.Plates[n]
	if b.State != cloud.BuildReady || pl.State != cloud.PartApproved || pl.File == "" {
		return nil, fail(http.StatusConflict, "only an approved plate with its file goes to a printer")
	}
	pr, err := s.loadPrinter(ctx, in.Printer)
	if err != nil {
		return nil, fail(http.StatusBadRequest, "no such printer")
	}
	if pr.Process != pl.Process {
		return nil, fail(http.StatusBadRequest, "the printer is not for "+pl.Process)
	}
	if pl.Sent != "" && pl.Sent != pr.ID {
		s.Store.Delete(ctx, sentKey(pl.Sent, p.ID, id, n))
	}
	pl.Sent, pl.SentAt, pl.Taken = pr.ID, s.now().UTC(), time.Time{}
	if err := s.putJSON(ctx, sentKey(pr.ID, p.ID, id, n), sentEntry{Project: p.ID, Build: id, Plate: n}); err != nil {
		return nil, err
	}
	if err := s.putJSON(ctx, buildKey(p.ID, id), b); err != nil {
		return nil, err
	}
	return s.projectBuild(ctx, p, b), nil
}

// deckPrinter checks that the printer id is a printer of the deck d.
func (s *Server) deckPrinter(ctx context.Context, d *Deck, id string) error {
	pr, err := s.loadPrinter(ctx, id)
	if err != nil || pr.Deck != d.ID {
		return fail(http.StatusForbidden, "the printer is not of this deck")
	}
	return nil
}

// sentPlates returns the plates that wait for the deck of the printer id: sent, and not taken.
func (s *Server) sentPlates(ctx context.Context, d *Deck, id string) ([]cloud.SentPlate, error) {
	if err := s.deckPrinter(ctx, d, id); err != nil {
		return nil, err
	}
	keys, err := s.Store.List(ctx, "printers/"+id+"-plates/")
	if err != nil {
		return nil, err
	}
	out := []cloud.SentPlate{}
	for _, k := range keys {
		var e sentEntry
		raw, _, err := s.Store.Get(ctx, k)
		if err != nil || json.Unmarshal(raw, &e) != nil {
			continue
		}
		b, err := s.loadBuild(ctx, e.Project, e.Build)
		if err != nil || e.Plate < 0 || e.Plate >= len(b.Plates) {
			s.Store.Delete(ctx, k)
			continue
		}
		pl := b.Plates[e.Plate]
		if pl.Sent != id || !pl.Taken.IsZero() || pl.State != cloud.PartApproved {
			s.Store.Delete(ctx, k)
			continue
		}
		p, _, err := s.loadProject(ctx, e.Project)
		if err != nil {
			continue
		}
		// The deck has no cookies of the service: a presigned address of the bucket.
		u, err := s.Grants.DirectURL(ctx, buildDir(e.Project, e.Build)+pl.File, urlTTL)
		if err != nil {
			continue
		}
		var names []string
		for _, pp := range pl.Parts {
			names = append(names, pp.Name)
		}
		out = append(out, cloud.SentPlate{Project: e.Project, ProjectName: p.Name, Build: e.Build, Plate: e.Plate,
			Name: strings.Join(names, ", "), File: pl.File, URL: u, Size: pl.Size, SHA256: pl.SHA256, Seconds: pl.Seconds})
	}
	return out, nil
}

// takePlate marks the plate of in as in the queue of the deck of the printer id.
func (s *Server) takePlate(ctx context.Context, d *Deck, id string, in cloud.PlateTaken) error {
	if err := s.deckPrinter(ctx, d, id); err != nil {
		return err
	}
	b, err := s.loadBuild(ctx, in.Project, in.Build)
	if err != nil {
		return err
	}
	if in.Plate < 0 || in.Plate >= len(b.Plates) || b.Plates[in.Plate].Sent != id {
		return fail(http.StatusConflict, "the plate is not sent to this printer")
	}
	b.Plates[in.Plate].Taken = s.now().UTC()
	if err := s.putJSON(ctx, buildKey(in.Project, in.Build), b); err != nil {
		return err
	}
	return s.Store.Delete(ctx, sentKey(id, in.Project, in.Build, in.Plate))
}
