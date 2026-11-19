package main

import "testing"

func TestSlicedForReason(t *testing.T) {
	taz := deckPrinterName{name: "TAZ Workhorse", files: "gcode"}
	x2d := deckPrinterName{name: "X2D", model: "Bambu Lab X2D", files: "gcode.3mf"}
	mini := deckPrinterName{name: "A", files: "gcode"}
	for _, c := range []struct {
		target string
		self   deckPrinterName
		others []deckPrinterName
		refuse bool
	}{
		{"", taz, []deckPrinterName{x2d}, false},
		{"Bambu Lab X2D 0.4 nozzle", taz, []deckPrinterName{x2d}, true},  // the file of the X2D on the TAZ
		{"Bambu Lab X2D 0.4 nozzle", x2d, []deckPrinterName{taz}, false}, // on the X2D
		{"Bambu Lab X1 Carbon", x2d, nil, true},                          // another Bambu model
		{"Bambu Lab X2D", taz, nil, true},                                // a deck of one printer: a G-code printer
		{"LulzBot TAZ Workhorse", taz, []deckPrinterName{x2d}, false},
		{"LulzBot TAZ Workhorse", x2d, []deckPrinterName{taz}, true}, // the file of the TAZ on the X2D
		{"Prusa MK4", taz, []deckPrinterName{x2d}, false},            // a name that the deck does not know
		{"Bambu Lab X2D", deckPrinterName{name: "Desk"}, nil, false}, // the kind of the printer is not known
		{"Anycubic Kobra", taz, []deckPrinterName{mini}, false},      // "A" is no word of the name
	} {
		got := slicedForReason(c.target, c.self, c.others)
		if (got != "") != c.refuse {
			t.Errorf("%q on %s: %q", c.target, c.self.name, got)
		}
	}
	if got := slicedForReason("Bambu Lab X2D 0.4 nozzle", taz, []deckPrinterName{x2d}); got != "Sliced for Bambu Lab X2D 0.4 nozzle; this printer is TAZ Workhorse" {
		t.Errorf("%q", got)
	}
}
