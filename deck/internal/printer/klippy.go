package printer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"printdeck/internal/klipper/toolhead"
	"printdeck/internal/maxmode"
	"sort"
	"strings"
	"sync"
	"time"

	"printdeck/internal/board"
	"printdeck/internal/gcodemeta"
	"printdeck/internal/klipper/cli"
	"printdeck/internal/klippy"
)

// Store is the file store of the Klipper backend: the folder of Klipper's [virtual_sdcard].
type Store interface {
	Source
	Files() []File
	Stat(path string) (fs.FileInfo, error)
	FullPath(path string) (string, error)
	Delete(path string) error
}

// Klippy is the printer backend: it follows the Klipper host over its API socket, and prints the
// files of the store with Klipper's virtual SD card.
type Klippy struct {
	socket string
	store  Store
	*View

	// ConfigPath is printer.cfg. With no [mcu] section in it, the state asks for the setup.
	ConfigPath string
	// Hist records the finished prints.
	Hist *History
	// RestartService restarts the Klipper service, which reads printer.cfg again. The API of
	// Klipper has no restart while the config has an error.
	RestartService func() error
	// StopService stops the Klipper service, so that the flash of the firmware can use the port.
	StopService func() error

	mu     sync.Mutex
	conn   *klippy.Conn
	status klippy.Status
	err    string // why Klipper is not ready
	// errDetail is the full message of Klipper's error state, with all its lines
	errDetail string
	st        State
	files     []File
	notify    func()

	boards  []board.Info // what the last probe found
	probing bool

	meta gcodemeta.Cache // the slicer data of the files

	console

	onKey func(key string) // SetOnKey

	// The text of each macro, for the steps of the start macro.
	macros map[string]string
	prep   prepTracker

	// The maintenance action that runs now, and the last one that ended (RunAction).
	action      string
	actionStart time.Time
	lastAction  ActionResult

	// From the connection to Klipper: its version, and the names of its objects (objects/list).
	version string
	objects []string
}

// klippyObjects are the printer objects that the deck follows. nil means all fields.
var klippyObjects = map[string][]string{
	"webhooks":       {"state", "state_message"},
	"print_stats":    {"state", "filename", "print_duration", "total_duration", "filament_used", "message"},
	"display_status": {"message"},
	"gcode_move":     {"homing_origin", "speed_factor", "extrude_factor"},
	"configfile":     {"save_config_pending"},
	"manual_probe":   {"is_active", "z_position"},
	"virtual_sdcard": {"file_position", "file_size", "progress"},
	"extruder":       {"temperature", "target"},
	"heater_bed":     {"temperature", "target"},
	"fan":            {"speed"},
	"toolhead":       {"position", "homed_axes", "axis_minimum", "axis_maximum"},
	"mcu":            {"mcu_version"},
	// The command stack of the Printdeck module of the Klipper host, for the steps of PRINT_START.
	"printdeck_display": {"commands"},
}

// HostVersion is the Klipper version of the host. The firmware of the board must be this version.
const HostVersion = cli.KlipperVersion

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
	st := k.st
	st.Action, st.ActionStart, st.LastAction = k.action, k.actionStart, k.lastAction
	return st
}

// ErrActionRunning is the error of RunAction while another action runs.
var ErrActionRunning = errors.New("Another action runs. Wait until it ends.")

