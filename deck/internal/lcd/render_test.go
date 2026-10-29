package lcd

import (
	"image/png"
	"os"
	"testing"
)

// TestRenderStatus writes pictures of the status screen to RENDER_DIR, to look at the design:
//
//	RENDER_DIR=/tmp go test ./internal/lcd -run Render
func TestRenderIcons(t *testing.T) {
	dir := os.Getenv("RENDER_DIR")
	if dir == "" {
		t.Skip()
	}
	names := []string{"status", "print", "preheat", "cool", "filament", "move", "maint", "machine", "pause", "play", "tune", "no"}
	for row := 0; row < 3; row++ {
		var c canvas
		for i, n := range names[row*4 : row*4+4] {
			c.bigIcon(n, 4+i*32, 20)
		}
		f, _ := os.Create(dir + "/icons24-" + string(rune('0'+row)) + ".png")
		png.Encode(f, BitsImage(c, 4, pixelOff, pixelOn))
		f.Close()
	}
}

func TestRenderStatus(t *testing.T) {
	dir := os.Getenv("RENDER_DIR")
	if dir == "" {
		t.Skip()
	}
	for name, s := range map[string]*Screen{
		"idle":     {Status: &Status{State: "Ready  PLA", Clock: "2:24PM", Message: "XYZ homed", Tool: 24, Bed: 23, Position: "X110 Y110 Z5.00", Hint: "Click for menu"}},
		"heating":  {Status: &Status{State: "Preparing", Clock: "2:24PM", Message: "Heating bed 52/60", Tool: 150, ToolT: 210, Bed: 52, BedT: 60, Fan: 0, Position: "X0 Y0 Z10.00"}},
		"printing": {Status: &Status{State: "Printing", Clock: "2:24PM", Message: "benchy.gcode", Tool: 210, ToolT: 210, ToolPow: 0.4, Bed: 60, BedT: 60, Fan: 1, Printing: true, Running: true, Progress: 0.42, Elapsed: "0:58", Left: "1:23", Done: "3:47PM", Layer: "17/120", Next: "1:32", Z: "3.20", File: "Garden pot"}},
		"cooling":  {Status: &Status{State: "Printing", Clock: "2:24PM", Tool: 120, Bed: 48, Fan: 0, Printing: true, Cooling: true, CoolTo: 35, Progress: 0.5, Left: "4 min", File: "Garden pot"}},
		"a-probe":  {Anim: "probe", Title: "Bed tilt", Caption: "0:42", Footer: "Stop on the deck"},
		"a-pid":    {Anim: "pid", Title: "PID tune", Caption: "3:10", Footer: "Stop on the deck"},
		"a-work":   {Anim: "work", Title: "PRINT_START", Caption: "0:05", Footer: "Stop on the deck"},
		"a-flash":  {Anim: "flash", Title: "Firmware", Caption: "Writing", Footer: "Keep the printer on", Progress: 0.4},
		"a-in":     {Anim: "in", Title: "Load filament", Caption: "", Footer: "Loading"},
		"home": {Menu: &Menu{Title: "Printdeck", Carousel: true, Selected: 2, Items: []MenuItem{{"Status", "status"},
			{"Print a file", "print"}, {"Preheat", "preheat"}, {"Cool down", "cool"}, {"Filament", "filament"}}}},
		"maint": {Menu: &Menu{Title: "Maintenance", Selected: 3, Items: []MenuItem{{"Back", "back"}, {"Bed tilt", "bed"},
			{"Probe Z offset", "probe"}, {"PID tune", "pid"}, {"Nozzle change", "maint"}, {"Firmware", "chip"}, {"Save config", "save"}}}},
		"swap": {Menu: &Menu{Title: "Filament swap", Selected: 1, Notes: []string{"3/5 Unload old filament"},
			Items: []MenuItem{{"Close", "back"}, {"Unload", "unload"}}}},
	} {
		for fr := 0; fr < 2; fr++ {
			f, _ := os.Create(dir + "/lcd-" + name + "-" + string(rune('0'+fr)) + ".png")
			png.Encode(f, s.Image(fr*3, 4))
			f.Close()
		}
	}
}
