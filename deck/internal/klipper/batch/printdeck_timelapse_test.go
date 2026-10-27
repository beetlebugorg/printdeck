package batch

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lapseFile is a print with 5 layer changes of Cura.
const lapseFile = `G90
G1 X100 Y100 Z0.3 F6000
;LAYER:0
G1 X120 Y100 F3000
;LAYER:1
G1 X120 Y120 Z0.6 F3000
;LAYER:2
G1 X100 Y120 Z0.9 F3000
;LAYER:3
G1 X100 Y100 Z1.2 F3000
;LAYER:4
G1 X110 Y110 Z1.5 F3000
GET_POSITION
`

// runLapse prints lapseFile from the G-code folder after the command settings, and returns the
// log. The module waits 0.1 s for a picture.
func runLapse(t *testing.T, settings string) string {
	t.Helper()
	dir := t.TempDir()
	cfg, err := os.ReadFile(printerCfg)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(cfg), "path: /opt/printer_data/gcodes", "path: "+dir, 1)
	text += "\n[printdeck_timelapse]\nwait: 0.1\n"
	config := filepath.Join(dir, "printer.cfg")
	os.WriteFile(config, []byte(text), 0o644)
	os.WriteFile(filepath.Join(dir, "lapse.gcode"), []byte(lapseFile), 0o644)
	input := filepath.Join(dir, "input.gcode")
	os.WriteFile(input, []byte("G28\n"+settings+"\nSDCARD_PRINT_FILE FILENAME=lapse.gcode\n"), 0o644)
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)
	result, msg := Run(config, dictFile, input, filepath.Join(dir, "out.serial"))
	if result != "exit" {
		t.Fatalf("run result %q: %s", result, msg)
	}
	return logs.String()
}

// The print parks at its layer changes for the pictures of the deck. No deck sends a picture, so
// each frame waits for the time of wait, and the print stops the frames after 3 misses. The
// toolhead goes back to the exact position of the print after each park.
func TestPrintdeckTimelapse(t *testing.T) {
	out := runLapse(t, "PRINTDECK_TIMELAPSE ENABLE=1 PARK=1 FIRST_LAYER=1")
	if n := strings.Count(out, "printdeck_timelapse: no picture for frame"); n != 3 {
		t.Errorf("%d frames with no picture, want 3:\n%s", n, out)
	}
	if !strings.Contains(out, "no more frames in this print") {
		t.Errorf("the frames did not stop after 3 misses:\n%s", out)
	}
	for _, key := range []string{"toolhead:", "gcode:"} {
		i := strings.Index(out, key)
		if i < 0 || !strings.Contains(out[i:min(len(out), i+80)], "X:110.000000 Y:110.000000 Z:1.500000") {
			t.Errorf("the %s position is not at the end of the file:\n%s", key, out[max(0, i):])
		}
	}
}

// With FIRST_LAYER=2 and no time lapse, the print pauses after the picture of the first layer, with
// a message.
func TestPrintdeckTimelapseFirstLayerPause(t *testing.T) {
	out := runLapse(t, "PRINTDECK_TIMELAPSE ENABLE=0 FIRST_LAYER=2")
	if n := strings.Count(out, "printdeck_timelapse: no picture for frame"); n != 1 {
		t.Errorf("%d frames, want 1:\n%s", n, out)
	}
	if !strings.Contains(out, "Paused: Check the first layer") {
		t.Errorf("no pause after the first layer:\n%s", out)
	}
	if strings.Contains(out, "Finished SD card print") {
		t.Errorf("the print did not pause:\n%s", out)
	}
}
