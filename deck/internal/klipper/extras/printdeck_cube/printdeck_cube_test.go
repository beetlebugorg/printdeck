package printdeck_cube_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "printdeck/internal/klipper/extras/all"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
	"printdeck/internal/klipper/simmcu"
)

type apiClient struct {
	t    *testing.T
	conn net.Conn
	rd   *bufio.Reader
	id   int
}

func (c *apiClient) call(method string, params map[string]any) map[string]any {
	c.t.Helper()
	c.id++
	msg, _ := json.Marshal(map[string]any{"id": c.id, "method": method, "params": params})
	if _, err := c.conn.Write(append(msg, 0x03)); err != nil {
		c.t.Fatal(err)
	}
	for {
		c.conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		data, err := c.rd.ReadBytes(0x03)
		if err != nil {
			c.t.Fatalf("%s: %v", method, err)
		}
		var resp map[string]any
		if err := json.Unmarshal(bytes.TrimSuffix(data, []byte{0x03}), &resp); err != nil {
			c.t.Fatal(err)
		}
		if id, ok := resp["id"].(float64); ok && int(id) == c.id {
			return resp
		}
	}
}

// start runs a host on the simulated TAZ Workhorse with a cube, and returns a client of its API.
func start(t *testing.T, cube string) *apiClient {
	t.Helper()
	dir := t.TempDir()
	base, err := os.ReadFile("../../../printer/sim/taz-workhorse.cfg")
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "printer.cfg")
	os.WriteFile(cfgPath, []byte(string(base)+"\n"+cube), 0o644)
	sock := filepath.Join(dir, "klippy.sock")
	r := reactor.New()
	p := host.New(r, host.StartArgs{"config_file": cfgPath, "apiserver": sock, "start_reason": "startup"})
	done := make(chan string)
	go func() { done <- p.Run() }()
	t.Cleanup(func() {
		r.RegisterAsyncCallback(func(float64) any { p.RequestExit("exit"); return nil }, reactor.Now)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("the host did not exit")
		}
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
	t.Cleanup(func() { conn.Close() })
	c := &apiClient{t: t, conn: conn, rd: bufio.NewReader(conn)}
	for i := 0; ; i++ {
		res, _ := c.call("info", map[string]any{})["result"].(map[string]any)
		if res["state"] == "ready" {
			break
		}
		if i > 200 || res["state"] == "error" || res["state"] == "shutdown" {
			t.Fatalf("state %v: %v", res["state"], res["state_message"])
		}
		time.Sleep(50 * time.Millisecond)
	}
	return c
}

func (c *apiClient) script(s string) string {
	res := c.call("gcode/script", map[string]any{"script": s})
	if e, ok := res["error"].(map[string]any); ok {
		return e["message"].(string)
	}
	return ""
}

func (c *apiClient) cube() map[string]any {
	res := c.call("objects/query", map[string]any{"objects": map[string]any{"printdeck_cube": nil}})
	st, _ := res["result"].(map[string]any)["status"].(map[string]any)
	out, _ := st["printdeck_cube"].(map[string]any)
	return out
}

const cubeSection = `[printdeck_cube]
pin: ^!PA1
left: 261
right: 271
back: -14
top: 3
travel_speed: 250
`

// The simulated board keeps the nozzle place of the test before, so each test homes.
const homeHere = "G28"

func TestMeasure(t *testing.T) {
	if testing.Short() {
		t.Skip("the simulator runs in real time: about 90 s")
	}
	for _, slack := range []float64{0, 0.25} {
		simmcu.SetCubeSlack(slack)
		c := start(t, cubeSection)
		if msg := c.script("CUBE_MEASURE"); !strings.Contains(msg, "Home X, Y and Z") {
			t.Fatalf("no home: %q", msg)
		}
		if msg := c.script(homeHere + "\nCUBE_MEASURE"); msg != "" {
			t.Fatalf("slack %.2f: %s", slack, msg)
		}
		got := c.cube()
		for _, k := range []string{"backlash_x", "backlash_y", "backlash_z"} {
			v, _ := got[k].(float64)
			// The steps are 0.01 mm, and the stop after a trigger is a part of a step.
			if math.Abs(v-slack) > 0.015 {
				t.Errorf("slack %.2f: %s %.3f", slack, k, v)
			}
		}
		for k, want := range map[string]float64{"top": 3, "left": 261, "right": 271, "back": -14} {
			if v, _ := got[k].(float64); math.Abs(v-want) > 0.02 {
				t.Errorf("slack %.2f: %s %.3f, want %.0f", slack, k, v, want)
			}
		}
	}
	simmcu.SetCubeSlack(0)
}

// A cube far under its place in the config: the pin of the probe misses it, and the nozzle stops
// over the bed.
func TestNoContact(t *testing.T) {
	if testing.Short() {
		t.Skip("the simulator runs in real time")
	}
	simmcu.SetCubeDrop(6)
	defer simmcu.SetCubeDrop(0)
	c := start(t, cubeSection)
	msg := c.script(homeHere + "\nCUBE_MEASURE")
	if !strings.Contains(msg, "the pin missed the top") {
		t.Fatalf("got %q", msg)
	}
}

// A cube that moved, as after the bed came off: 1.5 mm down and 1.5 mm to +X. The probe finds the
// top, and the side touches find the faces.
func TestMovedCube(t *testing.T) {
	if testing.Short() {
		t.Skip("the simulator runs in real time")
	}
	simmcu.SetCubeDrop(1.5)
	simmcu.SetCubeShift(1.5)
	defer simmcu.SetCubeDrop(0)
	defer simmcu.SetCubeShift(0)
	c := start(t, cubeSection)
	if msg := c.script(homeHere + "\nCUBE_MEASURE"); msg != "" {
		t.Fatal(msg)
	}
	got := c.cube()
	for k, want := range map[string]float64{"top": 1.5, "left": 262.5, "right": 272.5, "back": -14} {
		if v, _ := got[k].(float64); math.Abs(v-want) > 0.02 {
			t.Errorf("%s %.3f, want %.1f", k, v, want)
		}
	}
}

// The pin of the simulated probe triggers z_offset (0.86) over the top of the cube.
func TestProbeOffset(t *testing.T) {
	if testing.Short() {
		t.Skip("the simulator runs in real time")
	}
	c := start(t, cubeSection)
	if msg := c.script(homeHere + "\nCUBE_PROBE_OFFSET"); msg != "" {
		t.Fatal(msg)
	}
	if v, _ := c.cube()["probe_z_offset"].(float64); math.Abs(v-0.86) > 0.015 {
		t.Errorf("probe_z_offset %.3f, want 0.86", v)
	}
	if msg := c.script("CUBE_SAVE_PROBE_OFFSET"); msg != "" {
		t.Fatal(msg)
	}
	res := c.call("objects/query", map[string]any{"objects": map[string]any{"configfile": []string{"save_config_pending_items"}}})
	st := res["result"].(map[string]any)["status"].(map[string]any)["configfile"].(map[string]any)
	items, _ := st["save_config_pending_items"].(map[string]any)
	probe, _ := items["probe"].(map[string]any)
	if z, _ := probe["z_offset"].(string); z < "0.845" || z > "0.875" {
		t.Errorf("pending items %v", items)
	}
}
