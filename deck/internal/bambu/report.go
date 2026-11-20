package bambu

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Report is the part of the "print" report that the deck uses.
type Report struct {
	State     string `json:"gcode_state"` // IDLE, PREPARE, RUNNING, PAUSE, FINISH, FAILED, SLICING
	Percent   Num    `json:"mc_percent"`
	Remaining Num    `json:"mc_remaining_time"` // minutes
	Layer     Num    `json:"layer_num"`
	Layers    Num    `json:"total_layer_num"`
	Stage     Num    `json:"stg_cur"` // what the printer does now (StageText)
	Stages    []Num  `json:"stg"`     // the steps of the start of the print, in order (StepName)
	Subtask   string `json:"subtask_name"`
	File      string `json:"gcode_file"`
	TaskID    string `json:"task_id"`
	PrintType string `json:"print_type"` // "local", "cloud", "system" (a calibration of the printer)
	Error     Num    `json:"print_error"`
	// ErrSeq is "err": a counter in its first two characters and the print error, for example
	// "1A0300400C". A new error changes the counter.
	ErrSeq string `json:"err"`
	HMS    []HMS  `json:"hms"`

	Nozzle       Num    `json:"nozzle_temper"`
	NozzleTarget Num    `json:"nozzle_target_temper"`
	Bed          Num    `json:"bed_temper"`
	BedTarget    Num    `json:"bed_target_temper"`
	Chamber      Num    `json:"chamber_temper"`
	Fan          Num    `json:"cooling_fan_speed"` // 0 to 15
	AuxFan       Num    `json:"big_fan1_speed"`    // 0 to 15
	ChamberFan   Num    `json:"big_fan2_speed"`    // 0 to 15
	SpeedLevel   Num    `json:"spd_lvl"`           // 1 silent, 2 standard, 3 sport, 4 ludicrous
	SpeedPct     Num    `json:"spd_mag"`
	NozzleDiam   string `json:"nozzle_diameter"`
	NozzleType   string `json:"nozzle_type"`
	WiFi         string `json:"wifi_signal"`

	Device Device `json:"device"`
	IPCam  IPCam  `json:"ipcam"`
	XCam   *XCam  `json:"xcam"`
	AMS    struct {
		Units []AMSUnit `json:"ams"`
		Now   string    `json:"tray_now"` // the tray that feeds now (Unit*4+Index), "255" for none
		// Exist has a bit for each tray with a spool, in hex: bit Unit*4+Index.
		Exist string `json:"tray_exist_bits"`
	} `json:"ams"`
	// External are the spool holders out of the AMS (vir_slot): 254 and 255 on an X2D.
	External []Tray `json:"vir_slot"`
	// Mapping is the tray of each filament of the print now. Its code is not checked on a print
	// yet (AMSMapping).
	Mapping  []int64 `json:"mapping"`
	PlateIdx Num     `json:"plate_idx"`
	SDCard   bool    `json:"sdcard"`
	Lights   []struct {
		Node string `json:"node"`
		Mode string `json:"mode"`
	} `json:"lights_report"`
}

// Device is the "device" object of an H2D or an X2D: the two extruders, the chamber.
type Device struct {
	Extruder struct {
		Info []struct {
			ID   int `json:"id"`
			Temp Num `json:"temp"` // the temperature now in the low 16 bits, the target in the high 16 bits
			// Feed is the spool that feeds the extruder now: the AMS in the high byte and the
			// tray in the low byte, 0xFF00 or 0xFE00 an external spool, 0xFFFF none.
			Feed Num `json:"snow"`
		} `json:"info"`
		// State has the number of extruders in bits 0 to 3, and the extruder in use in bits 4
		// to 7.
		State Num `json:"state"`
	} `json:"extruder"`
	Nozzle struct {
		Info []struct {
			ID       int     `json:"id"`
			Diameter float64 `json:"diameter"`
			Type     string  `json:"type"`
		} `json:"info"`
	} `json:"nozzle"`
	// Plate is the build plate that the printer found: CurID is the code of the plate on the
	// heatbed (for example "P0101"), or "" when the printer found none.
	Plate struct {
		CurID string `json:"cur_id"`
		Base  Num    `json:"base"`
		Mat   Num    `json:"mat"`
	} `json:"plate"`
	CTC struct {
		Info struct {
			Temp Num `json:"temp"`
		} `json:"info"`
	} `json:"ctc"`
	// Cam is the camera of the printer. TimelapsePath is the file of the last time lapse video, for
	// example "/userdata/media/timelapse/video_2026-11-20_06-38-46_4.mp4" in the memory of the
	// printer, or a file on its card.
	Cam struct {
		TimelapsePath string `json:"timelapse_path"`
	} `json:"cam"`
}

