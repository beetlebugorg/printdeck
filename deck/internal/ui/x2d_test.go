package ui

import (
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"printdeck/internal/printer"
	"printdeck/internal/timelapse"
)

// x2dPrinter is a demo printer with two nozzles, an AMS of four trays and an external spool, as
// a Bambu Lab X2D.
type x2dPrinter struct {
	*DemoPrinter
	ams        printer.AMS
	tool2      []int
	selected   []int
	chamberSet []int
	loads      []int // the trays of LoadTray, and -1 for UnloadTray
	dries      []string
}

func (p *x2dPrinter) AMS() printer.AMS           { return p.ams }
func (p *x2dPrinter) LoadTray(slot int) error    { p.loads = append(p.loads, slot); return nil }
func (p *x2dPrinter) UnloadTray() error          { p.loads = append(p.loads, -1); return nil }
func (p *x2dPrinter) ReadTrayTag(slot int) error { return nil }
func (p *x2dPrinter) DryAMS(unit int, filament string, temp, hours int) error {
	p.dries = append(p.dries, fmt.Sprintf("%d %s %d %d", unit, filament, temp, hours))
	return nil
}
func (p *x2dPrinter) StopDryAMS(unit int) error  { p.dries = append(p.dries, "stop"); return nil }
func (p *x2dPrinter) LinkTray(int, string) error { return nil }
func (p *x2dPrinter) SetTool2Temp(t int) error   { p.tool2 = append(p.tool2, t); return nil }
func (p *x2dPrinter) SelectTool(n int) error     { p.selected = append(p.selected, n); return nil }
func (p *x2dPrinter) SetChamberTemp(t int) error { p.chamberSet = append(p.chamberSet, t); return nil }
func (p *x2dPrinter) AddTrayRoll(int, float64, string) (printer.Spool, error) {
	return printer.Spool{}, nil
}
func (p *x2dPrinter) Loaded() []printer.Loaded {
	var out []printer.Loaded
	for _, t := range p.ams.Trays {
		out = append(out, printer.Loaded{Color: t.Color, Material: t.Material, Fraction: 1, Active: t.Active,
			Empty: !t.Loaded, External: t.External})
	}
	return out
}

func newX2D() *x2dPrinter {
	st := printer.State{Reachable: true, Connected: true, Text: "Ready", Tool: 220, ToolTarget: 220,
		HasTool2: true, Tool2: 219, Tool2Target: 220, ToolNow: 1, Bed: 55, BedTarget: 55,
		HasChamber: true, Chamber: 32, HasFan: true, Fan: 100}
	tray := func(slot int, color string, feeds int) printer.AMSTray {
		return printer.AMSTray{Slot: slot, Index: slot, Loaded: true, Material: "PLA", Color: color, Remain: -1, Feeds: feeds}
	}
	ams := printer.AMS{Units: []printer.AMSUnit{{Temp: 30, HumidityPct: 20}}, Trays: []printer.AMSTray{
		tray(0, "#898989", -1), tray(1, "#dcdcdc", -1), tray(2, "#161616", 0), tray(3, "#f72323", -1),
		{Slot: 255, Unit: -1, Loaded: true, Material: "PLA", Color: "#dcdcdc", Remain: -1, External: true, Feeds: 1, Active: true},
	}}
	return &x2dPrinter{DemoPrinter: &DemoPrinter{View: -1, State_: st, Img: image.NewRGBA(image.Rect(0, 0, 40, 30)),
		Machine_: printer.Machine{OwnControls: true}}, ams: ams}
}

// An X2D shows both nozzles in the status panel, and a tap on the aux nozzle sets its temperature.
// The row of what is loaded has the four trays and the external spool.
func TestX2DPanel(t *testing.T) {
	a := New()
	p := newX2D()
	a.Printer = p
	f := frameText(a)
	for _, want := range []string{`"NOZZLE"`, `"AUX"`, `"BED"`, `"CHAMBER"`, `"FAN"`, "219°"} {
		if !strings.Contains(f, want) {
			t.Errorf("the panel has no %s", want)
		}
	}
	if n := strings.Count(f, "100%"); n < 5 {
		t.Errorf("%d squares with the part left, want 5", n)
	}
	shot(t, a, "x2d-main")
	aux, ok := a.KeyRect("Aux")
	if !ok {
		t.Fatal("no aux readout")
	}
	tap(a, aux)
	if f := frameText(a); !strings.Contains(strings.ToUpper(f), "AUX NOZZLE TEMPERATURE") {
		t.Fatal("a tap on the aux nozzle opened no keypad")
	}
	shot(t, a, "x2d-auxpad")
}

