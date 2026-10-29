// Package wire has the files of the cloud service that the deck writes and the pages read. It
// has no dependencies, so the WebAssembly of the pages stays small.
package wire

import "time"

// ShareStatus is status.json of a share: the state of the print for the share page.
type ShareStatus struct {
	// State is "heating", "printing", "paused", "cooling", "done", "cancelled" or "error", or
	// "waiting" while the deck has no connection to the printer.
	State   string `json:"state"`
	Job     string `json:"job"`
	Printer string `json:"printer"`
	// PrinterModel is the brand and the model of the printer, for example "LulzBot TAZ Workhorse", or "".
	PrinterModel string `json:"printer_model,omitempty"`
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
	// Move is the kind of the move now, for example "Outer wall", and Speed the speed of the head
	// in mm/s.
	Move  string  `json:"move,omitempty"`
	Speed float64 `json:"speed,omitempty"`
	// Fan is the part fan in percent, and SpeedPct and FlowPct the factors of Tune.
	Fan      int `json:"fan,omitempty"`
	SpeedPct int `json:"speed_pct,omitempty"`
	FlowPct  int `json:"flow_pct,omitempty"`
	// Roll is the roll in the printer, or nil.
	Roll *ShareRoll `json:"roll,omitempty"`
	// Filament is the filament of the print, or nil.
	Filament *ShareFilament `json:"filament,omitempty"`
	// LayerStarted is the start of the layer now, when the deck saw it start. LayerTimes are the
	// measured seconds of the layers that are done, from layer 1: 0 is a layer with no time (a
	// pause, or a layer before the share). Plan goes up when plan.json has the plan of the slicer.
	LayerStarted time.Time `json:"layer_started,omitzero"`
	LayerTimes   []float64 `json:"layer_times,omitempty"`
	Plan         int       `json:"plan,omitempty"`
	// LCD goes up at each new lcd.bin: the next LCDFrames frames of the display of the deck, 4 a
	// second. A frame is 128 x 64 dots, 16 bytes for each row from the top, the left dot in the
	// high bit: 1024 bytes.
	LCD       int `json:"lcd,omitempty"`
	LCDFrames int `json:"lcd_frames,omitempty"`
	// Model goes up at each new model.jpg: the picture of the model, with the part that is
	// printed in color.
	Model int `json:"model,omitempty"`
	// Updated is the time of this status.
	Updated time.Time `json:"updated"`
}

// ShareReady is ready.json of a share: the flag that the owner sets when the part is ready to
// pick up, a note, and the photos of the part. The service writes it.
type ShareReady struct {
	Ready  bool         `json:"ready"`
	At     time.Time    `json:"at,omitzero"`
	Note   string       `json:"note,omitempty"`
	Photos []SharePhoto `json:"photos,omitempty"`
	// Expires is the time of the delete of the share: 24 hours after the end of the print, or
	// after Ready.
	Expires time.Time `json:"expires,omitzero"`
}

// SharePhoto is a photo of a share: the file of the photo (at most 2048 px) and of its small
// copy (at most 480 px), under s/<id>/, and the size of the photo.
type SharePhoto struct {
	File  string `json:"file"`
	Small string `json:"small"`
	W     int    `json:"w"`
	H     int    `json:"h"`
}

// Grant lets the deck upload files under Prefix until Expires, with an S3 POST form: the form
// has Fields, the field "key" with a key under Prefix, the fields "Content-Type" and
// "Cache-Control", and the file last. A file has at most MaxBytes.
type Grant struct {
	URL      string            `json:"url"`
	Fields   map[string]string `json:"fields"`
	Prefix   string            `json:"prefix"`
	MaxBytes int64             `json:"max_bytes"`
	Expires  time.Time         `json:"expires"`
}

// ReadyRequest is the body of POST /api/v1/shares/<id>/ready.
type ReadyRequest struct {
	Ready bool   `json:"ready"`
	Note  string `json:"note"`
}

// PhotoReply is the reply to POST /api/v1/shares/<id>/photo: the ID of the photo, and a grant for
// its upload as the file "photo" under the prefix of the grant. POST .../photo/<id>/done then
// adds the photo to the share.
type PhotoReply struct {
	Photo string `json:"photo"`
	Grant Grant  `json:"grant"`
}

// ShareRoll is the roll in the printer: its name, its color (#rrggbb or ""), and the grams on it
// now and when full.
type ShareRoll struct {
	Name  string  `json:"name"`
	Color string  `json:"color,omitempty"`
	Left  float64 `json:"left"`
	Full  float64 `json:"full"`
}

// ShareFilament is the filament of the print: from the roll in the printer and its material in
// the slicer, else from the file. An empty field means that the deck does not know it.
type ShareFilament struct {
	// Name is the name of the filament, for example "Microcenter White PLA", and Brand its brand,
	// for example "Microcenter".
	Name  string `json:"name,omitempty"`
	Brand string `json:"brand,omitempty"`
	// Type is the type of the material, for example "PLA", and Color its color, #rrggbb.
	Type  string `json:"type,omitempty"`
	Color string `json:"color,omitempty"`
	// Diameter is the diameter of the filament in mm, and RollSize the grams of a full roll.
	Diameter float64 `json:"diameter,omitempty"`
	RollSize float64 `json:"roll_size,omitempty"`
	// The settings of the slicer: the layer height in mm, and the temperatures in °C.
	LayerHeight float64 `json:"layer_height,omitempty"`
	NozzleTemp  int     `json:"nozzle_temp,omitempty"`
	BedTemp     int     `json:"bed_temp,omitempty"`
}

// SharePlan is plan.json of a share: the seconds of each layer from the slicer, from layer 1.
type SharePlan struct {
	Layers []float64 `json:"layers"`
}
