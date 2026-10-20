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
}

// Busy reports that a job is running or paused.
func (s State) Busy() bool { return s.Printing || s.Paused }
