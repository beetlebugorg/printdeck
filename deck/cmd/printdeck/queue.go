package main

import (
	"context"
	"time"

	"printdeck/internal/ui"
)

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