// IPCam is the camera of the printer.
type IPCam struct {
	RTSP       string `json:"rtsp_url"`
	Resolution string `json:"resolution"`
	Timelapse  string `json:"timelapse"`    // "enable" or "disable"
	Record     string `json:"ipcam_record"` // "enable" or "disable": the recording of each print
}

// XCam is the camera AI of the printer: the checks that watch a print.
type XCam struct {
	FirstLayer  bool   `json:"first_layer_inspector"`
	Spaghetti   bool   `json:"spaghetti_detector"`
	PlateMarker bool   `json:"buildplate_marker_detector"`
	Halt        bool   `json:"print_halt"`             // a check pauses the print
	Sensitivity string `json:"halt_print_sensitivity"` // "low", "medium" or "high"
}

// AMSUnit is one AMS.
type AMSUnit struct {
	ID          string `json:"id"`
	Humidity    Num    `json:"humidity"`     // a level of 1 (dry) to 5 (wet)
	HumidityPct Num    `json:"humidity_raw"` // the relative humidity in percent
	Temp        Num    `json:"temp"`         // °C
	Trays       []Tray `json:"tray"`
	// DryTime is the drying time left in minutes, 0 when the AMS does not dry. DrySetting is the
	// drying that runs.
	DryTime    Num `json:"dry_time"`
	DrySetting struct {
		Temp     Num    `json:"dry_temperature"`
		Hours    Num    `json:"dry_duration"`
		Filament string `json:"dry_filament"`
	} `json:"dry_setting"`
}

// Tray is one slot of an AMS.
type Tray struct {
	ID     string `json:"id"`
	Type   string `json:"tray_type"`  // "PLA"
	Color  string `json:"tray_color"` // "898989FF"; "00000000" is no color
	Brand  string `json:"tray_sub_brands"`
	Remain Num    `json:"remain"`      // the percent left of a spool with an RFID tag of Bambu Lab, or -1
	TagUID string `json:"tag_uid"`     // the RFID tag, all zeros with no tag
	Weight Num    `json:"tray_weight"` // the grams of a full spool, from the RFID tag, or 0
	// TempMax is the highest nozzle temperature of the filament in °C, or 0.
	TempMax Num `json:"nozzle_temp_max"`
}

// Tag returns the RFID tag of the spool, or "" with no tag.
func (t Tray) Tag() string {
	if strings.Trim(t.TagUID, "0") == "" {
		return ""
	}
	return t.TagUID
}

// ColorHex returns the color of the tray as "#rrggbb", or "" with no color.
func (t Tray) ColorHex() string {
	c := strings.ToLower(t.Color)
	if len(c) < 6 || c == "00000000" {
		return ""
	}
	return "#" + c[:6]
}

// Known reports that the printer knows the filament left on the spool (Remain).
func (t Tray) Known() bool {
	return t.Remain >= 0 && t.Tag() != ""
}

// TrayLoaded reports that the AMS has a spool in the tray slot (Unit*4+Index).
func (r Report) TrayLoaded(slot int) bool {
	bits, err := strconv.ParseUint(r.AMS.Exist, 16, 64)
	return err == nil && slot >= 0 && slot < 64 && bits>>uint(slot)&1 == 1
}

// ActiveSlot returns the tray that feeds now (Unit*4+Index, or an external spool), or -1. On a
// printer with two extruders it is the spool of the extruder in use.
func (r Report) ActiveSlot() int {
	if id := r.ActiveExtruder(); id >= 0 {
		for _, e := range r.Extruders() {
			if e.ID == id {
				return e.Feed
			}
		}
		return -1
	}
	n, err := strconv.Atoi(r.AMS.Now)
	if err != nil || n >= 254 {
		return -1
	}
	return n
}

// AMSMapping returns the tray of each filament of the print, from Mapping: Unit*4+Index for a
// tray of an AMS, ExternalMain or ExternalDeputy for an external spool, or -1 for a filament with
// no tray. The code of a value is the AMS in the high byte and the tray in the low byte, as in
// "snow" (feedSlot). A two-color print of the X2D on 2026-11-19 had [2, 65280]: tray 3 of the AMS,
// and the external spool 255.
func (r Report) AMSMapping() []int {
	out := make([]int, len(r.Mapping))
	for i, v := range r.Mapping {
		out[i] = feedSlot(v)
	}
	return out
}

// HMS is one fault of the health system of the printer.
type HMS struct {
	Attr int64 `json:"attr"`
	Code int64 `json:"code"`
}

// String returns the HMS code as the wiki of Bambu Lab shows it: "0500-0300-0002-0001".
func (h HMS) String() string {
	return fmt.Sprintf("%04X-%04X-%04X-%04X", h.Attr>>16&0xffff, h.Attr&0xffff, h.Code>>16&0xffff, h.Code&0xffff)
}

// Num is a number that the printer sends as a number or as a string.
type Num float64

