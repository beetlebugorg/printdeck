package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"printdeck/internal/bedcool"
	"printdeck/internal/printer"
	"printdeck/internal/ui"
)

// startBedCool starts the early cool of the bed (Maintenance > Bed cool) on a Klipper printer.
// The settings and the learned cooling curve are in the file bedcool of the state folder. Each 5
// seconds the runner looks at the print, the loaded material and the size of the part.
func startBedCool(ctx context.Context, stateDir string, app *ui.App, pr printerBackend) {
	k, ok := pr.(*printer.Klippy)
	if !ok {
		return
	}
	file := filepath.Join(stateDir, "bedcool")
	s := bedcool.DefaultSettings
	if b, err := os.ReadFile(file); err == nil {
		s = bedcool.ParseSettings(b)
	}
	r := bedcool.NewRunner(s)
	r.OnSettings = func(s bedcool.Settings) {
		if err := os.WriteFile(file, s.Bytes(), 0o644); err != nil {
			log.Printf("bedcool: the settings were not saved: %v", err)
		}
	}
	r.Send = func(line string) error {
		log.Printf("bedcool: %s", line)
		return k.Gcode(line)
	}
	app.BedCool = r
	materialFile := filepath.Join(stateDir, "material")
	go func() {
		tick := time.NewTicker(5 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-tick.C:
				st := k.State()
				p := bedcool.Print{Busy: st.Reachable && st.Busy(), Paused: st.Paused, File: st.FilePath,
					Bed: st.Bed, BedTarget: st.BedTarget, Elapsed: st.PrintDuration, TimeLeft: st.TimeLeft,
					HasTimeLeft: st.HasTimeLeft}
				if b, err := os.ReadFile(materialFile); err == nil {
					p.Material = strings.TrimSpace(string(b))
				}
				if box, ok := k.View.PartBox(st); ok {
					p.Part = [3]float64{box.Max[0] - box.Min[0], box.Max[1] - box.Min[1], box.Max[2]}
					p.PartOK = true
				}
				r.Step(p, now)
			}
		}
	}()
}
