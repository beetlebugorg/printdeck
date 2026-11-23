package printer

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"printdeck/internal/bambu"
)

func TestBambuControls(t *testing.T) {
	f, err := bambu.NewFake("00TEST000000000", "12345678")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.Send(map[string]any{"gcode_state": "IDLE"})
	b := NewBambu(f.Addr(), f.Serial, f.Code, "", "X2D", nil)
	b.Client.Roots = f.Roots
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go b.Run(ctx, nil)
	waitFor(t, "link", func() bool { return b.State().Connected })
	waitFor(t, "version", func() bool { m, _ := b.Client.Version(); return m != "" })

	if err := b.Home("x", "y"); err != nil {
		t.Fatal(err)
	}
	b.update(bambu.Report{State: "IDLE"}, time.Now())
	st := b.State()
	if st.Homed != "xy" || !st.NoPosition || st.Axis[1] != [3]float64{256, 256, 260} {
		t.Errorf("homed %q nopos %v axis %v", st.Homed, st.NoPosition, st.Axis)
	}
	b.SetToolTemp(220)
	b.SetChamberTemp(45)
	b.Jog(10, 0, -1, 3000)
	b.SetFan(FanAux, 50)
	b.SpeedFactor(130)
	b.SetLight(false, true)
	b.MotorsOff()
	if st := b.State(); st.Homed != "" {
		t.Errorf("homed after motors off %q", st.Homed)
	}
	want := []string{"G28 X Y\n", "M104 S220\n", "M141 S45\n", "G91\nG1 X10.00 Y0.00 Z-1.00 F3000\nG90\n", "M106 P2 S127\n", "M18\n"}
	var got []string
	speed, light := "", ""
	waitFor(t, "requests", func() bool {
		got, speed, light = nil, "", ""
		for _, r := range f.Requests() {
			if p, ok := r["print"].(map[string]any); ok {
				switch p["command"] {
				case "gcode_line":
					got = append(got, p["param"].(string))
				case "print_speed":
					speed = p["param"].(string)
				}
			}
			if s, ok := r["system"].(map[string]any); ok && s["command"] == "ledctrl" {
				light = s["led_node"].(string) + " " + s["led_mode"].(string)
			}
		}
		return len(got) == len(want) && speed != "" && light != ""
	})
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("G-code %d: %q, want %q", i, got[i], want[i])
		}
	}
	if speed != "3" || light != "chamber_light on" {
		t.Errorf("speed %q light %q", speed, light)
	}
}

// A load of a tray sends ams_change_filament with the tray and the temperatures, and an unload
// targets 255. During a print, neither goes to the printer.
func TestBambuLoadTray(t *testing.T) {
	f, err := bambu.NewFake("00TEST000000000", "12345678")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.Send(map[string]any{"gcode_state": "IDLE"})
	b := NewBambu(f.Addr(), f.Serial, f.Code, "", "X2D", nil)
	b.Client.Roots = f.Roots
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go b.Run(ctx, nil)
	waitFor(t, "link", func() bool { return b.State().Connected })
	// The AMS comes from the printer, so the answers of the printer keep it in the state.
	var ams map[string]any
	json.Unmarshal([]byte(`{"gcode_state":"IDLE","ams":{"tray_now":"0","tray_exist_bits":"f","ams":[{"id":"0","tray":[
		{"id":"0","tray_type":"PLA","tray_color":"898989FF","nozzle_temp_max":"220"},
		{"id":"1","tray_type":"PLA","tray_color":"DCDCDCFF"},
		{"id":"2","tray_type":"PLA","tray_color":"161616FF","nozzle_temp_max":"230"},
		{"id":"3","tray_type":"PLA","tray_color":"F72323FF"}]}]}}`), &ams)
	f.Send(ams)
	waitFor(t, "the AMS", func() bool { return len(b.AMS().Trays) == 4 })
	if err := b.LoadTray(2); err != nil {
		t.Fatal(err)
	}
	if err := b.UnloadTray(); err != nil {
		t.Fatal(err)
	}
	var cmds []map[string]any
	waitFor(t, "requests", func() bool {
		cmds = nil
		for _, q := range f.Requests() {
			if p, ok := q["print"].(map[string]any); ok && p["command"] == "ams_change_filament" {
				cmds = append(cmds, p)
			}
		}
		return len(cmds) == 2
	})
	if c := cmds[0]; c["target"] != 2.0 || c["tar_temp"] != -1.0 || c["curr_temp"] != -1.0 || c["ams_id"] != 0.0 || c["slot_id"] != 2.0 {
		t.Errorf("load %v", c)
	}
	if c := cmds[1]; c["target"] != 255.0 || c["ams_id"] != 0.0 || c["slot_id"] != 255.0 || c["curr_temp"] != 220.0 {
		t.Errorf("unload %v", c)
	}
	b.update(amsReport("RUNNING", "0"), time.Now())
	if b.LoadTray(1) == nil || b.UnloadTray() == nil {
		t.Error("a load during a print")
	}
}

