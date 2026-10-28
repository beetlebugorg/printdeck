package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"printdeck/internal/ui"
)

// The switch of the deck program during a print. After an update during a print, the other slot
// has the new system, and the helper mounts its root file system read-only at system.NextRoot.
// "Restart the screen now" (updater.SwitchDeck) then:
//
//  1. checks that the deck program of the new slot works with the running Klipper host: both
//     have the same interface version (package hostapi),
//  2. writes the frames of the time lapse and the state of the share link to handoffDir (RAM),
//  3. writes the marker nextFile, and ends the deck program.
//
// systemd starts /usr/bin/printdeck again. runNextDeck finds the marker and runs the deck program
// of the new slot in its place, with envNextRoot. The Klipper host of the old slot keeps the
// print. The new deck program reads the handoff files. After the print, the Pi restarts into the
// new slot, as for each update during a print.
//
// If the new deck program ends maxNextStarts times before it runs for nextStable, runNextDeck
// renames the marker to failedFile and runs the deck program of the running system.

// handoffDir is the folder of the handoff: the RuntimeDirectory of printdeck-deck.service, in
// RAM. It stays when the deck program restarts.
var handoffDir = "/run/printdeck-deck"

// envNextRoot has the root of the slot of the deck program, in the deck program of an installed
// update. It is empty in the deck program of the running system.
const envNextRoot = "PRINTDECK_NEXT_ROOT"

const (
	nextFile      = "next.json"
	failedFile    = "next.failed"
	lapseHandoff  = "timelapse.handoff"
	shareHandoff  = "share.json"
	maxNextStarts = 3
	nextStable    = time.Minute
)

// nextDeck is the marker of the switch.
type nextDeck struct {
	Exe     string `json:"exe"`     // the deck program of the new slot
	Root    string `json:"root"`    // the mount point of the root file system of the new slot
	Version string `json:"version"` // the version of the new slot
	Starts  int    `json:"starts"`  // the starts since the last nextStable
}

// deckRoot is the root of the system of this deck program: "/", or the mount point of the slot
// of an installed update.
func deckRoot() string {
	if r := os.Getenv(envNextRoot); r != "" {
		return r
	}
	return "/"
}

// runsNext reports that this is the deck program of an installed update.
func runsNext() bool { return os.Getenv(envNextRoot) != "" }

func readNext(dir string) (nextDeck, error) {
	var n nextDeck
	b, err := os.ReadFile(filepath.Join(dir, nextFile))
	if err != nil {
		return n, err
	}
	if err := json.Unmarshal(b, &n); err != nil {
		return n, err
	}
	if n.Exe == "" || n.Root == "" {
		return n, errors.New("no program in the marker")
	}
	return n, nil
}

func writeNext(dir string, n nextDeck) error {
	b, _ := json.Marshal(n)
	tmp := filepath.Join(dir, nextFile+".new")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, nextFile))
}

// failNext renames the marker, so that the deck program of the running system runs from now on.
func failNext(dir, why string) {
	log.Printf("deck: the switch to the deck program of the update ends: %s", why)
	os.Rename(filepath.Join(dir, nextFile), filepath.Join(dir, failedFile))
}

// nextFailed reports that a switch to the deck program of an update failed in this boot.
func nextFailed(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, failedFile))
	return err == nil
}

// nextToRun returns the deck program to run in place of this one, from the marker in dir, and
// counts the start in the marker. It returns "" with no marker, in the deck program of the
// update, and after maxNextStarts starts.
func nextToRun(dir string) (nextDeck, bool) {
	if runsNext() {
		return nextDeck{}, false
	}
	n, err := readNext(dir)
	if errors.Is(err, os.ErrNotExist) {
		return n, false
	}
	if err != nil {
		failNext(dir, err.Error())
		return n, false
	}
	if n.Starts >= maxNextStarts {
		failNext(dir, fmt.Sprintf("it ended %d times", n.Starts))
		return n, false
	}
	if fi, err := os.Stat(n.Exe); err != nil || !fi.Mode().IsRegular() {
		failNext(dir, "no program "+n.Exe)
		return n, false
	}
	n.Starts++
	if err := writeNext(dir, n); err != nil {
		failNext(dir, err.Error())
		return n, false
	}
	return n, true
}

// markNextStable sets the starts of the marker to 0, in the deck program of the update after it
// ran for nextStable.
func markNextStable(dir string) {
	n, err := readNext(dir)
	if err != nil || n.Starts == 0 {
		return
	}
	n.Starts = 0
	if err := writeNext(dir, n); err != nil {
		log.Printf("deck: %v", err)
	}
}

// deckHandoff returns the function that writes the state of the deck for the next deck program:
// the frames of the time lapse, and the share link. It stops the time lapse and the uploads of
// the share.
func deckHandoff(app *ui.App) func() error {
	return func() error {
		var errs []error
		if r := sysTimelapse; r != nil {
			errs = append(errs, r.SaveHandoff(filepath.Join(handoffDir, lapseHandoff)))
		}
		if l, ok := app.Share.(*shareLink); ok {
			errs = append(errs, l.saveHandoff(filepath.Join(handoffDir, shareHandoff)))
		}
		return errors.Join(errs...)
	}
}
