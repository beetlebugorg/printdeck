package main

import (
	"context"
	"time"

	"printdeck/internal/cloud"
	"printdeck/internal/queue"
)

// printerLiveIdle is the time between two sends of the state of a printer with no print.
const printerLiveIdle = 10 * time.Minute

// watchPrinterLive sends what the printer of the deck does now to the service, with a project or
// not: each minute during a print, at once at a start, a pause, a resume and an end, and each
// printerLiveIdle with no print. Only the owner reads it (GET /api/v1/printers).
func (p *projectLink) watchPrinterLive(ctx context.Context, q *queue.Queue) {
	tick := time.NewTicker(liveLook)
	defer tick.Stop()
	var last cloud.PrinterLive
	var sent time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		id, printer := p.Identity(), p.printerID()
		if id == nil || printer == "" || !p.k.State().Reachable {
			continue // no state yet: the deck program just started, or the printer does not answer
		}
		in := p.printerLive(q)
		changed := in.Printing != last.Printing || in.Paused != last.Paused || in.Result != last.Result || in.Name != last.Name
		every := printerLiveIdle
		if in.Printing {
			every = liveEvery
		}
		if !changed && time.Since(sent) < every {
			continue
		}
		rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := p.Client.SendPrinterLive(rctx, id, printer, in)
		cancel()
		if err == nil {
			last, sent = in, time.Now()
		}
	}
}

// printerLive is the state of the printer now, and the files of the queue.
func (p *projectLink) printerLive(q *queue.Queue) cloud.PrinterLive {
	st := p.k.State()
	var in cloud.PrinterLive
	if st.Busy() {
		in.Printing, in.Paused, in.Name = true, st.Paused, st.Name()
		in.Started = time.Now().Add(-st.Elapsed).UTC().Truncate(time.Second)
		in.Percent, in.Layer, in.Layers = st.Completion, st.Layer, st.Layers
		if st.HasTimeLeft {
			in.Left = int(st.TimeLeft.Seconds())
		}
	} else if st.Finished != "" {
		in.Result, in.Name = st.Finished, st.FinishedName()
		if h := p.k.History(); len(h) > 0 {
			in.Ended = h[0].End.UTC()
		}
	}
	if q != nil {
		in.Queue, in.QueueSeconds = q.Len(), q.Seconds()
	}
	return in
}