// UnmarshalJSON reads 12, 12.5, "12" or "12.5". Another string is 0.
func (n *Num) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*n = 0
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		*n = 0
		return nil
	}
	*n = Num(v)
	return nil
}

// Int returns n as an int.
func (n Num) Int() int { return int(n) }

// Nozzles returns the temperature and the target of each extruder of an H2D or an X2D, in °C.
// ok is false for a printer with no "device.extruder" object.
func (r Report) Nozzles() (temp, target []float64, ok bool) {
	for _, e := range r.Device.Extruder.Info {
		v := int64(e.Temp)
		temp = append(temp, float64(v&0xffff))
		target = append(target, float64(v>>16&0xffff))
	}
	return temp, target, len(temp) > 0
}

// Extruder is one extruder of an H2D or an X2D.
type Extruder struct {
	ID           int
	Temp, Target float64
	// Feed is the spool that feeds the extruder: a tray of an AMS (Unit*4+Index), an external
	// spool (ExternalMain or ExternalDeputy), or -1.
	Feed int
}

// The IDs of the external spools (vir_slot) in the reports.
const (
	ExternalMain   = 255
	ExternalDeputy = 254
)

// feedSlot returns the tray of the code v of "snow": Unit*4+Index for a tray of an AMS, 255 or
// 254 for an external spool (0xFF00, 0xFE00), or -1 for none (0xFFFF, 0xFEFF).
func feedSlot(v int64) int {
	unit, tray := int(v>>8&0xff), int(v&0xff)
	switch {
	case v == 0xffff || v < 0:
		return -1
	case unit == ExternalMain || unit == ExternalDeputy:
		// 0xFF00 and 0xFE00 are the external spools. 0xFEFF is no spool: the X2D reports it for
		// the main nozzle with no filament in it.
		if tray != 0 {
			return -1
		}
		return unit
	case unit >= 0x80 || tray > 3:
		return -1 // an AMS HT, or a code that the deck does not know
	}
	return unit*4 + tray
}

// Extruders returns the extruders of an H2D or an X2D, in the order of their IDs, or none.
func (r Report) Extruders() []Extruder {
	var out []Extruder
	for _, e := range r.Device.Extruder.Info {
		v := int64(e.Temp)
		out = append(out, Extruder{ID: e.ID, Temp: float64(v & 0xffff), Target: float64(v >> 16 & 0xffff),
			Feed: feedSlot(int64(e.Feed))})
	}
	return out
}

// ActiveExtruder returns the ID of the extruder in use, or -1 for a printer with one extruder.
func (r Report) ActiveExtruder() int {
	if len(r.Device.Extruder.Info) < 2 {
		return -1
	}
	v := r.Device.Extruder.State.Int()
	n, id := v&0xf, v>>4&0xf
	if id >= n {
		return -1
	}
	return id
}

// ExternalTrays returns the external spools (vir_slot) with their IDs (ExternalMain,
// ExternalDeputy). A tray with no material has no spool, or no spool that the user set.
func (r Report) ExternalTrays() map[int]Tray {
	out := map[int]Tray{}
	for _, t := range r.External {
		if id, err := strconv.Atoi(t.ID); err == nil {
			out[id] = t
		}
	}
	return out
}

// ChamberTemp returns the temperature of the chamber in °C: device.ctc on an H2D or an X2D, else
// chamber_temper.
func (r Report) ChamberTemp() float64 {
	t, _ := r.ChamberHeat()
	return t
}

// ChamberHeat returns the temperature of the chamber and its target in °C. device.ctc of an H2D
// or an X2D has the temperature in the low 16 bits and the target in the high 16 bits, as the
// extruders (the target is not checked with a heated chamber yet). Other printers send
// chamber_temper, and no target.
func (r Report) ChamberHeat() (temp, target float64) {
	if v := int64(r.Device.CTC.Info.Temp); v != 0 {
		return float64(v & 0xffff), float64(v >> 16 & 0xffff)
	}
	return float64(r.Chamber), 0
}

// HasChamber reports that the printer sends a chamber temperature.
func (r Report) HasChamber() bool {
	return r.Device.CTC.Info.Temp != 0 || r.Chamber != 0
}

// FanPercent returns the part fan in percent.
func (r Report) FanPercent() float64 {
	return float64(r.Fan) / 15 * 100
}

// Elapsed returns an estimate of the time since the start of the print, for a print that the deck
// sees first after its start (after a restart of the deck). The printer reports no start time.
// mc_percent follows the time of the print, so the time so far is the time left times
// percent / (100 - percent). It is 0 at the start of a print, and with no time left.
func (r Report) Elapsed() time.Duration {
	p, left := float64(r.Percent), float64(r.Remaining)
	if p <= 0 || p >= 100 || left <= 0 {
		return 0
	}
	return time.Duration(left * p / (100 - p) * float64(time.Minute)).Round(time.Minute)
}