// The Filament page names the external spool and the nozzle of each spool.
func TestX2DFilament(t *testing.T) {
	a := New()
	a.Printer = newX2D()
	a.pages = []page{pageFilament}
	f := strings.ToUpper(frameText(a))
	for _, want := range []string{"EXTERNAL SPOOL · PLA · AUX", "TRAY 3 · PLA · NOZZLE"} {
		if !strings.Contains(f, want) {
			t.Errorf("the page has no %q", want)
		}
	}
	shot(t, a, "x2d-filament")
}

// The print page and the wide screen of an X2D: the tall readouts keep the value and the
// target apart.
func TestX2DPrintShots(t *testing.T) {
	for _, wide := range []bool{false, true} {
		a := New()
		p := newX2D()
		p.State_.Printing, p.State_.File, p.State_.Completion = true, "benchy.gcode.3mf", 81
		p.State_.Layer, p.State_.Layers, p.State_.HasTimeLeft, p.State_.TimeLeft = 82, 127, true, 17*time.Minute
		a.Printer = p
		a.Wide = wide
		f := frameText(a)
		if !strings.Contains(f, "→220") {
			t.Errorf("wide %v: no target", wide)
		}
		shot(t, a, fmt.Sprintf("x2d-print-wide%v", wide))
	}
}