// RunAction runs a maintenance action, for example "Bed tilt" with BED_TILT_CALIBRATE, and waits
// for its end. The state shows the action while it runs, on all screens, and its result after.
func (k *Klippy) RunAction(name string, lines ...string) error {
	k.mu.Lock()
	if k.action != "" {
		k.mu.Unlock()
		return ErrActionRunning
	}
	k.action, k.actionStart = name, time.Now()
	notify := k.notify
	k.mu.Unlock()
	if notify != nil {
		notify()
	}
	err := k.script(lines...)
	k.mu.Lock()
	k.lastAction = ActionResult{Name: name, End: time.Now()}
	if err != nil {
		k.lastAction.Err = err.Error()
	}
	k.action = ""
	k.mu.Unlock()
	if notify != nil {
		notify()
	}
	return err
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
	k.mu.Lock()
	k.notify = notify
	k.mu.Unlock()
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
		k.mu.Lock()
		k.errDetail = strings.TrimSpace(msg)
		k.mu.Unlock()
		k.setDown(firstLine(msg), notify)
		select {
		case <-ctx.Done():
			return
		case <-conn.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}

	// Updates can come before the first status is in k.status: they wait in early, and go into
	// the first status.
	live, early := false, klippy.Status{}
	status, err := conn.Subscribe(ctx, klippyObjects, func(_ float64, u klippy.Status) {
		k.mu.Lock()
		if !live {
			early.Merge(u)
			k.mu.Unlock()
			return
		}
		if k.status == nil { // setDown ended this connection
			k.mu.Unlock()
			return
		}
		k.status.Merge(u)
		k.mu.Unlock()
		k.refresh(notify)
	})
	if err != nil {
		k.setDown(err.Error(), notify)
		return
	}
	if status == nil {
		status = klippy.Status{}
	}
	k.mu.Lock()
	status.Merge(early)
	k.status, live = status, true
	k.mu.Unlock()
	if err := conn.SubscribeOutput(ctx, func(line string) {
		k.console.add("out", line)
		notify()
	}); err != nil {
		k.setDown(err.Error(), notify)
		return
	}
	// A printer with no display, or a host with no printdeck_display, refuses: then the deck has
	// no knob.
	conn.SubscribeInput(ctx, func(key string) {
		k.mu.Lock()
		onKey := k.onKey
		k.mu.Unlock()
		if onKey != nil {
			onKey(key)
		}
	})
	version, objects, macros := klipperObjects(ctx, conn)
	if l, mm, ok := motionLimits(ctx, conn); ok {
		k.View.SetLimits(l, mm)
	}
	k.mu.Lock()
	k.macros = macros
	k.conn, k.err = conn, ""
	k.version, k.objects = version, objects
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

// FileMeta returns what the slicer wrote about a file: the time, the filament, the thumbnail.
func (k *Klippy) FileMeta(path string) gcodemeta.Meta {
	full, err := k.store.FullPath(path)
	if err != nil {
		return gcodemeta.Meta{}
	}
	return k.meta.Get(full)
}

// DeleteFile removes a file, and reads the file list again. A file that prints now stays.
func (k *Klippy) DeleteFile(path string) error {
	if st := k.State(); st.Busy() && st.FilePath == path {
		return errors.New("This file prints now.")
	}
	if err := k.store.Delete(path); err != nil {
		return err
	}
	k.LoadFiles()
	return nil
}

// History returns the finished prints, newest first.
func (k *Klippy) History() []Record {
	if k.Hist == nil {
		return nil
	}
	return k.Hist.Records()
}

// Machine returns the version, the modules and the macros of the connected Klipper.
func (k *Klippy) Machine() Machine {
	k.mu.Lock()
	defer k.mu.Unlock()
	m := Machine{KlipperVersion: k.version}
	if k.conn == nil {
		return m
	}
	for _, o := range k.objects {
		if name, ok := strings.CutPrefix(o, "gcode_macro "); ok {
			if !strings.HasPrefix(name, "_") {
				m.Macros = append(m.Macros, name)
			}
		} else if !strings.Contains(o, " ") {
			m.Modules = append(m.Modules, o)
		}
	}
	return m
}

// klipperObjects asks Klipper for its version and the names of its objects, for example "probe",
// "bed_tilt" or "gcode_macro PRINT_START".
func klipperObjects(ctx context.Context, conn *klippy.Conn) (version string, objects []string, macros map[string]string) {
	macros = map[string]string{}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if res, err := conn.Call(cctx, "info", map[string]any{}); err == nil {
		var i struct {
			Version string `json:"software_version"`
		}
		json.Unmarshal(res, &i)
		version = i.Version
	}
	if res, err := conn.Call(cctx, "objects/list", map[string]any{}); err == nil {
		var l struct {
			Objects []string `json:"objects"`
		}
		json.Unmarshal(res, &l)
		objects = l.Objects
	}
	// objects/list has only the objects with a status. A module such as bed_tilt has none, so the
	// sections of the config are added too.
	if res, err := conn.Call(cctx, "objects/query", map[string]any{"objects": map[string]any{"configfile": []string{"config"}}}); err == nil {
		var q struct {
			Status struct {
				Configfile struct {
					Config map[string]any `json:"config"`
				} `json:"configfile"`
			} `json:"status"`
		}
		json.Unmarshal(res, &q)
		have := map[string]bool{}
		for _, o := range objects {
			have[o] = true
		}
		names := make([]string, 0, len(q.Status.Configfile.Config))
		for name, sec := range q.Status.Configfile.Config {
			names = append(names, name)
			if m, ok := strings.CutPrefix(strings.ToLower(name), "gcode_macro "); ok {
				if opts, ok := sec.(map[string]any); ok {
					if g, ok := opts["gcode"].(string); ok {
						macros[m] = g
					}
				}
			}
		}
		sort.Strings(names)
		for _, name := range names {
			if !have[name] {
				objects = append(objects, name)
			}
		}
	}
	return version, objects, macros
}

// motionLimits reads the motion limits of the printer from its config, with the defaults of
// Klipper (configfile settings), for the plan of a print. It also returns the settings of max mode
// when the config has a [max_mode] section with enable: True, and nil otherwise.
func motionLimits(ctx context.Context, conn *klippy.Conn) (toolhead.Limits, *maxmode.Settings, bool) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	res, err := conn.Call(cctx, "objects/query", map[string]any{"objects": map[string]any{"configfile": []string{"settings"}}})
	if err != nil {
		return toolhead.Limits{}, nil, false
	}
	var q struct {
		Status struct {
			Configfile struct {
				Settings map[string]map[string]any `json:"settings"`
			} `json:"configfile"`
		} `json:"status"`
	}
	json.Unmarshal(res, &q)
	pr, ex := q.Status.Configfile.Settings["printer"], q.Status.Configfile.Settings["extruder"]
	l := toolhead.Limits{MaxVelocity: num(pr["max_velocity"]), MaxAccel: num(pr["max_accel"]),
		MinCruiseRatio: num(pr["minimum_cruise_ratio"]), SquareCornerVelocity: num(pr["square_corner_velocity"]),
		MaxZVelocity: num(pr["max_z_velocity"]), MaxZAccel: num(pr["max_z_accel"]),
		InstantCornerVelocity:  num(ex["instantaneous_corner_velocity"]),
		MaxExtrudeOnlyVelocity: num(ex["max_extrude_only_velocity"]), MaxExtrudeAcc: num(ex["max_extrude_only_accel"])}
	var mm *maxmode.Settings
	if sec, ok := q.Status.Configfile.Settings["max_mode"]; ok && sec["enable"] == true {
		s := maxmode.Defaults()
		s.Enable = true
		s.Load(func(option string, def float64) float64 {
			if v, ok := sec[option].(float64); ok {
				return v
			}
			return def
		})
		if v, ok := sec["filament_diameter"].(float64); ok {
			s.FilamentDiameter = v
		} else if v, ok := ex["filament_diameter"].(float64); ok {
			s.FilamentDiameter = v
		}
		mm = &s
	}
	return l, mm, l.MaxVelocity > 0 && l.MaxAccel > 0
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
	if why != firstLine(k.errDetail) {
		k.errDetail = ""
	}
	k.err, k.status = why, nil
	k.mu.Unlock()
	k.refresh(notify)
}

