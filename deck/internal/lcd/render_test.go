package lcd

import (
	"image/png"
	"os"
	"testing"
)

// TestRenderStatus writes pictures of the status screen to RENDER_DIR, to look at the design:
//
//	RENDER_DIR=/tmp go test ./internal/lcd -run Render
func TestRenderStatus(t *testing.T) {
	dir := os.Getenv("RENDER_DIR")
	if dir == "" {
		t.Skip()
	}
	for name, s := range map[string]*Screen{
		"idle":     {Status: &Status{State: "Ready  PLA", Clock: "14:24", Message: "XYZ homed", Tool: 24, Bed: 23, Position: "X110 Y110 Z5.00", Hint: "Click for menu"}},
		"heating":  {Status: &Status{State: "Preparing", Clock: "14:24", Message: "Heating bed 52/60", Tool: 150, ToolT: 210, Bed: 52, BedT: 60, Fan: 0, Position: "X0 Y0 Z10.00"}},
		"printing": {Status: &Status{State: "Printing", Clock: "14:24", Message: "benchy.gcode", Tool: 210, ToolT: 210, Bed: 60, BedT: 60, Fan: 1, Printing: true, Progress: 0.42, Elapsed: "0:58", Left: "1:23"}},
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
