package ui

import (
	"strings"
	"testing"

	"printdeck/internal/bedcool"
	"printdeck/internal/printer"
)

// The Bed cool page shows the toggle, a hold for each material, and the cooling curve. A row of a
// material opens its holds, and a pick sets the hold.
func TestBedCoolPage(t *testing.T) {
	a, pr, cv := newPrintApp(idle)
	pr.Machine_ = printer.Machine{Modules: []string{"heater_bed", "bed_mesh"}}
	r := bedcool.NewRunner(bedcool.DefaultSettings)
	a.BedCool = r
	a.pages = []page{pageMaintenance}
	if f := simFrame(t, a, "maintenance-bedcool"); !strings.Contains(f, "On for PLA") {
		t.Fatal("no Bed cool row on Maintenance")
	}
	a.pages = []page{pageBedCool}
	f := simFrame(t, a, "bedcool")
	for _, s := range []string{"COOL BEFORE THE END", "Hold 50° · from 60° in about 5 min", "Not learned yet"} {
		if !strings.Contains(strings.ToUpper(f), strings.ToUpper(s)) {
			t.Errorf("no %q on the page", s)
		}
	}
	tapLabel(t, a, cv, "PETG")
	simFrame(t, a, "bedcool-petg")
	tapLabel(t, a, cv, "70°")
	if got := r.Settings().Hold["PETG"]; got != 70 {
		t.Errorf("PETG hold %d, want 70", got)
	}
	if got := r.Settings().Hold["PLA"]; got != 50 {
		t.Errorf("PLA hold %d, want 50", got)
	}
}

func TestJoinAnd(t *testing.T) {
	for w, want := range map[string]string{"": "", "A": "A", "A B": "A and B", "A B C": "A, B and C"} {
		if got := joinAnd(strings.Fields(w)); got != want {
			t.Errorf("%q: %q", w, got)
		}
	}
}
