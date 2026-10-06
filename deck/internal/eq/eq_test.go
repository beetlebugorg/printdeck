package eq

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func TestPatchPreamp(t *testing.T) {
	p := Patch(Gains{6, -3, 0, 2, 0})
	f := p["filters"].(map[string]any)
	gain := func(name string) float64 {
		return f[name].(map[string]any)["parameters"].(map[string]any)["gain"].(float64)
	}
	if gain("preamp") != -6 || gain("eq_bass") != 6 || gain("eq_lowmid") != -3 {
		t.Errorf("patch %v", p)
	}
	// Only cuts: no preamp change.
	if g := Patch(Gains{-4, 0, 0, 0, -2})["filters"].(map[string]any)["preamp"].(map[string]any)["parameters"].(map[string]any)["gain"]; g != -0.0 {
		t.Errorf("preamp for cuts = %v", g)
	}
}

func TestSetGainSteps(t *testing.T) {
	e := New("x", t.TempDir())
	e.SetGain(0, 3.3)
	e.SetGain(1, 40)
	if g := e.Gains(); g[0] != 3.5 || g[1] != MaxGain {
		t.Errorf("gains %v", g)
	}
	e.SetPreset("Bass")
	if e.Preset() != "Bass" {
		t.Errorf("preset %q", e.Preset())
	}
}

// TestLive sends a setting to a running CamillaDSP and reads it back. CAMILLA=127.0.0.1:1234 runs it.
func TestLive(t *testing.T) {
	addr := os.Getenv("CAMILLA")
	if addr == "" {
		t.Skip()
	}
	dir := t.TempDir()
	e := New(addr, dir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go e.Run(ctx, func() {})
	e.SetGain(0, 4)
	time.Sleep(500 * time.Millisecond)

	ws, err := websocket.Dial("ws://"+addr, "", "http://127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	websocket.JSON.Send(ws, "GetConfigJson")
	var r struct{ GetConfigJson struct{ Value string } }
	websocket.JSON.Receive(ws, &r)
	ws.Close()
	var cfg struct {
		Filters map[string]struct{ Parameters struct{ Gain float64 } }
	}
	json.Unmarshal([]byte(r.GetConfigJson.Value), &cfg)
	if cfg.Filters["eq_bass"].Parameters.Gain != 4 || cfg.Filters["preamp"].Parameters.Gain != -4 {
		t.Errorf("CamillaDSP has bass %v, preamp %v", cfg.Filters["eq_bass"].Parameters.Gain, cfg.Filters["preamp"].Parameters.Gain)
	}
	if !e.Connected() {
		t.Error("not connected")
	}
	if _, err := os.Stat(dir + "/eq.json"); err != nil {
		t.Error("setting not saved")
	}
	// Back to flat.
	e.SetPreset("Flat")
	time.Sleep(500 * time.Millisecond)
}
