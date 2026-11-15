package printdeck_homing_test

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

	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"

	_ "printdeck/internal/klipper/extras/all"
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
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
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

// start starts a host on the simulated TAZ with extra config, and returns a client of it. The host
// stops at the end of the test.
func start(t *testing.T, extra string) *apiClient {
	dir := t.TempDir()
	base, err := os.ReadFile("../../../printer/sim/taz-workhorse.cfg")
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "printer.cfg")
	os.WriteFile(cfgPath, []byte(string(base)+extra), 0o644)
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

// script runs G-code, and returns its error message ("" when it runs).
func (c *apiClient) script(gcode string) string {
	res := c.call("gcode/script", map[string]any{"script": gcode})
	if e, ok := res["error"].(map[string]any); ok {
		return e["message"].(string)
	}
	return ""
}

// toolhead returns the status of the toolhead.
func (c *apiClient) toolhead() map[string]any {
	res := c.call("objects/query", map[string]any{"objects": map[string]any{"toolhead": nil}})
	st, _ := res["result"].(map[string]any)["status"].(map[string]any)
	th, _ := st["toolhead"].(map[string]any)
	return th
}

// run starts a host on the simulated TAZ with extra config, homes Z, and returns the error message of
// the home ("" when it homes).
func run(t *testing.T, extra string) string {
	return start(t, extra).script("G28 Z")
}

// The second Z motor has a switch at another place than the first: in the simulated mechanics its
// pin (PA2) has no place, so it triggers at the start of the move, and the switch of stepper_z is far
// away. With max_skew, the home stops 3 mm after the first switch. Without it, the other stepper goes
// on to its switch, as in Klipper.
const z1 = `
[stepper_z1]
step_pin: PC4
dir_pin: PL7
enable_pin: !PA3
microsteps: 16
rotation_distance: 6.4
endstop_pin: ^PA2
`

func TestSkewStopsHome(t *testing.T) {
	msg := run(t, z1+"\n[printdeck_homing]\nmax_skew: 3\n")
	if !strings.HasPrefix(msg, "Homing stopped: ") || !strings.Contains(msg, "did not trigger within 3.0 mm") ||
		!strings.Contains(msg, "z1") || !strings.Contains(msg, "stepper_z ") {
		t.Errorf("home error %q", msg)
	}
	// Without the guard, the home ends with no error: stepper_z1 stopped at the start and stepper_z
	// moved on to its switch, about 150 mm. On the printer, that twists the gantry.
	if msg = run(t, z1); msg != "" {
		t.Errorf("home error without the guard %q", msg)
	}
}

// homeXY homes Z, moves the toolhead to X 200 Y 250, and homes X and Y. It returns the position
// after the home, and the time of the XY home on the printer (the print time of the toolhead).
func homeXY(t *testing.T, extra string) ([]any, float64) {
	c := start(t, extra)
	if msg := c.script("G28\nG90\nG1 X200 Y250 F6000\nM400"); msg != "" {
		t.Fatal(msg)
	}
	before := c.toolhead()["print_time"].(float64)
	if msg := c.script("G28 X Y\nM400"); msg != "" {
		t.Fatal(msg)
	}
	th := c.toolhead()
	if th["homed_axes"] != "xyz" {
		t.Errorf("homed axes %v", th["homed_axes"])
	}
	return th["position"].([]any), th["print_time"].(float64) - before
}

// X and Y in one move end at the same position as the home of each axis, in less time.
func TestHomeXYTogether(t *testing.T) {
	pos, one := homeXY(t, "\n[printdeck_homing]\nhome_xy_together: True\n")
	want, two := homeXY(t, "\n[printdeck_homing]\n")
	for i := 0; i < 3; i++ {
		if math.Abs(pos[i].(float64)-want[i].(float64)) > 1e-6 {
			t.Errorf("position %v, want %v", pos, want)
			break
		}
	}
	t.Logf("X and Y home in %.1f s together, %.1f s one after the other", one, two)
	if one >= two*0.8 {
		t.Errorf("the home in one move lasts %.1f s, the home of each axis %.1f s", one, two)
	}
}
