package api

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"printdeck/internal/cloud"
)

// The queues of the printers. A deck reports the files of its queue that are in a project of the
// service (POST /api/v1/printers/<id>/queue). The service keeps the files of each project in the
// project, x/<project>/queue/<printer>.json, so a project sees only its own files: their names,
// their places and their start times. printers/<id>-queue.json names the projects of the last
// report, so a project that leaves the queue loses its file.

// MaxQueue is the most files in a report of a queue.
const MaxQueue = 50

// queueStale is the age of a report after which the project page shows it no more: the deck is
// off, or it lost the network.
const queueStale = 6 * time.Hour

func queueKey(project, printer string) string { return "x/" + project + "/queue/" + printer + ".json" }
func removeDir(printer string) string          { return "printers/" + printer + "-remove/" }

// queueItemID is the form of the ID of a file in the queue of a deck.
var queueItemID = regexp.MustCompile(`^[0-9a-f]{8,32}$`)

// removeStale is the age of a removal after which the service drops it: the deck is off.
const removeStale = 7 * 24 * time.Hour
func queueProjectsKey(printer string) string  { return "printers/" + printer + "-queue.json" }

// projectQueue is x/<project>/queue/<printer>.json.
type projectQueue struct {
	Printer string             `json:"printer"` // the name of the printer
	Updated time.Time          `json:"updated"`
	Items   []cloud.QueueEntry `json:"items"`
}

// setQueue keeps the report of the queue of the printer id of the deck d.
func (s *Server) setQueue(ctx context.Context, d *Deck, id string, in cloud.QueueReport) error {
	pr, err := s.loadPrinter(ctx, id)
	if err != nil || pr.Deck != d.ID {
		return fail(http.StatusForbidden, "the printer is not of this deck")
	}
	if len(in.Items) > MaxQueue {
		return fail(http.StatusBadRequest, "the queue has too many files")
	}
	now := s.now().UTC()
	by := map[string][]cloud.QueueEntry{}
	for _, it := range in.Items {
		if !validID(it.Project) {
			continue
		}
		it.Name = clip(strings.TrimSpace(it.Name), 120)
		if !queueItemID.MatchString(it.ID) {
			it.ID = ""
		}
		by[it.Project] = append(by[it.Project], it)
	}
	var kept []string
	for project, items := range by {
		if _, _, err := s.loadProject(ctx, project); err != nil {
			continue
		}
		if err := s.putJSON(ctx, queueKey(project, id), projectQueue{Printer: pr.Name, Updated: now, Items: items}); err != nil {
			return err
		}
		kept = append(kept, project)
	}
	// The projects of the last report that have no file now.
	var last []string
	if b, _, err := s.Store.Get(ctx, queueProjectsKey(id)); err == nil {
		json.Unmarshal(b, &last)
	}
	for _, project := range last {
		if _, ok := by[project]; !ok {
			s.Store.Delete(ctx, queueKey(project, id))
		}
	}
	sort.Strings(kept)
	return s.putJSON(ctx, queueProjectsKey(id), kept)
}

// projectQueued returns the files of the project in the queues of the printers, the soonest first.
func (s *Server) projectQueued(ctx context.Context, project string) []cloud.ProjectQueued {
	keys, err := s.Store.List(ctx, "x/"+project+"/queue/")
	if err != nil {
		return nil
	}
	var out []cloud.ProjectQueued
	for _, k := range keys {
		b, _, err := s.Store.Get(ctx, k)
		if err != nil {
			continue
		}
		var q projectQueue
		if json.Unmarshal(b, &q) != nil || s.now().Sub(q.Updated) > queueStale {
			continue
		}
		printer := strings.TrimSuffix(path.Base(k), ".json")
		removing := map[string]bool{}
		if rk, err := s.Store.List(ctx, removeDir(printer)); err == nil {
			for _, r := range rk {
				removing[strings.TrimSuffix(path.Base(r), ".json")] = true
			}
		}
		for _, it := range q.Items {
			pq := cloud.ProjectQueued{Printer: q.Printer, Name: it.Name, Place: it.Place, Seconds: it.Seconds,
				Start: it.Start, Updated: q.Updated}
			if it.ID != "" {
				pq.ID, pq.PrinterID, pq.Removing = it.ID, printer, removing[it.ID]
			}
			out = append(out, pq)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

// removeQueued asks the deck of the printer to take the file item of the project out of its
// queue. The file must be in the last report of the queue for the project. The deck reads the
// removals each minute (queueRemoves).
func (s *Server) removeQueued(ctx context.Context, project, printer, item string, w *who) error {
	if !validID(printer) || !queueItemID.MatchString(item) {
		return fail(http.StatusBadRequest, "bad printer or file")
	}
	b, _, err := s.Store.Get(ctx, queueKey(project, printer))
	if err != nil {
		return fail(http.StatusNotFound, "the file is not in the queue")
	}
	var q projectQueue
	if json.Unmarshal(b, &q) != nil {
		return fail(http.StatusNotFound, "the file is not in the queue")
	}
	for _, it := range q.Items {
		if it.ID == item {
			by := "the owner"
			if w.name != "" {
				by = w.name
			}
			return s.putJSON(ctx, removeDir(printer)+item+".json",
				cloud.QueueRemove{Item: item, Project: project, Name: it.Name, By: by, At: s.now().UTC()})
		}
	}
	return fail(http.StatusNotFound, "the file is not in the queue")
}

// queueRemoves returns the removals that wait for the deck d of the printer id. It drops the
// removals older than removeStale.
func (s *Server) queueRemoves(ctx context.Context, d *Deck, id string) ([]cloud.QueueRemove, error) {
	if err := s.deckPrinter(ctx, d, id); err != nil {
		return nil, err
	}
	keys, err := s.Store.List(ctx, removeDir(id))
	if err != nil {
		return nil, err
	}
	out := []cloud.QueueRemove{}
	for _, k := range keys {
		b, _, err := s.Store.Get(ctx, k)
		var rm cloud.QueueRemove
		if err != nil || json.Unmarshal(b, &rm) != nil || s.now().Sub(rm.At) > removeStale {
			s.Store.Delete(ctx, k)
			continue
		}
		out = append(out, rm)
	}
	return out, nil
}

// removesDone drops the removals that the deck d of the printer id did, and takes the files out of
// the queues of their projects, so the page shows them no more before the next report.
func (s *Server) removesDone(ctx context.Context, d *Deck, id string, in cloud.QueueRemoved) error {
	if err := s.deckPrinter(ctx, d, id); err != nil {
		return err
	}
	if len(in.Items) > MaxQueue {
		return fail(http.StatusBadRequest, "too many files")
	}
	for _, item := range in.Items {
		if !queueItemID.MatchString(item) {
			continue
		}
		k := removeDir(id) + item + ".json"
		b, _, err := s.Store.Get(ctx, k)
		if err != nil {
			continue
		}
		var rm cloud.QueueRemove
		if json.Unmarshal(b, &rm) == nil && validID(rm.Project) {
			if qb, _, err := s.Store.Get(ctx, queueKey(rm.Project, id)); err == nil {
				var q projectQueue
				if json.Unmarshal(qb, &q) == nil {
					kept := q.Items[:0]
					for _, it := range q.Items {
						if it.ID != item {
							kept = append(kept, it)
						}
					}
					q.Items = kept
					if err := s.putJSON(ctx, queueKey(rm.Project, id), q); err != nil {
						return err
					}
				}
			}
		}
		if err := s.Store.Delete(ctx, k); err != nil {
			return err
		}
	}
	return nil
}
