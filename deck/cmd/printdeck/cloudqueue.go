package main

import (
	"context"
	"log"
	"time"

	"printdeck/internal/cloud"
	"printdeck/internal/queue"
)

// queueTurn is the time between two prints of the queue in the estimates of the start: the part
// comes off, and the printer heats again.
const queueTurn = 10 * time.Minute

// watchQueue reports the queue of the deck to the service: at each change, and each 2 minutes
// while it has files, so the start times stay fresh. Only the files of projects of the service go:
// each project sees its own files and their start times.
func (p *projectLink) watchQueue(ctx context.Context, q *queue.Queue) {
	if q == nil {
		return
	}
	changed := make(chan struct{}, 1)
	redraw := q.OnChange
	q.OnChange = func() {
		if redraw != nil {
			redraw()
		}
		select {
		case changed <- struct{}{}:
		default:
		}
	}
	tick := time.NewTicker(2 * time.Minute)
	defer tick.Stop()
	sent := false // the last report had files
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-changed:
		}
		report := p.queueReport(q)
		if len(report.Items) == 0 && !sent {
			continue
		}
		id, printer := p.Identity(), p.printerID()
		if id == nil || printer == "" {
			continue
		}
		rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := p.Client.SendQueue(rctx, id, printer, report)
		cancel()
		if err != nil {
			log.Printf("queue: the report to the service: %v", err)
			continue
		}
		sent = len(report.Items) > 0
	}
}

// queueReport is the report of the queue: the files of projects of the service, each with its
// place and the estimate of its start after the print that runs and the files before it.
func (p *projectLink) queueReport(q *queue.Queue) cloud.QueueReport {
	at := time.Now()
	if st := p.k.State(); st.Busy() {
		if st.HasTimeLeft {
			at = at.Add(st.TimeLeft)
		}
		at = at.Add(queueTurn)
	}
	report := cloud.QueueReport{Items: []cloud.QueueEntry{}}
	for i, it := range q.List() {
		if pid := p.serviceProjectID(it.Project); pid != "" && it.Project != "" {
			report.Items = append(report.Items, cloud.QueueEntry{ID: it.ID, Project: pid, Name: it.Name, Place: i + 1, Seconds: it.Seconds,
				Start: at.UTC().Truncate(time.Second), File: it.Path})
		}
		at = at.Add(time.Duration(it.Seconds)*time.Second + queueTurn)
	}
	return report
}

// applyRemoves takes out of the queue the files that the owner took out on a project page
// (GET printers/<id>/removes), and tells the service. A removal must name the project of the file.
// A removal of a file that is not in the queue is done too.
func (p *projectLink) applyRemoves(ctx context.Context, q *queue.Queue) {
	id, printer := p.Identity(), p.printerID()
	if q == nil || id == nil || printer == "" {
		return
	}
	rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	removes, err := p.Client.QueueRemoves(rctx, id, printer)
	cancel()
	if err != nil {
		log.Printf("queue: the removals: %v", err)
		return
	}
	if len(removes) == 0 {
		return
	}
	var done []string
	for _, rm := range removes {
		for _, it := range q.List() {
			if it.ID == rm.Item && p.serviceProjectID(it.Project) == rm.Project {
				q.Remove(it.ID)
				log.Printf("queue: %s took %s out of the queue on the project page", rm.By, it.Name)
			}
		}
		done = append(done, rm.Item)
	}
	rctx, cancel = context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := p.Client.QueueRemovesDone(rctx, id, printer, done); err != nil {
		log.Printf("queue: the removals done: %v", err)
	}
}