// TimeLeft returns the time left of the print.
func (r Report) TimeLeft() time.Duration {
	return time.Duration(r.Remaining) * time.Minute
}

// Light reports that the light node is on.
func (r Report) Light(node string) bool {
	for _, l := range r.Lights {
		if l.Node == node {
			return l.Mode == "on"
		}
	}
	return false
}

// HasLight reports that the printer has the light node.
func (r Report) HasLight(node string) bool {
	for _, l := range r.Lights {
		if l.Node == node {
			return true
		}
	}
	return false
}

// Busy reports that a print runs or is paused.
func (r Report) Busy() bool {
	switch r.State {
	case "PREPARE", "RUNNING", "PAUSE", "SLICING":
		return true
	}
	return false
}

// Paused reports that the print is paused.
func (r Report) Paused() bool { return r.State == "PAUSE" }

// Name returns the name of the print: the subtask, or the file with no folder and no extension.
func (r Report) Name() string {
	n := r.Subtask
	if n == "" {
		n = r.File
	}
	if i := strings.LastIndexByte(n, '/'); i >= 0 {
		n = n[i+1:]
	}
	for _, ext := range []string{".gcode.3mf", ".3mf", ".gcode"} {
		n = strings.TrimSuffix(n, ext)
	}
	return n
}

// stageText is the text of each value of stg_cur. The list comes from the Home Assistant
// integration of Bambu printers (ha-bambulab).
var stageText = map[int]string{
	0:  "Printing",
	1:  "Leveling the bed",
	2:  "Heating the bed",
	3:  "Sweeping the XY mechanism",
	4:  "Changing the filament",
	5:  "Paused (M400)",
	6:  "Paused: the filament ran out",
	7:  "Heating the nozzle",
	8:  "Calibrating the extrusion",
	9:  "Scanning the bed surface",
	10: "Inspecting the first layer",
	11: "Identifying the build plate",
	12: "Calibrating the micro lidar",
	13: "Homing the toolhead",
	14: "Cleaning the nozzle tip",
	15: "Checking the extruder temperature",
	16: "Paused by the user",
	17: "Paused: the front cover fell",
	18: "Calibrating the micro lidar",
	19: "Calibrating the extrusion flow",
	20: "Paused: a nozzle temperature fault",
	21: "Paused: a bed temperature fault",
	22: "Unloading the filament",
	23: "Paused: a skipped step",
	24: "Loading the filament",
	25: "Calibrating the motor noise",
	26: "Paused: the AMS is lost",
	27: "Paused: the heatbreak fan is slow",
	28: "Paused: a chamber temperature fault",
	29: "Cooling the chamber",
	30: "Paused by the G-code",
	31: "Showing the motor noise",
	32: "Paused: filament covers the nozzle",
	33: "Paused: a cutter fault",
	34: "Paused: a first layer fault",
	35: "Paused: the nozzle is clogged",
}

// stepNames are the steps of the start of a print, as the list of the steps names them, with the
// kind of the step for the screens (printer.PrepStep). A step that is not in the list has no name.
var stepNames = map[int][2]string{
	1:  {"Level the bed", "probe"},
	2:  {"Heat the bed", "heat-bed"},
	3:  {"Sweep the XY mechanism", ""},
	4:  {"Change the filament", ""},
	7:  {"Heat the nozzle", "heat-nozzle"},
	8:  {"Calibrate the extrusion", ""},
	9:  {"Scan the bed surface", "probe"},
	10: {"Inspect the first layer", ""},
	11: {"Identify the build plate", ""},
	12: {"Calibrate the micro lidar", ""},
	13: {"Home the toolhead", "home"},
	14: {"Clean the nozzle tip", "wipe"},
	15: {"Check the extruder temperature", ""},
	18: {"Calibrate the micro lidar", ""},
	19: {"Calibrate the extrusion flow", ""},
	22: {"Unload the filament", ""},
	24: {"Load the filament", ""},
	25: {"Calibrate the motor noise", ""},
	29: {"Cool the chamber", "cool"},
}

// StepName returns the name and the kind of the step n of the start of a print. ok is false
// for a step that the deck does not know.
func StepName(n int) (name, kind string, ok bool) {
	s, ok := stepNames[n]
	return s[0], s[1], ok
}

// Preparing reports that the printer runs a step of the start of a print: the print has no
// layer yet, and stg_cur is a step other than the print.
func (r Report) Preparing() bool {
	if r.State == "PREPARE" || r.State == "SLICING" {
		return true
	}
	n := r.Stage.Int()
	return r.State == "RUNNING" && r.Layer.Int() == 0 && n > 0 && n < 255 && !r.Paused()
}

// StageText returns what the printer does now, or "" when stg_cur has no text.
func (r Report) StageText() string {
	return stageText[r.Stage.Int()]
}
