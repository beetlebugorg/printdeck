package main

import (
	"testing"
	"time"

	"printdeck/internal/printer"
	"printdeck/internal/ui"
)

// With no connection to Klipper, the share waits and does not end: a new deck program during a
// print starts with no Klipper. After shareWaitLimit the share ends as stopped. A finished print
// ends the share.
func TestShareStatusWaits(t *testing.T) {
	d := &ui.DemoPrinter{State_: printer.State{Err: "Printer is not ready", Starting: true}}
	l := &shareLink{pr: demoBackend{d}}
	if s := l.status(); s.State != "waiting" {
		t.Fatalf("no Klipper: %q, want waiting", s.State)
	}
	l.waiting = time.Now().Add(-shareWaitLimit - time.Second)
	if s := l.status(); s.State != "error" {
		t.Errorf("after the limit: %q, want error", s.State)
	}
	d.State_ = printer.State{Connected: true, Printing: true, File: "a.gcode", FilePath: "a.gcode"}
	if s := l.status(); s.State == "waiting" || !l.waiting.IsZero() {
		t.Errorf("printing: %q, waiting %v", s.State, l.waiting)
	}
	d.State_ = printer.State{Connected: true, Finished: "complete", FinishedFile: "a.gcode", FinishedPath: "a.gcode"}
	if s := l.status(); s.State != "done" {
		t.Errorf("finished: %q, want done", s.State)
	}
}

// A share of a print that ended goes at the start of another print: another file, or the same
// file again. The print of the share and a printer with no print keep it.
func TestShareNewPrint(t *testing.T) {
	now := time.Date(2026, 11, 3, 16, 0, 0, 0, time.UTC)
	started := now.Add(-3 * time.Hour)
	busy := func(path string, elapsed time.Duration) printer.State {
		return printer.State{Connected: true, Printing: true, File: path, FilePath: path, Elapsed: elapsed}
	}
	for _, c := range []struct {
		name string
		st   printer.State
		want bool
	}{
		{"no print", printer.State{Connected: true, Finished: "complete", FinishedPath: "a.gcode"}, false},
		{"the print of the share", busy("a.gcode", 3*time.Hour), false},
		{"another file", busy("b.gcode", time.Minute), true},
		{"the same file again", busy("a.gcode", time.Minute), true},
	} {
		if got := newPrint(c.st, "a.gcode", started, now); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
