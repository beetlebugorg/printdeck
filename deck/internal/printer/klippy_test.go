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
		"print_stats": {"state": "printing", "filename": "parts/cap.gcode", "print_duration": 600.0,
			"total_duration": 660.0, "filament_used": 812.5},
		"virtual_sdcard": {"file_size": 1000.0, "file_position": 250.0, "progress": 0.25},
		"extruder":       {"temperature": 209.6, "target": 210.0},
		"heater_bed":     {"temperature": 60.0, "target": 60.0},
		"fan":            {"speed": 0.5},
		"toolhead":       {"position": []any{10.0, 20.0, 3.6, 100.0}, "homed_axes": "xyz"},
		"display_status": {"message": "Layer 3"},
		"gcode_move":     {"homing_origin": []any{0.0, 0.0, -0.05, 0.0}, "speed_factor": 1.1, "extrude_factor": 0.95},
		"configfile":     {"save_config_pending": true},
	}, "")
	want := State{Reachable: true, Connected: true, Text: "Printing", Printing: true, File: "cap.gcode",
		FilePath: "parts/cap.gcode", FileSize: 1000, FilePos: 250, Completion: 25, TimeLeft: 30 * time.Minute,
		HasTimeLeft: true, Tool: 209.6, ToolTarget: 210, Bed: 60, BedTarget: 60, Fan: 50, HasFan: true, Z: 3.6, HasZ: true,
		X: 10, Y: 20, Homed: "xyz", Elapsed: 11 * time.Minute, PrintDuration: 10 * time.Minute, FilamentUsed: 812.5, Message: "Layer 3",
		ZOffset: -0.05, SpeedPct: 110, FlowPct: 95, SaveConfigPending: true}
	if st != want {
		t.Errorf("printing:\n got %+v\nwant %+v", st, want)
	}
	if st := klippyState(klippy.Status{"print_stats": {"state": "paused"}}, ""); !st.Paused || !st.Busy() || st.Text != "Paused" {
		t.Errorf("paused: %+v", st)
	}
	st = klippyState(klippy.Status{"print_stats": {"state": "complete", "filename": "parts/x.gcode",
		"total_duration": 3720.0, "filament_used": 4100.0}}, "")
	if st.Busy() || st.File != "" || st.Finished != "complete" || st.FinishedFile != "x.gcode" ||
		st.FinishedPath != "parts/x.gcode" || st.FinishedTime != 62*time.Minute || st.FinishedFilament != 4100 {
		t.Errorf("complete: %+v", st)
	}
	if st := klippyState(klippy.Status{"print_stats": {"state": "error", "filename": "x.gcode", "message": "Heater fault"}}, ""); st.Finished != "error" || st.FinishedError != "Heater fault" {
		t.Errorf("error: %+v", st)
	}
	if st := klippyState(klippy.Status{"print_stats": {"state": "standby"}}, ""); st.Finished != "" || st.SpeedPct != 100 {
		t.Errorf("standby: %+v", st)
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
		case "objects/query":
			result = map[string]any{"eventtime": 1.0, "status": map[string]any{"toolhead": map[string]any{
				"homed_axes": "xyz", "position": []any{10.0, 20.0, 0.3, 0.0},
				"axis_minimum": []any{-20.0, -10.0, 0.0, 0.0}, "axis_maximum": []any{300.0, 290.0, 290.0, 0.0}}}}
		case "objects/subscribe":
			result = map[string]any{"eventtime": 1.0, "status": map[string]any{
				"print_stats": map[string]any{"state": "standby"},
				"extruder":    map[string]any{"temperature": 24.0, "target": 0.0},
			}}
		}
		ans, _ := json.Marshal(map[string]any{"id": req.ID, "result": result})
		nc.Write(append(ans, 0x03))
		if req.Method == "objects/subscribe" {
			// Klipper can send an update at once, before the deck asks for anything else.
			key := req.Params["response_template"].(map[string]any)["key"]
			u, _ := json.Marshal(map[string]any{"key": key, "params": map[string]any{"eventtime": 1.1,
				"status": map[string]any{"extruder": map[string]any{"temperature": 30.0}}}})
			nc.Write(append(u, 0x03))
		}
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
func (emptyStore) FullPath(string) (string, error)    { return "", fs.ErrNotExist }
func (emptyStore) Delete(string) error                { return fs.ErrNotExist }

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
	// The update after the subscription is in the state.
	if st := k.State(); st.Text != "Operational" || st.Tool != 30 {
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

func TestRunAction(t *testing.T) {
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
	<-ready
	if err := k.RunAction("Bed tilt", "BED_TILT_CALIBRATE"); err != nil {
		t.Fatal(err)
	}
	st := k.State()
	if st.Action != "" || st.LastAction.Name != "Bed tilt" || st.LastAction.Err != "" || st.LastAction.End.IsZero() {
		t.Errorf("state after the action: %q %+v", st.Action, st.LastAction)
	}
	if scripts, _ := f.got(); scripts[len(scripts)-1] != "BED_TILT_CALIBRATE" {
		t.Errorf("scripts %q", scripts)
	}
}

// A cancel stops the file, then turns off the heaters and the part fan (CANCEL_PRINT of Klipper
// leaves them on), lifts the nozzle, and parks it at the middle and the top when the print is cool.
func TestCancelTurnsOffHeaters(t *testing.T) {
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
	<-ready
	k.Cancel()
	want := "pause_resume/cancel|gcode/script|gcode/script|gcode/script|gcode/script"
	wantScripts := "TURN_OFF_HEATERS|M107|G91\nG1 Z10.00 F600\nG90|G90\nG1 Z280.0 F1200\nG1 X140.0 Y140.0 F3000"
	var got string
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		scripts, methods := f.got()
		var tail []string
		for _, m := range methods {
			if m == "pause_resume/cancel" || m == "gcode/script" {
				tail = append(tail, m)
			}
		}
		got = strings.Join(tail, "|")
		if got == want && strings.Join(scripts, "|") == wantScripts {
			return
		}
	}
	scripts, _ := f.got()
	t.Errorf("calls %q, scripts %q; want %q, scripts %q", got, scripts, want, wantScripts)
}
