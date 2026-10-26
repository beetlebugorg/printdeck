package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"printdeck/internal/camera"
	"printdeck/internal/ui"
)

// startCamera starts the camera of the deck for app: the webcam of the API and the Camera page of
// the Settings. The settings are in the file camera of the state folder; the deck writes it only
// when the Camera page changes them. Only Linux has the capture (V4L2), so on other systems the
// app has no camera.
func startCamera(ctx context.Context, stateDir string, app *ui.App) {
	if runtime.GOOS != "linux" {
		return
	}
	file := filepath.Join(stateDir, "camera")
	var s camera.Settings
	if b, err := os.ReadFile(file); err == nil {
		s = camera.ParseSettings(b)
	}
	hub := camera.NewHub(s)
	hub.OnSettings = func(s camera.Settings) {
		if err := os.WriteFile(file, s.Bytes(), 0o644); err != nil {
			log.Printf("camera: the settings were not saved: %v", err)
		}
	}
	go hub.Run(ctx)
	app.Camera = hub
}
