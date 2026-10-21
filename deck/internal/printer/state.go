// Package printer has what the deck screens and the print server share about the printer: its state,
// its files, and the picture of the print. A backend fills them in: today the OctoPrint client, and
// next the Klipper backend.
//
//	state.go     State and File
//	view.go      View: the picture of the print and the layer scrubber
//	activity.go  the "what the printer does now" text, from the state and the G-code
package printer

import "time"

// File is a G-code file that the printer can print.
type File struct {
	Path string
	Name string
	Date time.Time
}

// State is the printer and its job, as the deck shows them.
type State struct {
	Reachable     bool
	Err           string
	ErrDetail     string // all the lines of Klipper's message, when Klipper reports an error
	Text          string // "Printing", "Operational", "Offline", ...
	Connected     bool   // the printer answers
	Printing      bool   // printing, pausing, or paused: a job is active
	Paused        bool
	File          string
	FilePath      string // path of the file in the file store
	FileSize      int64
	FileDate      int64   // upload time (Unix): a file uploaded again under the same name has a new one
	FilePos       int64   // bytes of the file that the printer has read
	Completion    float64 // 0..100
	TimeLeft      time.Duration
	HasTimeLeft   bool
	Activity      string // what the printer does now: "Wiping nozzle", "Heating bed", "Printing"
	Layer, Layers int
	Fan           float64 // percent
	HasFan        bool
	Z             float64 // height of the head, mm
	HasZ          bool

	// The move at the print position: its kind and its commanded speed in mm/s. The printer buffers a few
	// moves, so this can be a moment ahead of the nozzle.
	NowKind          string
	NowSpeed         float64
	HasNow           bool
	Tool, ToolTarget float64
	Bed, BedTarget   float64

	// The position of the head (X, Y and Z with HasZ), and the homed axes, for example "xyz".
	X, Y  float64
	Homed string
	// Axis is the travel of X, Y and Z: Axis[0] the minimum, Axis[1] the maximum.
	Axis [2][3]float64
	// The time since the start of the print, and the filament used in mm.
	Elapsed      time.Duration
	FilamentUsed float64
	// Message is the message of M117.
	Message string
	// The Z offset of the head (SET_GCODE_OFFSET), and the speed and flow factors in percent.
	ZOffset           float64
	SpeedPct, FlowPct int
	// The end of the last print: Finished is "complete", "cancelled" or "error", or "" when no
	// print has ended since Klipper started.
	Finished                   string
	FinishedFile, FinishedPath string
	FinishedTime               time.Duration
	FinishedFilament           float64 // mm
	FinishedError              string
	SaveConfigPending          bool // a calibration changed the config, and SAVE_CONFIG writes it

	// ManualProbe is true while a manual Z probe runs (PROBE_CALIBRATE, Z_ENDSTOP_CALIBRATE): the
	// nozzle is at ManualZ, and TESTZ, ACCEPT and ABORT finish it.
	ManualProbe bool
	ManualZ     float64

	// Preparing is true while the start of a print runs: heating, homing, probing, priming. HasWipe
	// is true when the start wipes the nozzle.
	Preparing, HasWipe bool

	// NeedsSetup is true when printer.cfg has no [mcu] section: Klipper has no printer to use.
	NeedsSetup bool
	// Firmware is the version of the firmware on the board. FirmwareOld is true when it is not
	// the Klipper commit of the host.
	Firmware    string
	FirmwareOld bool

	// Action is the maintenance action that the deck runs now (RunAction), for example "Bed
	// tilt", or "". LastAction is the last action that ended.
	Action      string
	ActionStart time.Time
	LastAction  ActionResult
}

// ActionResult is the end of a maintenance action: its name, its error ("" when it worked), and
// the time of its end.
type ActionResult struct {
	Name string
	Err  string
	End  time.Time
}

// Busy reports that a job is running or paused.
func (s State) Busy() bool { return s.Printing || s.Paused }

// Machine is what the printer has: the Klipper version, its modules and its macros.
type Machine struct {
	KlipperVersion string
	Modules        []string // the objects of Klipper with no name, for example "probe" or "bed_tilt"
	Macros         []string // the macros that a person can run (no name with "_" first)
	// Mirror is true when the printer has the macro _PRINTDECK of printdeck.cfg: the deck can
	// show its lines on the display of the printer.
	Mirror bool
}

// Has reports that the printer has a module.
func (m Machine) Has(module string) bool {
	for _, x := range m.Modules {
		if x == module {
			return true
		}
	}
	return false
}
