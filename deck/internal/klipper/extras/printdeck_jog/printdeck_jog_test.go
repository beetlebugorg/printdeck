package printdeck_jog_test

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
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
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

func (c *apiClient) script(s string) string {
	c.t.Helper()
	res := c.call("gcode/script", map[string]any{"script": s})
	if e, ok := res["error"].(map[string]any); ok {
		return e["message"].(string)
	}
	return ""
}

func (c *apiClient) hold(id int, hold bool) {
	c.t.Helper()
	if res := c.call("printdeck/jog", map[string]any{"id": id, "hold": hold}); res["error"] != nil {
		c.t.Fatalf("hold: %v", res)
	}
}

// pos is the X, Y and Z of the toolhead and of the G-code.
func (c *apiClient) pos() (th, g [3]float64) {
	c.t.Helper()
	res := c.call("objects/query", map[string]any{"objects": map[string]any{"toolhead": []any{"position"},
		"gcode_move": []any{"gcode_position"}}})
	st := res["result"].(map[string]any)["status"].(map[string]any)
	a := st["toolhead"].(map[string]any)["position"].([]any)
	b := st["gcode_move"].(map[string]any)["gcode_position"].([]any)
	for i := range 3 {
		th[i], g[i] = a[i].(float64), b[i].(float64)
	}
	return
}

// start runs a host on the simulated TAZ Workhorse, and returns two clients of its API: one for
// PRINTDECK_JOG, one for the holds.
func start(t *testing.T) (*apiClient, *apiClient) {
	t.Helper()
	dir := t.TempDir()
	base, err := os.ReadFile("../../../printer/sim/taz-workhorse.cfg")
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "printer.cfg")
	os.WriteFile(cfgPath, []byte(string(base)+"\n[printdeck_jog]\n"), 0o644)
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
	dial := func() *apiClient {
		var conn net.Conn
		var err error
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
		return &apiClient{t: t, conn: conn, rd: bufio.NewReader(conn)}
	}
	c := dial()
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
	return c, dial()
}

// jog runs PRINTDECK_JOG in the background, and returns a channel with its error and the time
// when it ended.
type jogEnd struct {
	err string
	at  time.Time
}

func jog(c *apiClient, cmd string) chan jogEnd {
	out := make(chan jogEnd, 1)
	go func() {
		e := c.script(cmd)
		out <- jogEnd{e, time.Now()}
	}()
	return out
}

func TestHoldMove(t *testing.T) {
	if testing.Short() {
		t.Skip("the simulator runs in real time: about 30 s")
	}
	c, h := start(t)
	if e := c.script("G28\nG90\nG1 X100 Y100 Z50 F6000\nM400"); e != "" {
		t.Fatal(e)
	}

	// 1. A hold of 1 s at 20 mm/s. The move stops a short time after the release.
	done := jog(c, "PRINTDECK_JOG ID=1 AXIS=X DIR=1 SPEED=20")
	for t0 := time.Now(); time.Since(t0) < time.Second; time.Sleep(100 * time.Millisecond) {
		h.hold(1, true)
	}
	h.hold(1, false)
	released := time.Now()
	end := <-done
	if end.err != "" {
		t.Fatal(end.err)
	}
	th, g := c.pos()
	t.Logf("hold 1 s: X %.3f, the command ended %.2f s after the release", th[0], end.at.Sub(released).Seconds())
	if th[0] < 115 || th[0] > 135 || th[1] != 100 || th[2] != 50 {
		t.Errorf("toolhead %v after a hold of 1 s at 20 mm/s from X 100", th)
	}
	if g != th {
		t.Errorf("G-code position %v, toolhead %v", g, th)
	}
	if d := end.at.Sub(released); d > 600*time.Millisecond {
		t.Errorf("the move ended %v after the release", d)
	}

	// 2. A release before the command: no move.
	h.hold(2, false)
	if e := c.script("PRINTDECK_JOG ID=2 AXIS=Y DIR=-1 SPEED=20"); e != "" {
		t.Fatal(e)
	}
	if th2, _ := c.pos(); th2 != th {
		t.Errorf("a released jog moved the toolhead from %v to %v", th, th2)
	}

	// 3. No hold: the move stops by itself.
	t0 := time.Now()
	end = <-jog(c, "PRINTDECK_JOG ID=3 AXIS=Y DIR=-1 SPEED=10")
	th3, _ := c.pos()
	t.Logf("no hold: Y %.3f after %.2f s", th3[1], end.at.Sub(t0).Seconds())
	if end.err != "" || th3[1] >= 100 || th3[1] < 85 {
		t.Errorf("no hold: %q, toolhead %v", end.err, th3)
	}

	// 4. A hold past the limit: the toolhead stops at the limit.
	done = jog(c, "PRINTDECK_JOG ID=4 AXIS=X DIR=1 SPEED=150")
	stop := time.After(4 * time.Second)
loop:
	for {
		select {
		case end = <-done:
			break loop
		case <-stop:
			t.Fatal("the move did not stop at the limit")
		case <-time.After(100 * time.Millisecond):
			h.hold(4, true)
		}
	}
	th4, _ := c.pos()
	if end.err != "" || math.Abs(th4[0]-293) > 1e-6 {
		t.Errorf("limit: %q, toolhead %v", end.err, th4)
	}

	// 5. A normal move after the jogs goes to its place.
	if e := c.script("G1 X150 Y120 F6000\nM400"); e != "" {
		t.Fatal(e)
	}
	if th5, _ := c.pos(); th5[0] != 150 || th5[1] != 120 {
		t.Errorf("after the jogs: %v", th5)
	}
}

// The axis must be homed.
func TestHoldNeedsHome(t *testing.T) {
	c, _ := start(t)
	if e := c.script("PRINTDECK_JOG ID=1 AXIS=X DIR=1 SPEED=20"); e == "" {
		t.Error("PRINTDECK_JOG moved an axis that is not homed")
	}
}
