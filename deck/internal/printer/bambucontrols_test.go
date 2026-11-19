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
