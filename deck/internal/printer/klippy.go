package printer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"sync"
	"time"

	"printdeck/internal/klippy"
)

// Store is the file store of the Klipper backend: the folder of Klipper's [virtual_sdcard].
type Store interface {
	Source
	Files() []File
	Stat(path string) (fs.FileInfo, error)
}

// Klippy is the backend for the Python Klipper host. It follows klippy over its API socket, and
// prints the files of the store with Klipper's virtual SD card. It goes away when Printdeck has its
// own Go port of the Klipper host.
type Klippy struct {
	socket string
	store  Store
	*View

	mu     sync.Mutex
	conn   *klippy.Conn
	status klippy.Status
	err    string // why Klipper is not ready
	st     State
	files  []File
}

// klippyObjects are the printer objects that the deck follows. nil means all fields.
var klippyObjects = map[string][]string{
	"webhooks":       {"state", "state_message"},
	"print_stats":    {"state", "filename", "print_duration", "message"},
	"virtual_sdcard": {"file_position", "file_size", "progress"},
	"extruder":       {"temperature", "target"},
	"heater_bed":     {"temperature", "target"},
	"fan":            {"speed"},
	"toolhead":       {"position", "homed_axes"},
}

// controlTimeout is how long a control waits for Klipper. Homing and heating can take minutes.
const controlTimeout = 5 * time.Minute

// ErrNotReady is the error of a control while Klipper is not ready.
var ErrNotReady = errors.New("Klipper is not ready")

func NewKlippy(socket string, store Store) *Klippy {
	k := &Klippy{socket: socket, store: store, err: "Klipper is not running"}
	k.View = NewView(store)
	return k
}

func (k *Klippy) State() State {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.st
}

func (k *Klippy) Files() []File {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.files
}

func (k *Klippy) LoadFiles() {
	files := k.store.Files()
	k.mu.Lock()
	k.files = files
	k.mu.Unlock()
}

