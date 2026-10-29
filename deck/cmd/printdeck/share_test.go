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
