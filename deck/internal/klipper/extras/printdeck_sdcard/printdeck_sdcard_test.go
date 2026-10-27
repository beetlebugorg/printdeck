package printdeck_sdcard_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"printdeck/internal/fatfs"
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
		c.conn.SetReadDeadline(time.Now().Add(20 * time.Second))
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

// The host reads the SD card of the simulated board through the board: it lists the root folder
// and a subfolder with the long names, and copies a file of many blocks to the G-code folder.
func TestListAndCopy(t *testing.T) {
	big := bytes.Repeat([]byte("G1 X10 Y10 E0.5\n"), 2000)
	img, err := fatfs.Build([]fatfs.BuildFile{
		{Path: "Printdeck plaque.gcode", Data: big},
		{Path: "parts/hex planter.gcode", Data: []byte("G28\n")},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	simmcu.SetCard(func() []byte { return img })
	defer simmcu.SetCard(nil)

	dir := t.TempDir()
	gcodes := filepath.Join(dir, "gcodes")
	os.Mkdir(gcodes, 0o755)
	base, err := os.ReadFile("../../../printer/sim/taz-workhorse.cfg")
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(base) + "\n[virtual_sdcard]\npath: " + gcodes + "\n\n[printdeck_sdcard]\ncs_pin: PB0\n"
	cfgPath := filepath.Join(dir, "printer.cfg")
	os.WriteFile(cfgPath, []byte(cfg), 0o644)
	sock := filepath.Join(dir, "klippy.sock")

	r := reactor.New()
	p := host.New(r, host.StartArgs{"config_file": cfgPath, "apiserver": sock, "start_reason": "startup"})
	done := make(chan string)
	go func() { done <- p.Run() }()
	defer func() {
		r.RegisterAsyncCallback(func(float64) any { p.RequestExit("exit"); return nil }, reactor.Now)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("the host did not exit")
		}
	}()
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

	list := func(path string) []any {
		res := c.call("printdeck/sdcard/list", map[string]any{"path": path})
		out, ok := res["result"].(map[string]any)
		if !ok {
			t.Fatalf("list %q: %v", path, res)
		}
		return out["files"].([]any)
	}
	root := list("")
	if len(root) != 2 || root[0].(map[string]any)["name"] != "Printdeck plaque.gcode" ||
		root[0].(map[string]any)["size"] != float64(len(big)) || root[1].(map[string]any)["dir"] != true {
		t.Errorf("root %v", root)
	}
	if sub := list("parts"); len(sub) != 1 || sub[0].(map[string]any)["name"] != "hex planter.gcode" {
		t.Errorf("parts %v", sub)
	}

	if res := c.call("printdeck/sdcard/copy", map[string]any{"path": "Printdeck plaque.gcode", "name": "plaque.gcode"}); res["error"] != nil {
		t.Fatalf("copy: %v", res)
	}
	var st map[string]any
	for i := 0; i < 600; i++ {
		q := c.call("objects/query", map[string]any{"objects": map[string]any{"printdeck_sdcard": nil}})
		st = q["result"].(map[string]any)["status"].(map[string]any)["printdeck_sdcard"].(map[string]any)
		if st["copying"] == "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if st["done"] != "plaque.gcode" || st["error"] != "" {
		t.Fatalf("status %v", st)
	}
	got, err := os.ReadFile(filepath.Join(gcodes, "plaque.gcode"))
	if err != nil || !bytes.Equal(got, big) {
		t.Errorf("the copy has %d bytes, want %d (%v)", len(got), len(big), err)
	}

	// An empty slot.
	simmcu.SetCard(nil)
	res := c.call("printdeck/sdcard/list", map[string]any{"path": ""})
	if e, _ := res["error"].(map[string]any); e == nil || e["message"] != "no SD card in the printer" {
		t.Errorf("an empty slot: %v", res)
	}
}
