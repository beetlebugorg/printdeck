package printer

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"printdeck/internal/klippy"
)

func TestKlippyState(t *testing.T) {
	st := klippyState(klippy.Status{
		"print_stats":    {"state": "printing", "filename": "parts/cap.gcode", "print_duration": 600.0},
		"virtual_sdcard": {"file_size": 1000.0, "file_position": 250.0, "progress": 0.25},
		"extruder":       {"temperature": 209.6, "target": 210.0},
		"heater_bed":     {"temperature": 60.0, "target": 60.0},
		"fan":            {"speed": 0.5},
		"toolhead":       {"position": []any{10.0, 20.0, 3.6, 100.0}},
	}, "")
	want := State{Reachable: true, Connected: true, Text: "Printing", Printing: true, File: "cap.gcode",
		FilePath: "parts/cap.gcode", FileSize: 1000, FilePos: 250, Completion: 25, TimeLeft: 30 * time.Minute,
		HasTimeLeft: true, Tool: 209.6, ToolTarget: 210, Bed: 60, BedTarget: 60, Fan: 50, HasFan: true, Z: 3.6, HasZ: true}
	if st != want {
		t.Errorf("printing:\n got %+v\nwant %+v", st, want)
	}
	if st := klippyState(klippy.Status{"print_stats": {"state": "paused"}}, ""); !st.Paused || !st.Busy() || st.Text != "Paused" {
		t.Errorf("paused: %+v", st)
	}
	if st := klippyState(klippy.Status{"print_stats": {"state": "complete", "filename": "x.gcode"}}, ""); st.Busy() || st.File != "" {
		t.Errorf("complete: %+v", st)
	}
	if st := klippyState(nil, "mcu 'mcu': Unable to connect"); st.Connected || st.Err != "mcu 'mcu': Unable to connect" {
		t.Errorf("down: %+v", st)
	}
}

func TestCommandsGcode(t *testing.T) {
	if g := HomeGcode("x", "y"); g != "G28 X Y" {
		t.Errorf("home %q", g)
	}
	if g := strings.Join(JogGcode(1.5, -2, 0, 3000), "|"); g != "G91|G1 X1.500 Y-2.000 F3000|G90" {
		t.Errorf("jog %q", g)
	}
	if JogGcode(0, 0, 0, 3000) != nil {
		t.Error("a jog of nothing makes G-code")
	}
	if g := FanGcode(50); g != "M106 S127" {
		t.Errorf("fan %q", g)
	}
}

// fakeKlippy records the G-code scripts and answers like a ready klippy.
type fakeKlippy struct {
	mu      sync.Mutex
	scripts []string
	methods []string
}

func (f *fakeKlippy) serve(t *testing.T) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "klippy.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			nc, err := l.Accept()
			if err != nil {
				return
			}
			go f.conn(nc)
		}
	}()
	return sock
}

func (f *fakeKlippy) conn(nc net.Conn) {
	r := bufio.NewReader(nc)
	for {
		b, err := r.ReadBytes(0x03)
		if err != nil {
			return
		}
		var req struct {
			ID     int            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		json.Unmarshal(b[:len(b)-1], &req)
		f.mu.Lock()
		f.methods = append(f.methods, req.Method)
		if req.Method == "gcode/script" {
			f.scripts = append(f.scripts, req.Params["script"].(string))
		}
		f.mu.Unlock()
		var result any = map[string]any{}
		switch req.Method {
		case "info":
			result = map[string]any{"state": "ready", "state_message": "Printer is ready"}
		case "objects/subscribe":
			result = map[string]any{"eventtime": 1.0, "status": map[string]any{
				"print_stats": map[string]any{"state": "standby"},
				"extruder":    map[string]any{"temperature": 24.0, "target": 0.0},
			}}
		}
		ans, _ := json.Marshal(map[string]any{"id": req.ID, "result": result})
		nc.Write(append(ans, 0x03))
	}
}

func (f *fakeKlippy) got() ([]string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.scripts...), append([]string(nil), f.methods...)
}

type emptyStore struct{}

func (emptyStore) Open(string) (io.ReadCloser, error) { return nil, fs.ErrNotExist }
func (emptyStore) Models() []File                     { return nil }
func (emptyStore) Files() []File                      { return []File{{Path: "cap.gcode", Name: "cap.gcode"}} }
func (emptyStore) Stat(string) (fs.FileInfo, error)   { return nil, fs.ErrNotExist }

func TestKlippyBackend(t *testing.T) {
	f := &fakeKlippy{}
	k := NewKlippy(f.serve(t), emptyStore{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{}, 1)
	go k.Run(ctx, func() {
		if k.State().Connected {
			select {
			case ready <- struct{}{}:
			default:
			}
		}
	})
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatalf("the backend did not connect: %+v", k.State())
	}
	if st := k.State(); st.Text != "Operational" || st.Tool != 24 {
		t.Errorf("state %+v", st)
	}
	if files := k.Files(); len(files) != 1 {
		t.Errorf("files %+v", files)
	}

	k.Home()
	k.Jog(0, 0, 0.5, 600)
	k.SetToolTemp(210)
	k.BabyStep(-0.05)
	k.EmergencyStop()
	scripts, methods := f.got()
	want := []string{"G28", "G91\nG1 Z0.500 F600\nG90", "M104 S210", "SET_GCODE_OFFSET Z_ADJUST=-0.050 MOVE=1"}
	if strings.Join(scripts, "|") != strings.Join(want, "|") {
		t.Errorf("scripts\n got %q\nwant %q", scripts, want)
	}
	if methods[len(methods)-1] != "emergency_stop" {
		t.Errorf("last method %q, want emergency_stop", methods[len(methods)-1])
	}
}

func TestKlippyNotRunning(t *testing.T) {
	k := NewKlippy(filepath.Join(t.TempDir(), "none.sock"), emptyStore{})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	k.Run(ctx, func() {})
	if st := k.State(); st.Connected || st.Err != "Klipper is not running" {
		t.Errorf("state %+v", st)
	}
	if err := k.Home(); err != ErrNotReady {
		t.Errorf("home with no Klipper: %v", err)
	}
}
