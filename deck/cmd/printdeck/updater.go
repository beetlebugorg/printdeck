package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"printdeck/internal/system"
	"printdeck/internal/ui"
)

// updater checks the GitHub releases of Printdeck one minute after the start and then once a day,
// and installs a release when the user asks. The system helper checks the signature of the update
// file before it writes a slot.
type updater struct {
	stateFile string // the channel: "beta", or "stable"
	busy      func() bool
	redraw    func()

	mu  sync.Mutex
	st  ui.UpdateStatus
	rel system.Release
}

func newUpdater(stateDir string, busy func() bool, redraw func()) *updater {
	u := &updater{stateFile: filepath.Join(stateDir, "update-channel"), busy: busy, redraw: redraw}
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

func (u *updater) Install() {
	u.mu.Lock()
	rel, newer := u.rel, u.st.Newer
	if u.st.Busy != "" || !newer {
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
		if u.busy() {
			fail("A print runs. Install the update after the print.")
			return
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
		u.set(func(st *ui.UpdateStatus) { st.Busy = "Installing" })
		log.Printf("update: installing %s", rel.Tag)
		// The helper checks the signature, writes the other slot, and restarts.
		if err := system.AskHelper(system.HelperRequest{Action: "update"}); err != nil {
			fail("The update failed: " + err.Error())
		}
	}()
}
