package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"printdeck/internal/hostapi"
	"printdeck/internal/klippy"
	"printdeck/internal/printer"
	"printdeck/internal/system"
	"printdeck/internal/ui"
)

// updater checks the GitHub releases of Printdeck one minute after the start and then once a day,
// and installs a release when the user asks. The system helper checks the signature of the update
// file before it writes a slot.
//
// An install writes the other slot (the helper action "stage"). With no print, the deck then
// restarts into it at once. During a print, the update waits: the deck restarts into it when the
// printer is idle after the print (restartDue). Until then, SwitchDeck can run the deck program of
// the update (handoff.go).
type updater struct {
	stateFile string // the channel: "beta", or "stable"
	redraw    func()
	sys       updateSystem
	state     func() printer.State
	machine   func() printer.Machine
	// lastInput is the time of the last touch of a screen, or nil.
	lastInput func() time.Time
	// saving reports that the time lapse writes the video of a print, or is nil.
	saving func() bool
	// handoff writes the state of the deck for the next deck program, and stops the parts that
	// it saves (handoff.go). exit ends the deck program: systemd starts it again.
	handoff    func() error
	exit       func()
	handoffDir string

	mu  sync.Mutex
	st  ui.UpdateStatus
	rel system.Release
	// The interface version of the deck program of the staged update of the time nextFor.
	nextAPI    int
	nextErr    error
	nextFor    time.Time
	nextAt     time.Time // the time of the last check
	activeAt   time.Time // the last time that printerActive was true
	restarting bool
	restartAt  time.Time
	started    time.Time
}

// updateSystem is the system side of an update: the helper, and the files of the staged update.
// Tests and the simulator replace it.
type updateSystem interface {
	// Stage writes system.UpdateBundle to the other slot, with no restart.
	Stage(progress system.Progress) error
	// Staged returns the update that waits for the restart.
	Staged() (system.Staged, bool)
	// Reboot restarts into the staged update.
	Reboot() error
	// HostAPI returns the interface version of the running Klipper host.
	HostAPI(ctx context.Context) (int, error)
	// NextAPI returns the interface version of the deck program of the staged update.
	NextAPI(ctx context.Context) (int, error)
	// NextExe is the deck program of the staged update, and NextRoot the root of its slot.
	NextExe() string
	NextRoot() string
}

// helperSystem is the updateSystem of the Printdeck OS.
type helperSystem struct{ socket string }

func (helperSystem) Stage(p system.Progress) error {
	return system.AskHelperProgress(system.HelperRequest{Action: "stage"}, p)
}
func (helperSystem) Staged() (system.Staged, bool) { return system.ReadStaged() }
func (helperSystem) Reboot() error {
	return system.AskHelper(system.HelperRequest{Action: "reboot-staged"})
}
func (h helperSystem) HostAPI(ctx context.Context) (int, error) { return klippy.HostAPI(ctx, h.socket) }

// NextAPI runs the deck program of the staged update with "host-api".
func (h helperSystem) NextAPI(ctx context.Context) (int, error) {
	out, err := exec.CommandContext(ctx, h.NextExe(), "host-api").Output()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}
func (helperSystem) NextExe() string  { return filepath.Join(system.NextRoot, "usr/bin/printdeck") }
func (helperSystem) NextRoot() string { return system.NextRoot }

func newUpdater(stateDir string, sys updateSystem, pr ui.Printer, redraw func()) *updater {
	u := &updater{stateFile: filepath.Join(stateDir, "update-channel"), redraw: redraw, sys: sys,
		state: pr.State, machine: pr.Machine, handoffDir: handoffDir, started: time.Now()}
	u.activeAt = u.started
	u.st.Current = osVersion()
	if b, err := os.ReadFile(u.stateFile); err == nil {
		u.st.Beta = strings.TrimSpace(string(b)) == "beta"
	}
	return u
}

func (u *updater) Status() ui.UpdateStatus {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.st
}

func (u *updater) set(f func(st *ui.UpdateStatus)) {
	u.mu.Lock()
	f(&u.st)
	u.mu.Unlock()
	u.redraw()
}

// run checks for releases until ctx ends.
func (u *updater) run(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(time.Minute):
	}
	for {
		u.check(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(24 * time.Hour):
		}
	}
}

func (u *updater) Check() { go u.check(context.Background()) }