// refresh makes the state from the status of Klipper, and updates the picture of the print.
func (k *Klippy) refresh(notify func()) {
	k.mu.Lock()
	st := klippyState(k.status, k.err)
	if !st.Reachable {
		st.ErrDetail = k.errDetail
	}
	k.mu.Unlock()
	if !st.Reachable && k.ConfigPath != "" {
		st.NeedsSetup = NeedsSetup(k.ConfigPath)
	}
	if st.FilePath != "" {
		if fi, err := k.store.Stat(st.FilePath); err == nil {
			st.FileDate = fi.ModTime().Unix()
		}
	}
	k.View.Activity(&st)
	k.View.TimeLeft(&st, time.Now())
	k.mu.Lock()
	var stack []string
	if cmds, ok := k.status["printdeck_display"]["commands"].([]any); ok {
		for _, c := range cmds {
			if s, ok := c.(string); ok {
				stack = append(stack, s)
			}
		}
	}
	if st.Busy() {
		steps := k.prep.steps(stack, k.macros)
		st.PrepN = copy(st.Prep[:], steps)
		if st.PrepN > 0 {
			st.Preparing = true
		}
	}
	k.mu.Unlock()
	if k.Hist != nil && st.Reachable {
		k.Hist.add(st, time.Now())
	}
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
	st.Firmware = str(s["mcu"]["mcu_version"])
	st.FirmwareOld = st.Firmware != "" && !board.SameVersion(st.Firmware, HostVersion)
	ps := s["print_stats"]
	switch state := str(ps["state"]); state {
	case "printing":
		st.Text, st.Printing = "Printing", true
	case "paused":
		st.Text, st.Printing, st.Paused = "Paused", true, true
	case "complete", "cancelled", "error":
		// Klipper keeps the file and the times of the last print until the next one.
		st.Text = "Operational"
		if f := str(ps["filename"]); f != "" {
			st.Finished, st.FinishedPath, st.FinishedFile = state, f, path.Base(f)
			st.FinishedTime = time.Duration(num(ps["total_duration"]) * float64(time.Second))
			st.FinishedFilament = num(ps["filament_used"])
			st.FinishedError = str(ps["message"])
		}
	default: // standby
		st.Text = "Operational"
	}
	st.Message = str(s["display_status"]["message"])
	gm := s["gcode_move"]
	if o, ok := gm["homing_origin"].([]any); ok && len(o) >= 3 {
		st.ZOffset = num(o[2])
	}
	st.SpeedPct, st.FlowPct = int(num(gm["speed_factor"])*100+0.5), int(num(gm["extrude_factor"])*100+0.5)
	if _, ok := gm["speed_factor"]; !ok {
		st.SpeedPct, st.FlowPct = 100, 100
	}
	st.SaveConfigPending, _ = s["configfile"]["save_config_pending"].(bool)
	st.ManualProbe, _ = s["manual_probe"]["is_active"].(bool)
	st.ManualZ = num(s["manual_probe"]["z_position"])
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
		st.Elapsed = time.Duration(num(ps["total_duration"]) * float64(time.Second))
		st.PrintDuration = time.Duration(num(ps["print_duration"]) * float64(time.Second))
		st.FilamentUsed = num(ps["filament_used"])
	}
	st.Tool, st.ToolTarget = num(s["extruder"]["temperature"]), num(s["extruder"]["target"])
	st.Bed, st.BedTarget = num(s["heater_bed"]["temperature"]), num(s["heater_bed"]["target"])
	if v, ok := s["fan"]["speed"]; ok {
		st.Fan, st.HasFan = num(v)*100, true
	}
	if pos, ok := s["toolhead"]["position"].([]any); ok && len(pos) >= 3 {
		st.X, st.Y, st.Z, st.HasZ = num(pos[0]), num(pos[1]), num(pos[2]), true
	}
	st.Homed = str(s["toolhead"]["homed_axes"])
	for i, key := range []string{"axis_minimum", "axis_maximum"} {
		if v, ok := s["toolhead"][key].([]any); ok && len(v) >= 3 {
			for j := range 3 {
				st.Axis[i][j] = num(v[j])
			}
		}
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

// UseConfig writes one of Klipper's example configs as printer.cfg, with serial as the port of the
// board, and restarts Klipper.
func (k *Klippy) UseConfig(file, serial string) error {
	if err := UseExample(k.ConfigPath, file, serial); err != nil {
		return err
	}
	return k.RestartKlipper()
}

// FlashBoard writes the Klipper firmware of the image to the board on device, or to the board of
// [mcu] in printer.cfg when device is "". It stops Klipper for the flash, and starts it again.
func (k *Klippy) FlashBoard(device string, progress board.FlashProgress) error {
	if device == "" {
		device = MCUSerial(k.ConfigPath)
	}
	if device == "" {
		return errors.New("printer.cfg has no serial port in [mcu]")
	}
	if k.StopService != nil {
		if err := k.StopService(); err != nil {
			return err
		}
	}
	err := board.Flash(device, progress)
	if rerr := k.RestartKlipper(); err == nil {
		err = rerr
	}
	return err
}

// SetOnKey sets the function that gets each key of the knob of the printer display
// (printdeck_display).
func (k *Klippy) SetOnKey(f func(key string)) {
	k.mu.Lock()
	k.onKey = f
	k.mu.Unlock()
}

// ShowLCD shows a screen on the display of the printer, for 15 seconds. The deck sends it again
// before then. nil gives the display back to Klipper.
func (k *Klippy) ShowLCD(s *LCDScreen) error {
	return k.call("printdeck/screen", map[string]any{"screen": s})
}

// SendConsole runs a command of the console in the background. Klipper writes the reply, and a
// refusal as "!! message", to the console.
func (k *Klippy) SendConsole(line string) {
	k.console.add("in", line)
	k.later(func() error {
		err := k.script(line)
		if errors.Is(err, ErrNotReady) {
			k.console.add("error", err.Error())
		}
		k.mu.Lock()
		notify := k.notify
		k.mu.Unlock()
		if notify != nil {
			notify()
		}
		return err
	})
}
func (k *Klippy) Home(axes ...string) error { return k.script(HomeGcode(axes...)) }
func (k *Klippy) SetToolTemp(t int) error   { return k.script(fmt.Sprintf("M104 S%d", t)) }
func (k *Klippy) SetBedTemp(t int) error    { return k.script(fmt.Sprintf("M140 S%d", t)) }
func (k *Klippy) Fan(percent int) error     { return k.script(FanGcode(percent)) }
func (k *Klippy) MotorsOff() error          { return k.script("M84") }
func (k *Klippy) SpeedFactor(p int) error   { return k.script(SpeedGcode(p)) }
func (k *Klippy) FlowFactor(p int) error    { return k.script(FlowGcode(p)) }
func (k *Klippy) BabyStep(mm float64) error { return k.script(BabyStepGcode(mm)) }
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
// FirmwareRestart resets the board and restarts Klipper. It works also while Klipper is not ready,
// for example after an MCU shutdown: then it uses a connection of its own.
func (k *Klippy) FirmwareRestart() error {
	k.mu.Lock()
	conn := k.conn
	k.mu.Unlock()
	if conn != nil {
		return k.call("gcode/firmware_restart", nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := klippy.Dial(ctx, k.socket)
	if err != nil {
		return ErrNotReady
	}
	defer c.Close()
	_, err = c.Call(ctx, "gcode/firmware_restart", nil)
	return err
}

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
