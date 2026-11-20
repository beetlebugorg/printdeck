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