func (u *updater) check(ctx context.Context) {
	u.mu.Lock()
	if u.st.Checking || u.st.Busy != "" {
		u.mu.Unlock()
		return
	}
	u.st.Checking = true
	channel := "stable"
	if u.st.Beta {
		channel = "beta"
	}
	u.mu.Unlock()
	u.redraw()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	rel, ok, err := system.LatestRelease(ctx, channel)
	u.set(func(st *ui.UpdateStatus) {
		st.Checking, st.Checked, st.Err = false, true, ""
		if err != nil {
			st.Err = "The check failed: " + err.Error()
			return
		}
		st.Latest, st.Notes, st.Newer = "", "", false
		if ok {
			st.Latest, st.Notes, st.Newer = rel.Tag, rel.Notes, system.Newer(rel.Tag, st.Current)
			u.rel = rel
		}
	})
	if ok && system.Newer(rel.Tag, u.Status().Current) {
		log.Printf("update: %s is available", rel.Tag)
	}
}

func (u *updater) SetChannel(beta bool) {
	ch := "stable"
	if beta {
		ch = "beta"
	}
	os.WriteFile(u.stateFile, []byte(ch+"\n"), 0o644)
	u.set(func(st *ui.UpdateStatus) { st.Beta = beta })
	u.Check()
}

