package ui

import (
	"testing"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// A resin printer has no heaters, no filament and no toolhead: its screens have no keys for them,
// and its job screen has no Pause and no Cancel. A FDM printer keeps them.
func TestResinScreens(t *testing.T) {
	for _, wide := range []bool{false, true} {
		a, pr, cv := newPrintApp(idle)
		a.Wide = wide
		cv = gfx.New(a.Screen().W, a.Screen().H)
		has := func(label string) bool {
			_, ok := a.KeyRect(label)
			return ok
		}
		a.Draw(cv)
		fdm := "Cool down"
		if wide {
			fdm = "Toolhead"
		}
		if !has(fdm) || !has("Nozzle") {
			t.Fatalf("wide %v: a FDM printer with no %q or no Nozzle", wide, fdm)
		}
		pr.Machine_ = printer.Machine{Process: printer.ProcessResin, Board: "CBD V1.4.1"}
		a.Draw(cv)
		for _, gone := range []string{"Cool down", "Nozzle", "Bed"} {
			if has(gone) {
				t.Errorf("wide %v: a resin printer has %q", wide, gone)
			}
		}
		if !has("Plate") {
			t.Errorf("wide %v: no height of the plate", wide)
		}
		if has("Home all") || (wide && !has("Home Z")) {
			t.Errorf("wide %v: the home keys of a resin printer", wide)
		}
		for _, gone := range []string{"Filament", "Toolhead"} {
			if has(gone) {
				t.Errorf("wide %v: a resin printer has the page key %q", wide, gone)
			}
		}
		if !has("Build plate") {
			t.Errorf("wide %v: no Build plate key", wide)
		}
		if wide {
			if !has("Files") || !has("Machine") || !has("Build plate") {
				t.Error("no Files, Build plate or Machine key")
			}
		}

		// The Build plate page: Z only.
		if wide {
			a.open(pageMove)
			a.Draw(cv)
			for _, k := range []string{"Home Z", "Plate up", "Plate down"} {
				if !has(k) {
					t.Errorf("the Build plate page has no %q", k)
				}
			}
			if has("Home all") || has("Joystick") {
				t.Error("the Build plate page has X and Y")
			}
			a.back()
		}

		// A print: the window of the print, with Stop, Menu and Pause, and no Tune.
		pr.State_ = printer.State{Reachable: true, Connected: true, Printing: true, Text: "Printing", FilePath: "resin-1",
			Completion: 40, Z: 12.5, HasZ: true}
		a.Draw(cv)
		for _, gone := range []string{"Tune", "Cancel"} {
			if has(gone) {
				t.Errorf("wide %v: a resin print has %q", wide, gone)
			}
		}
		if !has("Stop") || !has("Pause") || !has("Menu") {
			t.Errorf("wide %v: a resin print with no Stop, Pause or Menu", wide)
		}
		if !has("Print") {
			t.Errorf("wide %v: no window of the print", wide)
		}

		// A pause: Resume and Stop, and none of the keys of a FDM pause.
		pr.State_.Paused = true
		a.Draw(cv)
		for _, gone := range []string{"Change filament", "Move head", "Tune"} {
			if has(gone) {
				t.Errorf("wide %v: a paused resin print has %q", wide, gone)
			}
		}
		if !has("Resume") || !has("Stop") {
			t.Errorf("wide %v: a paused resin print with no Resume or Stop", wide)
		}

		// The Machine page: no Macros, no Console, no Maintenance.
		pr.State_ = idle
		a.open(pageMachine)
		a.Draw(cv)
		for _, gone := range []string{"Macros", "Console", "Maintenance", "Restart…"} {
			if has(gone) {
				t.Errorf("wide %v: the Machine page of a resin printer has %q", wide, gone)
			}
		}
	}
}

// A resin printer that does not answer has its own screen: no Klipper.
func TestResinOffline(t *testing.T) {
	a, pr, _ := newPrintApp(printer.State{Text: "Offline", Err: "No answer from the printer at 192.168.76.58:3000"})
	cv := gfx.New(a.Screen().W, a.Screen().H)
	pr.Machine_ = printer.Machine{Process: printer.ProcessResin}
	a.Setup = &printerHost{DemoSetup: &DemoSetup{}}
	a.Draw(cv)
	if _, ok := a.KeyRect("Printer"); !ok {
		t.Error("no Printer key")
	}
	for _, k := range []string{"No Klipper", "Klipper error", "Simulated printer"} {
		if _, ok := a.KeyRect(k); ok {
			t.Errorf("the screen has %q", k)
		}
	}
}

// The status panel shows an upload to the printer while it runs, and its end for a time.
func TestUploadPanel(t *testing.T) {
	now := time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC)
	u := printer.Upload{Name: "plate.cbddlp", Done: 20e6, Size: 80e6, Started: now.Add(-100 * time.Second), Waiting: 1}
	p, ok := uploadPanel(u, now)
	if !ok || p.head != "Sending to the printer · 25% · tap to stop" || p.side != "5 min left" || p.detail != "20.0 of 80.0 MB · 0.20 MB/s · 1 waiting" {
		t.Errorf("running: %+v", p)
	}
	u.Done, u.Result, u.Ended, u.Waiting = u.Size, "sent", now, 0
	if p, ok := uploadPanel(u, now.Add(time.Minute)); !ok || p.head != "File on the printer" {
		t.Errorf("sent: %+v", p)
	}
	if _, ok := uploadPanel(u, now.Add(uploadShown+time.Second)); ok {
		t.Error("an old upload")
	}
	u.Result, u.Err = "failed", "chitu: M29: Error"
	if p, ok := uploadPanel(u, now); !ok || p.head != "Upload failed" || p.detail != u.Err {
		t.Errorf("failed: %+v", p)
	}
}

// The status panel shows an upload from ChiTuBox while its packets come in.
func TestReceivePanel(t *testing.T) {
	now := time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC)
	r := printer.Receive{Name: "testprint.cbddlp", Bytes: 12e6, Started: now.Add(-15 * time.Second), Updated: now}
	if p, ok := receivePanel(r, now); !ok || p.head != "Receiving from ChiTuBox" || p.detail != "12.0 MB · 0.80 MB/s" {
		t.Errorf("receiving: %+v", p)
	}
	if _, ok := receivePanel(r, now.Add(20*time.Second)); ok {
		t.Error("an upload with no packet for 20 s")
	}
	r.Done = true
	if _, ok := receivePanel(r, now); ok {
		t.Error("a done upload")
	}
}

// An upload shows in the panel after a print that ended: the upload is newer.
func TestUploadPanelAfterPrint(t *testing.T) {
	a, _, _ := newPrintApp(idle)
	st := printer.State{Reachable: true, Finished: "cancelled", Upload: printer.Upload{Name: "plate.cbddlp", Size: 100, Done: 10, Started: a.Now()}}
	if p := a.panelLines(st); p.head != "Sending to the printer · 10% · tap to stop" {
		t.Errorf("panel %+v", p)
	}
}
