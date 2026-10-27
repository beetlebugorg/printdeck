package main

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"printdeck/internal/camera"
	"printdeck/internal/printer"
	"printdeck/internal/ui"
)

// startCamera starts the cameras of the deck for app: the webcam of the API and the Camera page of
// the Settings. The settings are in the file camera of the state folder; the deck writes it when
// the Camera page changes them, and once to change the file of an older deck. The calibrations of
// the views are in the file camera-calibration.json; the deck writes it when the user calibrates a
// camera or clears a calibration. Only Linux has the capture (V4L2), so on other systems the app
// has no camera, except the test cameras (camera.TestCameraEnv).
func startCamera(ctx context.Context, stateDir string, app *ui.App, pr printerBackend) {
	if !cameraOn(runtime.GOOS) {
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
	calFile := filepath.Join(stateDir, "camera-calibration.json")
	if b, err := os.ReadFile(calFile); err == nil {
		cs, err := camera.ParseCalibrations(b)
		if err != nil {
			log.Printf("camera: %v", err)
		}
		hub.LoadCalibrations(cs)
	}
	hub.OnCalibration = func(cs []*camera.Calibration) {
		if len(cs) == 0 {
			if err := os.Remove(calFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
				log.Printf("camera: the calibration was not removed: %v", err)
			}
			return
		}
		if err := os.WriteFile(calFile, camera.CalibrationsBytes(cs), 0o644); err != nil {
			log.Printf("camera: the calibration was not saved: %v", err)
		}
	}
	if k, ok := pr.(*printer.Klippy); ok {
		hub.SetScene(&printScene{k: k})
	}
	go hub.Run(ctx)
	app.Camera = hub
}

// cameraOn reports that the app has a camera on the system goos: Linux, or any system with the
// test camera.
func cameraOn(goos string) bool { return goos == "linux" || os.Getenv(camera.TestCameraEnv) != "" }

// printScene gives the places of the printer to the views of the camera (camera.Scene): the live
// place of the nozzle, and the boxes of the print from its G-code. It reads them again after a
// short time, so a stream of 10 frames a second asks Klipper at most 10 times a second.
type printScene struct {
	k *printer.Klippy

	mu       sync.Mutex
	nozzleAt time.Time
	nozzle   [3]float64
	nozzleOK bool

	boxAt           time.Time
	part, first     camera.Box
	partOK, firstOK bool
	lastPos         int64 // the file position of the print at the last read while it was busy
}

// The times that printScene keeps a read.
const (
	nozzleAge = 80 * time.Millisecond
	boxAge    = time.Second
)

// sceneMargin is the space around the moves of the print in the boxes of the views, in mm.
const sceneMargin = 5.0

// Nozzle returns the live place of the nozzle (motion_report), when the printer is homed.
func (s *printScene) Nozzle() ([3]float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.nozzleAt) < nozzleAge {
		return s.nozzle, s.nozzleOK
	}
	s.nozzleAt, s.nozzleOK = time.Now(), false
	q, err := s.k.Query(map[string][]string{"motion_report": {"live_position"}, "toolhead": {"homed_axes"}})
	if err != nil {
		return s.nozzle, false
	}
	if h, _ := q["toolhead"]["homed_axes"].(string); h != "xyz" {
		return s.nozzle, false
	}
	a, _ := q["motion_report"]["live_position"].([]any)
	if len(a) < 3 {
		return s.nozzle, false
	}
	for i := range 3 {
		v, ok := a[i].(float64)
		if !ok {
			return s.nozzle, false
		}
		s.nozzle[i] = v
	}
	s.nozzleOK = true
	return s.nozzle, true
}

// Part returns the box of the part: the extents of the extruding moves of the print with a
// margin, from the bed to the height of the print now (or the last height of the last print).
func (s *printScene) Part() (camera.Box, bool) {
	s.readBoxes()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.part, s.partOK
}

// FirstLayer returns the box of the first layer: the extents of its moves with a margin, from the
// bed to the top of the layer.
func (s *printScene) FirstLayer() (camera.Box, bool) {
	s.readBoxes()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.first, s.firstOK
}

// readBoxes reads the boxes of the print again when they are older than boxAge.
func (s *printScene) readBoxes() {
	s.mu.Lock()
	if time.Since(s.boxAt) < boxAge {
		s.mu.Unlock()
		return
	}
	s.boxAt = time.Now()
	st := s.k.State()
	if st.Busy() {
		s.lastPos = st.FilePos
	}
	upto := int64(math.MaxInt64)
	if s.lastPos > 0 {
		upto = s.lastPos
	}
	s.mu.Unlock()

	part, first, partOK, firstOK := s.k.View.PrintBox(st, upto)
	var pb, fb camera.Box
	if partOK || firstOK {
		place := gcodeToPlace(s.k)
		pb = placeBox(place, part, 0, part.Max[2]+sceneMargin)
		fb = placeBox(place, first, 0, first.Max[2])
	}
	s.mu.Lock()
	s.part, s.first, s.partOK, s.firstOK = pb, fb, partOK, firstOK
	s.mu.Unlock()
}

// placeBox is the box b of the G-code with the margin in X and Y, from the height z0 to z1, in
// the places of the printer: the box of the 8 moved corners.
func placeBox(place func(x, y, z float64) (float64, float64, float64), b printer.Box, z0, z1 float64) camera.Box {
	var out camera.Box
	for i := range 8 {
		x, y, z := b.Min[0]-sceneMargin, b.Min[1]-sceneMargin, z0
		if i&1 != 0 {
			x = b.Max[0] + sceneMargin
		}
		if i&2 != 0 {
			y = b.Max[1] + sceneMargin
		}
		if i&4 != 0 {
			z = z1
		}
		px, py, pz := place(x, y, z)
		p := [3]float64{px, py, pz}
		for j := range 3 {
			if i == 0 || p[j] < out.Min[j] {
				out.Min[j] = p[j]
			}
			if i == 0 || p[j] > out.Max[j] {
				out.Max[j] = p[j]
			}
		}
	}
	return out
}
