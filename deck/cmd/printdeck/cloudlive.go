package main

import (
	"context"
	"log"
	"strconv"
	"time"

	"printdeck/internal/cloud"
	"printdeck/internal/printer"
)

// The deck sends the progress of a print in a project to the project each liveEvery, and at once
// at the start, at a pause, and at the end. It looks at the state each liveLook: the state is in
// memory. liveTries is the most uploads of the picture of the model for one print.
const (
	liveEvery = time.Minute
	liveLook  = 15 * time.Second
	liveTries = 3
)

// livePrint is the print of a project that the deck sends to the project now.
type livePrint struct {
	key     string // the file and the start of the print
	path    string
	started time.Time
	project string // the ID of the project at the service
	sent    time.Time
	seen    time.Time // the last look at the print while it printed
	percent float64
	paused  bool
	model   bool // the picture of the model is up
	tries   int  // the uploads of the picture
}

// watchLive sends the progress of the print of a project to the project, until ctx ends.
func (p *projectLink) watchLive(ctx context.Context) {
	tick := time.NewTicker(liveLook)
	defer tick.Stop()
	var l *livePrint
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		l = p.sendLive(ctx, l, time.Now())
	}
}

// sendLive sends the print now to its project when it is due, and returns the print that the deck
// sends now, or nil.
func (p *projectLink) sendLive(ctx context.Context, l *livePrint, now time.Time) *livePrint {
	st := p.k.State()
	name := ""
	if st.Busy() {
		name = p.k.NextProject()
	}
	return p.liveStep(ctx, l, st, name, now)
}

// liveStep is sendLive with the state st of the printer and the name of the project of the print.
func (p *projectLink) liveStep(ctx context.Context, l *livePrint, st printer.State, name string, now time.Time) *livePrint {
	id := p.Identity()
	if id == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if l != nil && (!st.Busy() || st.FilePath != l.path) {
		// The print ended: the project shows its result until the print comes up. The history
		// can get the record a moment after the end.
		if !p.endLive(ctx, id, l) {
			if now.Sub(l.seen) < 2*time.Minute {
				return l
			}
			l.leave(ctx, p, id) // no record: the print is not in the history
		}
		l = nil
	}
	if name == "" {
		return l.leave(ctx, p, id)
	}
	cp, err := p.serviceProject(ctx, id, name, false)
	if err != nil {
		log.Printf("projects: live: %v", err)
		return l
	}
	if l != nil && l.project != cp.ID {
		l = l.leave(ctx, p, id) // the print went to another project
	}
	if l == nil {
		start := now.Add(-st.Elapsed).Truncate(time.Second)
		l = &livePrint{key: st.FilePath + "@" + strconv.FormatInt(start.Unix(), 10), path: st.FilePath,
			started: start, project: cp.ID}
	}
	l.seen, l.percent = now, st.Completion
	if now.Sub(l.sent) < liveEvery && st.Paused == l.paused {
		return l
	}
	in := cloud.LiveRequest{Key: l.key, Name: st.Name(), Started: l.started.UTC(), Percent: st.Completion,
		Layer: st.Layer, Layers: st.Layers, Paused: st.Paused, Model: l.model, Printer: p.model}
	if st.HasTimeLeft {
		in.Left = int(st.TimeLeft.Seconds())
	}
	if p.shareOf != nil {
		in.Share = p.shareOf(printer.Record{Path: st.FilePath, End: now})
	}
	out, err := p.Client.SendLive(ctx, id, l.project, in)
	if err != nil {
		log.Printf("projects: live: %v", err)
		return l
	}
	l.sent, l.paused = now, st.Paused
	if out.Grant != nil && l.tries < liveTries {
		l.tries++
		r := printer.Record{Path: st.FilePath, File: st.File, Title: st.Title, Result: "complete", End: l.started}
		if store := p.k.SpoolStore(); store != nil {
			if sp, ok := store.In(); ok {
				r.Spool, r.SpoolID = sp.Label(), sp.ID
			}
		}
		if pic := p.recordPicture(r); pic != nil {
			const cache = "private, max-age=31536000, immutable"
			if cloud.PostFile(ctx, p.Client.HTTP, *out.Grant, "model.jpg", "image/jpeg", cache, pic) == nil {
				l.model = true
				in.Model = true
				p.Client.SendLive(ctx, id, l.project, in)
			}
		}
	}
	return l
}

// endLive sends the result of the print l to its project. It reports that the history has the
// record of the print.
func (p *projectLink) endLive(ctx context.Context, id *cloud.Identity, l *livePrint) bool {
	for _, r := range p.k.History() {
		if r.Path != l.path || r.End.Before(l.started) {
			continue
		}
		in := cloud.LiveRequest{Key: l.key, Name: r.Name(), Started: l.started.UTC(), Percent: l.percent,
			Result: r.Result, Model: l.model, Printer: p.model}
		if r.Result == "complete" {
			in.Percent = 100
		}
		if r.Project == "" {
			l.leave(ctx, p, id) // the print left the project at its end
		} else if _, err := p.Client.SendLive(ctx, id, l.project, in); err != nil {
			log.Printf("projects: live: %v", err)
		}
		return true
	}
	return false
}

// leave tells the project of l that the deck prints nothing in it now. It returns nil.
func (l *livePrint) leave(ctx context.Context, p *projectLink, id *cloud.Identity) *livePrint {
	if l == nil {
		return nil
	}
	if err := p.Client.EndLive(ctx, id, l.project); err != nil {
		log.Printf("projects: live: %v", err)
	}
	return nil
}
