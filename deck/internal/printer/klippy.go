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
	// Spools are the rolls of filament. Each finished print uses filament of the roll in the
	// printer.
	Spools *Spools
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
	// projectPath is the file of the print now after projectOfFile looked at it.
	projectPath string
	lastAction  ActionResult
	// tiltByAction reports that the last bed tilt calibration ran as a maintenance action
	// (RunAction), with a result to save. A print resets it.
	tiltByAction bool

	// From the connection to Klipper: its version, its interface version, and the names of its
	// objects (objects/list).
	version  string
	hostAPI  int
	diameter float64 // filament_diameter of the extruder, in mm
	settings Machine // the settings of printer.cfg in Machine (machineSettings)
	timer    timer   // the timings of the running print
	objects  []string
	lastPos  int64     // the file position of the last busy state, for the history
	startup  bool      // Klipper reports "startup"
	since    time.Time // the start of the deck, or the last restart of Klipper, for startGrace

	// sim is the name of the simulated printer of printer.cfg, or "" (SimulatedName). simRead is
	// false until the deck reads printer.cfg, and again after a change to printer.cfg.
	sim     string
	simRead bool
}

// startGrace is how long Klipper may be away after the deck or a restart of Klipper started,
// before the deck shows that it does not run.
const startGrace = 30 * time.Second

// klippyObjects are the printer objects that the deck follows. nil means all fields.
var klippyObjects = map[string][]string{
	"webhooks":       {"state", "state_message"},
	"print_stats":    {"state", "filename", "print_duration", "total_duration", "filament_used", "message"},
	"display_status": {"message"},
	"gcode_move":     {"homing_origin", "speed_factor", "extrude_factor"},
	"configfile":     {"save_config_pending", "save_config_pending_items"},
	"manual_probe":   {"is_active", "z_position"},
	"virtual_sdcard": {"file_position", "file_size", "progress"},
	"extruder":       {"temperature", "target", "power"},
	"heater_bed":     {"temperature", "target", "power"},
	"fan":            {"speed"},
	"toolhead":       {"position", "homed_axes", "axis_minimum", "axis_maximum"},
	"mcu":            {"mcu_version"},
	"motion_report":  {"live_velocity", "live_extruder_velocity"},
	// The command stack of the Printdeck module of the Klipper host, for the steps of PRINT_START.
	"printdeck_display": {"commands"},
	// A pause of the file with a message, and the height check on a part (PRINTDECK_PAUSE).
	"printdeck_pause": {"active", "message", "check", "checked", "delta", "roll"},
	// The copy of a file from the SD card of the printer.
	"printdeck_sdcard": {"id", "copying", "copied", "size", "done", "error"},
	// The park for the pictures of the time lapse and of the first layer.
	"printdeck_timelapse": {"enable", "park", "first_layer", "every", "frame", "taken", "waiting", "first", "lapse",
		"layer", "stopped", "park_y"},
}

// HostVersion is the Klipper version of the host. The firmware of the board must be this version.
const HostVersion = cli.KlipperVersion

// controlTimeout is how long a control waits for Klipper. Homing and heating can take minutes.
const controlTimeout = 5 * time.Minute

// ErrNotReady is the error of a control while Klipper is not ready.
var ErrNotReady = errors.New("Klipper is not ready")

func NewKlippy(socket string, store Store) *Klippy {
	k := &Klippy{socket: socket, store: store, err: "Klipper is not running", since: time.Now()}
	k.View = NewView(store)
	return k
}

