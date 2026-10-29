package main

import (
	"context"
	"image"
	"log"
	"os"
	"path/filepath"
	"time"

	"printdeck/internal/camera"
	"printdeck/internal/printer"
	"printdeck/internal/timelapse"
	"printdeck/internal/ui"
)

// startTimelapse starts the time lapse and the first layer picture of the prints (Settings > Time
// lapse). It needs the camera of the deck and the Klipper host. The frames come from the camera of
// the time lapse role, and the first layer picture from the camera of the picture role. The park
// of a print is at the middle of the part in Y, so a bed that moves in Y is at one place in each
// frame. With a calibration of the camera, the frames are the crop of the plate. The settings are in the file
// timelapse of the state folder; the deck writes it only when the page changes them. The frames of
// a print stay in memory. The deck writes one video to the folder videos at the end of a print,
// and the history record of the print gets its name.
func startTimelapse(ctx context.Context, stateDir, configPath string, app *ui.App, pr printerBackend) {
	hub, ok := app.Camera.(*camera.Hub)
	k, isKlippy := pr.(*printer.Klippy)
	if !ok || !isKlippy {
		return
	}
	file := filepath.Join(stateDir, "timelapse")
	model := printer.ConfigModel(configPath)
	s := timelapse.DefaultSettings
	if b, err := os.ReadFile(file); err == nil {
		s = timelapse.ParseSettings(b)
	}
	r := timelapse.NewRunner(s, k, hub.Role(camera.RoleLapse), func() timelapse.Print {
		st := k.State()
		return timelapse.Print{Busy: st.Busy(), File: st.FilePath, Known: st.Reachable}
	}, videoDir(stateDir))
	// The frames of the print from the deck program before this one (handoff.go).
	if ok, err := r.LoadHandoff(filepath.Join(handoffDir, lapseHandoff)); err != nil {
		log.Printf("timelapse: %v", err)
	} else if ok {
		log.Printf("timelapse: %d frames of the print from the deck program before this one", r.Info().Frames)
	}
	r.OnSettings = func(s timelapse.Settings) {
		if err := os.WriteFile(file, s.Bytes(), 0o644); err != nil {
			log.Printf("timelapse: the settings were not saved: %v", err)
		}
	}
	r.Picture = hub.Role(camera.RolePicture)
	r.ParkY = func(file string) (float64, bool) {
		b, ok := wholePart(k, file)
		if !ok {
			return 0, false
		}
		_, y, _ := gcodeToPlace(k)((b.Min[0]+b.Max[0])/2, (b.Min[1]+b.Max[1])/2, 0)
		return y, true
	}
	r.PartCrop = func(file string, sz image.Point) (image.Rectangle, bool) {
		lapse := hub.Role(camera.RoleLapse)
		// The calibration of the frames now: with a camera that zooms to the part, the crop is
		// most of the frame.
		cal, ok := lapse.Calibration()
		box, bedY, hasPart := partBox(k, file, model)
		if !ok || !hasPart {
			return image.Rectangle{}, false
		}
		return cal.PartCrop(box, bedY, sz, hub.Settings().Camera(lapse.ID()).Rotate)
	}
	r.OnFirstLayer = func(f *camera.Frame, file string) { app.ShowFirstLayer() }
	r.OnPark = func(ctx context.Context, done int) {
		if st := k.State(); st.Busy() {
			hub.FocusPrint(ctx, done, printHeight(k, st))
		}
	}
	r.OnVideo = func(name, file string) {
		k.SetVideo(file, name)
		app.Redraw()
	}
	app.TimeLapse = r
	sysTimelapse = r
	go r.Run(ctx)
	go followPart(ctx, hub, k, model)
	go focusPart(ctx, hub, k, r)
}

