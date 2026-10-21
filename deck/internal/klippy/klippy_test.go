package klippy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"
)

// fakeKlippy answers on a Unix socket like klippy's webhooks.
type fakeKlippy struct {
	t      *testing.T
	path   string
	conns  chan net.Conn
	handle func(req map[string]any) (result any, errMsg string)
}

func newFake(t *testing.T, handle func(map[string]any) (any, string)) *fakeKlippy {
	t.Helper()
	path := filepath.Join(t.TempDir(), "klippy.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	f := &fakeKlippy{t: t, path: path, conns: make(chan net.Conn, 1), handle: handle}
	go func() {
		for {
			nc, err := l.Accept()
			if err != nil {
				return
			}
			f.conns <- nc
			go f.serve(nc)
		}
	}()
	return f
}

func (f *fakeKlippy) serve(nc net.Conn) {
	r := bufio.NewReader(nc)
	for {
		b, err := r.ReadBytes(separator)
		if err != nil {
			return
		}
		var req map[string]any
		json.Unmarshal(b[:len(b)-1], &req)
		result, errMsg := f.handle(req)
		ans := map[string]any{"id": req["id"]}
		if errMsg != "" {
			ans["error"] = map[string]any{"error": "WebRequestError", "message": errMsg}
		} else {
			ans["result"] = result
		}
		write(nc, ans)
	}
}

func write(nc net.Conn, v any) {
	b, _ := json.Marshal(v)
	nc.Write(append(b, separator))
}

func dial(t *testing.T, f *fakeKlippy) *Conn {
	t.Helper()
	c, err := Dial(context.Background(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestCallAndError(t *testing.T) {
	f := newFake(t, func(req map[string]any) (any, string) {
		if req["method"] == "gcode/script" {
			return nil, "Must home axis first"
		}
		return map[string]any{"state": "ready"}, ""
	})
	c := dial(t, f)
	res, err := c.Call(context.Background(), "info", nil)
	if err != nil {
		t.Fatal(err)
	}
	var info struct{ State string }
	json.Unmarshal(res, &info)
	if info.State != "ready" {
		t.Errorf("state %q, want ready", info.State)
	}
	err = c.Script(context.Background(), "G1 X10")
	var kerr *Error
	if !errors.As(err, &kerr) || kerr.Message != "Must home axis first" {
		t.Errorf("error %v, want the klippy message", err)
	}
}

func TestSubscribeMergesUpdates(t *testing.T) {
	f := newFake(t, func(req map[string]any) (any, string) {
		return map[string]any{"eventtime": 1.0, "status": map[string]any{
			"extruder": map[string]any{"temperature": 25.0, "target": 0.0}}}, ""
	})
	c := dial(t, f)
	nc := <-f.conns
	updates := make(chan Status, 1)
	status, err := c.Subscribe(context.Background(), map[string][]string{"extruder": nil},
		func(_ float64, u Status) { updates <- u })
	if err != nil {
		t.Fatal(err)
	}
	write(nc, map[string]any{"key": "printdeck-1", "params": map[string]any{"eventtime": 1.25,
		"status": map[string]any{"extruder": map[string]any{"temperature": 30.5}}}})
	select {
	case u := <-updates:
		status.Merge(u)
	case <-time.After(time.Second):
		t.Fatal("no update")
	}
	if status["extruder"]["temperature"] != 30.5 || status["extruder"]["target"] != 0.0 {
		t.Errorf("merged status %v", status)
	}
}

func TestClosedConnection(t *testing.T) {
	f := newFake(t, func(req map[string]any) (any, string) { return map[string]any{}, "" })
	c := dial(t, f)
	nc := <-f.conns
	nc.Close() // klippy restarts
	select {
	case <-c.Done():
	case <-time.After(time.Second):
		t.Fatal("Done is not closed")
	}
	if _, err := c.Call(context.Background(), "info", nil); !errors.Is(err, ErrClosed) {
		t.Errorf("call on a closed connection: %v", err)
	}
}

func TestSubscribeOutput(t *testing.T) {
	f := newFake(t, func(req map[string]any) (any, string) { return map[string]any{}, "" })
	c := dial(t, f)
	nc := <-f.conns
	lines := make(chan string, 1)
	if err := c.SubscribeOutput(context.Background(), func(l string) { lines <- l }); err != nil {
		t.Fatal(err)
	}
	write(nc, map[string]any{"key": "printdeck-output-1", "params": map[string]any{"response": "ok B:60.0 /60.0"}})
	select {
	case l := <-lines:
		if l != "ok B:60.0 /60.0" {
			t.Errorf("line %q", l)
		}
	case <-time.After(time.Second):
		t.Fatal("no output")
	}
}
