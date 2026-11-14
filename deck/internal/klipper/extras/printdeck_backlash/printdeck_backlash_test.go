package printdeck_backlash_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"math"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "printdeck/internal/klipper/extras/all"
	"printdeck/internal/klipper/extras/printdeck_backlash"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
)

func near(t *testing.T, step string, got, want []float64) {
	t.Helper()
	for i := range want {
		if math.Abs(got[i]-want[i]) > 1e-9 {
			t.Fatalf("%s: got %v, want %v", step, got[:len(want)], want)
		}
	}
}

func TestComp(t *testing.T) {
	c := printdeck_backlash.Comp{Backlash: [2]float64{0.2, 0.3}, HomeDir: [2]float64{-1, -1},
		Min: [2]float64{0, 0}, Max: [2]float64{100, 100}}
	near(t, "start", c.Position([]float64{10, 10, 5, 0}), []float64{10, 10, 5, 0})
	// Away from home: the correction is added.
	near(t, "+X", c.Apply([]float64{20, 10, 5, 0}), []float64{20.2, 10, 5, 0})
	near(t, "+X again", c.Apply([]float64{30, 10, 5, 1}), []float64{30.2, 10, 5, 1})
	near(t, "position", c.Position([]float64{30.2, 10, 5, 1}), []float64{30, 10, 5, 1})
	// Toward home: no correction.
	near(t, "-X", c.Apply([]float64{25, 10, 5, 1}), []float64{25, 10, 5, 1})
	// Y only: X keeps its state.
	near(t, "+Y", c.Apply([]float64{25, 20, 5, 1}), []float64{25, 20.3, 5, 1})
	near(t, "+X +Y", c.Apply([]float64{26, 21, 5, 1}), []float64{26.2, 21.3, 5, 1})
	// The limits.
	near(t, "limit", c.Apply([]float64{100, 21, 5, 1}), []float64{100, 21.3, 5, 1})
	near(t, "limit position", c.Position([]float64{100, 21.3, 5, 1}), []float64{99.8, 21, 5, 1})
	// A move of another module resets the correction.
	near(t, "reset", c.Position([]float64{50, 50, 5, 1}), []float64{50, 50, 5, 1})
	near(t, "after reset -X", c.Apply([]float64{40, 50, 5, 1}), []float64{40, 50, 5, 1})

	h := printdeck_backlash.Comp{Backlash: [2]float64{0.2, 0}, HomeDir: [2]float64{1, -1}, Min: [2]float64{0, 0}, Max: [2]float64{100, 100}}
	h.Position([]float64{50, 50, 0, 0})
	near(t, "home +X, move -X", h.Apply([]float64{40, 50, 0, 0}), []float64{39.8, 50, 0, 0})
	near(t, "home +X, move +X", h.Apply([]float64{45, 50, 0, 0}), []float64{45, 50, 0, 0})
}

type client struct {
	t  *testing.T
	c  net.Conn
	rd *bufio.Reader
	id int
}

func (c *client) call(method string, params map[string]any) map[string]any {
	c.t.Helper()
	c.id++
	msg, _ := json.Marshal(map[string]any{"id": c.id, "method": method, "params": params})
	c.c.Write(append(msg, 0x03))
	for {
		c.c.SetReadDeadline(time.Now().Add(60 * time.Second))
		data, err := c.rd.ReadBytes(0x03)
		if err != nil {
			c.t.Fatalf("%s: %v", method, err)
		}
		var resp map[string]any
		json.Unmarshal(bytes.TrimSuffix(data, []byte{0x03}), &resp)
		if id, ok := resp["id"].(float64); ok && int(id) == c.id {
			return resp
		}
	}
}

func (c *client) script(s string) {
	c.t.Helper()
	res := c.call("gcode/script", map[string]any{"script": s})
	if e, ok := res["error"]; ok {
		c.t.Fatalf("%s: %v", s, e)
	}
}

func (c *client) positions() (toolhead, gcode []float64) {
	res := c.call("objects/query", map[string]any{"objects": map[string]any{"toolhead": []string{"position"}, "gcode_move": []string{"position"}}})
	st := res["result"].(map[string]any)["status"].(map[string]any)
	conv := func(v any) []float64 {
		var out []float64
		for _, x := range v.([]any) {
			out = append(out, x.(float64))
		}
		return out
	}
	return conv(st["toolhead"].(map[string]any)["position"]), conv(st["gcode_move"].(map[string]any)["position"])
}

// The correction goes through gcode_move and bed_mesh to the toolhead, and the G-code position
// stays without it.
func TestMoves(t *testing.T) {
	if testing.Short() {
		t.Skip("the simulator runs in real time")
	}
	dir := t.TempDir()
	base, err := os.ReadFile("../../../printer/sim/taz-workhorse.cfg")
	if err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "printer.cfg")
	os.WriteFile(cfg, append(base, []byte("\n[printdeck_backlash]\nx: 0.25\ny: 0.1\n")...), 0o644)
	sock := filepath.Join(dir, "klippy.sock")
	r := reactor.New()
	p := host.New(r, host.StartArgs{"config_file": cfg, "apiserver": sock, "start_reason": "startup"})
	done := make(chan string)
	go func() { done <- p.Run() }()
	t.Cleanup(func() {
		r.RegisterAsyncCallback(func(float64) any { p.RequestExit("exit"); return nil }, reactor.Now)
		<-done
	})
	var conn net.Conn
	for i := 0; i < 500; i++ {
		if conn, err = net.Dial("unix", sock); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	c := &client{t: t, c: conn, rd: bufio.NewReader(conn)}
	for i := 0; ; i++ {
		res, _ := c.call("info", map[string]any{})["result"].(map[string]any)
		if res["state"] == "ready" {
			break
		}
		if i > 200 {
			t.Fatalf("state %v: %v", res["state"], res["state_message"])
		}
		time.Sleep(50 * time.Millisecond)
	}
	c.script("SET_KINEMATIC_POSITION X=100 Y=100 Z=50\nG90\nG1 X110 Y110 F6000")
	th, gm := c.positions()
	near(t, "+X +Y toolhead", th, []float64{110.25, 110.1, 50})
	near(t, "+X +Y gcode", gm, []float64{110, 110, 50})
	c.script("G1 X105 Y105")
	th, gm = c.positions()
	near(t, "-X -Y toolhead", th, []float64{105, 105, 50})
	near(t, "-X -Y gcode", gm, []float64{105, 105, 50})
	c.script("SET_BACKLASH X=0\nG1 X120")
	th, _ = c.positions()
	near(t, "X 0", th, []float64{120, 105, 50})
}