// Install downloads the latest release and installs it (stage).
func (u *updater) Install() {
	u.mu.Lock()
	rel, newer := u.rel, u.st.Newer
	if u.st.Busy != "" || !newer || u.st.HasStaged {
		u.mu.Unlock()
		return
	}
	u.st.Busy, u.st.Err, u.st.Done, u.st.Total = "Downloading", "", 0, rel.Size
	u.mu.Unlock()
	u.redraw()
	go func() {
		fail := func(msg string) {
			os.Remove(system.UpdateBundle)
			u.set(func(st *ui.UpdateStatus) { st.Busy, st.Err = "", msg })
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		last := time.Time{}
		err := system.Download(ctx, rel, func(done, total int64) {
			if time.Since(last) > 250*time.Millisecond || done == total {
				last = time.Now()
				u.set(func(st *ui.UpdateStatus) { st.Done, st.Total = done, total })
			}
		})
		if err != nil {
			fail("The download failed: " + err.Error())
			return
		}
		log.Printf("update: installing %s", rel.Tag)
		if _, err := u.stage(nil); err != nil {
			fail("The update failed: " + err.Error())
		}
	}()
}

// stage writes system.UpdateBundle to the other slot with the helper. With no print, the deck
// then restarts into it. During a print, the update waits for the end of the print, and stage
// reports that it waits. progress, or nil, gets the steps too. The status shows the steps.
func (u *updater) stage(progress system.Progress) (waits bool, err error) {
	u.set(func(st *ui.UpdateStatus) { st.Busy, st.Err, st.Done, st.Total = system.StepUnpack, "", 0, 0 })
	// The helper checks the signature and writes the other slot at a low priority. It tells its
	// steps.
	err = u.sys.Stage(func(step string, done, total int64) {
		u.set(func(st *ui.UpdateStatus) { st.Busy, st.Done, st.Total = step, done, total })
		if progress != nil {
			progress(step, done, total)
		}
	})
	if err != nil {
		u.set(func(st *ui.UpdateStatus) { st.Busy, st.Done, st.Total = "", 0, 0 })
		u.refresh(context.Background())
		return false, err
	}
	u.set(func(st *ui.UpdateStatus) { st.Busy, st.Done, st.Total = "", 0, 0 })
	u.refresh(context.Background())
	if u.state().Busy() {
		log.Printf("update: the update is in the other slot; the deck restarts into it after the print")
		return true, nil
	}
	if err := u.reboot("the update is installed", time.Now()); err != nil {
		// The helper refused, for example for a print that started now. The update waits.
		log.Printf("update: %v", err)
		return true, nil
	}
	return false, nil
}

// reboot asks the helper to restart into the staged update, at the time now.
func (u *updater) reboot(why string, now time.Time) error {
	u.mu.Lock()
	if u.restarting {
		u.mu.Unlock()
		return nil
	}
	u.restarting, u.restartAt = true, now
	u.mu.Unlock()
	u.set(func(st *ui.UpdateStatus) { st.Busy, st.Done, st.Total, st.Err = "Restarting", 0, 0, "" })
	log.Printf("update: restarting into the update: %s", why)
	if err := u.sys.Reboot(); err != nil {
		u.mu.Lock()
		u.restarting = false
		u.mu.Unlock()
		u.set(func(st *ui.UpdateStatus) { st.Busy = "" })
		return err
	}
	return nil
}

// RestartNow restarts into the staged update, when no print runs.
func (u *updater) RestartNow() {
	go func() {
		if !u.Status().HasStaged {
			return
		}
		if u.state().Busy() {
			u.set(func(st *ui.UpdateStatus) { st.Err = "A print runs. The Printdeck restarts after the print." })
			return
		}
		if err := u.reboot("Restart now", time.Now()); err != nil {
			u.set(func(st *ui.UpdateStatus) { st.Err = "The restart failed: " + err.Error() })
		}
	}()
}

// SwitchDeck runs the deck program of the staged update in place of this one (handoff.go).
func (u *updater) SwitchDeck() {
	go func() {
		if err := u.switchDeck(); err != nil {
			log.Printf("update: the switch of the deck program: %v", err)
			u.set(func(st *ui.UpdateStatus) { st.Busy, st.Err = "", err.Error() })
		}
	}()
}

// switchReady checks that SwitchDeck can run now, and returns the staged update and the
// interface version of its deck program.
func (u *updater) switchReady() error {
	_, _, err := u.switchCheckNow()
	return err
}

func (u *updater) switchCheckNow() (system.Staged, int, error) {
	s := u.Status()
	switch {
	case !s.HasStaged:
		return system.Staged{}, 0, errors.New("No update waits for the restart.")
	case s.Busy != "":
		return system.Staged{}, 0, errors.New(s.Busy + " now.")
	case nextFailed(u.handoffDir):
		return system.Staged{}, 0, errors.New("The new screen program did not start before. The Printdeck restarts into the update after the print.")
	}
	staged, ok := u.sys.Staged()
	if !ok {
		return staged, 0, errors.New("No update waits for the restart.")
	}
	if stagedRuns(staged) {
		return staged, 0, errors.New("The screen runs the program of the update already.")
	}
	if fi, err := os.Stat(u.sys.NextExe()); err != nil || !fi.Mode().IsRegular() {
		return staged, 0, errors.New("The program of the update is not there. The Printdeck restarts into the update after the print.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	host, herr := u.sys.HostAPI(ctx)
	next, nerr := u.sys.NextAPI(ctx)
	if ok, note := switchCheck(host, next, herr, nerr); !ok {
		u.set(func(st *ui.UpdateStatus) { st.CanSwitch, st.SwitchNote = false, note })
		return staged, 0, errors.New(note)
	}
	if fi, err := os.Stat(u.handoffDir); err != nil || !fi.IsDir() {
		return staged, 0, fmt.Errorf("No folder %s for the switch.", u.handoffDir)
	}
	return staged, next, nil
}

func (u *updater) switchDeck() error {
	staged, next, err := u.switchCheckNow()
	if err != nil {
		return err
	}
	u.set(func(st *ui.UpdateStatus) { st.Busy, st.Done, st.Total, st.Err = "Restarting the screen", 0, 0, "" })
	log.Printf("update: switching to the deck program of %s (interface %d)", staged.Version, next)
	// From here on, the deck program ends. With no marker, the deck program of the running
	// system starts again, and reads the handoff.
	if u.handoff != nil {
		if err := u.handoff(); err != nil {
			log.Printf("update: the handoff: %v", err)
		}
	}
	// The copy in RAM keeps the slot free for a new install. With no copy, the program runs
	// from the slot.
	exe, root := u.sys.NextExe(), u.sys.NextRoot()
	if e, r, err := nextCopy(u.handoffDir, exe, root, staged.Written); err != nil {
		log.Printf("update: the copy of the deck program of the update: %v", err)
	} else {
		exe, root = e, r
	}
	if err := writeNext(u.handoffDir, nextDeck{Exe: exe, Root: root, Version: staged.Version, Written: staged.Written}); err != nil {
		log.Printf("update: the marker of the switch: %v", err)
	}
	u.exit()
	return nil
}

// switchCheck reports that the deck program of the staged update can run with the running Klipper
// host and helper: both report the same interface version. The text says why it cannot.
func switchCheck(host, next int, hostErr, nextErr error) (bool, string) {
	switch {
	case nextErr != nil || next <= 0:
		return false, "The new screen program has no interface version, so the screen changes at the restart after the print."
	case hostErr != nil || host <= 0:
		return false, "The Klipper host does not report its interface version, so the screen changes at the restart after the print."
	case host != next:
		return false, "The new screen program needs the new Klipper host, so the screen changes at the restart after the print."
	}
	return true, ""
}

// The waits before the restart into a staged update after a print.
const (
	// restartQuiet is the time with no print, no heating, no action and no touch before the
	// restart. In this time, the deck shows the end of the print, sends its messages, and saves
	// the time lapse.
	restartQuiet = 2 * time.Minute
	// restartCoolMax is the longest wait for the bed to cool to printer.ReleaseBed, after
	// restartQuiet.
	restartCoolMax = 30 * time.Minute
	// restartGiveUp is the time after a restart request with no restart: then the deck shows the
	// staged update again, and tries again later.
	restartGiveUp = 2 * time.Minute
)

// printerActive reports that the printer works or the deck cannot tell: a print or a pause, a
// heater with a target, a maintenance action, a manual probe, a copy from the SD card of the
// printer, the save of a time lapse video, or a Klipper host that does not answer.
func printerActive(st printer.State, saving bool) bool {
	return !st.Reachable || st.Busy() || st.ToolTarget > 0 || st.BedTarget > 0 || st.Action != "" ||
		st.ManualProbe || st.SDCopy.Path != "" || saving
}

// restartDue reports that the deck restarts into a staged update now. quiet is the time since the
// printer was last active (printerActive) or a person touched a screen.
func restartDue(st printer.State, saving bool, quiet time.Duration) bool {
	if printerActive(st, saving) || quiet < restartQuiet {
		return false
	}
	return st.Bed <= printer.ReleaseBed || quiet >= restartCoolMax
}

// watch follows the staged update until ctx ends: it reads its state, and restarts into it when
// restartDue.
func (u *updater) watch(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		u.step(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// step is one turn of watch at the time now.
func (u *updater) step(ctx context.Context, now time.Time) {
	u.refresh(ctx)
	st := u.state()
	saving := u.saving != nil && u.saving()
	if runsNext() {
		u.checkHost(st)
		if now.Sub(u.started) >= nextStable {
			markNextStable(u.handoffDir)
		}
	}
	u.mu.Lock()
	if u.restarting && now.Sub(u.restartAt) > restartGiveUp {
		u.restarting, u.activeAt = false, now
		if u.st.Busy == "Restarting" {
			u.st.Busy = ""
		}
	}
	if printerActive(st, saving) {
		u.activeAt = now
	}
	last := u.activeAt
	if u.lastInput != nil {
		if t := u.lastInput(); t.After(last) {
			last = t
		}
	}
	due := u.st.HasStaged && u.st.Busy == "" && !u.restarting && restartDue(st, saving, now.Sub(last))
	u.mu.Unlock()
	if due {
		if err := u.reboot("the printer is idle after the print", now); err != nil {
			log.Printf("update: %v", err)
			u.mu.Lock()
			u.activeAt = now // try again after restartQuiet
			u.mu.Unlock()
		}
	}
}

// checkHost ends the deck program of an update that does not work with the running Klipper host:
// the deck program of the running system starts in its place.
func (u *updater) checkHost(st printer.State) {
	if !st.Reachable {
		return
	}
	if v := u.machine().HostAPI; v != hostapi.Version {
		failNext(u.handoffDir, fmt.Sprintf("the Klipper host has the interface version %d, this program %d", v, hostapi.Version))
		if u.handoff != nil {
			u.handoff()
		}
		u.exit()
	}
}

// refresh reads the staged update, and checks that its deck program can run now. It redraws only
// after a change.
func (u *updater) refresh(ctx context.Context) {
	staged, ok := u.sys.Staged()
	u.mu.Lock()
	// The check runs once for each staged update, and again each minute after a failure.
	needNext := ok && !stagedRuns(staged) && (!staged.Written.Equal(u.nextFor) || u.nextErr != nil && time.Since(u.nextAt) > time.Minute)
	u.mu.Unlock()
	var next int
	var nerr error
	if needNext {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		next, nerr = u.sys.NextAPI(cctx)
		cancel()
		if nerr != nil {
			log.Printf("update: the interface version of the new deck program: %v", nerr)
		}
	}
	host := u.machine().HostAPI
	failed := nextFailed(u.handoffDir)
	u.mu.Lock()
	old := u.st
	st := &u.st
	st.HasStaged, st.Staged = ok, staged.Version
	st.System = ""
	if runsNext() {
		st.System = systemVersion()
	}
	if needNext {
		u.nextFor, u.nextAPI, u.nextErr, u.nextAt = staged.Written, next, nerr, time.Now()
	}
	st.SwitchFailed = failed
	st.CanSwitch, st.SwitchNote = false, ""
	if ok && !stagedRuns(staged) {
		var herr error
		if host == 0 {
			herr = errors.New("no interface version")
		}
		st.CanSwitch, st.SwitchNote = switchCheck(host, u.nextAPI, herr, u.nextErr)
		if failed {
			st.CanSwitch = false
		}
	}
	changed := *st != old
	u.mu.Unlock()
	if changed {
		u.redraw()
	}
}
