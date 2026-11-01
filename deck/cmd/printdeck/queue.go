package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"printdeck/internal/notify"
	"printdeck/internal/printer"
	"printdeck/internal/queue"
	"printdeck/internal/ui"
)

// startQueue gives the app its print queue, in queue.json of the state folder, and asks for the
// next file at the end of each print. On a resin deck, a start from ChiTuBox during a print goes
// to the queue, and a start with files in the queue asks on the deck.
func startQueue(ctx context.Context, stateDir string, pr printerBackend, app *ui.App) {
	app.Queue = &queue.Queue{Path: filepath.Join(stateDir, "queue.json"), OnChange: app.Redraw}
	phones := phonesIn(stateDir)
	go watchQueue(ctx, pr, app, func(m notify.Message) { sendPhones(ctx, phones, m) })
	if c, ok := pr.(*printer.ChiTu); ok {
		c.Enqueue = func(name string) error { return app.Enqueue(name, "slicer") }
		c.QueueLen = app.Queue.Len
		c.AskSent = app.AskSentFile
	}
}

// watchQueue asks for the next file of the queue at the end of each print: when the printer goes
// from a print to no print. It also asks when a file from a slicer or from the service comes into
// the queue while the printer is free. The question is on each screen, and only a press starts
// the file.
func watchQueue(ctx context.Context, pr printerBackend, app *ui.App, push func(notify.Message)) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	busy := pr.State().Busy()
	seen := map[string]bool{}
	arrived(seen, app.Queue.List()) // the files of the queue at the start ask nothing
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		st := pr.State()
		now := st.Reachable && st.Busy()
		if arrived(seen, app.Queue.List()) && st.Reachable && !now {
			app.AskQueueNext()
		}
		if busy && !now && st.Reachable && app.Queue.Len() > 0 {
			app.AskQueueNext()
			if it, ok := app.Queue.Next(); ok && push != nil {
				push(nextMessage(it, pr.Machine().Process == printer.ProcessResin))
			}
		}
		if st.Reachable {
			busy = now
		}
	}
}

// arrived reports that items has a file from a slicer or from the service that is not in seen.
// seen gets the IDs of items, and keeps no other IDs.
func arrived(seen map[string]bool, items []queue.Item) bool {
	out := false
	ids := map[string]bool{}
	for _, it := range items {
		ids[it.ID] = true
		if !seen[it.ID] && it.From != "deck" {
			out = true
		}
		seen[it.ID] = true
	}
	for id := range seen {
		if !ids[id] {
			delete(seen, id)
		}
	}
	return out
}

// nextMessage is the push message of the next file of the queue at the end of a print: its name,
// its material and its time, and what to do before Start on the deck.
func nextMessage(it queue.Item, resin bool) notify.Message {
	parts := []string{it.Name}
	switch {
	case it.MaterialName != "":
		parts = append(parts, it.MaterialName)
	case it.Material != "":
		parts = append(parts, it.Material)
	}
	if it.Seconds > 0 {
		m := (it.Seconds + 59) / 60
		parts = append(parts, fmt.Sprintf("%d:%02d", m/60, m%60))
	}
	what := "Take the part off the bed, then press Start on the deck."
	if resin {
		what = "Check the vat and the build plate, then press Start on the deck."
	}
	return notify.Message{Title: "Next in the queue", Body: strings.Join(parts, " · ") + ". " + what, Tags: "printer"}
}