// The checks of the camera AI come from the report, and a change sends xcam_control_set or the
// command of the camera.
func TestBambuCameraChecks(t *testing.T) {
	f, err := bambu.NewFake("00TEST000000000", "12345678")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.Send(map[string]any{"gcode_state": "IDLE"})
	b := NewBambu(f.Addr(), f.Serial, f.Code, "", "X2D", nil)
	b.Client.Roots = f.Roots
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go b.Run(ctx, nil)
	waitFor(t, "link", func() bool { return b.State().Connected })
	f.Send(map[string]any{"xcam": map[string]any{"first_layer_inspector": true, "spaghetti_detector": true,
		"buildplate_marker_detector": false, "halt_print_sensitivity": "medium"},
		"ipcam": map[string]any{"timelapse": "disable", "ipcam_record": "enable"}})
	waitFor(t, "xcam", func() bool { return b.CameraChecks().Known })
	ch := b.CameraChecks()
	if !ch.FirstLayer || !ch.Spaghetti || ch.PlateMark || ch.Sensitivity != "medium" || ch.Timelapse || !ch.Record {
		t.Fatalf("%+v", ch)
	}
	b.SetCameraCheck(CheckSpaghetti, false)
	b.SetCameraCheck(CheckTimelapse, true)
	var xcam, cam map[string]any
	waitFor(t, "requests", func() bool {
		for _, q := range f.Requests() {
			if x, ok := q["xcam"].(map[string]any); ok {
				xcam = x
			}
			if c, ok := q["camera"].(map[string]any); ok {
				cam = c
			}
		}
		return xcam != nil && cam != nil
	})
	if xcam["command"] != "xcam_control_set" || xcam["module_name"] != "spaghetti_detector" || xcam["control"] != false ||
		xcam["halt_print_sensitivity"] != "medium" {
		t.Errorf("xcam %v", xcam)
	}
	if cam["command"] != "ipcam_timelapse" || cam["control"] != "enable" {
		t.Errorf("camera %v", cam)
	}
}

// A drying of the AMS sends ams_filament_drying with mode 1, and a stop mode 0. A drying out of
// range, or during a print, does not go to the printer.
func TestBambuDryAMS(t *testing.T) {
	f, err := bambu.NewFake("00TEST000000000", "12345678")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.Send(map[string]any{"gcode_state": "IDLE"})
	b := NewBambu(f.Addr(), f.Serial, f.Code, "", "X2D", nil)
	b.Client.Roots = f.Roots
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go b.Run(ctx, nil)
	waitFor(t, "link", func() bool { return b.State().Connected })
	if err := b.DryAMS(0, "PLA", 55, 8); err != nil {
		t.Fatal(err)
	}
	if err := b.StopDryAMS(0); err != nil {
		t.Fatal(err)
	}
	if b.DryAMS(0, "PC", 90, 8) == nil {
		t.Error("a drying at 90°")
	}
	var dries []map[string]any
	waitFor(t, "requests", func() bool {
		dries = nil
		for _, q := range f.Requests() {
			if p, ok := q["print"].(map[string]any); ok && p["command"] == "ams_filament_drying" {
				dries = append(dries, p)
			}
		}
		return len(dries) == 2
	})
	if d := dries[0]; d["ams_id"] != 0.0 || d["temp"] != 55.0 || d["duration"] != 8.0 || d["mode"] != 1.0 || d["filament"] != "PLA" {
		t.Errorf("drying %v", d)
	}
	if d := dries[1]; d["mode"] != 0.0 {
		t.Errorf("stop %v", d)
	}
	b.update(amsReport("RUNNING", "0"), time.Now())
	if b.DryAMS(0, "PLA", 55, 8) == nil {
		t.Error("a drying during a print")
	}
}

// A factor beside the percent of the level goes to the next level on that side.
func TestSpeedFactorSteps(t *testing.T) {
	for _, tc := range []struct{ now, nowPct, p, want int }{
		{2, 100, 110, 3},
		{2, 100, 90, 1},
		{3, 124, 134, 4},
		{3, 124, 114, 2},
		{1, 50, 40, 1},
		{4, 166, 176, 4},
		{2, 100, 100, 2},
		{1, 50, 166, 4},
		{4, 166, 50, 1},
		{0, 0, 130, 3},
	} {
		if got := speedLevelFor(tc.now, tc.nowPct, tc.p); got != tc.want {
			t.Errorf("level %d at %d%%, factor %d: level %d, want %d", tc.now, tc.nowPct, tc.p, got, tc.want)
		}
	}
}
