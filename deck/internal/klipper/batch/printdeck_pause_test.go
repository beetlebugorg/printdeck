package batch

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A print pauses with PRINTDECK_PAUSE, checks the height of a part with a paper test, and resumes.
// The part is 0.6 mm higher than in the file, so the print goes on 0.6 mm higher.
func TestPrintdeckPause(t *testing.T) {
	dir := t.TempDir()
	cfg, err := os.ReadFile(printerCfg)
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "printer.cfg")
	os.WriteFile(config, cfg, 0o644) // printer.cfg has [printdeck_pause], with the wipe of the printer
	gcode := filepath.Join(dir, "pause.gcode")
	os.WriteFile(gcode, []byte(`G28
M104 S210
PRINTDECK_PURGE LENGTH=10
G1 X100 Y100 Z25 F6000
PRINTDECK_PAUSE MESSAGE="Put the part in the pocket" CHECK_X=150 CHECK_Y=150 CHECK_Z=20
PRINTDECK_CHECK_HEIGHT
TESTZ Z=-0.4
ACCEPT
PRINTDECK_RESUME
G1 X120 Y100 Z20.2 F3000
GET_POSITION
`), 0o644)
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)
	result, msg := Run(config, dictFile, gcode, filepath.Join(dir, "out.serial"))
	if result != "exit" {
		t.Fatalf("run result %q: %s", result, msg)
	}
	out := logs.String()
	if !strings.Contains(out, "0.600 mm higher") {
		t.Errorf("no height check in the log:\n%s", out)
	}
	i := strings.Index(out, "toolhead:")
	if i < 0 || !strings.Contains(out[i:i+60], "Z:20.800") {
		t.Errorf("the toolhead is not 0.6 mm higher:\n%s", out[max(0, i):])
	}
}
