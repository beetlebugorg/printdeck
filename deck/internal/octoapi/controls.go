package octoapi

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"printdeck/internal/printer"
)

// The controls of the touch screen for the apps: the filament, the AMS, the aux nozzle, the
// chamber, the part fan, the speed and the flow of a print, and the Z offset. Each route calls the
// method of the backend that the touch screen calls, so the printer acts as on a tap on the deck.
// A route answers 204, 400 for a bad value, 404 when the printer has no such control, or 409 with
// the reason of the printer. These are fixed commands with checked values, so "Signed prints
// only" does not refuse them, as it does not refuse a jog or a temperature target. It refuses
// only free G-code (POST /api/printer/command).
//
// Where OctoPrint has a route, the deck uses it: the speed (printhead "feedrate"), the flow (tool
// "flowrate"), the aux nozzle (tool "tool1") and the chamber (/api/printer/chamber).
//
// /api/plugin/printdeck lists the controls of the printer (Server.controls) and the state that
// shows their effect (controlState), so an app shows only those controls, and checks each one.

// The optional methods of a backend for the controls.
type (
	fanSetter   interface{ Fan(percent int) error }
	speedSetter interface{ SpeedFactor(percent int) error }
	flowSetter  interface{ FlowFactor(percent int) error }
	extruder    interface {
		Extrude(mm float64, speed int) error
	}
	babyStepper   interface{ BabyStep(mm float64) error }
	chamberSetter interface{ SetChamberTemp(t int) error }
	tool2Setter   interface{ SetTool2Temp(t int) error }
	speedLeveler  interface {
		SpeedLevels() (names []string, now int)
		SetSpeedLevel(level int) error
	}
	trayLoader interface {
		LoadTray(slot int) error
		UnloadTray() error
	}
	amsDryer interface {
		DryAMS(unit int, filament string, temp, hours int) error
		StopDryAMS(unit int) error
	}
	amsReader interface{ AMS() printer.AMS }
)

// The names of the controls in /api/plugin/printdeck.
const (
	ControlJog      = "jog"
	ControlHome     = "home"
	ControlFan      = "fan"
	ControlSpeed    = "speed"
	ControlFlow     = "flow"
	ControlZOffset  = "zoffset"
	ControlExtrude  = "extrude"  // extrude and retract a length
	ControlFilament = "filament" // load and unload the filament of the extruder, or of a tray
	ControlDry      = "ams_dry"  // dry the filament of an AMS
	ControlChamber  = "chamber"  // the target of the chamber
	ControlTool1    = "tool1"    // the target of the aux nozzle: "tool1" in /api/printer/tool
)

// The lengths of the load and the unload of the filament, as the swap of the touch screen has
// them, when Server.SwapLengths is nil.
const (
	defaultUnloadMM = 80
	defaultLoadMM   = 70
)

// The limits of the values of the controls.
const (
	maxFactor   = 300 // speed and flow in percent
	minFactor   = 10
	maxExtrude  = 150 // mm in one request
	maxChamber  = 80  // °C
	maxDryTemp  = 90
	maxDryHours = 24
	maxZAdjust  = 0.5 // mm in one request
)

// controls returns the controls of the printer, for /api/plugin/printdeck.
func (s *Server) controls() []string {
	var out []string
	p := s.Printer
	_, bambu := p.(*printer.Bambu)
	add := func(ok bool, name string) {
		if ok {
			out = append(out, name)
		}
	}
	_, fan := p.(fanSetter)
	_, speed := p.(speedSetter)
	_, flow := p.(flowSetter)
	_, ext := p.(extruder)
	_, baby := p.(babyStepper)
	_, trays := p.(trayLoader)
	_, dry := p.(amsDryer)
	_, chamber := p.(chamberSetter)
	_, tool2 := p.(tool2Setter)
	st := p.State()
	add(!s.Resin, ControlJog)
	add(!s.Resin, ControlHome)
	add(fan, ControlFan)
	add(speed, ControlSpeed)
	add(flow && !bambu, ControlFlow) // the printer has no flow command (Bambu.FlowFactor)
	add(baby, ControlZOffset)
	add(ext, ControlExtrude)
	add(ext && !trays || trays && s.hasTrays(), ControlFilament)
	add(dry && s.hasTrays(), ControlDry)
	add(chamber && st.HasChamber, ControlChamber)
	add(tool2 && st.HasTool2, ControlTool1)
	return out
}

// hasTrays reports that the printer has an AMS with trays.
func (s *Server) hasTrays() bool {
	a, ok := s.Printer.(amsReader)
	return ok && len(a.AMS().Trays) > 0
}

