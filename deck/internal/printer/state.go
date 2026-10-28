// Package printer has what the deck screens and the print server share about the printer: its state,
// its files, and the picture of the print. A backend fills them in: today the OctoPrint client, and
// next the Klipper backend.
//
//	state.go     State and File
//	view.go      View: the picture of the print and the layer scrubber
//	activity.go  the "what the printer does now" text, from the state and the G-code
package printer

import (
	"time"

	"printdeck/internal/camera"
	"printdeck/internal/gcodemeta"
)

// File is a G-code file that the printer can print.
type File struct {
	Path string
	Name string
	Date time.Time
}

// State is the printer and its job, as the deck shows them.
type State struct {
	Reachable   bool
	Err         string
	ErrDetail   string // all the lines of Klipper's message, when Klipper reports an error
	Text        string // "Printing", "Operational", "Offline", ...
	Connected   bool   // the printer answers
	Printing    bool   // printing, pausing, or paused: a job is active
	Paused      bool
	File        string
	FilePath    string // path of the file in the file store
	Title       string // the title in the file, or "": Name is the name for the screens
	FileSize    int64
	FileDate    int64   // upload time (Unix): a file uploaded again under the same name has a new one
	FilePos     int64   // bytes of the file that the printer has read
	Completion  float64 // 0..100
	TimeLeft    time.Duration
	HasTimeLeft bool
	// BedCoolLeft is the time that the bed needs to cool to BedCoolTo: the target of the end of
	// the print, or ReleaseBed after a print. HasBedCoolLeft is false when the bed does not cool.
	BedCoolLeft    time.Duration
	BedCoolTo      float64
	HasBedCoolLeft bool
	// ReleaseLeft is the time until the part is easy to remove: the time left of the print, and
	// the time that the bed then needs to cool to ReleaseBed.
	ReleaseLeft    time.Duration
	HasReleaseLeft bool
	// BedCoolFrom is the bed temperature at the start of the cooling, for its progress.
	BedCoolFrom   float64
	Activity      string // what the printer does now: "Wiping nozzle", "Heating bed", "Printing"
	Layer, Layers int
	Fan           float64 // percent
	HasFan        bool
	Z             float64 // height of the head, mm
	HasZ          bool

	// The move at the print position: its kind and its commanded speed in mm/s. The printer buffers a few
	// moves, so this can be a moment ahead of the nozzle.
	NowKind  string
	NowSpeed float64
	HasNow   bool
	// The live speed of the head and of the filament into the extruder, in mm/s, from
	// motion_report of Klipper, during a print. HasLive is false when Klipper does not report them.
	LiveSpeed, LiveExtrude float64
	HasLive                bool

	Tool, ToolTarget float64
	Bed, BedTarget   float64
	// The power of each heater now, 0 to 1: the PWM of the heater. A heater that holds its
	// temperature switches on and off.
	ToolPower, BedPower float64

	// The position of the head (X, Y and Z with HasZ), and the homed axes, for example "xyz".
	X, Y  float64
	Homed string
	// Axis is the travel of X, Y and Z: Axis[0] the minimum, Axis[1] the maximum.
	Axis [2][3]float64
	// The time since the start of the print, and the filament used in mm.
	Elapsed time.Duration
	// PrintDuration is the time of the print without the pauses (print_duration of Klipper).
	PrintDuration time.Duration
	FilamentUsed  float64
	// Message is the message of M117.
	Message string
	// The Z offset of the head (SET_GCODE_OFFSET), and the speed and flow factors in percent.
	ZOffset           float64
	SpeedPct, FlowPct int
	// The end of the last print: Finished is "complete", "cancelled" or "error", or "" when no
	// print has ended since Klipper started.
	Finished                   string
	FinishedFile, FinishedPath string
	FinishedTitle              string // the title in the file, or ""
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
	// Finishing is true while the end of a print runs (PRINT_END), for example while the bed cools.
	Finishing bool
	// Prep has the steps of the start macro, or of the end macro, from the commands that run now
	// (PrepN of them). It is a fixed array, so that State stays comparable.
	Prep  [8]PrepStep
	PrepN int

	// NeedsSetup is true when printer.cfg has no [mcu] section: Klipper has no printer to use.
	NeedsSetup bool
	// Simulated is the name of the simulated printer when printer.cfg has "serial: sim", or "".
	// Then the prints run on the simulated board, and no real printer moves.
	Simulated string
	// PausePrompt is the message of a pause of the file (PRINTDECK_PAUSE), for example "Put the
	// case in the pocket", or "". PauseCheck is true when the pause has a height check on a part,
	// PauseChecked when it is done, and PauseDelta is the height of the part less the height in
	// the file, in mm.
	PauseActive              bool // a PRINTDECK_PAUSE: PRINTDECK_RESUME primes and resumes
	PausePrompt              string
	PauseCheck, PauseChecked bool
	PauseDelta               float64
	// PauseRoll is the ID of the roll of a color change of the file (PRINTDECK_PAUSE ROLL), or "".
	PauseRoll string
	// SDCopy is the copy of a file from the SD card of the printer ([printdeck_sdcard]).
	SDCopy SDCopy

	// Starting is true while Klipper starts: it reports "startup", or its socket is not there yet,
	// soon after the deck or Klipper started (startGrace). After that, the error shows.
	Starting bool
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

// Name is the name of the print for the screens: the title in the file, or else the cleaned file
// name.
func (s State) Name() string { return gcodemeta.Meta{Title: s.Title}.Name(s.File) }

// FinishedName is the name of the last print for the screens.
func (s State) FinishedName() string {
	return gcodemeta.Meta{Title: s.FinishedTitle}.Name(s.FinishedFile)
}

// Machine is what the printer has: the Klipper version, its modules and its macros.
type Machine struct {
	KlipperVersion string
	// HostAPI is the interface version of the Klipper host (package hostapi), or 0 when it is
	// not known.
	HostAPI int
	Modules []string // the objects of Klipper with no name, for example "probe" or "bed_tilt"
	Macros  []string // the macros that a person can run (no name with "_" first)
	// FilamentDiameter is filament_diameter of the extruder in printer.cfg, in mm; 0 is not known.
	FilamentDiameter float64
	// From printer.cfg, 0 when it is not known: the nozzle_diameter of the extruder, the
	// position_min and position_max of the X and Y steppers, the pressure_advance of the
	// extruder, and the max_accel and square_corner_velocity of the printer.
	NozzleDiameter       float64
	MinX, MaxX           float64
	MinY, MaxY           float64
	PressureAdvance      float64
	MaxAccel             float64
	SquareCornerVelocity float64
	// Kinematics is kinematics of the [printer] section, for example "cartesian" or "corexy";
	// "" is not known.
	Kinematics string
	// MeshMin and MeshMax are mesh_min and mesh_max of [bed_mesh], with HasMesh: the probed
	// area, inside the bed.
	MeshMin, MeshMax [2]float64
	HasMesh          bool
	// ZHomesMax is true when the Z endstop is at the top of the travel (position_endstop of
	// stepper_z at position_max), as on a TAZ: a homing of Z then moves the nozzle up, away from
	// the bed.
	ZHomesMax bool
	// The park_x and park_y options of [printdeck_timelapse], with HasParkX and HasParkY.
	ParkX, ParkY       float64
	HasParkX, HasParkY bool
}

// meshMargin is how far the print area of the calibration towers goes past the area of the bed
// mesh, in mm. The towers are camera.TowerInset inside the print area, so 5 mm inside the mesh.
const meshMargin = camera.TowerInset - 5

// TowerArea is the print area of the camera calibration towers: the area of the bed mesh with a
// margin, or else the guess of camera.PrintArea from the travel of X and Y. ok is false when
// printer.cfg has neither.
func (m Machine) TowerArea() (lo, hi [2]float64, ok bool) {
	switch {
	case m.HasMesh && m.MeshMax[0] > m.MeshMin[0] && m.MeshMax[1] > m.MeshMin[1]:
		return [2]float64{m.MeshMin[0] - meshMargin, m.MeshMin[1] - meshMargin},
			[2]float64{m.MeshMax[0] + meshMargin, m.MeshMax[1] + meshMargin}, true
	case m.MaxX > m.MinX && m.MaxY > m.MinY:
		lo, hi = camera.PrintArea([2]float64{m.MinX, m.MinY}, [2]float64{m.MaxX, m.MaxY})
		return lo, hi, true
	}
	return lo, hi, false
}

// BedMovesY reports that the bed of the printer moves in Y, from its kinematics: a cartesian
// printer, such as a TAZ, a Prusa i3 or an Ender 3, moves the bed in Y. It is the default of the
// setting of the camera calibration.
func (m Machine) BedMovesY() bool { return m.Kinematics == "cartesian" }

// Has reports that the printer has a module.
func (m Machine) Has(module string) bool {
	for _, x := range m.Modules {
		if x == module {
			return true
		}
	}
	return false
}
