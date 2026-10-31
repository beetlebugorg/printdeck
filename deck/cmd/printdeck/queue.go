package main

import (
	"context"
	"path/filepath"
	"time"

	"printdeck/internal/printer"
	"printdeck/internal/queue"
	"printdeck/internal/ui"
)

// startQueue gives the app its print queue, in queue.json of the state folder, and asks for the
// next file at the end of each print. On a resin deck, a start from ChiTuBox during a print goes
// to the queue, and a start with files in the queue asks on the deck.
func startQueue(ctx context.Context, stateDir string, pr printerBackend, app *ui.App) {
	app.Queue = &queue.Queue{Path: filepath.Join(stateDir, "queue.json"), OnChange: app.Redraw}
	go watchQueue(ctx, pr, app)
	if c, ok := pr.(*printer.ChiTu); ok {
		c.Enqueue = func(name string) error { return app.Enqueue(name, "slicer") }
		c.QueueLen = app.Queue.Len
		c.AskSent = app.AskSentFile
	}
}

// watchQueue asks for the next file of the queue at the end of each print: when the printer goes
// from a print to no print. The question is on each screen, and only a press starts the file.
func watchQueue(ctx context.Context, pr printerBackend, app *ui.App) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	busy := pr.State().Busy()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		st := pr.State()
		now := st.Reachable && st.Busy()
		if busy && !now && st.Reachable && app.Queue.Len() > 0 {
			app.AskQueueNext()
		}
		if st.Reachable {
			busy = now
		}
	}
}