// has reports that the printer has the control, else answers 404.
func (s *Server) has(w http.ResponseWriter, name string) bool {
	for _, c := range s.controls() {
		if c == name {
			return true
		}
	}
	apiError(w, http.StatusNotFound, "The printer has no such control")
	return false
}

// controlResult answers a control: 204, 404 for a control that the printer does not have, or
// 409 with the reason of the printer.
func controlResult(w http.ResponseWriter, err error) {
	if errors.Is(err, printer.ErrNotSupported) {
		apiError(w, http.StatusNotFound, "The printer has no such control")
		return
	}
	commandResult(w, err)
}

// printing refuses a control while a print runs, and with paused also while it is paused. The
// touch screen has the same rule.
func (s *Server) printing(w http.ResponseWriter, paused bool) bool {
	st := s.Printer.State()
	if st.Busy() && (paused || !st.Paused) {
		msg := "A print runs: pause it first"
		if paused {
			msg = "A print runs: this waits for the end of the print"
		}
		apiError(w, http.StatusConflict, msg)
		return true
	}
	return false
}

// factor checks a speed or a flow factor in percent.
func factor(w http.ResponseWriter, f *int) (int, bool) {
	if f == nil || *f < minFactor || *f > maxFactor {
		apiError(w, http.StatusBadRequest, fmt.Sprintf("factor must be %d to %d", minFactor, maxFactor))
		return 0, false
	}
	return *f, true
}

// feedrate sets the speed of the print: a factor in percent, or a level of a Bambu Lab printer
// by its name (controlState.SpeedLevels). A Bambu Lab printer takes the level nearest to a factor.
func (s *Server) feedrate(w http.ResponseWriter, f *int, level string) {
	if level != "" {
		l, ok := s.Printer.(speedLeveler)
		if !ok {
			apiError(w, http.StatusNotFound, "The printer has no speed levels")
			return
		}
		names, _ := l.SpeedLevels()
		for i, n := range names {
			if strings.EqualFold(n, level) {
				controlResult(w, l.SetSpeedLevel(i+1))
				return
			}
		}
		apiError(w, http.StatusBadRequest, "No such speed level: "+level)
		return
	}
	p, ok := factor(w, f)
	if !ok || !s.has(w, ControlSpeed) {
		return
	}
	controlResult(w, s.Printer.(speedSetter).SpeedFactor(p))
}

// flowrate sets the flow of the print in percent.
func (s *Server) flowrate(w http.ResponseWriter, f *int) {
	p, ok := factor(w, f)
	if !ok || !s.has(w, ControlFlow) {
		return
	}
	controlResult(w, s.Printer.(flowSetter).FlowFactor(p))
}

// POST /api/printer/chamber {"command": "target", "target": °C}: the target of the chamber, 0
// for off.
func (s *Server) serveChamber(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Command string `json:"command"`
		Target  *int   `json:"target"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Command != "target" || req.Target == nil || *req.Target < 0 || *req.Target > maxChamber {
		apiError(w, http.StatusBadRequest, fmt.Sprintf(`command must be "target", with a target of 0 to %d`, maxChamber))
		return
	}
	if !s.operational(w) || !s.has(w, ControlChamber) {
		return
	}
	controlResult(w, s.Printer.(chamberSetter).SetChamberTemp(*req.Target))
}

// POST /api/plugin/printdeck/fan {"percent": 0-100}: the part fan.
func (s *Server) serveFan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Percent *int `json:"percent"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Percent == nil || *req.Percent < 0 || *req.Percent > 100 {
		apiError(w, http.StatusBadRequest, "percent must be 0 to 100")
		return
	}
	if !s.operational(w) || !s.has(w, ControlFan) {
		return
	}
	controlResult(w, s.Printer.(fanSetter).Fan(*req.Percent))
}

