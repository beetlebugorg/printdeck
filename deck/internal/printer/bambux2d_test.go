package printer

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"printdeck/internal/bambu"
)

// x2dReport is a part of a report of the X2D of the user during a print (2026-11-19): extruder 0
// has the external spool 255, extruder 1 has tray 3 of the AMS, and extruder 0 prints.
const x2dReport = `{
	"gcode_state": "RUNNING", "stg": [29, 2, 13, 11, 4, 8, 14, 54, 1, 3, 39, 51], "stg_cur": 0, "layer_num": 33,
	"ams": {"ams": [{"id": "0", "tray": [
		{"id": "0", "tray_type": "PLA", "tray_color": "898989FF"},
		{"id": "1", "tray_type": "PLA", "tray_color": "DCDCDCFF"},
		{"id": "2", "tray_type": "PLA", "tray_color": "161616FF"},
		{"id": "3", "tray_type": "PLA", "tray_color": "F72323FF"}]}],
		"tray_exist_bits": "f", "tray_now": "0"},
	"vir_slot": [
		{"id": "254", "tray_type": "", "tray_color": "00000000"},
		{"id": "255", "tray_type": "PLA", "tray_color": "DCDCDCFF"}],
	"device": {"extruder": {"state": 2, "info": [
		{"id": 0, "temp": 14418140, "snow": 65280},
		{"id": 1, "temp": 14418139, "snow": 2}]}}
}`

