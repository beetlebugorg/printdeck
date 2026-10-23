package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"printdeck/internal/notify"
	"printdeck/internal/printer"
	"printdeck/internal/ui"
)

// phonesIn returns the paired phones of the Printdeck app in the state folder (push.json). The
// API and the loop of startNotify share them.
var phonesByDir struct {
	sync.Mutex
	m map[string]*notify.Phones
}

func phonesIn(stateDir string) *notify.Phones {
	phonesByDir.Lock()
	defer phonesByDir.Unlock()
	if phonesByDir.m == nil {
		phonesByDir.m = map[string]*notify.Phones{}
	}
	p := phonesByDir.m[stateDir]
	if p == nil {
		p = &notify.Phones{Path: filepath.Join(stateDir, "push.json")}
		phonesByDir.m[stateDir] = p
	}
	return p
}

// sendPhones sends a message to each paired phone. A phone that removed the app is removed. The
// log has the ID of a phone only.
func sendPhones(ctx context.Context, phones *notify.Phones, m notify.Message) {
	for _, ph := range phones.List() {
		err := notify.SendPhone(ctx, ph, m, time.Now())
		switch {
		case err == notify.ErrGone:
			phones.Remove(ph.ID)
			log.Printf("notify: phone %s removed the app; it is unpaired", ph.ID)
		case err != nil:
			log.Printf("notify: phone %s: %s: %v", ph.ID, m.Title, err)
		}
	}
}

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
	phones := phonesIn(stateDir)
	app.Phones = phones
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
			for _, m := range msgs {
				sendPhones(ctx, phones, m)
				if t == "" {
					continue
				}
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