// Run follows klippy until ctx ends. It connects again after klippy restarts. notify is called after
// each change of the state.
func (k *Klippy) Run(ctx context.Context, notify func()) {
	for ctx.Err() == nil {
		k.follow(ctx, notify)
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
}

// follow connects, waits until Klipper is ready, and subscribes to its objects until the connection
// ends.
func (k *Klippy) follow(ctx context.Context, notify func()) {
	conn, err := klippy.Dial(ctx, k.socket)
	if err != nil {
		k.setDown("Klipper is not running", notify)
		return
	}
	defer conn.Close()
	for {
		state, msg, err := info(ctx, conn)
		if err != nil {
			k.setDown("Klipper is not running", notify)
			return
		}
		if state == "ready" {
			break
		}
		// "startup", "shutdown" or "error": the message says why, for example a missing MCU.
		k.setDown(firstLine(msg), notify)
		select {
		case <-ctx.Done():
			return
		case <-conn.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}

	status, err := conn.Subscribe(ctx, klippyObjects, func(_ float64, u klippy.Status) {
		k.mu.Lock()
		k.status.Merge(u)
		k.mu.Unlock()
		k.refresh(notify)
	})
	if err != nil {
		k.setDown(err.Error(), notify)
		return
	}
	k.mu.Lock()
	k.conn, k.status, k.err = conn, status, ""
	k.mu.Unlock()
	k.LoadFiles()
	k.refresh(notify)

	select {
	case <-ctx.Done():
	case <-conn.Done():
	}
	k.mu.Lock()
	k.conn = nil
	k.mu.Unlock()
	k.setDown("Klipper is not running", notify)
}

func info(ctx context.Context, conn *klippy.Conn) (state, msg string, err error) {
	ictx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	res, err := conn.Call(ictx, "info", map[string]any{"client_info": map[string]any{"program": "Printdeck"}})
	if err != nil {
		return "", "", err
	}
	var i struct {
		State        string `json:"state"`
		StateMessage string `json:"state_message"`
	}
	if err := json.Unmarshal(res, &i); err != nil {
		return "", "", err
	}
	return i.State, i.StateMessage, nil
}

func (k *Klippy) setDown(why string, notify func()) {
	k.mu.Lock()
	k.err, k.status = why, nil
	k.mu.Unlock()
	k.refresh(notify)
}

// refresh makes the state from the status of Klipper, and updates the picture of the print.
func (k *Klippy) refresh(notify func()) {
	k.mu.Lock()
	st := klippyState(k.status, k.err)
	k.mu.Unlock()
	if st.FilePath != "" {
		if fi, err := k.store.Stat(st.FilePath); err == nil {
			st.FileDate = fi.ModTime().Unix()
		}
	}
	k.View.Activity(&st)
	k.mu.Lock()
	k.st = st
	k.mu.Unlock()
	k.View.Update(st)
	notify()
}

// klippyState makes the deck's State from the status of Klipper. down is why Klipper is not ready,
// or "" when it is.
func klippyState(s klippy.Status, down string) State {
	if down != "" {
		return State{Err: down, Text: "Offline"}
	}
	st := State{Reachable: true, Connected: true}
	ps := s["print_stats"]
	switch str(ps["state"]) {
	case "printing":
		st.Text, st.Printing = "Printing", true
	case "paused":
		st.Text, st.Printing, st.Paused = "Paused", true, true
	default: // standby, complete, cancelled, error
		st.Text = "Operational"
	}
	if st.Busy() {
		st.FilePath = str(ps["filename"])
		st.File = path.Base(st.FilePath)
		sd := s["virtual_sdcard"]
		st.FileSize, st.FilePos = int64(num(sd["file_size"])), int64(num(sd["file_position"]))
		progress := num(sd["progress"])
		st.Completion = progress * 100
		// The time left from the time so far. The look-ahead estimate replaces this later.
		if d := num(ps["print_duration"]); progress > 0.02 && d > 0 {
			st.TimeLeft, st.HasTimeLeft = time.Duration((d/progress-d)*float64(time.Second)), true
		}
	}
	st.Tool, st.ToolTarget = num(s["extruder"]["temperature"]), num(s["extruder"]["target"])
	st.Bed, st.BedTarget = num(s["heater_bed"]["temperature"]), num(s["heater_bed"]["target"])
	if v, ok := s["fan"]["speed"]; ok {
		st.Fan, st.HasFan = num(v)*100, true
	}
	if pos, ok := s["toolhead"]["position"].([]any); ok && len(pos) >= 3 {
		st.Z, st.HasZ = num(pos[2]), true
	}
	return st
}

// script runs G-code and waits for it. A refusal of Klipper comes back as the error.
func (k *Klippy) script(lines ...string) error {
	return k.call("gcode/script", map[string]any{"script": strings.Join(lines, "\n")})
}

func (k *Klippy) call(method string, params any) error {
	k.mu.Lock()
	conn := k.conn
	k.mu.Unlock()
	if conn == nil {
		return ErrNotReady
	}
	ctx, cancel := context.WithTimeout(context.Background(), controlTimeout)
	defer cancel()
	_, err := conn.Call(ctx, method, params)
	return err
}

// later runs a call in the background, for the buttons that do not show an error.
func (k *Klippy) later(f func() error) {
	go f()
}

func (k *Klippy) Pause()  { k.later(func() error { return k.call("pause_resume/pause", nil) }) }
func (k *Klippy) Resume() { k.later(func() error { return k.call("pause_resume/resume", nil) }) }
func (k *Klippy) Cancel() { k.later(func() error { return k.call("pause_resume/cancel", nil) }) }

// Connect restarts the firmware after an error or an emergency stop.
func (k *Klippy) Connect() { k.later(k.FirmwareRestart) }

func (k *Klippy) Heat(tool, bed int) {
	k.later(func() error { return k.script(fmt.Sprintf("M104 S%d", tool), fmt.Sprintf("M140 S%d", bed)) })
}

// PrintFile starts a file of the store.
func (k *Klippy) PrintFile(path string) {
	k.later(func() error { return k.script(fmt.Sprintf("SDCARD_PRINT_FILE FILENAME=%q", path)) })
}

func (k *Klippy) Gcode(lines ...string) error { return k.script(lines...) }
func (k *Klippy) Home(axes ...string) error   { return k.script(HomeGcode(axes...)) }
func (k *Klippy) SetToolTemp(t int) error     { return k.script(fmt.Sprintf("M104 S%d", t)) }
func (k *Klippy) SetBedTemp(t int) error      { return k.script(fmt.Sprintf("M140 S%d", t)) }
func (k *Klippy) Fan(percent int) error       { return k.script(FanGcode(percent)) }
func (k *Klippy) MotorsOff() error            { return k.script("M84") }
func (k *Klippy) SpeedFactor(p int) error     { return k.script(SpeedGcode(p)) }
func (k *Klippy) FlowFactor(p int) error      { return k.script(FlowGcode(p)) }
func (k *Klippy) BabyStep(mm float64) error   { return k.script(BabyStepGcode(mm)) }
func (k *Klippy) Extrude(mm float64, speed int) error {
	return k.script(ExtrudeGcode(mm, speed)...)
}

func (k *Klippy) Jog(x, y, z float64, speed int) error {
	g := JogGcode(x, y, z, speed)
	if g == nil {
		return nil
	}
	return k.script(g...)
}

// EmergencyStop stops the printer at once. Klipper must be restarted afterwards (FirmwareRestart).
func (k *Klippy) EmergencyStop() error { return k.call("emergency_stop", nil) }

// FirmwareRestart restarts Klipper and its firmware after an emergency stop or an error.
func (k *Klippy) FirmwareRestart() error { return k.call("gcode/firmware_restart", nil) }

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return "Klipper is starting"
	}
	return s
}
