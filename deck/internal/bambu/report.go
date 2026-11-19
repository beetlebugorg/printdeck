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
	Subtask   string `json:"subtask_name"`
	File      string `json:"gcode_file"`
	TaskID    string `json:"task_id"`
	PrintType string `json:"print_type"` // "local", "cloud", "system" (a calibration of the printer)
	Error     Num    `json:"print_error"`
	HMS       []HMS  `json:"hms"`

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
		} `json:"info"`
		State Num `json:"state"`
	} `json:"extruder"`
	Nozzle struct {
		Info []struct {
			ID       int     `json:"id"`
			Diameter float64 `json:"diameter"`
			Type     string  `json:"type"`
		} `json:"info"`
	} `json:"nozzle"`
	CTC struct {
		Info struct {
			Temp Num `json:"temp"`
		} `json:"info"`
	} `json:"ctc"`
}

// IPCam is the camera of the printer.
type IPCam struct {
	RTSP       string `json:"rtsp_url"`
	Resolution string `json:"resolution"`
}

// AMSUnit is one AMS.
type AMSUnit struct {
	ID          string `json:"id"`
	Humidity    Num    `json:"humidity"`     // a level of 1 (dry) to 5 (wet)
	HumidityPct Num    `json:"humidity_raw"` // the relative humidity in percent
	Temp        Num    `json:"temp"`         // °C
	Trays       []Tray `json:"tray"`
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

// ActiveSlot returns the tray that feeds now (Unit*4+Index), or -1.
func (r Report) ActiveSlot() int {
	n, err := strconv.Atoi(r.AMS.Now)
	if err != nil || n >= 254 {
		return -1
	}
	return n
}

// AMSMapping returns the tray (Unit*4+Index) of each filament of the print, from Mapping, or
// -1 for a filament with no tray. The code of a value is the AMS in the high byte and the tray
// in the low byte; 0xFE00 and higher is no tray. This code is a guess from the reports of an idle
// X2D: the first print checks it.
func (r Report) AMSMapping() []int {
	out := make([]int, len(r.Mapping))
	for i, v := range r.Mapping {
		unit, tray := int(v>>8), int(v&0xff)
		if unit >= 0xfe || tray > 3 {
			out[i] = -1
			continue
		}
		out[i] = unit*4 + tray
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

// StageText returns what the printer does now, or "" when stg_cur has no text.
func (r Report) StageText() string {
	return stageText[r.Stage.Int()]
}
