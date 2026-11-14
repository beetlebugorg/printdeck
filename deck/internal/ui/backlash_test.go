package ui

import (
	"testing"

	"printdeck/internal/printer"
)

func TestBacklashLine(t *testing.T) {
	for _, c := range []struct {
		m    printer.Machine
		want string
	}{
		{printer.Machine{Modules: []string{"printdeck_cube"}}, "No correction · measure on the cube"},
		{printer.Machine{Modules: []string{"printdeck_backlash"}}, "Correction off"},
		{printer.Machine{Modules: []string{"printdeck_backlash"}, BacklashX: 0.31, BacklashY: 0.23}, "Correction X 0.31 mm, Y 0.23 mm"},
	} {
		if got := backlashLine(c.m); got != c.want {
			t.Errorf("%v: got %q, want %q", c.m.Modules, got, c.want)
		}
	}
}
