package ui

import (
	"image"
	"strings"
	"testing"

	"printdeck/internal/printer"
)

// x2dPrinter is a demo printer with two nozzles, an AMS of four trays and an external spool, as
// a Bambu Lab X2D.
type x2dPrinter struct {
	*DemoPrinter
	ams        printer.AMS
	tool2      []int
	selected   []int
	chamberSet []int
}

func (p *x2dPrinter) AMS() printer.AMS           { return p.ams }
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
