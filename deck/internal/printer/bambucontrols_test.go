package printer

import (
	"context"
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
	r := amsReport("IDLE", "0")
	r.AMS.Units[0].Trays[2].TempMax = 230
	b.update(r, time.Now())
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
	if c := cmds[0]; c["target"] != 2.0 || c["tar_temp"] != 230.0 || c["curr_temp"] != 220.0 || c["ams_id"] != 0.0 || c["slot_id"] != 2.0 {
		t.Errorf("load %v", c)
	}
	if c := cmds[1]; c["target"] != 255.0 || c["ams_id"] != 255.0 {
		t.Errorf("unload %v", c)
	}
	b.update(amsReport("RUNNING", "0"), time.Now())
	if b.LoadTray(1) == nil || b.UnloadTray() == nil {
		t.Error("a load during a print")
	}
}
