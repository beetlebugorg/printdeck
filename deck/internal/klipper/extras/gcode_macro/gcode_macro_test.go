package gcode_macro

import (
	"strings"
	"testing"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
)

type fakeTool struct{}

func (fakeTool) Status(float64) map[string]any {
	return map[string]any{"position": gcode.Coord{1, 2, 3, 4}, "homed_axes": "xy"}
}

const cfg = `
[gcode_macro M109]
rename_existing: M99109
gcode:
    {% set s = params.S|default(params.R|default(0))|float %}
    M99109 S{s}

[gcode_macro SHOW]
variable_count: 1
variable_name: 'abc'
gcode:
    CAPTURE MSG={name}:{count}:{printer.tool.position.z}:{'x' in printer.tool.homed_axes}:{rawparams}

[gcode_macro FAIL]
gcode:
    { action_raise_error("stop here") }
`

func setup(t *testing.T) (*host.Printer, *[]string) {
	p := host.New(reactor.New(), host.StartArgs{})
	p.AddObject("tool", fakeTool{})
	pc := configfile.NewPrinterConfig(p)
	config, err := pc.ReadConfigData(cfg, "test.cfg")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	g := gcode.Of(p)
	g.RegisterCommand("M109", func(gcmd *gcode.Command) { got = append(got, "orig "+gcmd.Commandline()) }, false, "")
	g.RegisterCommand("CAPTURE", func(gcmd *gcode.Command) { got = append(got, gcmd.Commandline()) }, false, "")
	for _, s := range config.PrefixSections("gcode_macro ") {
		p.TryLoadObject(config, s.Name())
	}
	p.SendEvent("klippy:connect")
	p.SendEvent("klippy:ready")
	return p, &got
}

func TestMacros(t *testing.T) {
	p, got := setup(t)
	g := gcode.Of(p)
	g.RunScript("M109 R215")
	g.RunScript("SHOW A=1 b=2")
	g.RunScript("SET_GCODE_VARIABLE MACRO=SHOW VARIABLE=count VALUE=5")
	g.RunScript("SHOW")
	want := []string{"orig M99109 S215.0", "CAPTURE MSG=abc:1:3.0:True:A=1 b=2", "CAPTURE MSG=abc:5:3.0:True:"}
	if strings.Join(*got, "|") != strings.Join(want, "|") {
		t.Errorf("got  %q\nwant %q", *got, want)
	}
	func() {
		defer func() {
			r := recover()
			e, ok := r.(*gcode.CommandError)
			if !ok || e.Msg != "Error evaluating 'gcode_macro FAIL:gcode': gcode.CommandError: stop here\n" {
				t.Errorf("error %#v", r)
			}
		}()
		g.RunScript("FAIL")
	}()
}
