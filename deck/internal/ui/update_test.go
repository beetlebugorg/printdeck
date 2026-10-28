package ui

import (
	"strings"
	"testing"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// countUpdater is an Updater that counts the calls.
type countUpdater struct {
	DemoUpdater
	installs, restarts, switches int
}

func (c *countUpdater) Install()    { c.installs++ }
func (c *countUpdater) RestartNow() { c.restarts++ }
func (c *countUpdater) SwitchDeck() { c.switches++ }

func updateApp(st printer.State, u UpdateStatus, wide bool) (*App, *countUpdater, *gfx.Canvas) {
	a := New()
	a.Printer = &DemoPrinter{View: -1, State_: st}
	a.Wide = wide
	up := &countUpdater{DemoUpdater: DemoUpdater{S: u}}
	a.Updater = up
	a.pages = []page{pageMachine, pageUpdate}
	return a, up, gfx.New(a.Screen().W, a.Screen().H)
}

var (
	idlePrinter  = printer.State{Reachable: true, Connected: true, Text: "Operational"}
	printingNow  = printer.State{Reachable: true, Connected: true, Text: "Printing", Printing: true, File: "clip.gcode"}
	stagedUpdate = UpdateStatus{Current: "v0.3.0", Latest: "v0.4.0", Checked: true, Newer: true, HasStaged: true,
		Staged: "v0.4.0", CanSwitch: true}
)

func TestUpdateLine(t *testing.T) {
	for _, c := range []struct {
		u        UpdateStatus
		printing bool
		want     string
	}{
		{stagedUpdate, true, "Ready · restarts after print"},
		{stagedUpdate, false, "Ready · restarts soon"},
		{UpdateStatus{Busy: "Writing the system"}, true, "Writing the system…"},
		{UpdateStatus{Checked: true}, false, "Up to date"},
	} {
		if got, _ := updateLine(c.u, c.printing); got != c.want {
			t.Errorf("updateLine(%+v, %v) = %q, want %q", c.u, c.printing, got, c.want)
		}
	}
}

// During a print, Install asks with the text of the wait, and installs.
func TestUpdateInstallDuringPrint(t *testing.T) {
	for _, wide := range []bool{false, true} {
		u := UpdateStatus{Current: "v0.3.0", Latest: "v0.4.0", Checked: true, Newer: true}
		a, up, cv := updateApp(printingNow, u, wide)
		tapRegion(t, a, cv, "Install v0.4.0")
		if a.confirm == nil || !strings.Contains(a.confirm.question, "after the print") {
			t.Fatalf("wide %v: question %+v", wide, a.confirm)
		}
		a.confirm.onYes()
		if up.installs != 1 {
			t.Errorf("wide %v: %d installs", wide, up.installs)
		}
	}
}

// An installed update during a print: Restart the screen now, after a question. No Restart now.
func TestUpdateSwitchKey(t *testing.T) {
	for _, wide := range []bool{false, true} {
		a, up, cv := updateApp(printingNow, stagedUpdate, wide)
		a.Draw(cv)
		if _, ok := a.KeyRect("Restart now"); ok {
			t.Errorf("wide %v: Restart now during a print", wide)
		}
		tapRegion(t, a, cv, "Restart the screen now")
		if a.confirm == nil || !strings.Contains(a.confirm.question, "The print keeps running") {
			t.Fatalf("wide %v: question %+v", wide, a.confirm)
		}
		a.confirm.onYes()
		if up.switches != 1 {
			t.Errorf("wide %v: %d switches", wide, up.switches)
		}
	}
	// With no compatible deck program, no key.
	u := stagedUpdate
	u.CanSwitch, u.SwitchNote = false, "The new screen program needs the new Klipper host."
	a, _, cv := updateApp(printingNow, u, false)
	a.Draw(cv)
	if _, ok := a.KeyRect("Restart the screen now"); ok {
		t.Error("Restart the screen now with no compatible program")
	}
	if note := stagedNote(u, printingNow); !strings.Contains(note, "Klipper host") {
		t.Errorf("note %q", note)
	}
}

// An installed update with no print: Restart now, after a question.
func TestUpdateRestartNow(t *testing.T) {
	a, up, cv := updateApp(idlePrinter, stagedUpdate, false)
	a.Draw(cv)
	if _, ok := a.KeyRect("Restart the screen now"); ok {
		t.Error("Restart the screen now with no print")
	}
	tapRegion(t, a, cv, "Restart now")
	if a.confirm == nil {
		t.Fatal("no question")
	}
	a.confirm.onYes()
	if up.restarts != 1 {
		t.Errorf("%d restarts", up.restarts)
	}
}

// After the switch, the page and the information of Machine show the two versions, and no
// install.
func TestUpdateSwitched(t *testing.T) {
	u := stagedUpdate
	u.Current, u.System, u.CanSwitch = "v0.4.0", "v0.3.0", false
	a, _, cv := updateApp(printingNow, u, false)
	c, rec := gfx.NewRecording(a.Screen().W, a.Screen().H)
	a.Draw(c)
	f := string(rec.JSON())
	for _, want := range []string{"SCREEN", "SYSTEM", "INSTALLED", "v0.3.0"} {
		if !strings.Contains(f, want) {
			t.Errorf("no %q on the page", want)
		}
	}
	a.Draw(cv)
	for _, k := range []string{"Restart the screen now", "Restart now", "Install v0.4.0"} {
		if _, ok := a.KeyRect(k); ok {
			t.Errorf("key %q after the switch", k)
		}
	}
	a.pages = []page{pageMachine}
	c, rec = gfx.NewRecording(a.Screen().W, a.Screen().H)
	a.Draw(c)
	if f := string(rec.JSON()); !strings.Contains(f, "SCREEN v0.4.0") || !strings.Contains(f, "SYSTEM v0.3.0") {
		t.Error("the information of Machine has no versions of the screen and the system")
	}
}
