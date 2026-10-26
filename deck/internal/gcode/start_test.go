package gcode

import (
	"strings"
	"testing"
)

// ScanStart reads the start macro as Parse does: the last PRINT_START line, with no comment.
func TestScanStart(t *testing.T) {
	src := ";FLAVOR:Marlin\nM117 Hello\nPRINT_START BED=55 EXTRUDER=200\n" +
		"print_start BED=60 EXTRUDER=210 WIPE=160 ; the second one\nG1 X1 E1\n"
	m, _ := Parse(strings.NewReader(src))
	s := ScanStart(strings.NewReader(src))
	if s.End != m.StartEnd || s.BedTemp != m.BedTemp || s.ExtTemp != m.ExtTemp || s.WipeTemp != m.WipeTemp {
		t.Errorf("scan %+v, parse %d %v %v %v", s, m.StartEnd, m.BedTemp, m.ExtTemp, m.WipeTemp)
	}
	if s.Line != "print_start BED=60 EXTRUDER=210 WIPE=160" || s.ExtTemp != 210 {
		t.Errorf("scan %+v", s)
	}
	if s := ScanStart(strings.NewReader("G28\nG1 X1\n")); s.End != 0 || s.Line != "" {
		t.Errorf("no macro: %+v", s)
	}
}
