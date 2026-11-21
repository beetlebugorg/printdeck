package octoapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"printdeck/internal/gcodestore"
	"printdeck/internal/printer"
)

// klipperFake is a printer with the controls of a Klipper printer.
type klipperFake struct {
	fakePrinter
	calls []string
}

func (p *klipperFake) Fan(n int) error { p.calls = append(p.calls, fmt.Sprint("fan ", n)); return nil }
func (p *klipperFake) SpeedFactor(n int) error {
	p.calls = append(p.calls, fmt.Sprint("speed ", n))
	return nil
}
func (p *klipperFake) FlowFactor(n int) error {
	p.calls = append(p.calls, fmt.Sprint("flow ", n))
	return nil
}
func (p *klipperFake) BabyStep(mm float64) error {
	p.calls = append(p.calls, fmt.Sprintf("baby %.3f", mm))
	return nil
}
func (p *klipperFake) Extrude(mm float64, speed int) error {
	if p.st.Tool < 170 {
		return errors.New("Extrude below minimum temp")
	}
	p.calls = append(p.calls, fmt.Sprint("extrude ", mm))
	return nil
}

// trayFake is a printer with an AMS, a chamber and an aux nozzle.
type trayFake struct {
	klipperFake
}

func (p *trayFake) AMS() printer.AMS {
	return printer.AMS{Units: []printer.AMSUnit{{Index: 0, Temp: 30, Humidity: 2, Drying: true, DryTemp: 55}},
		Trays: []printer.AMSTray{{Slot: 0, Unit: 0, Index: 0, Loaded: true, Material: "PLA", Active: true},
			{Slot: 2, Unit: 0, Index: 2, Loaded: true, Material: "PETG"}}}
}
func (p *trayFake) Loaded() []printer.Loaded {
	return []printer.Loaded{{Material: "PLA", Fraction: -1, Active: true}, {Material: "PETG", Fraction: -1}}
}
func (p *trayFake) SpeedLevels() ([]string, int) { return []string{"Silent", "Standard", "Sport"}, 2 }
func (p *trayFake) SetSpeedLevel(level int) error {
	p.calls = append(p.calls, fmt.Sprint("level ", level))
	return nil
}
func (p *trayFake) LoadTray(slot int) error {
	p.calls = append(p.calls, fmt.Sprint("load ", slot))
	return nil
}
func (p *trayFake) UnloadTray() error { p.calls = append(p.calls, "unload"); return nil }
func (p *trayFake) DryAMS(unit int, filament string, temp, hours int) error {
	p.calls = append(p.calls, fmt.Sprintf("dry %d %s %d %d", unit, filament, temp, hours))
	return nil
}
func (p *trayFake) StopDryAMS(unit int) error {
	p.calls = append(p.calls, fmt.Sprint("stop ", unit))
	return nil
}
func (p *trayFake) SetChamberTemp(t int) error {
	p.calls = append(p.calls, fmt.Sprint("chamber ", t))
	return nil
}
func (p *trayFake) SetTool2Temp(t int) error {
	p.calls = append(p.calls, fmt.Sprint("tool1 ", t))
	return nil
}

