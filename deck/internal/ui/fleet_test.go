package ui

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/internal/touch"
)

// fleetDeck is a deck with three printers: the TAZ prints, the Mini is ready, and the Phenom
// finished a print.
func fleetDeck(t *testing.T) (*Switcher, []*DemoPrinter) {
	t.Helper()
	now := time.Date(2026, 11, 18, 14, 24, 0, 0, time.UTC)
	states := []printer.State{
		{Reachable: true, Connected: true, Text: "Printing", Printing: true, File: "idler_tube_clamp_right.gcode",
			Completion: 43, TimeLeft: 72 * time.Minute, HasTimeLeft: true, Tool: 215, ToolTarget: 215, Bed: 60, BedTarget: 60},
		{Reachable: true, Connected: true, Text: "Ready", Tool: 24, Bed: 24},
		{Reachable: true, Connected: true, Text: "Ready", Finished: "complete", FinishedFile: "pod_body.ctb"},
	}
	ids := []Identity{
		{ID: "taz", Name: "TAZ Workhorse", Label: "TAZ", Color: PrinterColors[0]},
		{ID: "mini", Name: "Mini 2", Label: "MINI", Color: PrinterColors[1]},
		{ID: "phenom", Name: "Phenom", Label: "PHEN", Color: PrinterColors[2], Resin: true},
	}
	var prs []*DemoPrinter
	var apps []*App
	for _, st := range states {
		pr := &DemoPrinter{View: -1, State_: st, Img: image.NewRGBA(image.Rect(0, 0, 40, 30))}
		a := New()
		a.Printer = pr
		a.Now = func() time.Time { return now }
		prs = append(prs, pr)
		apps = append(apps, a)
	}
	return NewSwitcher(apps, ids), prs
}

func frameText(a *App) string {
	c, rec := gfx.NewRecording(a.Screen().W, a.Screen().H)
	a.Draw(c)
	return string(rec.JSON())
}

// shot writes the frame of a to FLEET_SHOTS/name.png, when FLEET_SHOTS is set.
func shot(t *testing.T, a *App, name string) {
	dir := os.Getenv("FLEET_SHOTS")
	if dir == "" {
		return
	}
	c := gfx.New(a.Screen().W, a.Screen().H)
	a.Draw(c)
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	png.Encode(f, c.RGBA)
}

// tap sends a tap at the middle of r to a.
func tap(a *App, r image.Rectangle) {
	p := r.Min.Add(r.Size().Div(2))
	x, y := float64(p.X)/float64(a.Screen().W), float64(p.Y)/float64(a.Screen().H)
	a.handle(touch.Event{Kind: touch.Down, X: x, Y: y})
	a.handle(touch.Event{Kind: touch.Up, X: x, Y: y})
}

// Each page of a deck with many printers names its printer, and the chip opens the list. A
// printer that ended a print shows a banner on the other printers, and Show switches.
func TestFleetScreens(t *testing.T) {
	s, prs := fleetDeck(t)
	taz := s.Active()
	// The end of the Phenom print was before the start: no banner.
	if n := taz.Fleet.Notes(); len(n) != 0 {
		t.Fatalf("notes at the start: %v", n)
	}
	f := frameText(taz)
	if !strings.Contains(f, "TAZ") {
		t.Fatal("the main page has no printer chip")
	}
	shot(t, taz, "main")
	chip, ok := taz.KeyRect("Printer")
	if !ok {
		t.Fatal("no printer chip")
	}
	tap(taz, chip)
	if !taz.fleetList {
		t.Fatal("the chip opened no list")
	}
	f = frameText(taz)
	for _, want := range []string{"PRINTERS", "TAZ Workhorse", "Mini 2", "Phenom", "ON SCREEN", "RESIN", "Printing · 43 %"} {
		if !strings.Contains(f, want) {
			t.Errorf("the list has no %q", want)
		}
	}
	shot(t, taz, "list")
	// A tap on the Mini switches to it.
	frameText(taz)
	mini, ok := taz.KeyRect("Mini 2")
	if !ok {
		t.Fatal("no Mini card")
	}
	tap(taz, mini)
	if s.Active() != s.Apps()[1] || taz.fleetList {
		t.Fatal("the tap did not switch to the Mini")
	}
	m := s.Active()
	m.pages = []page{pageFiles}
	if f := frameText(m); !strings.Contains(f, "MINI · FILES") {
		t.Error("the Files page does not name the printer")
	}
	shot(t, m, "files")
	m.pages = nil
	// A new end of a print on the TAZ: a banner on the Mini.
	prs[0].State_ = printer.State{Reachable: true, Connected: true, Text: "Ready", Finished: "complete",
		FinishedFile: "idler_tube_clamp_right.gcode", FinishedTime: 78 * time.Minute}
	notes := m.Fleet.Notes()
	if len(notes) != 1 || notes[0].ID != "taz" || !strings.Contains(notes[0].Text, "Print done") {
		t.Fatalf("notes: %v", notes)
	}
	if f := frameText(m); !strings.Contains(strings.ToUpper(f), "SHOW TAZ") {
		t.Fatal("no banner")
	}
	shot(t, m, "banner")
	// With a dialog open, the banner waits.
	m.confirm = &confirmBox{question: "Preheat for PLA?", yes: "Preheat", onYes: func() {}}
	f = frameText(m)
	if strings.Contains(strings.ToUpper(f), "SHOW TAZ") {
		t.Error("the banner covers a dialog")
	}
	if !strings.Contains(strings.ToUpper(f), "PREHEAT ON MINI") || !strings.Contains(f, "MINI · CONFIRM") {
		t.Error("the dialog does not name the printer")
	}
	shot(t, m, "confirm")
	m.confirm = nil
	frameText(m)
	show, ok := m.KeyRect("Show TAZ")
	if !ok {
		t.Fatal("no Show key")
	}
	tap(m, show)
	if s.Active() != taz {
		t.Fatal("Show did not switch")
	}
	if n := m.Fleet.Notes(); len(n) != 0 {
		t.Fatalf("the note stays after Show: %v", n)
	}
}

// A deck with one printer shows no chip and no bar.
func TestNoFleet(t *testing.T) {
	a := New()
	a.Printer = &DemoPrinter{View: -1, State_: printer.State{Reachable: true, Connected: true, Text: "Ready"}}
	if _, ok := a.KeyRect("Printers"); ok {
		t.Fatal("a chip on a deck with one printer")
	}
	a.pages = []page{pageFiles}
	if f := frameText(a); strings.Contains(f, " · FILES") {
		t.Fatal("a label in the title with one printer")
	}
}

// The moves: deeper is forward, back is back, the same depth fades, and a switch slides.
func TestTransitionFor(t *testing.T) {
	main, files, rolls := pageKey{}, pageKey{1, "files"}, pageKey{2, "rolls"}
	cases := []struct {
		same      bool
		last, now pageKey
		dir       int
		want      transition
	}{
		{true, main, main, 0, transitionNone},
		{true, main, files, 0, transitionForward},
		{true, rolls, files, 0, transitionBack},
		{true, files, pageKey{1, "filament"}, 0, transitionFade},
		{false, files, main, 1, transitionForward},
		{false, files, main, -1, transitionBack},
	}
	for _, c := range cases {
		if got := transitionFor(c.same, c.last, c.now, c.dir); got != c.want {
			t.Errorf("%+v: %v, want %v", c, got, c.want)
		}
	}
}
