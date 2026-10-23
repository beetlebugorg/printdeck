package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"printdeck/internal/notify"
	"printdeck/internal/printer"
	"printdeck/internal/ui"
)

// startNotify sets up the push messages of a print (Settings > Notifications): the topic is kept
// in the file notify of the state folder. It sends the messages of the events of each print, and a
// test message to a new topic.
func startNotify(ctx context.Context, stateDir string, pr printerBackend, app *ui.App) {
	file := filepath.Join(stateDir, "notify")
	var topic atomic.Value
	b, _ := os.ReadFile(file)
	topic.Store(strings.TrimSpace(string(b)))
	app.NotifyTopic = topic.Load().(string)
	app.OnNotify = func(t string) {
		topic.Store(t)
		if err := os.WriteFile(file, []byte(t+"\n"), 0o644); err != nil {
			app.Toast("The topic was not saved: " + err.Error())
		}
		if t == "" {
			return
		}
		go func() {
			m := notify.Message{Title: "Printdeck", Body: "The messages of your prints come here.", Tags: "printer"}
			if err := notify.Send(ctx, t, m); err != nil {
				app.Toast("The test message did not go: " + err.Error())
			}
		}()
	}
	go func() {
		var w notify.Watcher
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			st := pr.State()
			msgs := w.Step(st, rollCheck(pr, st))
			t := topic.Load().(string)
			if t == "" {
				continue
			}
			for _, m := range msgs {
				if err := notify.Send(ctx, t, m); err != nil {
					log.Printf("notify: %s: %v", m.Title, err)
				}
			}
		}
	}()
}

// rollCheck is the roll in the printer and the filament of the rest of the print, in grams.
func rollCheck(pr printerBackend, st printer.State) notify.Roll {
	rolls := pr.SpoolStore()
	if rolls == nil || !st.Busy() {
		return notify.Roll{}
	}
	sp, ok := rolls.In()
	if !ok {
		return notify.Roll{}
	}
	meta := pr.FileMeta(st.FilePath)
	d, rho := pr.Machine().Diameter(), rolls.Density(sp)
	used := printer.GramsAt(st.FilamentUsed, d, rho)
	var need float64
	switch {
	case meta.FilamentMM > 0:
		need = printer.GramsAt(max(0, meta.FilamentMM-st.FilamentUsed), d, rho)
	case meta.FilamentG > 0:
		need = max(0, meta.FilamentG-used)
	default:
		return notify.Roll{}
	}
	return notify.Roll{Name: sp.Number() + " " + sp.Label(), Left: max(0, sp.Left()-used), Need: need, Known: true}
}
