package octoapi

import (
	"encoding/json"
	"testing"

	"printdeck/internal/printer"
)

type amsFake struct{ ams printer.AMS }

func (f amsFake) AMS() printer.AMS { return f.ams }
func (f amsFake) Loaded() []printer.Loaded {
	var out []printer.Loaded
	for _, t := range f.ams.Trays {
		l := printer.Loaded{Color: t.Color, Material: t.Material, Fraction: -1, Empty: !t.Loaded, External: t.External}
		if t.Slot == 0 {
			l.Fraction = 0.8 // a roll of the deck
		}
		out = append(out, l)
	}
	return out
}

type rollFake struct{}

func (rollFake) Loaded() []printer.Loaded {
	return []printer.Loaded{{Material: "PLA", Color: "#ff0000", Fraction: 0.5}}
}

// The loaded filaments of an X2D: the trays with a spool in their order, and the external spool
// last. An empty tray is out of the list.
func TestFilaments(t *testing.T) {
	ams := amsFake{printer.AMS{Trays: []printer.AMSTray{
		{Slot: 0, Index: 0, Loaded: true, Material: "PLA", Color: "#898989", Brand: "Bambu PLA Basic", Feeds: -1, InfoIdx: "GFA00"},
		{Slot: 1, Index: 1, Feeds: -1},
		{Slot: 2, Index: 2, Loaded: true, Material: "PETG", Color: "#161616", Feeds: 0},
		{Slot: 255, Unit: -1, Loaded: true, Material: "PLA", Color: "#dcdcdc", External: true, Feeds: 1},
	}}}
	b, _ := json.Marshal(filamentsOf(ams))
	want := `[{"slot":"A1","index":0,"tray":0,"material":"PLA","color":"#898989","name":"Bambu PLA Basic","remain":0.8,"idx":"GFA00"},` +
		`{"slot":"A3","index":1,"tray":2,"nozzle":"main","material":"PETG","color":"#161616","remain":null,"idx":""},` +
		`{"slot":"Ext","index":2,"tray":255,"nozzle":"aux","material":"PLA","color":"#dcdcdc","remain":null,"idx":""}]`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
	b, _ = json.Marshal(filamentsOf(rollFake{}))
	if string(b) != `[{"slot":"","index":0,"tray":-1,"material":"PLA","color":"#ff0000","remain":0.5,"idx":""}]` {
		t.Errorf("a Klipper roll: %s", b)
	}
	if filamentsOf(struct{}{}) != nil {
		t.Error("a printer with no spools has filaments")
	}
}
