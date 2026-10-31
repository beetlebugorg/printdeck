package api

import (
	"context"
	"encoding/json"
	"net/http"
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
		for _, it := range q.Items {
			out = append(out, cloud.ProjectQueued{Printer: q.Printer, Name: it.Name, Place: it.Place, Seconds: it.Seconds,
				Start: it.Start, Updated: q.Updated})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}
