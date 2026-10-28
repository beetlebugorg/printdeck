package system

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeKlippyState answers objects/query on a Unix socket with the state of print_stats, or with
// an error answer for "error", or with no answer for "hang".
func fakeKlippyState(t *testing.T, state string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "pd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "k.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				r := bufio.NewReader(c)
				b, err := r.ReadBytes(0x03)
				if err != nil {
					return
				}
				var req map[string]any
				json.Unmarshal(b[:len(b)-1], &req)
				ans := map[string]any{"id": req["id"]}
				switch state {
				case "hang":
					time.Sleep(10 * time.Second)
					return
				case "error":
					ans["error"] = map[string]any{"error": "WebRequestError", "message": "Klippy not ready"}
				default:
					ans["result"] = map[string]any{"status": map[string]any{"print_stats": map[string]any{"state": state}}}
				}
				out, _ := json.Marshal(ans)
				c.Write(append(out, 0x03))
			}()
		}
	}()
	return path
}

// The helper restarts only when the Klipper host says that no print runs, or when no host runs.
func TestPrinting(t *testing.T) {
	for state, want := range map[string]bool{"printing": true, "paused": true, "standby": false, "complete": false,
		"cancelled": false, "error": false} {
		KlippySocket = fakeKlippyState(t, state)
		if got := printing(); got != want {
			t.Errorf("%s: printing = %v, want %v", state, got, want)
		}
	}
	KlippySocket = filepath.Join(t.TempDir(), "none.sock")
	if printing() {
		t.Error("no socket: printing")
	}
	// A socket file with no process: the host ended.
	dir, _ := os.MkdirTemp("/tmp", "pd")
	defer os.RemoveAll(dir)
	stale := filepath.Join(dir, "k.sock")
	l, _ := net.Listen("unix", stale)
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	l.Close()
	KlippySocket = stale
	if printing() {
		t.Error("a socket with no process: printing")
	}
}

func TestPrintingNoAnswer(t *testing.T) {
	if testing.Short() {
		t.Skip("waits 5 s")
	}
	KlippySocket = fakeKlippyState(t, "hang")
	if !printing() {
		t.Error("a host with no answer: no print")
	}
}
