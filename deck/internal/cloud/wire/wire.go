// Package wire has the files of the cloud service that the deck writes and the pages read. It
// has no dependencies, so the WebAssembly of the pages stays small.
package wire

import "time"

// ShareStatus is status.json of a share: the state of the print for the share page.
type ShareStatus struct {
	// State is "heating", "printing", "paused", "cooling", "done", "cancelled" or "error".
	State   string `json:"state"`
	Job     string `json:"job"`
	Printer string `json:"printer"`
	// Activity is what the printer does now, for example "Heating bed".
	Activity string `json:"activity,omitempty"`
	// Progress is 0 to 1.
	Progress float64 `json:"progress"`
	Layer    int     `json:"layer,omitempty"`
	Layers   int     `json:"layers,omitempty"`
	// Height is the height of the nozzle, and MaxHeight the height of the model, in mm.
	Height    float64 `json:"height,omitempty"`
	MaxHeight float64 `json:"max_height,omitempty"`
	// TimeLeft is the time left of the print, and ReadyLeft the time until the part comes off
	// the bed: the time left and the cooling of the bed. Both are in seconds, or -1.
	TimeLeft  int `json:"time_left"`
	ReadyLeft int `json:"ready_left"`
	// The temperatures, in °C.
	Tool       float64 `json:"tool"`
	ToolTarget float64 `json:"tool_target"`
	Bed        float64 `json:"bed"`
	BedTarget  float64 `json:"bed_target"`
	Material   string  `json:"material,omitempty"`
	// Grams is the filament that the print used, and GramsTotal the filament of the file.
	Grams      float64   `json:"grams,omitempty"`
	GramsTotal float64   `json:"grams_total,omitempty"`
	Started    time.Time `json:"started,omitzero"`
	Ended      time.Time `json:"ended,omitzero"`
	// Expires is the time of the delete of the share, after the end.
	Expires time.Time `json:"expires,omitzero"`
	// Views are the names of the camera views, with a file cam-<view>.jpg each. Frame goes up at
	// each new frame.
	Views []string `json:"views,omitempty"`
	Frame int      `json:"frame"`
	// Lapse is the count of time lapse frames, lapse/0.jpg to lapse/<Lapse-1>.jpg, and LapseFPS
	// the rate of the video.
	Lapse    int `json:"lapse"`
	LapseFPS int `json:"lapse_fps,omitempty"`
	// Updated is the time of this status.
	Updated time.Time `json:"updated"`
}
