package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"printdeck/internal/cloud"
)

// The printers of the owner: printers/<id>.json. Each deck registers the printers that it drives
// or watches, with a key of its own for each printer. The prints and the live tiles of the
// projects have the ID of their printer, so one project has the prints of many printers.

// MaxPrinters is the most printers at the service.
const MaxPrinters = 64

func printerKey(id string) string { return "printers/" + id + ".json" }

// printerLiveKey is the file of what the printer id does now (PrinterLive). Only the list of the
// printers of the owner has it.
func printerLiveKey(id string) string { return "printers/" + id + "-live.json" }

// printerLiveStale is the age of a live record after which the list shows it no more.
const printerLiveStale = 24 * time.Hour

// printers returns the printers at the service, sorted by name.
func (s *Server) printers(ctx context.Context) ([]cloud.Printer, error) {
	keys, err := s.Store.List(ctx, "printers/")
	if err != nil {
		return nil, err
	}
	out := []cloud.Printer{}
	for _, k := range keys {
		if !strings.HasSuffix(k, ".json") {
			continue
		}
		b, _, err := s.Store.Get(ctx, k)
		if err != nil {
			continue
		}
		var p cloud.Printer
		if json.Unmarshal(b, &p) == nil && p.ID != "" {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// loadPrinter returns the printer id, or ErrNotFound.
func (s *Server) loadPrinter(ctx context.Context, id string) (*cloud.Printer, error) {
	if !validID(id) {
		return nil, ErrNotFound
	}
	b, _, err := s.Store.Get(ctx, printerKey(id))
	if err != nil {
		return nil, err
	}
	var p cloud.Printer
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// printerOf returns the ID of the printer id when the deck d reports it, or "".
func (s *Server) printerOf(ctx context.Context, d *Deck, id string) string {
	if id == "" {
		return ""
	}
	p, err := s.loadPrinter(ctx, id)
	if err != nil || p.Deck != d.ID {
		return ""
	}
	return p.ID
}

// registerPrinter adds the printer of the deck d with the key of the request, or changes it.
func (s *Server) registerPrinter(ctx context.Context, d *Deck, in cloud.PrinterRequest) (any, error) {
	in.Key, in.Name, in.Model = clip(in.Key, 40), clip(in.Name, 64), clip(in.Model, 64)
	if in.Key == "" || in.Name == "" {
		return nil, fail(http.StatusBadRequest, "a printer needs a key and a name")
	}
	switch in.Process {
	case "fdm", "resin":
	default:
		return nil, fail(http.StatusBadRequest, "the process of a printer is fdm or resin")
	}
	all, err := s.printers(ctx)
	if err != nil {
		return nil, err
	}
	p := cloud.Printer{Deck: d.ID, Key: in.Key, Added: s.now().UTC()}
	for _, o := range all {
		if o.Deck == d.ID && o.Key == in.Key {
			p = o
		}
	}
	if p.ID == "" {
		if len(all) >= MaxPrinters {
			return nil, fail(http.StatusBadRequest, "the service has too many printers")
		}
		p.ID = newID(8)
	}
	p.Name, p.Process, p.Model = in.Name, in.Process, in.Model
	if err := s.putJSON(ctx, printerKey(p.ID), p); err != nil {
		return nil, err
	}
	return p, nil
}

// routePrinters serves /api/v1/printers: a deck registers a printer (POST), and the owner or a deck
// lists the printers (GET).
func (s *Server) routePrinters(ctx context.Context, r *http.Request, body []byte) (any, error) {
	w, err := s.whoFor(ctx, r, body, "")
	if err != nil {
		return nil, err
	}
	if err := ownerOnly(w); err != nil {
		return nil, err
	}
	switch r.Method {
	case http.MethodGet:
		list, err := s.printers(ctx)
		if err != nil {
			return nil, err
		}
		// What each printer does now: for the owner and the decks only.
		for i := range list {
			b, _, err := s.Store.Get(ctx, printerLiveKey(list[i].ID))
			if err != nil {
				continue
			}
			var l cloud.PrinterLive
			if json.Unmarshal(b, &l) == nil && s.now().Sub(l.Updated) < printerLiveStale {
				list[i].Live = &l
			}
		}
		return list, nil
	case http.MethodPost:
		if w.deck == nil {
			return nil, fail(http.StatusUnauthorized, "only a deck registers a printer")
		}
		var in cloud.PrinterRequest
		if json.Unmarshal(body, &in) != nil {
			return nil, fail(http.StatusBadRequest, "bad JSON")
		}
		return s.registerPrinter(ctx, w.deck, in)
	}
	return nil, fail(http.StatusMethodNotAllowed, "no such request")
}

// routeSentPlates serves GET /api/v1/printers/<id>/plates (the plates that wait for the deck)
// and POST /api/v1/printers/<id>/plates/taken.
func (s *Server) routeSentPlates(ctx context.Context, r *http.Request, body []byte, id, what string) (any, error) {
	w, err := s.whoFor(ctx, r, body, "")
	if err != nil {
		return nil, err
	}
	if w.deck == nil {
		return nil, fail(http.StatusUnauthorized, "only a deck reads its plates")
	}
	if !validID(id) {
		return nil, fail(http.StatusBadRequest, "bad printer")
	}
	switch {
	case what == "plates" && r.Method == http.MethodGet:
		return s.sentPlates(ctx, w.deck, id)
	case what == "plates/taken" && r.Method == http.MethodPost:
		var in cloud.PlateTaken
		if json.Unmarshal(body, &in) != nil {
			return nil, fail(http.StatusBadRequest, "bad JSON")
		}
		return map[string]bool{"ok": true}, s.takePlate(ctx, w.deck, id, in)
	}
	return nil, fail(http.StatusMethodNotAllowed, "no such request")
}

// routeRemoves serves GET /api/v1/printers/<id>/removes (the files of the queue that the owner
// took out on the project page) and POST /api/v1/printers/<id>/removes/done.
func (s *Server) routeRemoves(ctx context.Context, r *http.Request, body []byte, id, what string) (any, error) {
	w, err := s.whoFor(ctx, r, body, "")
	if err != nil {
		return nil, err
	}
	if w.deck == nil {
		return nil, fail(http.StatusUnauthorized, "only a deck reads its removals")
	}
	if !validID(id) {
		return nil, fail(http.StatusBadRequest, "bad printer")
	}
	switch {
	case what == "removes" && r.Method == http.MethodGet:
		return s.queueRemoves(ctx, w.deck, id)
	case what == "removes/done" && r.Method == http.MethodPost:
		var in cloud.QueueRemoved
		if json.Unmarshal(body, &in) != nil {
			return nil, fail(http.StatusBadRequest, "bad JSON")
		}
		return map[string]bool{"ok": true}, s.removesDone(ctx, w.deck, id, in)
	}
	return nil, fail(http.StatusMethodNotAllowed, "no such request")
}

// routePrinterLive serves POST /api/v1/printers/<id>/live: a deck sends what its printer does now.
func (s *Server) routePrinterLive(ctx context.Context, r *http.Request, body []byte, id string) (any, error) {
	w, err := s.whoFor(ctx, r, body, "")
	if err != nil {
		return nil, err
	}
	if w.deck == nil {
		return nil, fail(http.StatusUnauthorized, "only a deck sends the state of its printer")
	}
	if !validID(id) {
		return nil, fail(http.StatusBadRequest, "bad printer")
	}
	if err := s.deckPrinter(ctx, w.deck, id); err != nil {
		return nil, err
	}
	var in cloud.PrinterLive
	if json.Unmarshal(body, &in) != nil {
		return nil, fail(http.StatusBadRequest, "bad JSON")
	}
	in.Name = clip(in.Name, 120)
	in.Percent = max(0, min(100, in.Percent))
	in.Updated = s.now().UTC()
	return map[string]bool{"ok": true}, s.putJSON(ctx, printerLiveKey(id), in)
}

// routeQueue serves POST /api/v1/printers/<id>/queue: a deck reports the queue of its printer.
func (s *Server) routeQueue(ctx context.Context, r *http.Request, body []byte, id string) (any, error) {
	w, err := s.whoFor(ctx, r, body, "")
	if err != nil {
		return nil, err
	}
	if w.deck == nil {
		return nil, fail(http.StatusUnauthorized, "only a deck reports a queue")
	}
	if !validID(id) {
		return nil, fail(http.StatusBadRequest, "bad printer")
	}
	var in cloud.QueueReport
	if json.Unmarshal(body, &in) != nil {
		return nil, fail(http.StatusBadRequest, "bad JSON")
	}
	return map[string]bool{"ok": true}, s.setQueue(ctx, w.deck, id, in)
}

// printersOf returns the printers with the IDs ids, for a project view. public leaves out the deck
// and the key.
func (s *Server) printersOf(ctx context.Context, ids map[string]bool, public bool) []cloud.Printer {
	var out []cloud.Printer
	for id := range ids {
		p, err := s.loadPrinter(ctx, id)
		if errors.Is(err, ErrNotFound) || err != nil {
			continue
		}
		if public {
			p.Deck, p.Key = "", ""
		}
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
