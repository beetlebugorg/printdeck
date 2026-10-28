package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"printdeck/internal/hostapi"
	"printdeck/internal/klippy"
	"printdeck/internal/printer"
	"printdeck/internal/remote"
	"printdeck/internal/system"
	"printdeck/internal/ui"
)

// The simulated system update of the desktop app (printdeck app -sim -fake-update): the Software
// update page and the system page /system with an update system that writes no disk and does not
// restart. It is for tests of the pages. PRINTDECK_SIM_NEXT_API sets the interface version of the
// simulated deck program of the update (default: hostapi.Version).

// simUpdateSystem is the updateSystem of the simulator.
type simUpdateSystem struct {
	dir    string // the folder of the app
	socket string // the API socket of the Klipper host

	mu        sync.Mutex
	staged    system.Staged
	hasStaged bool
}

func (s *simUpdateSystem) Stage(progress system.Progress) error {
	defer os.Remove(system.UpdateBundle)
	if _, err := os.Stat(system.UpdateBundle); err != nil {
		return errors.New("no bundle")
	}
	progress(system.StepUnpack, 0, 0)
	time.Sleep(300 * time.Millisecond)
	progress(system.StepCheck, 0, 0)
	time.Sleep(300 * time.Millisecond)
	for i := int64(0); i <= 20; i++ {
		progress(system.StepWrite, i*5, 100)
		time.Sleep(100 * time.Millisecond)
	}
	s.mu.Lock()
	s.staged, s.hasStaged = system.Staged{Slot: "b", Version: "v9.9.9-sim", Written: time.Now()}, true
	s.mu.Unlock()
	return nil
}

func (s *simUpdateSystem) Staged() (system.Staged, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.staged, s.hasStaged
}

func (s *simUpdateSystem) Reboot() error {
	log.Printf("sim: the Printdeck restarts into the update here; the simulator keeps running")
	return nil
}

func (s *simUpdateSystem) HostAPI(ctx context.Context) (int, error) {
	return klippy.HostAPI(ctx, s.socket)
}

func (s *simUpdateSystem) NextAPI(context.Context) (int, error) {
	if v := os.Getenv("PRINTDECK_SIM_NEXT_API"); v != "" {
		return strconv.Atoi(v)
	}
	return hostapi.Version, nil
}

func (s *simUpdateSystem) NextExe() string {
	exe, _ := os.Executable()
	return exe
}

func (s *simUpdateSystem) NextRoot() string { return s.dir }

// startSimUpdate adds the simulated system update to the app, and the system page to mux.
func startSimUpdate(ctx context.Context, dir, socket string, k *printer.Klippy, app *ui.App, mux *http.ServeMux, klippyLog string) {
	system.UpdateBundle = filepath.Join(dir, "update", "printdeck-update.tar.xz")
	up := newUpdater(dir, &simUpdateSystem{dir: dir, socket: socket}, k, app.Redraw)
	up.lastInput, up.handoff = app.LastInput, deckHandoff(app)
	up.saving = func() bool { r := sysTimelapse; return r != nil && r.Info().Saving }
	up.exit = func() { log.Printf("sim: the deck program ends here for the switch; the simulator keeps running") }
	app.Updater = up
	go up.watch(ctx)
	// The system page is open to a browser on this computer, as the pages of the rolls.
	open := remote.New()
	open.NoLogin = true
	registerSystemPage(mux, open, k, up, app.Metrics, klippyLog, "")
}