func x2dParse(t *testing.T, s string) bambu.Report {
	t.Helper()
	var r bambu.Report
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

// The X2D has two nozzles: the main nozzle with the AMS (extruder 1) and the aux nozzle with the
// external spool (extruder 0). The external spool is a fifth spool, and it feeds now.
func TestBambuX2DNozzles(t *testing.T) {
	b := NewBambu("192.168.1.50", "00TEST000000000", "12345678", "", "X2D", &History{Path: filepath.Join(t.TempDir(), "h.json")})
	b.update(x2dParse(t, x2dReport), time.Now())
	st := b.State()
	if !st.HasTool2 || st.Tool != 219 || st.ToolTarget != 220 || st.Tool2 != 220 || st.Tool2Target != 220 || st.ToolNow != 1 {
		t.Fatalf("nozzles %+v", st)
	}
	ams := b.AMS()
	if len(ams.Trays) != 5 {
		t.Fatalf("%d trays, want 5: %+v", len(ams.Trays), ams.Trays)
	}
	ext := ams.Trays[4]
	if !ext.External || ext.Slot != bambu.ExternalMain || !ext.Loaded || ext.Color != "#dcdcdc" || ext.Feeds != 1 || !ext.Active {
		t.Errorf("external %+v", ext)
	}
	if tr := ams.Trays[2]; tr.Feeds != 0 || tr.Active {
		t.Errorf("tray 3 %+v", tr)
	}
	if tr := ams.Trays[0]; tr.Feeds != -1 || tr.Active {
		t.Errorf("tray 1 %+v (tray_now 0 is not the spool of a nozzle)", tr)
	}
	if l := b.Loaded(); len(l) != 5 || !l[4].External || !l[4].Active {
		t.Errorf("loaded %+v", l)
	}
	// After the print: the main nozzle has no spool (0xFEFF) and prints. No sixth spool, and no
	// spool feeds.
	r := x2dParse(t, x2dReport)
	r.Device.Extruder.Info[1].Feed = 0xfeff
	r.Device.Extruder.State = 0x12
	b.update(r, time.Now())
	ams = b.AMS()
	if len(ams.Trays) != 5 {
		t.Fatalf("%d spools after the print, want 5", len(ams.Trays))
	}
	for _, tr := range ams.Trays {
		if tr.Active {
			t.Errorf("spool %d feeds with no spool in the main nozzle", tr.Slot)
		}
	}
}

// Before the first layer, the steps of the start of the print show with the step now.
func TestBambuX2DSteps(t *testing.T) {
	b := NewBambu("192.168.1.50", "00TEST000000000", "12345678", "", "X2D", &History{Path: filepath.Join(t.TempDir(), "h.json")})
	r := x2dParse(t, x2dReport)
	r.Layer, r.Stage = 0, 2
	b.update(r, time.Now())
	st := b.State()
	if !st.Preparing || st.Activity != "Heating the bed" || st.PrepN == 0 {
		t.Fatalf("prepare %+v", st)
	}
	steps := st.Prep[:st.PrepN]
	if steps[0].Name != "Cool the chamber" || !steps[0].Done || steps[1].Name != "Heat the bed" || !steps[1].Now || steps[2].Done {
		t.Errorf("steps %+v", steps)
	}
	// A step during the print shows as the message.
	r.Layer, r.Stage = 40, 4
	b.update(r, time.Now())
	if st := b.State(); st.Preparing || st.Message != "Changing the filament" {
		t.Errorf("filament change: preparing %v message %q", st.Preparing, st.Message)
	}
}

// A print that the deck sees first at 33 %, with 64 minutes left (after a restart of the deck):
// the time so far is about 32 minutes.
func TestBambuElapsedAfterRestart(t *testing.T) {
	b := NewBambu("192.168.1.50", "00TEST000000000", "12345678", "", "X2D", &History{Path: filepath.Join(t.TempDir(), "h.json")})
	r := x2dParse(t, x2dReport)
	r.Percent, r.Remaining = 33, 64
	now := time.Date(2026, 11, 19, 18, 0, 0, 0, time.UTC)
	b.update(r, now)
	if st := b.State(); st.Elapsed != 32*time.Minute {
		t.Errorf("elapsed %v, want 32m", st.Elapsed)
	}
}

// On the X2D, a filament on the aux nozzle takes the external spool, and a filament on the main
// nozzle a tray of the AMS, also when another tray has a nearer color.
func TestBambuX2DMatchByNozzle(t *testing.T) {
	b := NewBambu("192.168.1.50", "00TEST000000000", "12345678", "", "X2D", &History{Path: filepath.Join(t.TempDir(), "h.json")})
	b.update(x2dParse(t, x2dReport), time.Now())
	pl := bambu.Plate{Index: 1, Filaments: []bambu.Filament{
		{ID: 1, Type: "PLA", Color: "#000000", Nozzle: 1},
		{ID: 2, Type: "PLA", Color: "#d0d0cc", Nozzle: 2},
	}}
	m := b.MatchTrays(pl)
	if len(m) != 2 || m[0].Slot != 2 || m[1].Slot != bambu.ExternalMain {
		t.Fatalf("matches %+v", m)
	}
}

// The nozzles of the X2D for the slicers: extruder 1 is the main nozzle with the AMS, extruder 0
// the aux nozzle. The plate is the code that the printer found, or "".
func TestBambuX2DNozzleInfo(t *testing.T) {
	b := NewBambu("192.168.1.50", "00TEST000000000", "12345678", "", "X2D", &History{Path: filepath.Join(t.TempDir(), "h.json")})
	r := x2dParse(t, `{"gcode_state":"IDLE","device":{"plate":{"base":1,"mat":1,"cur_id":"P0101"},"nozzle":{"info":[
		{"id":0,"diameter":0.4,"type":"HS01"},{"id":1,"diameter":0.4,"type":"HS01"}]}}}`)
	b.update(r, time.Now())
	n := b.Nozzles()
	if len(n) != 2 || n[0].Role != "aux" || n[1].Role != "main" || n[1].Type != "HS01" || n[1].Diameter != 0.4 {
		t.Errorf("nozzles %+v", n)
	}
	if b.PlateID() != "P0101" {
		t.Errorf("plate %q", b.PlateID())
	}
}

// A print of a file that the deck sent has the file of the store as its path: the screens show
// its model and its picture, and Reprint prints it again. The view follows the layer.
func TestBambuSentPrintPath(t *testing.T) {
	b := NewBambu("192.168.1.50", "00TEST000000000", "12345678", "", "X2D", &History{Path: filepath.Join(t.TempDir(), "h.json")})
	b.sent = bambuSent{path: "part.gcode.3mf", name: "part.gcode.3mf"}
	r := x2dParse(t, x2dReport)
	r.Subtask, r.File, r.TaskID, r.Layer = "part", "/data/Metadata/plate_1.gcode", "795187646", 12
	b.update(r, time.Now())
	if st := b.State(); st.FilePath != "part.gcode.3mf" {
		t.Errorf("path %q", st.FilePath)
	}
	if l := b.ViewLayer(); l != 12 {
		t.Errorf("view layer %d, want 12", l)
	}
	b.SetViewLayer(5)
	if l := b.ViewLayer(); l != 5 {
		t.Errorf("view layer %d after the slider, want 5", l)
	}
	// A print that the deck did not send keeps its own name.
	r.Subtask, r.TaskID = "Panda", "9542"
	b.update(bambu.Report{State: "IDLE"}, time.Now())
	b.update(r, time.Now())
	if st := b.State(); st.FilePath == "part.gcode.3mf" {
		t.Errorf("a print of the printer has the path of the sent file")
	}
}

// Before the first layer the X2D reports no time: the deck gives the time of the slicer of the
// file that it sent, and less of it for each layer that is done.
func TestBambuTimeFromSlicer(t *testing.T) {
	dir := t.TempDir()
	write3MF(t, filepath.Join(dir, "part.gcode.3mf"), `<config><plate><metadata key="index" value="1"/><metadata key="prediction" value="11640"/></plate></config>`)
	b := NewBambu("192.168.1.50", "00TEST000000000", "12345678", "", "X2D", &History{Path: filepath.Join(t.TempDir(), "h.json")})
	b.Store = dirStore(dir)
	b.sent = bambuSent{path: "part.gcode.3mf", name: "part.gcode.3mf"}
	r := x2dParse(t, x2dReport)
	r.Subtask, r.TaskID, r.Layer, r.Layers, r.Remaining, r.Percent = "part", "7", 0, 156, 0, 0
	b.update(r, time.Now())
	if st := b.State(); !st.HasTimeLeft || st.TimeLeft != 194*time.Minute {
		t.Errorf("before the first layer: %v %v", st.HasTimeLeft, st.TimeLeft)
	}
	r.Layer = 79
	b.update(r, time.Now())
	if st := b.State(); st.TimeLeft != 97*time.Minute || st.Completion != 50 {
		t.Errorf("at layer 79 of 156: %v, %v%%", st.TimeLeft, st.Completion)
	}
	// The time of the printer, once it reports one.
	r.Remaining, r.Percent = 90, 52
	b.update(r, time.Now())
	if st := b.State(); st.TimeLeft != 90*time.Minute || st.Completion != 52 {
		t.Errorf("the time of the printer: %v, %v%%", st.TimeLeft, st.Completion)
	}
}

// A step that is not in the list of the steps (a wait between two steps) keeps the list, with
// the step before as the step now.
func TestBambuPrepStaysThroughUnknownStep(t *testing.T) {
	r := bambu.Report{State: "PREPARE", Stages: []bambu.Num{2, 13, 11, 4, 8}}
	r.Stage = 11
	out, n, now := bambuPrep(r, -1)
	if n == 0 || now != 2 || !out[2].Now {
		t.Fatalf("steps %d, now %d", n, now)
	}
	r.Stage = 200 // a step that the deck does not know
	_, n, now = bambuPrep(r, now)
	if n == 0 || now != 2 {
		t.Fatalf("the list went away: %d steps, now %d", n, now)
	}
	if name, _, ok := bambu.StepName(54); !ok || name == "" {
		t.Errorf("step 54 has no name")
	}
}

// Each spool has its nozzle, loaded or not: the AMS from the info of its unit (bits 8 to 11), and
// the external spools from their IDs. The values are from the report of the X2D of the user.
func TestBambuX2DSpoolNozzles(t *testing.T) {
	b := NewBambu("192.168.1.50", "00TEST000000000", "12345678", "", "X2D", &History{Path: filepath.Join(t.TempDir(), "h.json")})
	r := x2dParse(t, x2dReport)
	r.AMS.Units[0].Info = json.RawMessage(`"11001103"`)
	for i := range r.Device.Extruder.Info {
		r.Device.Extruder.Info[i].Feed = 0xffff // no spool in a nozzle
	}
	b.update(r, time.Now())
	for _, tr := range b.AMS().Trays {
		want := 0 // the AMS is on extruder 1, the main nozzle
		if tr.Slot == bambu.ExternalMain {
			want = 1 // external spool 255 is on extruder 0, the aux nozzle
		}
		if tr.Nozzle != want || tr.Feeds != -1 {
			t.Errorf("slot %d: nozzle %d, feeds %d, want nozzle %d", tr.Slot, tr.Nozzle, tr.Feeds, want)
		}
	}
}
