package system

import (
	"strings"
	"testing"

	"printdeck/internal/klipper/jinja"
)

// TestDisplayAnimation renders the animation of the display layout with the template engine of
// the Klipper host: each animation fills the bottom row of 16 characters.
func TestDisplayAnimation(t *testing.T) {
	i := strings.Index(displayCfg, "[display_data __printdeck animation]")
	src := displayCfg[strings.Index(displayCfg[i:], "text:\n")+i+len("text:\n"):]
	var lines []string
	for _, l := range strings.Split(src, "\n") {
		lines = append(lines, strings.TrimPrefix(l, "  "))
	}
	tpl, err := jinja.Parse(strings.Join(lines, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, anim := range []string{"in", "out", "heat", "work"} {
		for _, pt := range []float64{0, 1.25, 3.5, 7} {
			macro := jinja.NewDict()
			macro.Set("anim", anim)
			toolhead := jinja.NewDict()
			toolhead.Set("estimated_print_time", pt)
			printer := jinja.NewDict()
			printer.Set("gcode_macro _PRINTDECK", macro)
			printer.Set("toolhead", toolhead)
			out, err := tpl.Render(map[string]any{"printer": printer})
			if err != nil {
				t.Fatalf("%s: %v", anim, err)
			}
			row := strings.ReplaceAll(out, "\n", "")
			if len(row) != 16 || strings.TrimSpace(row) == "" {
				t.Errorf("%s at %g: %q", anim, pt, row)
			}
		}
	}
}