// partBox returns the box of the time lapse crop and the camera zoom of the print of the file in
// the toolhead coordinates: the whole plate of the bed, as high as the part. With no known bed,
// it is the box of the part. It also returns the toolhead Y of the bed in the frames of the time
// lapse: park_y of the config, or else the middle of the part. It is false while the G-code of
// the print is not loaded.
func partBox(k *printer.Klippy, file, model string) (camera.Box, float64, bool) {
	b, ok := wholePart(k, file)
	if !ok {
		return camera.Box{}, 0, false
	}
	place := gcodeToPlace(k)
	_, bedY, _ := place((b.Min[0]+b.Max[0])/2, (b.Min[1]+b.Max[1])/2, 0)
	if bed := printer.BedOf(k.Machine(), model); bed.Known() {
		b.Min[0], b.Min[1], b.Max[0], b.Max[1] = bed.Min[0], bed.Min[1], bed.Max[0], bed.Max[1]
	}
	box := placeBox(place, b, 0, b.Max[2])
	if m := k.Machine(); m.HasParkY {
		bedY = m.ParkY
	}
	if ax := k.State().Axis; ax[1][1] > ax[0][1] {
		bedY = max(ax[0][1], min(ax[1][1], bedY))
	}
	return box, bedY, true
}

// followEvery is the time between two checks of the print for the follow of the part.
const followEvery = time.Second

// followPart zooms the cameras with the setting "Zoom to the part" to the plate of each print
// (camera.Hub.FollowPart), from the time the deck knows the box of the part to the end of the
// print. The time lapse sets its crop at its first frame, at the end of a layer. The zoom comes
// before that frame when the deck knows the box of the part a second or more before it.
func followPart(ctx context.Context, hub *camera.Hub, k *printer.Klippy, model string) {
	tick := time.NewTicker(followEvery)
	defer tick.Stop()
	following := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		st := k.State()
		if !st.Busy() {
			if following {
				hub.EndFollow()
				following = false
			}
			continue
		}
		if box, bedY, ok := partBox(k, st.FilePath, model); ok {
			hub.FollowPart(box, bedY)
			following = true
		}
	}
}

// focusAtOnce is the longest time of the focus searches of a print with no park.
const focusAtOnce = 20 * time.Second

// focusPart runs the focus searches on the part of each print (camera.Hub.FocusPrint): after the
// first layer, and then each camera.PartFocusEvery mm of height. With a park of the time lapse,
// the searches run at the parks (Runner.OnPark), with the toolhead out of the picture. With no
// park, they run at once.
func focusPart(ctx context.Context, hub *camera.Hub, k *printer.Klippy, r *timelapse.Runner) {
	tick := time.NewTicker(followEvery)
	defer tick.Stop()
	printing := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		st := k.State()
		if !st.Busy() {
			if printing {
				hub.EndFocusPrint()
				printing = false
			}
			continue
		}
		printing = true
		hub.StartFocusPrint()
		done := st.Layer - 1
		if st.Paused || done < 1 || r.Parks(done) {
			continue
		}
		fctx, cancel := context.WithTimeout(ctx, focusAtOnce)
		hub.FocusPrint(fctx, done, printHeight(k, st))
		cancel()
	}
}

// printHeight returns the height of the print at the file position now: the top of the moves of
// the G-code up to it, or the height of the head while the G-code loads.
func printHeight(k *printer.Klippy, st printer.State) float64 {
	if part, _, ok, _ := k.View.PrintBox(st, st.FilePos); ok {
		return part.Max[2]
	}
	return st.Z
}

// wholePart returns the box of the moves of the part of the print of the file from its second
// layer on (printer.View.PartBox), in the coordinates of the G-code: the box has no purge line,
// skirt or brim. It is false while the G-code of the print is not loaded.
func wholePart(k *printer.Klippy, file string) (printer.Box, bool) {
	st := k.State()
	if !st.Busy() || st.FilePath != file {
		return printer.Box{}, false
	}
	return k.View.PartBox(st)
}

// videoDir is the folder of the time lapse videos in the state folder.
func videoDir(stateDir string) string { return filepath.Join(stateDir, "videos") }