// The menu of a tray loads its filament into the nozzle, after a question; the menu of the spool
// that feeds unloads it. During a print, the menu has neither.
func TestX2DLoadTray(t *testing.T) {
	a := New()
	p := newX2D()
	a.Printer = p
	a.askTray(p, p.ams.Trays[0], 1)
	var load func()
	for _, c := range a.confirm.choices {
		if c.title == "Load into the nozzle" {
			load = c.tap
		}
	}
	if load == nil {
		t.Fatalf("no load: %+v", a.confirm.choices)
	}
	load()
	if a.confirm == nil || a.confirm.yes != "Load" {
		t.Fatal("the load asks no question")
	}
	a.confirm.onYes()
	deadline := time.After(2 * time.Second)
	for len(p.loads) == 0 {
		select {
		case f := <-a.posted:
			f()
		case <-deadline:
			t.Fatal("no load")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if p.loads[0] != 0 {
		t.Errorf("loads %v", p.loads)
	}
	a.askTray(p, p.ams.Trays[4], 1) // the external spool feeds the aux nozzle
	found := false
	for _, c := range a.confirm.choices {
		found = found || c.title == "Unload the aux nozzle"
	}
	if !found {
		t.Errorf("no unload: %+v", a.confirm.choices)
	}
	p.State_.Printing = true
	a.askTray(p, p.ams.Trays[0], 1)
	for _, c := range a.confirm.choices {
		if strings.HasPrefix(c.title, "Load") || strings.HasPrefix(c.title, "Unload") {
			t.Errorf("%q during a print", c.title)
		}
	}
}

// printerLapseFake is a time lapse that the printer makes.
type printerLapseFake struct {
	DemoTimeLapse
	sets []bool
}

func (p *printerLapseFake) SetSettings(s timelapse.Settings) { p.sets = append(p.sets, s.On); p.S = s }
func (p *printerLapseFake) LapseNote() string {
	return "The deck copies each video from the drive of the printer"
}

// Settings, Time lapse of a printer that makes its time lapse: the toggle is the setting of the
// printer, and the page has no settings of the time lapse of the deck.
func TestPrinterLapsePage(t *testing.T) {
	a := New()
	a.Printer = newX2D()
	pl := &printerLapseFake{}
	a.TimeLapse = pl
	a.pages = []page{pageTimeLapse}
	f := frameText(a)
	if !strings.Contains(f, "MADE BY THE PRINTER") || strings.Contains(f, "PARK FOR THE FRAMES") {
		t.Fatalf("page: %s", f)
	}
	a.toggleLapse()
	if len(pl.sets) != 1 || !pl.sets[0] {
		t.Errorf("sets %v", pl.sets)
	}
	shot(t, a, "x2d-timelapse")
}

// The AMS row of the Filament page dries the AMS: a preset for the filament of its trays first,
// then a question. A drying AMS shows the time left, and a tap stops it.
func TestX2DDryAMS(t *testing.T) {
	a := New()
	p := newX2D()
	a.Printer = p
	a.pages = []page{pageFilament}
	if f := frameText(a); !strings.Contains(f, "tap to dry") {
		t.Fatalf("no drying row: %s", f)
	}
	a.askDry(p, p.ams.Units[0], p.ams, 1)
	if a.confirm == nil || len(a.confirm.choices) == 0 || a.confirm.choices[0].title != "PLA" {
		t.Fatalf("choices %+v", a.confirm)
	}
	a.confirm.choices[0].tap()
	a.confirm.onYes()
	deadline := time.After(2 * time.Second)
	for len(p.dries) == 0 {
		select {
		case f := <-a.posted:
			f()
		case <-deadline:
			t.Fatal("no drying")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if p.dries[0] != "0 PLA 55 8" {
		t.Errorf("dries %v", p.dries)
	}
	p.ams.Units[0].Drying, p.ams.Units[0].DryLeft, p.ams.Units[0].DryFilament, p.ams.Units[0].DryTemp = true, 200*time.Minute, "PLA", 55
	if f := frameText(a); !strings.Contains(f, "Drying PLA 55° · 3:20 left") {
		t.Errorf("no drying state: %s", f)
	}
	shot(t, a, "x2d-drying")
}

// A failed print shows the whole reason of the printer, with no line cut.
func TestFailedReasonWhole(t *testing.T) {
	for _, wide := range []bool{false, true} {
		a := New()
		p := newX2D()
		reason := "The print has stopped because the available hotend quantity or model does not match the sliced file. Please verify the hotend model and quantity before restarting the print job. (05004047)"
		p.State_ = printer.State{Reachable: true, Connected: true, Text: "Ready", Finished: "error", FinishedFile: "cover-screw-fine_0.1mm_PLA_Bambu-Lab-X2D_2h60m.gcode.3mf",
			FinishedError: reason}
		a.Printer = p
		a.Wide = wide
		f := frameText(a)
		for _, w := range []string{"restarting the print", "(05004047)", "Cover screw fine 0.1mm PLA Bambu Lab X2D 2h60m", "Error 05004047"} {
			if !strings.Contains(f, w) {
				t.Errorf("wide %v: no %q", wide, w)
			}
		}
		shot(t, a, fmt.Sprintf("x2d-failed-wide%v", wide))
	}
}

// The question for a new spool names the color of the spool. The rolls in no tray come first with
// their colors, the nearest color first, and a roll in a tray is not a choice.
func TestTrayQuestionRolls(t *testing.T) {
	for _, wide := range []bool{false, true} {
		a := New()
		p := newX2D()
		store := &printer.Spools{}
		roll := func(material, color string) printer.Spool {
			sp := store.AddOut("", material, 1000, 0, 0, time.Now())
			store.SetColor(sp.ID, color)
			sp, _ = store.Get(sp.ID)
			return sp
		}
		red, inTray := roll("PLA", "#f72323"), roll("PLA", "#f98c37")
		orange := roll("PLA", "#f98c36")
		roll("PETG", "#f98c36")
		roll("PLA", "#161616")
		p.Spools_ = store
		p.ams.Trays[0].Roll = inTray.ID
		ext := printer.AMSTray{Slot: 255, Unit: -1, Loaded: true, Material: "PLA", Color: "#f98c36", External: true}
		a.Printer = p
		a.Wide = wide
		for n := 0; n < 6; n++ { // more rolls than a screen shows
			roll("PLA", "#2a62d0")
		}
		var shown *confirmBox
		a.rollPicker(p, ext, "External spool", "External spool now has orange PLA. Which roll is it?", false,
			func(printer.Spool) {}, []menuItem{{"Do not count it", "", dim, func() {}}},
			func(b *confirmBox) { shown = b; a.confirm = b }, nil)
		frameText(a)
		if !strings.Contains(frameText(a), "More choices") && !strings.Contains(strings.ToUpper(frameText(a)), "MORE CHOICES") {
			t.Errorf("wide %v: no row to the other choices", wide)
		}
		if want := rollTitle(orange); shown.choices[0].title != want || shown.swatches[0] != "#f98c36" {
			t.Errorf("wide %v: first choice %q %q, want %q", wide, shown.choices[0].title, shown.swatches[0], want)
		}
		for _, ch := range shown.choices {
			if strings.HasPrefix(ch.title, inTray.Number()+" ") {
				t.Errorf("wide %v: the roll of another tray is a choice", wide)
			}
		}
		if !strings.Contains(rollTitle(red), "Red PLA") {
			t.Errorf("the title of the red roll: %s", rollTitle(red))
		}
		shot(t, a, fmt.Sprintf("tray-question-wide%v", wide))
	}
}