// POST /api/plugin/printdeck/zoffset {"adjust": mm}: moves the nozzle up (positive) or down by
// adjust, as the baby steps of the touch screen. z_offset in the state has the offset now.
func (s *Server) serveZOffset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Adjust *float64 `json:"adjust"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Adjust == nil || *req.Adjust == 0 || math.Abs(*req.Adjust) > maxZAdjust {
		apiError(w, http.StatusBadRequest, fmt.Sprintf("adjust must be -%g to %g mm, and not 0", maxZAdjust, maxZAdjust))
		return
	}
	if !s.operational(w) || !s.has(w, ControlZOffset) {
		return
	}
	controlResult(w, s.Printer.(babyStepper).BabyStep(*req.Adjust))
}

// POST /api/plugin/printdeck/filament: the filament of the extruder.
//
//   - {"action": "extrude", "mm": n}: extrude n mm, or retract with a negative n. The nozzle must
//     be hot: Klipper refuses a cold extrusion, and the reply has its reason.
//   - {"action": "load"} and {"action": "unload"} on a Klipper printer: the G-code of the load and
//     the unload of the swap of the touch screen, with its lengths. The nozzle must be hot.
//   - {"action": "load", "slot": "A1"} on a Bambu Lab printer: load the spool of the slot of
//     filaments[] in /api/plugin/printdeck ("tray" takes the number of the tray in its place),
//     at the temperature of its material, as the AMS page does. {"action": "unload"} pulls the
//     filament back to its tray.
//
// During a print that is not paused, the deck refuses each action with 409.
func (s *Server) serveFilament(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string  `json:"action"`
		MM     float64 `json:"mm"`
		Slot   string  `json:"slot"`
		Tray   *int    `json:"tray"`
	}
	if !decode(w, r, &req) || !s.operational(w) || s.printing(w, false) {
		return
	}
	_, trays := s.Printer.(trayLoader)
	switch {
	case req.Action == "extrude":
		if req.MM == 0 || req.MM > maxExtrude || req.MM < -maxExtrude {
			apiError(w, http.StatusBadRequest, fmt.Sprintf("mm must be -%d to %d, and not 0", maxExtrude, maxExtrude))
			return
		}
		if !s.has(w, ControlExtrude) {
			return
		}
		controlResult(w, s.Printer.(extruder).Extrude(req.MM, 300))
	case trays && req.Action == "load":
		if !s.has(w, ControlFilament) {
			return
		}
		tray, ok := s.trayOf(req.Slot, req.Tray)
		if !ok {
			apiError(w, http.StatusNotFound, "No spool in that slot")
			return
		}
		controlResult(w, s.Printer.(trayLoader).LoadTray(tray))
	case trays && req.Action == "unload":
		if !s.has(w, ControlFilament) {
			return
		}
		controlResult(w, s.Printer.(trayLoader).UnloadTray())
	case req.Action == "load" || req.Action == "unload":
		if !s.has(w, ControlFilament) {
			return
		}
		unload, load := float64(defaultUnloadMM), float64(defaultLoadMM)
		if s.SwapLengths != nil {
			unload, load = s.SwapLengths()
		}
		// The same lines as the swap of the touch screen (deck/internal/ui/swap.go).
		lines := []string{"M83", "G1 E10 F300", fmt.Sprintf("G1 E-%.0f F1500", unload), "M82"}
		if req.Action == "load" {
			lines = []string{"M83", fmt.Sprintf("G1 E%.0f F600", load), "G1 E20 F150", "M82"}
		}
		controlResult(w, s.Printer.Gcode(lines...))
	default:
		apiError(w, http.StatusBadRequest, `action must be "load", "unload" or "extrude"`)
	}
}

// trayOf returns the number of the tray of a loaded spool: the tray, or the tray of the slot
// ("A1", "Ext") in the filaments of the printer. A slot that two spools have (the external spool
// of each nozzle of an X2D) needs the tray.
func (s *Server) trayOf(slot string, tray *int) (int, bool) {
	n, found := 0, 0
	for _, f := range filamentsOf(s.Printer) {
		if tray != nil && f.Tray == *tray || tray == nil && slot != "" && strings.EqualFold(f.Slot, slot) {
			n = f.Tray
			found++
		}
	}
	return n, found == 1
}

// POST /api/plugin/printdeck/ams/{unit}/dry {"temp": °C, "hours": n, "filament": "PLA"} dries
// the filament of the AMS unit: its letter ("A") or its number from 0. DELETE stops the drying.
func (s *Server) serveDry(w http.ResponseWriter, r *http.Request) {
	unit, ok := amsUnit(r.PathValue("unit"))
	if !ok {
		apiError(w, http.StatusBadRequest, `The unit is a letter ("A") or a number from 0`)
		return
	}
	stop := r.Method == http.MethodDelete
	var req struct {
		Temp     int    `json:"temp"`
		Hours    int    `json:"hours"`
		Filament string `json:"filament"`
	}
	if !stop {
		if !decode(w, r, &req) {
			return
		}
		if req.Temp <= 0 || req.Temp > maxDryTemp || req.Hours <= 0 || req.Hours > maxDryHours {
			apiError(w, http.StatusBadRequest, fmt.Sprintf("temp must be 1 to %d, and hours 1 to %d", maxDryTemp, maxDryHours))
			return
		}
	}
	if !s.operational(w) || !s.has(w, ControlDry) {
		return
	}
	d := s.Printer.(amsDryer)
	if stop {
		controlResult(w, d.StopDryAMS(unit))
		return
	}
	controlResult(w, d.DryAMS(unit, req.Filament, req.Temp, req.Hours))
}

// amsUnit reads the unit of an AMS: "A" to "P", or "0" to "15".
func amsUnit(v string) (int, bool) {
	if len(v) == 1 && v[0] >= 'A' && v[0] <= 'P' {
		return int(v[0] - 'A'), true
	}
	n, err := strconv.Atoi(v)
	return n, err == nil && n >= 0 && n <= 15
}

// position is a position of the head in mm.
type position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// heaterJSON is a heater: its temperature and its target in °C.
type heaterJSON struct {
	Actual float64 `json:"actual"`
	Target float64 `json:"target"`
}

// dryingJSON is the drying of an AMS: its temperature, the seconds left, and the filament.
type dryingJSON struct {
	Temp     int    `json:"temp"`
	Left     int    `json:"left"`
	Filament string `json:"filament,omitempty"`
}

// amsUnitJSON is a unit of an AMS.
type amsUnitJSON struct {
	Unit        string  `json:"unit"`
	Temp        float64 `json:"temp"`
	HumidityPct float64 `json:"humidity_pct"`
	// Humidity is the level of the printer, 1 (dry) to 5 (wet).
	Humidity int         `json:"humidity"`
	Drying   *dryingJSON `json:"drying"`
}

// controlState is the state that shows the effect of the controls, in /api/plugin/printdeck.
type controlState struct {
	// Homed is the axes that the printer homed, for example "xyz", or "".
	Homed string `json:"homed"`
	// Position is the position of the head, or null when the deck does not know it.
	Position *position `json:"position"`
	// Speed and Flow are the factors in percent, or 0 when the printer does not report them.
	Speed int `json:"speed,omitempty"`
	Flow  int `json:"flow,omitempty"`
	// SpeedLevels are the names of the speed levels of a Bambu Lab printer, and SpeedLevel the
	// level now, from 1, or 0. The speed route takes a name ("level").
	SpeedLevels []string `json:"speed_levels,omitempty"`
	SpeedLevel  int      `json:"speed_level,omitempty"`
	// ZOffset is the Z offset of the baby steps in mm.
	ZOffset float64 `json:"z_offset"`
	// Tool1 and Chamber are the aux nozzle and the chamber, or null.
	Tool1   *heaterJSON `json:"tool1"`
	Chamber *heaterJSON `json:"chamber"`
	// Fan is the part fan in percent, or null when the printer does not report it.
	Fan *float64 `json:"fan"`
	// Action is the action of the deck that runs now (a load, a calibration), or "".
	Action string `json:"action"`
	// Preparing is the step of the start of a print that is starting, or "".
	Preparing string `json:"preparing"`
	// AMS are the units of the AMS of a Bambu Lab printer.
	AMS []amsUnitJSON `json:"ams,omitempty"`
	// Messages are the messages of the printer, one for each line: the HMS messages of a Bambu
	// Lab printer have their codes in the text.
	Messages []string `json:"messages,omitempty"`
}

// state returns the state of the controls.
func (s *Server) state() controlState {
	st := s.Printer.State()
	out := controlState{Homed: st.Homed, Speed: st.SpeedPct, Flow: st.FlowPct, ZOffset: st.ZOffset, Action: st.Action}
	if !st.NoPosition && st.Homed != "" && st.HasZ {
		out.Position = &position{st.X, st.Y, st.Z}
	}
	if l, ok := s.Printer.(speedLeveler); ok {
		out.SpeedLevels, out.SpeedLevel = l.SpeedLevels()
	}
	if st.HasTool2 {
		out.Tool1 = &heaterJSON{st.Tool2, st.Tool2Target}
	}
	if st.HasChamber {
		out.Chamber = &heaterJSON{st.Chamber, st.ChamberTarget}
	}
	if st.HasFan {
		f := st.Fan
		out.Fan = &f
	}
	if st.Preparing {
		out.Preparing = st.Activity
	}
	if a, ok := s.Printer.(amsReader); ok {
		for _, u := range a.AMS().Units {
			j := amsUnitJSON{Unit: string(rune('A' + u.Index)), Temp: u.Temp, HumidityPct: u.HumidityPct, Humidity: u.Humidity}
			if u.Drying {
				j.Drying = &dryingJSON{u.DryTemp, int(u.DryLeft.Seconds()), u.DryFilament}
			}
			out.AMS = append(out.AMS, j)
		}
	}
	for _, l := range strings.Split(st.ErrDetail, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out.Messages = append(out.Messages, l)
		}
	}
	return out
}
