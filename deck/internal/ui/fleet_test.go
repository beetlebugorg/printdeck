package ui

import (
	"fmt"
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

// Stop all stops the printer that prints and heats, and not the idle printers: a stop of an idle
// printer only makes it need a firmware restart.
func TestStopAll(t *testing.T) {
	sw, prs := fleetDeck(t)
	if err := sw.Apps()[1].Fleet.StopAll(); err != nil {
		t.Fatal(err)
	}
	if prs[0].Stops != 1 || prs[1].Stops != 0 || prs[2].Stops != 0 {
		t.Errorf("stops %d %d %d, want 1 0 0", prs[0].Stops, prs[1].Stops, prs[2].Stops)
	}
}

// The status bar at the top of each page shows each printer and its state, and a tap on it opens
// the list of the printers from any page.
func TestFleetStatusBar(t *testing.T) {
	s, _ := fleetDeck(t)
	a := s.Active()
	a.pages = []page{pageMachine}
	f := frameText(a)
	for _, want := range []string{"TAZ 43%", "MINI Ready", "PHEN Done"} {
		if !strings.Contains(f, want) {
			t.Errorf("the status bar has no %q", want)
		}
	}
	tap(a, image.Rect(0, 0, a.Screen().W, fleetStatusH))
	if !a.fleetList {
		t.Fatal("a tap on the status bar opened no list")
	}
}

// After a restart the deck shows the printer that it showed last (Restore), and each switch goes
// to OnSwitch.
func TestSwitcherRestore(t *testing.T) {
	s, _ := fleetDeck(t)
	var saved []string
	s.OnSwitch = func(id string) { saved = append(saved, id) }
	s.Restore("phenom")
	if s.Active() != s.Apps()[2] || len(saved) != 0 {
		t.Fatalf("restore: active %v saved %v", s.Active() == s.Apps()[2], saved)
	}
	s.Restore("gone") // a printer that the deck no longer has
	if s.Active() != s.Apps()[2] {
		t.Fatal("an unknown printer changed the screen")
	}
	s.Show("mini")
	if s.Active() != s.Apps()[1] || len(saved) != 1 || saved[0] != "mini" {
		t.Fatalf("show: saved %v", saved)
	}
}

// loadedPrinter is a demo printer with an AMS of four trays.
type loadedPrinter struct {
	*DemoPrinter
	items []printer.Loaded
}

func (p loadedPrinter) Loaded() []printer.Loaded { return p.items }

// Four trays with the part left do not fit with their material: the row shows the part left
// only, and each square stays in the panel.
func TestLoadedFits(t *testing.T) {
	a := New()
	full := printer.Loaded{Material: "PLA", Color: "#888888", Fraction: 1}
	a.Printer = loadedPrinter{&DemoPrinter{View: -1, State_: printer.State{Reachable: true, Connected: true, Text: "Ready"}},
		[]printer.Loaded{full, full, full, full}}
	c, rec := gfx.NewRecording(a.Screen().W, a.Screen().H)
	r := image.Rect(16, 100, 448, 120)
	w := a.drawLoaded(c, r)
	f := string(rec.JSON())
	if w > r.Dx()*2/3 || strings.Contains(f, "PLA 100%") || strings.Count(f, "100%") != 4 {
		t.Errorf("width %d, frame %s", w, f)
	}
	shot(t, a, "loaded")
}

// A question of a printer that is not on the screen shows as a banner on the screen, and Show goes
// to it. A question of the whole deck (Switcher.Ask) opens on the printer on the screen.
func TestFleetQuestions(t *testing.T) {
	s, _ := fleetDeck(t)
	taz, mini := s.Apps()[0], s.Apps()[1]
	mini.Ask("New spool in Tray 2", "OK", "Back", func(bool) {})
	notes := taz.Fleet.Notes()
	if len(notes) == 0 || notes[0].ID != "mini" || notes[0].Text != "Asks · New spool in Tray 2" {
		t.Fatalf("notes %v", notes)
	}
	if f := frameText(taz); !strings.Contains(strings.ToUpper(f), "SHOW MINI") {
		t.Fatal("no banner of the question")
	}
	show, ok := taz.KeyRect("Show MINI")
	if !ok {
		t.Fatal("no Show key")
	}
	tap(taz, show)
	if s.Active() != mini {
		t.Fatal("Show did not go to the printer of the question")
	}
	if f := frameText(mini); !strings.Contains(f, "New spool in Tray 2") {
		t.Error("the question is not on the screen of its printer")
	}
	// The Allow of an app goes to the printer on the screen.
	s.Ask("Allow zslice to use this Printdeck and its printers?", "Allow", "Deny", func(bool) {})
	if q := mini.hub.question(); q == nil || !strings.HasPrefix(q.question, "Allow zslice") || taz.hub.question() != nil {
		t.Fatal("the deck question is not on the screen")
	}
}

// TestFleetPagesShots writes each page of a deck with many printers, tall and wide, to
// FLEET_SHOTS: each page starts under the status bar.
func TestFleetPagesShots(t *testing.T) {
	if os.Getenv("FLEET_SHOTS") == "" {
		t.Skip("FLEET_SHOTS is not set")
	}
	s, _ := fleetDeck(t)
	for _, wide := range []bool{false, true} {
		for _, a := range s.Apps()[:2] {
			a.Wide = wide
			for _, p := range []page{0, pageFiles, pageMove, pageFilament, pageMachine, pageSettings, pageCamera,
				pageHistory, pageStats, pageConsole, pageMacros, pageMaintenance, pageSpools, pageQueue, pageProjects,
				pageMonitor, pagePrinters} {
				a.pages = nil
				if p != 0 {
					a.pages = []page{p}
				}
				name := fmt.Sprintf("%s-%d-wide%v", a.Fleet.Self().ID, p, wide)
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Logf("%s: the demo printer has no data for the page: %v", name, r)
						}
					}()
					shot(t, a, name)
				}()
			}
		}
	}
}