func newWith(t *testing.T, p Printer) (*Server, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	s := New(p, &gcodestore.Store{Dir: dir}, mainKey, filepath.Join(dir, ".keys.json"))
	mux := http.NewServeMux()
	s.Register(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return s, ts
}

func post(t *testing.T, url, body string) (int, string) {
	t.Helper()
	r, m := do(t, "POST", url, mainKey, strings.NewReader(body), "application/json")
	e, _ := m["error"].(string)
	return r.StatusCode, e
}

// A printer with no controls answers 404, and lists no controls.
func TestControlsNone(t *testing.T) {
	s, ts := newWith(t, &fakePrinter{st: printer.State{Connected: true}})
	if c := s.controls(); !slices.Equal(c, []string{ControlJog, ControlHome}) {
		t.Errorf("controls %v", c)
	}
	for _, c := range [][2]string{
		{"/api/plugin/printdeck/fan", `{"percent": 50}`},
		{"/api/plugin/printdeck/zoffset", `{"adjust": 0.025}`},
		{"/api/plugin/printdeck/filament", `{"action": "extrude", "mm": 5}`},
		{"/api/printer/chamber", `{"command": "target", "target": 40}`},
		{"/api/printer/printhead", `{"command": "feedrate", "factor": 120}`},
		{"/api/printer/tool", `{"command": "flowrate", "factor": 95}`},
	} {
		if st, _ := post(t, ts.URL+c[0], c[1]); st != 404 {
			t.Errorf("%s: %d", c[0], st)
		}
	}
	if st, _ := post(t, ts.URL+"/api/printer/tool", `{"command":"target","targets":{"tool1":200}}`); st != 404 {
		t.Errorf("tool1: %d", st)
	}
}

// The controls of a Klipper printer: the fan, the speed, the flow, and the filament with the
// lengths of the swap. A print that runs refuses the filament.
func TestControlsKlipper(t *testing.T) {
	p := &klipperFake{fakePrinter: fakePrinter{st: printer.State{Connected: true, Tool: 215}}}
	s, ts := newWith(t, p)
	s.SwapLengths = func() (float64, float64) { return 90, 60 }
	if c := s.controls(); !slices.Equal(c, []string{ControlJog, ControlHome, ControlFan, ControlSpeed, ControlFlow, ControlZOffset, ControlExtrude, ControlFilament}) {
		t.Errorf("controls %v", c)
	}
	for _, c := range []struct {
		path, body string
		status     int
	}{
		{"/api/plugin/printdeck/fan", `{"percent": 50}`, 204},
		{"/api/plugin/printdeck/fan", `{"percent": 101}`, 400},
		{"/api/plugin/printdeck/fan", `{}`, 400},
		{"/api/printer/printhead", `{"command": "feedrate", "factor": 120}`, 204},
		{"/api/printer/printhead", `{"command": "feedrate", "factor": 5}`, 400},
		{"/api/printer/printhead", `{"command": "feedrate", "level": "Sport"}`, 404},
		{"/api/printer/tool", `{"command": "flowrate", "factor": 95}`, 204},
		{"/api/plugin/printdeck/zoffset", `{"adjust": -0.025}`, 204},
		{"/api/plugin/printdeck/zoffset", `{"adjust": 2}`, 400},
		{"/api/plugin/printdeck/filament", `{"action": "extrude", "mm": -10}`, 204},
		{"/api/plugin/printdeck/filament", `{"action": "extrude", "mm": 500}`, 400},
		{"/api/plugin/printdeck/filament", `{"action": "unload"}`, 204},
		{"/api/plugin/printdeck/filament", `{"action": "load"}`, 204},
		{"/api/plugin/printdeck/filament", `{"action": "purge"}`, 400},
	} {
		if st, e := post(t, ts.URL+c.path, c.body); st != c.status {
			t.Errorf("%s %s: %d %q, not %d", c.path, c.body, st, e, c.status)
		}
	}
	if want := []string{"fan 50", "speed 120", "flow 95", "baby -0.025", "extrude -10"}; !slices.Equal(p.calls, want) {
		t.Errorf("calls %v, not %v", p.calls, want)
	}
	want := []string{"M83", "G1 E10 F300", "G1 E-90 F1500", "M82", "M83", "G1 E60 F600", "G1 E20 F150", "M82"}
	if !slices.Equal(p.gcode, want) {
		t.Errorf("gcode %v", p.gcode)
	}
	// A cold nozzle: the reason of the printer.
	p.st.Tool = 25
	if st, e := post(t, ts.URL+"/api/plugin/printdeck/filament", `{"action": "extrude", "mm": 5}`); st != 409 || e != "Extrude below minimum temp" {
		t.Errorf("cold: %d %q", st, e)
	}
	// A print runs: the filament waits. A paused print can change its filament.
	p.st.Tool, p.st.Printing = 215, true
	if st, _ := post(t, ts.URL+"/api/plugin/printdeck/filament", `{"action": "unload"}`); st != 409 {
		t.Errorf("printing: %d", st)
	}
	// Jog and home wait for the end of a print. A paused print can jog, and not home.
	for _, c := range []string{`{"command": "jog", "x": 10}`, `{"command": "home", "axes": ["x"]}`} {
		if st, _ := post(t, ts.URL+"/api/printer/printhead", c); st != 409 {
			t.Errorf("printing %s: %d", c, st)
		}
	}
	p.st.Paused = true
	if st, _ := post(t, ts.URL+"/api/plugin/printdeck/filament", `{"action": "unload"}`); st != 204 {
		t.Errorf("paused: %d", st)
	}
	if st, _ := post(t, ts.URL+"/api/printer/printhead", `{"command": "jog", "x": 10}`); st != 204 {
		t.Errorf("paused jog: %d", st)
	}
	if st, _ := post(t, ts.URL+"/api/printer/printhead", `{"command": "home", "axes": ["x"]}`); st != 409 {
		t.Errorf("paused home: %d", st)
	}
}

// The controls of a printer with an AMS, a chamber and an aux nozzle.
func TestControlsTrays(t *testing.T) {
	p := &trayFake{klipperFake{fakePrinter: fakePrinter{st: printer.State{Connected: true, Tool: 215,
		HasChamber: true, HasTool2: true}}}}
	s, ts := newWith(t, p)
	want := []string{ControlJog, ControlHome, ControlFan, ControlSpeed, ControlFlow, ControlZOffset, ControlExtrude, ControlFilament, ControlDry, ControlChamber, ControlTool1}
	if c := s.controls(); !slices.Equal(c, want) {
		t.Errorf("controls %v", c)
	}
	for _, c := range []struct {
		path, body string
		status     int
	}{
		{"/api/plugin/printdeck/filament", `{"action": "load", "slot": "A3"}`, 204},
		{"/api/plugin/printdeck/filament", `{"action": "load", "tray": 0}`, 204},
		{"/api/plugin/printdeck/filament", `{"action": "load", "slot": "B1"}`, 404},
		{"/api/plugin/printdeck/filament", `{"action": "load"}`, 404},
		{"/api/plugin/printdeck/filament", `{"action": "unload"}`, 204},
		{"/api/plugin/printdeck/ams/A/dry", `{"temp": 55, "hours": 8, "filament": "PLA"}`, 204},
		{"/api/plugin/printdeck/ams/0/dry", `{"temp": 55, "hours": 99}`, 400},
		{"/api/printer/chamber", `{"command": "target", "target": 45}`, 204},
		{"/api/printer/chamber", `{"command": "target", "target": 200}`, 400},
		{"/api/printer/printhead", `{"command": "feedrate", "level": "sport"}`, 204},
		{"/api/printer/printhead", `{"command": "feedrate", "level": "warp"}`, 400},
		{"/api/printer/tool", `{"command":"target","targets":{"tool1":220}}`, 204},
		{"/api/printer/tool", `{"command":"target","targets":{"tool0":210,"tool1":220}}`, 204},
		{"/api/printer/tool", `{"command":"target","targets":{"tool2":220}}`, 400},
	} {
		if st, e := post(t, ts.URL+c.path, c.body); st != c.status {
			t.Errorf("%s %s: %d %q, not %d", c.path, c.body, st, e, c.status)
		}
	}
	r, _ := do(t, "DELETE", ts.URL+"/api/plugin/printdeck/ams/A/dry", mainKey, nil, "")
	if r.StatusCode != 204 {
		t.Errorf("stop dry: %d", r.StatusCode)
	}
	calls := []string{"load 2", "load 0", "unload", "dry 0 PLA 55 8", "chamber 45", "level 3", "tool1 220", "tool1 220", "stop 0"}
	if !slices.Equal(p.calls, calls) {
		t.Errorf("calls %v, not %v", p.calls, calls)
	}
}

// /api/plugin/printdeck lists the controls.
func TestControlsInPlugin(t *testing.T) {
	_, ts := newWith(t, &klipperFake{fakePrinter: fakePrinter{st: printer.State{Connected: true}}})
	r, m := do(t, "GET", ts.URL+"/api/plugin/printdeck", mainKey, nil, "")
	list, _ := m["controls"].([]any)
	if r.StatusCode != 200 || len(list) != 8 || list[0] != "jog" {
		t.Errorf("%d %v", r.StatusCode, m["controls"])
	}
}

// The state of the controls in /api/plugin/printdeck, and tool1 and the chamber in /api/printer.
func TestControlState(t *testing.T) {
	p := &trayFake{klipperFake{fakePrinter: fakePrinter{st: printer.State{Connected: true, Homed: "xyz", X: 10, Y: 20,
		Z: 5, HasZ: true, SpeedPct: 110, ZOffset: -0.05, HasTool2: true, Tool2: 30, Tool2Target: 220, ToolNow: 1,
		HasChamber: true, Chamber: 35, ErrDetail: "Line one (0300_1A00)\nLine two"}}}}
	_, ts := newWith(t, p)
	_, m := do(t, "GET", ts.URL+"/api/plugin/printdeck", mainKey, nil, "")
	pos, _ := m["position"].(map[string]any)
	tool1, _ := m["tool1"].(map[string]any)
	ams, _ := m["ams"].([]any)
	msgs, _ := m["messages"].([]any)
	fils, _ := m["filaments"].([]any)
	if m["homed"] != "xyz" || pos["z"] != 5.0 || m["speed"] != 110.0 || m["z_offset"] != -0.05 || m["speed_level"] != 2.0 ||
		tool1["target"] != 220.0 || m["tool_now"] != 1.0 || len(ams) != 1 || len(msgs) != 2 || len(fils) != 2 {
		t.Errorf("state %v", m)
	}
	if u, _ := ams[0].(map[string]any); u["unit"] != "A" || u["drying"] == nil {
		t.Errorf("ams %v", ams)
	}
	if f, _ := fils[0].(map[string]any); f["active"] != true || f["slot"] != "A1" {
		t.Errorf("filament %v", fils[0])
	}
	_, pr := do(t, "GET", ts.URL+"/api/printer", mainKey, nil, "")
	temp, _ := pr["temperature"].(map[string]any)
	if temp["tool1"] == nil || temp["chamber"] == nil {
		t.Errorf("temperature %v", temp)
	}
}

// The backends of the deck have the controls that the routes call.
var (
	_ fanSetter     = (*printer.Klippy)(nil)
	_ speedSetter   = (*printer.Klippy)(nil)
	_ flowSetter    = (*printer.Klippy)(nil)
	_ extruder      = (*printer.Klippy)(nil)
	_ fanSetter     = (*printer.Bambu)(nil)
	_ speedSetter   = (*printer.Bambu)(nil)
	_ extruder      = (*printer.Bambu)(nil)
	_ babyStepper   = (*printer.Klippy)(nil)
	_ speedLeveler  = (*printer.Bambu)(nil)
	_ trayLoader    = (*printer.Bambu)(nil)
	_ amsDryer      = (*printer.Bambu)(nil)
	_ chamberSetter = (*printer.Bambu)(nil)
	_ tool2Setter   = (*printer.Bambu)(nil)
)
