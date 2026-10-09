package octoprint

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// The commands go to a fake OctoPrint, which records them.
func TestControlCommands(t *testing.T) {
	type call struct {
		Path string
		Body map[string]any
	}
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		json.Unmarshal(b, &m)
		calls = append(calls, call{r.URL.Path, m})
		if r.Header.Get("X-Api-Key") != "k" {
			w.WriteHeader(403)
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()
	c := New(srv.URL, "k")

	c.Home("x", "y")
	c.Jog(1.5, -2, 0, 3000)
	c.Extrude(-80, 1500)
	c.Fan(50)
	c.BabyStep(-0.05)
	c.SetToolTemp(210)

	want := []call{
		{"/api/printer/printhead", map[string]any{"command": "home", "axes": []any{"x", "y"}}},
		{"/api/printer/printhead", map[string]any{"command": "jog", "x": 1.5, "y": -2.0, "z": 0.0, "absolute": false, "speed": 3000.0}},
		{"/api/printer/command", map[string]any{"commands": []any{"M83", "G1 E-80.00 F1500", "M82"}}},
		{"/api/printer/command", map[string]any{"commands": []any{"M106 S127"}}},
		{"/api/printer/command", map[string]any{"commands": []any{"SET_GCODE_OFFSET Z_ADJUST=-0.050 MOVE=1"}}},
		{"/api/printer/tool", map[string]any{"command": "target", "targets": map[string]any{"tool0": 210.0}}},
	}
	for i, w := range want {
		// Poke can add reads of the state; only the POSTs are recorded in order here.
		if i >= len(calls) || !reflect.DeepEqual(calls[i], w) {
			t.Errorf("call %d = %+v, want %+v", i, calls[i], w)
		}
	}
}

func TestControlBusy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(409) }))
	defer srv.Close()
	if err := New(srv.URL, "k").Home(); err == nil || err.Error() != "The printer is busy or not connected" {
		t.Errorf("err = %v", err)
	}
}