func (k *Klippy) State() State {
	k.mu.Lock()
	defer k.mu.Unlock()
	st := k.st
	st.Action, st.ActionStart, st.LastAction = k.action, k.actionStart, k.lastAction
	// A bed tilt from the start of a print is measured again at each print: nothing to save.
	if st.TiltOnlyPending && !k.tiltByAction {
		st.SaveConfigPending = false
	}
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
	for _, l := range lines {
		if strings.Contains(strings.ToUpper(l), "BED_TILT_CALIBRATE") {
			k.tiltByAction = true
		}
	}
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
		k.startup = state == "startup"
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
	version, hostAPI, objects, macros := klipperObjects(ctx, conn)
	l, mm, ok, diameter := motionLimits(ctx, conn)
	settings := machineSettings(ctx, conn)
	if ok {
		k.View.SetLimits(l, mm)
	}
	k.mu.Lock()
	k.diameter, k.settings = diameter, settings
	k.macros = macros
	k.conn, k.err, k.startup = conn, "", false
	k.version, k.hostAPI, k.objects = version, hostAPI, objects
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

// SetVideo gives the time lapse video name to the record of the last print of the file path.
func (k *Klippy) SetVideo(path, name string) {
	if k.Hist != nil {
		k.Hist.SetVideo(path, name, time.Now())
	}
}

// SetNote sets the note of the finished print r. It reports that the history has the print.
func (k *Klippy) SetNote(r Record, note string) bool {
	return k.Hist != nil && k.Hist.SetNote(r.Path, r.End, note)
}

// SetProject sets the project of the finished print r. It reports that the history has the print.
func (k *Klippy) SetProject(r Record, project string) bool {
	return k.Hist != nil && k.Hist.SetProject(r.Path, r.End, project)
}

// SetNextProject sets the project of the print now, for its record at the end. It reports that
// a print runs.
func (k *Klippy) SetNextProject(project string) bool {
	st := k.State()
	if k.Hist == nil || !st.Busy() || st.FilePath == "" {
		return false
	}
	k.Hist.SetNextProject(st.FilePath, project)
	return true
}

// NextProject returns the project of the print now, or "".
func (k *Klippy) NextProject() string {
	st := k.State()
	if k.Hist == nil || !st.Busy() {
		return ""
	}
	return k.Hist.NextProject(st.FilePath)
}

// Machine returns the version, the modules and the macros of the connected Klipper.
// Rates returns the timings of this printer, from its history.
func (k *Klippy) Rates() Rates {
	if k.Hist == nil {
		return ComputeRates(nil)
	}
	return k.Hist.Rates()
}

// SpoolStore returns the rolls of filament, or nil.
func (k *Klippy) SpoolStore() *Spools { return k.Spools }

// useSpool subtracts the filament of the print with the history key from the roll in the
// printer, and records the roll and the cost.
func (k *Klippy) useSpool(key string, mm float64) {
	if k.Spools == nil || mm <= 0 {
		return
	}
	d := k.Machine().Diameter()
	var grams float64
	sp, cost, ok := k.Spools.Use(func(rho float64) float64 {
		grams = GramsAt(mm, d, rho)
		return grams
	})
	if ok {
		k.Hist.setSpool(key, sp, grams, cost)
	}
}

func (k *Klippy) Machine() Machine {
	k.mu.Lock()
	defer k.mu.Unlock()
	m := k.settings
	m.KlipperVersion, m.HostAPI, m.FilamentDiameter = k.version, k.hostAPI, k.diameter
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

// klipperObjects asks Klipper for its version, its interface version (hostapi), and the names of
// its objects, for example "probe", "bed_tilt" or "gcode_macro PRINT_START".
func klipperObjects(ctx context.Context, conn *klippy.Conn) (version string, hostAPI int, objects []string, macros map[string]string) {
	macros = map[string]string{}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if res, err := conn.Call(cctx, "info", map[string]any{}); err == nil {
		var i struct {
			Version string `json:"software_version"`
			API     int    `json:"printdeck_api"` // hostapi.InfoKey
		}
		json.Unmarshal(res, &i)
		version, hostAPI = i.Version, i.API
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
	return version, hostAPI, objects, macros
}

// motionLimits reads the motion limits of the printer from its config, with the defaults of
// Klipper (configfile settings), for the plan of a print, and the filament diameter. It also
// returns the settings of max mode when the config has a [max_mode] section with enable: True,
// and nil otherwise.
func motionLimits(ctx context.Context, conn *klippy.Conn) (l toolhead.Limits, mm *maxmode.Settings, ok bool, filamentDiameter float64) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	res, err := conn.Call(cctx, "objects/query", map[string]any{"objects": map[string]any{"configfile": []string{"settings"}}})
	if err != nil {
		return toolhead.Limits{}, nil, false, 0
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
	l = toolhead.Limits{MaxVelocity: num(pr["max_velocity"]), MaxAccel: num(pr["max_accel"]),
		MinCruiseRatio: num(pr["minimum_cruise_ratio"]), SquareCornerVelocity: num(pr["square_corner_velocity"]),
		MaxZVelocity: num(pr["max_z_velocity"]), MaxZAccel: num(pr["max_z_accel"]),
		InstantCornerVelocity:  num(ex["instantaneous_corner_velocity"]),
		MaxExtrudeOnlyVelocity: num(ex["max_extrude_only_velocity"]), MaxExtrudeAcc: num(ex["max_extrude_only_accel"])}
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
	return l, mm, l.MaxVelocity > 0 && l.MaxAccel > 0, num(ex["filament_diameter"])
}

// machineSettings reads the settings of printer.cfg that Machine has: the nozzle, the travel of X
// and Y, the pressure advance, the acceleration, the kinematics, the Z endstop, the bed mesh, the
// points of bed_tilt, and the park of the time lapse (configfile settings, with the defaults of
// Klipper).
func machineSettings(ctx context.Context, conn *klippy.Conn) Machine {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	res, err := conn.Call(cctx, "objects/query", map[string]any{"objects": map[string]any{"configfile": []string{"settings"}}})
	if err != nil {
		return Machine{}
	}
	var q struct {
		Status struct {
			Configfile struct {
				Settings map[string]map[string]any `json:"settings"`
			} `json:"configfile"`
		} `json:"status"`
	}
	json.Unmarshal(res, &q)
	s := q.Status.Configfile.Settings
	kin, _ := s["printer"]["kinematics"].(string)
	parkX, hasParkX := s["printdeck_timelapse"]["park_x"].(float64)
	parkY, hasParkY := s["printdeck_timelapse"]["park_y"].(float64)
	pair := func(v any) ([2]float64, bool) {
		a, _ := v.([]any)
		if len(a) != 2 {
			return [2]float64{}, false
		}
		x, okX := a[0].(float64)
		y, okY := a[1].(float64)
		return [2]float64{x, y}, okX && okY
	}
	meshMin, hasMin := pair(s["bed_mesh"]["mesh_min"])
	meshMax, hasMax := pair(s["bed_mesh"]["mesh_max"])
	var tilt [][2]float64
	if pts, ok := s["bed_tilt"]["points"].([]any); ok {
		for _, pt := range pts {
			if xy, ok := pair(pt); ok {
				tilt = append(tilt, xy)
			}
		}
	}
	zEnd, hasZEnd := s["stepper_z"]["position_endstop"].(float64)
	zMax, hasZMax := s["stepper_z"]["position_max"].(float64)
	return Machine{
		ZHomesMax: hasZEnd && hasZMax && zEnd >= zMax-1,
		MeshMin:   meshMin, MeshMax: meshMax, HasMesh: hasMin && hasMax, TiltPoints: tilt,
		Kinematics: kin, ParkX: parkX, HasParkX: hasParkX, ParkY: parkY, HasParkY: hasParkY,
		NozzleDiameter: num(s["extruder"]["nozzle_diameter"]),
		MinX:           num(s["stepper_x"]["position_min"]), MaxX: num(s["stepper_x"]["position_max"]),
		MinY: num(s["stepper_y"]["position_min"]), MaxY: num(s["stepper_y"]["position_max"]),
		PressureAdvance:      num(s["extruder"]["pressure_advance"]),
		MaxAccel:             num(s["printer"]["max_accel"]),
		SquareCornerVelocity: num(s["printer"]["square_corner_velocity"]),
	}
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
	if k.err == "" {
		k.since = time.Now() // Klipper was ready: it restarts, for example after RESTART
	}
	k.err, k.status = why, nil
	if why == "Klipper is not running" {
		k.startup = false
	}
	k.mu.Unlock()
	k.refresh(notify)
}

// refresh makes the state from the status of Klipper, and updates the picture of the print.
func (k *Klippy) refresh(notify func()) {
	k.mu.Lock()
	st := klippyState(k.status, k.err)
	if !st.Reachable {
		st.ErrDetail = k.errDetail
		st.Starting = (k.startup || k.err == "Klipper is not running") && time.Since(k.since) < startGrace
	}
	k.mu.Unlock()
	if !st.Reachable && k.ConfigPath != "" {
		st.NeedsSetup = NeedsSetup(k.ConfigPath)
	}
	st.Simulated = k.simulated()
	if st.FilePath != "" {
		if fi, err := k.store.Stat(st.FilePath); err == nil {
			st.FileDate = fi.ModTime().Unix()
		}
		st.Title = k.FileMeta(st.FilePath).Title
	}
	if st.FinishedPath != "" {
		st.FinishedTitle = k.FileMeta(st.FinishedPath).Title
	}
	k.View.Activity(&st)
	now := time.Now()
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
		// A start macro has two steps or more. An end macro can have one, such as the cooling
		// of the bed.
		minSteps := 2
		if st.Finishing {
			minSteps = 1
		}
		steps := k.prep.steps(stack, k.macros, minSteps)
		st.PrepN = copy(st.Prep[:], steps)
		if st.PrepN > 0 && !st.Finishing {
			st.Preparing = true
		}
	}
	// The timings of the print: the steps of the start macro, the plan, and the cooling of the
	// bed after the print.
	k.timer.update(st, now)
	k.timer.observe(st, now)
	if st.Busy() {
		if total, ok := k.View.PlanTotal(fileKey(st)); ok {
			k.timer.planned = total
		}
	}
	k.mu.Unlock()
	rates := ComputeRates(nil)
	if k.Hist != nil {
		rates = k.Hist.Rates()
	}
	k.mu.Lock()
	prepLeft := rates.prepLeft(st, &k.timer, now)
	if to := coolTarget(st); to > 0 {
		st.BedCoolLeft, st.HasBedCoolLeft = k.timer.coolLeft(rates, st, to, now)
		st.BedCoolTo, st.BedCoolFrom = to, st.Bed
		if !k.timer.fallStart.IsZero() {
			st.BedCoolFrom = k.timer.fallFrom
		}
	}
	// The cooling of the bed to ReleaseBed after the print: after the end macro, or after the
	// cooling that runs now.
	from := st.BedTarget
	if st.HasBedCoolLeft {
		from = st.BedCoolTo
	} else if from == 0 && st.Preparing {
		from = rates.Target["heat-bed"]
	}
	release, hasRelease := k.timer.releaseCool(rates, from)
	k.mu.Unlock()
	k.View.TimeLeft(&st, now, prepLeft, rates.PlanFactor())
	switch {
	case !st.Busy() || !hasRelease:
	case st.Finishing && st.HasBedCoolLeft:
		st.ReleaseLeft, st.HasReleaseLeft = st.BedCoolLeft+release, true
	case st.HasTimeLeft:
		st.ReleaseLeft, st.HasReleaseLeft = st.TimeLeft+release, true
	}
	if k.Hist != nil && st.Reachable {
		k.mu.Lock()
		phases, planned := append([]Phase(nil), k.timer.phases...), k.timer.planned
		if st.Busy() {
			k.lastPos = st.FilePos
		}
		pos := k.lastPos
		k.mu.Unlock()
		material := func(path string) string { return k.FileMeta(path).Material }
		if key := k.Hist.add(st, now, phases, planned, pos, material); key != "" {
			k.useSpool(key, st.FinishedFilament)
			k.mu.Lock()
			k.timer.coolStart, k.timer.coolKey, k.timer.coolFrom = now, key, st.Bed
			k.mu.Unlock()
		}
		k.mu.Lock()
		seconds, key, done := k.timer.cooling(st, now)
		from, room := k.timer.coolFrom, k.timer.roomTemp()
		k.mu.Unlock()
		if done {
			k.Hist.setCool(key, seconds, from, room)
		}
	}
	k.projectOfFile(st)
	k.mu.Lock()
	k.st = st
	if st.Printing {
		k.tiltByAction = false // the start of a print measures the tilt again
	}
	k.mu.Unlock()
	k.View.Update(st)
	notify()
}

// projectOfFile puts a print in the project of its file at its start: the line ";Printdeck
// project:" of the Cura plugin. A project that the user picked on the deck wins.
func (k *Klippy) projectOfFile(st State) {
	if k.Hist == nil {
		return
	}
	k.mu.Lock()
	seen := k.projectPath
	k.projectPath = ""
	if st.Busy() {
		k.projectPath = st.FilePath
	}
	k.mu.Unlock()
	if !st.Busy() || st.FilePath == "" || st.FilePath == seen {
		return
	}
	p := []rune(k.FileMeta(st.FilePath).Project)
	if len(p) > 0 && k.Hist.NextProject(st.FilePath) == "" {
		k.Hist.SetNextProject(st.FilePath, string(p[:min(len(p), MaxProject)]))
	}
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
	if items, ok := s["configfile"]["save_config_pending_items"].(map[string]any); ok && len(items) == 1 {
		_, st.TiltOnlyPending = items["bed_tilt"]
	}
	if active, _ := s["printdeck_pause"]["active"].(bool); active {
		st.PauseActive = true
		st.PausePrompt = str(s["printdeck_pause"]["message"])
		st.PauseCheck, _ = s["printdeck_pause"]["check"].(bool)
		st.PauseChecked, _ = s["printdeck_pause"]["checked"].(bool)
		st.PauseDelta = num(s["printdeck_pause"]["delta"])
		st.PauseRoll = str(s["printdeck_pause"]["roll"])
	}
	if sd := s["printdeck_sdcard"]; sd != nil {
		st.SDCopy = SDCopy{ID: int(num(sd["id"])), Path: str(sd["copying"]), Copied: int64(num(sd["copied"])), Size: int64(num(sd["size"])),
			Done: str(sd["done"]), Err: str(sd["error"])}
	}
	st.ManualProbe, _ = s["manual_probe"]["is_active"].(bool)
	st.ManualZ = num(s["manual_probe"]["z_position"])
	if st.Busy() {
		st.FilePath = str(ps["filename"])
		st.File = path.Base(st.FilePath)
		sd := s["virtual_sdcard"]
		st.FileSize, st.FilePos = int64(num(sd["file_size"])), int64(num(sd["file_position"]))
		if mr := s["motion_report"]; mr != nil {
			if _, ok := mr["live_velocity"]; ok {
				st.LiveSpeed, st.LiveExtrude, st.HasLive = num(mr["live_velocity"]), num(mr["live_extruder_velocity"]), true
			}
		}
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
	st.ToolPower, st.BedPower = num(s["extruder"]["power"]), num(s["heater_bed"]["power"])
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

// Pause pauses the print. With [printdeck_pause], PRINTDECK_PAUSE also parks the nozzle away from
// the print, so that a filament change purges beside it.
func (k *Klippy) Pause() {
	if k.Machine().Has("printdeck_pause") {
		k.later(func() error { return k.script("PRINTDECK_PAUSE") })
		return
	}
	k.later(func() error { return k.call("pause_resume/pause", nil) })
}

// Resume resumes the print. After a PRINTDECK_PAUSE, PRINTDECK_RESUME primes the nozzle and takes the
// height check, then resumes.
func (k *Klippy) Resume() {
	if k.State().PauseActive {
		k.later(func() error { return k.script("PRINTDECK_RESUME") })
		return
	}
	k.later(func() error { return k.call("pause_resume/resume", nil) })
}

// Cancel cancels the print. CANCEL_PRINT of Klipper only stops the file, so the heaters and the
// part fan go off after it, also when the cancel fails. A printer with no [fan] has no M107. Then
// the printer presents the bed (present).
func (k *Klippy) Cancel() {
	k.later(func() error {
		err := k.call("pause_resume/cancel", nil)
		if e := k.script("TURN_OFF_HEATERS"); err == nil {
			err = e
		}
		k.script("M107")
		k.present()
		return err
	})
}

// cancelLift is the lift of the nozzle off the print after a cancel, in mm.
const cancelLift = 10.0

// present lifts the nozzle off the print, then moves X to the middle and Y to its maximum, and Z to
// the top. On a printer with a moving bed, as the TAZ Workhorse and the Mini 2, the maximum of Y puts
// the bed at the front. An axis that is not homed does not move.
func (k *Klippy) present() {
	th, ok := k.toolhead()
	if !ok {
		return
	}
	z := strings.Contains(th.homed, "z")
	if z {
		if dz := min(cancelLift, th.max[2]-th.pos[2]); dz > 0 {
			k.script("G91", fmt.Sprintf("G1 Z%.2f F600", dz), "G90")
		}
	}
	lines := []string{"G90"}
	if strings.Contains(th.homed, "x") && strings.Contains(th.homed, "y") {
		lines = append(lines, fmt.Sprintf("G1 X%.1f Y%.1f F3000", (th.min[0]+th.max[0])/2, th.max[1]))
	}
	if z {
		lines = append(lines, fmt.Sprintf("G1 Z%.1f F1200", th.max[2]))
	}
	if len(lines) > 1 {
		k.script(lines...)
	}
}

// toolheadState is the homed axes, the position and the limits of the axes, in mm.
type toolheadState struct {
	homed         string
	pos, min, max [3]float64
}

// toolhead reads the toolhead of Klipper.
func (k *Klippy) toolhead() (toolheadState, bool) {
	q, err := k.Query(map[string][]string{"toolhead": {"homed_axes", "position", "axis_minimum", "axis_maximum"}})
	if err != nil {
		return toolheadState{}, false
	}
	th := q["toolhead"]
	vec := func(key string) ([3]float64, bool) {
		var v [3]float64
		a, _ := th[key].([]any)
		if len(a) < 3 {
			return v, false
		}
		for i := range v {
			f, ok := a[i].(float64)
			if !ok {
				return v, false
			}
			v[i] = f
		}
		return v, true
	}
	var t toolheadState
	t.homed, _ = th["homed_axes"].(string)
	var ok1, ok2, ok3 bool
	t.pos, ok1 = vec("position")
	t.min, ok2 = vec("axis_minimum")
	t.max, ok3 = vec("axis_maximum")
	return t, ok1 && ok2 && ok3
}

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
	k.configChanged()
	return k.RestartKlipper()
}

// UseSim writes the config of a simulated printer (SimModels) as printer.cfg, and restarts
// Klipper. The real printer.cfg stays as printer.cfg.real. An empty id puts the real printer.cfg
// back. It returns an error during a print.
func (k *Klippy) UseSim(id string) error {
	if k.ConfigPath == "" {
		return errors.New("The deck has no printer.cfg")
	}
	if k.State().Busy() {
		return errors.New("Wait until the print ends")
	}
	var err error
	if id == "" {
		err = LeaveSim(k.ConfigPath)
	} else {
		err = UseSim(k.ConfigPath, id)
	}
	k.configChanged()
	if err != nil {
		return err
	}
	return k.RestartKlipper()
}

// configChanged makes the next state read printer.cfg again.
func (k *Klippy) configChanged() {
	k.mu.Lock()
	k.simRead = false
	k.mu.Unlock()
}

// simulated returns the name of the simulated printer of printer.cfg, or "". It reads printer.cfg
// once after each change.
func (k *Klippy) simulated() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	if !k.simRead && k.ConfigPath != "" {
		k.sim, k.simRead = SimulatedName(k.ConfigPath), true
	}
	return k.sim
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

// Query reads objects of Klipper now, for example the live position of motion_report.
func (k *Klippy) Query(objects map[string][]string) (klippy.Status, error) {
	k.mu.Lock()
	conn := k.conn
	k.mu.Unlock()
	if conn == nil {
		return nil, ErrNotReady
	}
	ctx, cancel := context.WithTimeout(context.Background(), controlTimeout)
	defer cancel()
	return conn.Query(ctx, objects)
}
