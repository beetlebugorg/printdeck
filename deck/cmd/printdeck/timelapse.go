package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"printdeck/internal/camera"
	"printdeck/internal/printer"
	"printdeck/internal/timelapse"
	"printdeck/internal/ui"
)

// startTimelapse starts the time lapse and the first layer picture of the prints (Settings > Time
// lapse). It needs the camera of the deck and the Klipper host. The settings are in the file
// timelapse of the state folder; the deck writes it only when the page changes them. The frames of
// a print stay in memory. The deck writes one video to the folder videos at the end of a print.
func startTimelapse(ctx context.Context, stateDir string, app *ui.App, pr printerBackend) {
	hub, ok := app.Camera.(*camera.Hub)
	k, isKlippy := pr.(*printer.Klippy)
	if !ok || !isKlippy {
		return
	}
	file := filepath.Join(stateDir, "timelapse")
	s := timelapse.DefaultSettings
	if b, err := os.ReadFile(file); err == nil {
		s = timelapse.ParseSettings(b)
	}
	r := timelapse.NewRunner(s, k, hub, func() timelapse.Print {
		st := k.State()
		return timelapse.Print{Busy: st.Busy(), File: st.FilePath}
	}, videoDir(stateDir))
	r.OnSettings = func(s timelapse.Settings) {
		if err := os.WriteFile(file, s.Bytes(), 0o644); err != nil {
			log.Printf("timelapse: the settings were not saved: %v", err)
		}
	}
	r.OnFirstLayer = func(f *camera.Frame, file string) { app.ShowFirstLayer() }
	app.TimeLapse = r
	sysTimelapse = r
	go r.Run(ctx)
}

// videoDir is the folder of the time lapse videos in the state folder.
func videoDir(stateDir string) string { return filepath.Join(stateDir, "videos") }
