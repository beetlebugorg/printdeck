package gcodemeta

import (
	"strings"
	"testing"
)

func TestCleanName(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		// The machine prefix of Cura
		{"TAZWH_hex_planter_v3.gcode", "Hex planter"},
		{"CE3E3V2_part.gcode", "Part"},
		{"UMS3_bracket (1).gcode", "Bracket"},
		{"MK3S_cable_clip.gcode", "Cable clip"},
		{"TAZ6_gear-v2.gco", "Gear"},
		{"LTAZWSE05MM_tower-esun-pla-orange.gcode", "Tower esun pla orange"},
		{"UM2_Spool holder.gcode", "Spool holder"},
		// No machine prefix
		{"benchy.gcode", "Benchy"},
		{"3DBenchy.gcode", "3DBenchy"},
		{"printdeck_body.gcode", "Printdeck body"},
		{"cable_clip_x8.gcode", "Cable clip x8"},
		{"PETG_bracket.gcode", "PETG bracket"},
		{"PLA_test.gcode", "PLA test"},
		{"LED_holder.gcode", "LED holder"},
		{"Ender-3_bed-clip.gcode", "Ender 3 bed clip"},
		{"2024_calendar.gcode", "2024 calendar"},
		{"Ab_part.gcode", "Ab part"},
		{"xyzCalibration_cube.gcode", "XyzCalibration cube"},
		{"élan_vase.gcode", "Élan vase"},
		// The prefix stays when no name follows it.
		{"MK3S_.gcode", "MK3S"},
		{"MK3S_v2.gcode", "V2"},
		// Versions and copies
		{"bracket_v12.gcode", "Bracket"},
		{"bracket-V2.gcode", "Bracket"},
		{"bracket v3.gcode", "Bracket"},
		{"bracket(2).gcode", "Bracket"},
		{"UMS3_bracket_v2 (1).gcode", "Bracket"},
		{"v2.gcode", "V2"},
		{"bracket_v2b.gcode", "Bracket v2b"},
		{"bracketv2.gcode", "Bracketv2"},
		// Spaces and dashes
		{"__hook__.gcode", "Hook"},
		{"hook   with  spaces.gcode", "Hook with spaces"},
		{"spacer_10-20mm.gcode", "Spacer 10-20mm"},
		{"-dash-.gcode", "Dash"},
		// Folders and extensions
		{"jobs/TAZ6_gear.gcode", "Gear"},
		{`jobs\gear.gcode`, "Gear"},
		{"case.G", "Case"},
		{"case.GCODE", "Case"},
		{"notes.txt", "Notes.txt"},
		{"model.3mf.gcode", "Model.3mf"},
		// Never empty
		{"_.gcode", "_"},
		{"---.gcode", "---"},
		{".gcode", ".gcode"},
		{"", ""},
	} {
		if got := CleanName(c.in); got != c.want {
			t.Errorf("CleanName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMetaName(t *testing.T) {
	if got := (Meta{Title: "Hex planter"}).Name("TAZWH_x.gcode"); got != "Hex planter" {
		t.Errorf("got %q", got)
	}
	if got := (Meta{}).Name("TAZWH_x.gcode"); got != "X" {
		t.Errorf("got %q", got)
	}
}

// The Printdeck plugin for Cura writes the title in the first lines of the file.
func TestTitle(t *testing.T) {
	g := ";Printdeck title:  Hex planter \n;FLAVOR:Marlin\n;Generated with Cura_SteamEngine 5.13.0\nG28\n"
	m, err := Parse(strings.NewReader(g), int64(len(g)))
	if err != nil || m.Title != "Hex planter" {
		t.Errorf("%+v %v", m, err)
	}
	// A title line after the first lines is part of the print, for example a macro.
	var b strings.Builder
	for range titleLines {
		b.WriteString("G1 X1\n")
	}
	b.WriteString(";Printdeck title: Late\n")
	m, _ = Parse(strings.NewReader(b.String()), int64(b.Len()))
	if m.Title != "" {
		t.Errorf("title %q after line %d", m.Title, titleLines)
	}
	// An empty title, and the control characters of a title
	g = ";PRINTDECK TITLE:\t\n;printdeck title: A\x01B\n"
	m, _ = Parse(strings.NewReader(g), int64(len(g)))
	if m.Title != "AB" {
		t.Errorf("title %q", m.Title)
	}
}
