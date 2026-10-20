package gcode

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
)

func newDispatch(t *testing.T) *Dispatch {
	p := host.New(reactor.New(), host.StartArgs{})
	return Of(p)
}

func catch(f func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = r.(*CommandError).Msg
		}
	}()
	f()
	return ""
}

// The dispatcher reads each line as Klipper's dispatcher does (testdata/lines.json, from
// tools/klipper-reference/dumpgcode.py).
func TestParseMatchesKlipper(t *testing.T) {
	b, err := os.ReadFile("testdata/lines.json")
	if err != nil {
		t.Fatal(err)
	}
	var want []map[string]any
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}
	g := newDispatch(t)
	g.gcodeHandlers = map[string]Handler{}
	for _, w := range want {
		got := map[string]any{"line": w["line"]}
		g.defaultHandler = func(gcmd *Command) {
			got["cmd"] = gcmd.Command()
			got["params"] = toAny(gcmd.Params())
			got["raw"] = gcmd.RawParams()
			if msg := catch(func() { g.extendedParams(gcmd) }); msg != "" {
				got["error"] = msg
			} else {
				got["extended"] = toAny(gcmd.Params())
			}
		}
		g.processCommands([]string{w["line"].(string)}, false)
		if !reflect.DeepEqual(got, w) {
			t.Errorf("%q:\n got %v\nwant %v", w["line"], got, w)
		}
	}
}

func toAny(m map[string]string) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func TestRegisterAndRun(t *testing.T) {
	g := newDispatch(t)
	var out []string
	g.RegisterOutputHandler(func(msg string) { out = append(out, msg) })
	var speed float64
	g.RegisterCommand("SET_SPEED", func(gcmd *Command) { speed = gcmd.GetFloat("SPEED", Min(0.), Max(1.)) }, true, "Set the speed")
	g.ProcessCommands([]string{"SET_SPEED SPEED=0.5", "SET_SPEED SPEED=2"})
	if speed != 0.5 {
		t.Errorf("speed = %v", speed)
	}
	want := []string{"ok", "!! Error on 'SET_SPEED SPEED=2': SPEED must have maximum of 1.0", "ok"}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("responses %q, want %q", out, want)
	}
	if msg := catch(func() { g.RunScript("SET_SPEED") }); msg != "Error on 'SET_SPEED': missing SPEED" {
		t.Errorf("RunScript error %q", msg)
	}
	for _, bad := range []string{"set_speed", "1ABC", "A1BC", "SET-SPEED"} {
		func() {
			defer func() {
				if r := recover(); r == nil || !strings.Contains(r.(error).Error(), "invalid name") {
					t.Errorf("%q: %v", bad, r)
				}
			}()
			g.RegisterCommand(bad, func(*Command) {}, false, "")
		}()
	}
}

func TestMux(t *testing.T) {
	g := newDispatch(t)
	g.isPrinterReady = true
	g.gcodeHandlers = g.readyGcodeHandlers
	var got string
	a, b := "fan_a", "fan_b"
	g.RegisterMuxCommand("SET_FAN", "FAN", &a, func(*Command) { got = a }, "")
	g.RegisterMuxCommand("SET_FAN", "FAN", &b, func(*Command) { got = b }, "")
	g.RunScript("SET_FAN FAN=fan_b")
	if got != b {
		t.Errorf("got %q", got)
	}
	if msg := catch(func() { g.RunScript("SET_FAN FAN=fan") }); msg != "The value 'fan' is not valid for FAN. Did you mean 'fan_b'?" {
		t.Errorf("%q", msg)
	}
	if msg := catch(func() { g.RunScript("SET_FAN FAN=x") }); msg != "The value 'x' is not valid for FAN. Options: 'fan_a', 'fan_b'" {
		t.Errorf("%q", msg)
	}
}

func TestShlex(t *testing.T) {
	for s, want := range map[string][]string{
		`a "b c" 'd e'`: {"a", "b c", "d e"},
		`x="" y=''`:     {"x=", "y="},
		`a"b"c 'it''s'`: {"abc", "its"},
		`"a\"b" "c\d"`:  {`a"b`, `c\d`},
		`a;b c`:         {"a"},
		"a # c\nd":      {"a", "d"},
	} {
		got, err := shlexSplit(s)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %q %v", s, got, err)
		}
	}
}
