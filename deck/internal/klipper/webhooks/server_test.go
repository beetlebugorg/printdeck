//go:build linux

package webhooks_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	_ "printdeck/internal/klipper/extras/all"
	"printdeck/internal/klipper/fakemcu"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/msgproto"
	"printdeck/internal/klipper/reactor"
)

// apiClient is a client of the API socket.
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
		c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
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

// The Go host runs the printer of printer/printer.cfg on a fake board, and serves the API on a
// Unix socket. A client gets the state, the status of the objects, and runs G-code.
func TestServerSocket(t *testing.T) {
	dict, err := os.ReadFile("../testdata/klipper.dict")
	if err != nil {
		t.Fatal(err)
	}
	board, err := fakemcu.New(dict)
	if err != nil {
		t.Fatal(err)
	}
	m, ptyPath, err := fakemcu.OpenPty()
	if err != nil {
		t.Skipf("no pseudo-terminals: %v", err)
	}
	defer m.Close()
	board.Handle("stepper_get_position", func(params map[string]msgproto.Value) []string {
		return []string{fmt.Sprintf("stepper_position oid=%d pos=0", params["oid"])}
	})
	go board.Serve(m)

	dir := t.TempDir()
	cfg, err := os.ReadFile("../../../../printer/printer.cfg")
	if err != nil {
		t.Fatal(err)
	}
	cfg = regexp.MustCompile(`(?m)^serial: .*$`).ReplaceAll(cfg, []byte("serial: "+ptyPath))
	cfg = regexp.MustCompile(`(?m)^restart_method: .*$`).ReplaceAll(cfg, nil)
	cfgPath := filepath.Join(dir, "printer.cfg")
	os.WriteFile(cfgPath, cfg, 0o644)
	sock := filepath.Join(dir, "klippy.sock")

	r := reactor.New()
	p := host.New(r, host.StartArgs{"config_file": cfgPath, "apiserver": sock, "start_reason": "startup"})
	done := make(chan string)
	go func() { done <- p.Run() }()
	defer func() {
		r.RegisterAsyncCallback(func(float64) any { p.RequestExit("exit"); return nil }, reactor.Now)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("the host did not exit")
		}
	}()

	var conn net.Conn
	for i := 0; i < 200; i++ {
		if conn, err = net.Dial("unix", sock); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	c := &apiClient{t: t, conn: conn, rd: bufio.NewReader(conn)}

	var state string
	for i := 0; i < 100; i++ {
		info := c.call("info", map[string]any{"client_info": map[string]any{"program": "test"}})
		res, _ := info["result"].(map[string]any)
		state, _ = res["state"].(string)
		if state == "ready" || state == "error" || state == "shutdown" {
			if state != "ready" {
				t.Fatalf("state %s: %v", state, res["state_message"])
			}
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if state != "ready" {
		t.Fatalf("state %q", state)
	}

	sub := c.call("objects/subscribe", map[string]any{
		"objects":           map[string]any{"toolhead": []any{"homed_axes", "position"}, "print_stats": nil},
		"response_template": map[string]any{"method": "status"}})
	status := sub["result"].(map[string]any)["status"].(map[string]any)
	th := status["toolhead"].(map[string]any)
	if th["homed_axes"] != "" {
		t.Errorf("homed_axes %v", th["homed_axes"])
	}
	if status["print_stats"].(map[string]any)["state"] != "standby" {
		t.Errorf("print_stats %v", status["print_stats"])
	}

	res := c.call("gcode/script", map[string]any{"script": "M117 Hello deck"})
	if _, isErr := res["error"]; isErr {
		t.Fatalf("gcode/script: %v", res)
	}
	q := c.call("objects/query", map[string]any{"objects": map[string]any{"display_status": nil}})
	ds := q["result"].(map[string]any)["status"].(map[string]any)["display_status"].(map[string]any)
	if ds["message"] != "Hello deck" {
		t.Errorf("display_status %v", ds)
	}
	res = c.call("gcode/script", map[string]any{"script": "G1 X10"})
	e, _ := res["error"].(map[string]any)
	if e == nil || e["message"] != "Must home axis first: 10.000 0.000 0.000 [0.000]" {
		t.Errorf("G1 before G28: %v", res)
	}
}
