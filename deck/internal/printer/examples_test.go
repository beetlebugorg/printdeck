package printer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExamples(t *testing.T) {
	ex := Examples()
	if len(ex) < 100 {
		t.Fatalf("%d examples", len(ex))
	}
	var taz Example
	withMCU := 0
	for _, e := range ex {
		if e.File == "printer-lulzbot-taz6-2017.cfg" {
			taz = e
		}
		if strings.ContainsAny(e.File, "\n ") || !strings.HasSuffix(e.File, ".cfg") {
			t.Errorf("file name %q", e.File)
		}
		if e.MCU != "" {
			withMCU++
		}
	}
	var wh Example
	for _, e := range ex {
		if e.File == "printer-lulzbot-taz-workhorse-2019.cfg" {
			wh = e
		}
	}
	if wh.Name() != "LulzBot TAZ Workhorse" || wh.MCU != "atmega2560" {
		t.Errorf("Workhorse: %+v", wh)
	}
	if taz.Name() != "LulzBot TAZ6" || taz.Year != "2017" || taz.MCU != "atmega2560" || !taz.Fits("atmega2560") {
		t.Errorf("TAZ 6: %+v", taz)
	}
	if withMCU < len(ex)*2/3 {
		t.Errorf("only %d of %d examples have a chip", withMCU, len(ex))
	}
}

func TestExampleConfig(t *testing.T) {
	text, err := ExampleConfig("printer-lulzbot-taz6-2017.cfg", "/dev/serial/by-id/usb-UltiMachine_RAMBo-if00")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(text, "\nserial: "); n != 1 {
		t.Errorf("%d serial lines", n)
	}
	if !strings.Contains(text, "[mcu]\nserial: /dev/serial/by-id/usb-UltiMachine_RAMBo-if00\n") {
		t.Errorf("no serial in [mcu]:\n%s", text[strings.Index(text, "[mcu]"):][:120])
	}
	if !strings.HasSuffix(text, "[include /usr/share/printdeck/klipper/printdeck.cfg]\n") {
		t.Error("no include of printdeck.cfg at the end")
	}
	if NeedsSetup(writeTemp(t, text)) {
		t.Error("the config still needs the setup")
	}
}

func writeTemp(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "printer.cfg")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUseExample(t *testing.T) {
	path := writeTemp(t, "old\n")
	if err := UseExample(path, "printer-lulzbot-taz6-2017.cfg", "/dev/ttyACM0"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path + ".bak"); string(b) != "old\n" {
		t.Errorf("backup %q", b)
	}
	if NeedsSetup(path) {
		t.Error("the new config still needs the setup")
	}
}

func TestMCUSerial(t *testing.T) {
	path := writeTemp(t, "[printer]\nkinematics: cartesian\n[mcu]\nbaud: 250000\nserial: /dev/ttyACM0  # the RAMBo\n[mcu aux]\nserial: /dev/ttyUSB1\n")
	if s := MCUSerial(path); s != "/dev/ttyACM0" {
		t.Errorf("serial %q", s)
	}
}

func TestSuggestedBrand(t *testing.T) {
	if b := SuggestedBrand("UltiMachine ultimachine.com RAMBo"); b != "LulzBot" {
		t.Errorf("RAMBo: %q", b)
	}
	if b := SuggestedBrand("1a86 USB Serial"); b != "" {
		t.Errorf("CH340: %q", b)
	}
}

// A Workhorse config of an earlier version gets the SD card section one time, before the include of
// the Printdeck sections. A config of another printer stays the same.
func TestAddSDCard(t *testing.T) {
	text, err := ExampleConfig(workhorseExample, "/dev/ttyACM0")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(text, "# The SD card in the slot")
	end := strings.Index(text, "cs_pin: PB0\n")
	if start < 0 || end < start {
		t.Fatal("the Workhorse example has no SD card section")
	}
	old := text[:start] + text[end+len("cs_pin: PB0\n\n"):]
	path := writeTemp(t, old)
	if changed, err := AddSDCard(path); !changed || err != nil {
		t.Fatalf("changed %v, %v", changed, err)
	}
	b, _ := os.ReadFile(path)
	got := string(b)
	if n := strings.Count(got, "[printdeck_sdcard]"); n != 1 {
		t.Errorf("%d SD card sections", n)
	}
	if !strings.HasSuffix(got, "cs_pin: PB0\n\n# Added by Printdeck: keep this line last.\n[include /usr/share/printdeck/klipper/printdeck.cfg]\n") {
		t.Errorf("the section is not before the include:\n%s", got[len(got)-300:])
	}
	if changed, _ := AddSDCard(path); changed {
		t.Error("a second section")
	}
	taz6, _ := ExampleConfig("printer-lulzbot-taz6-2017.cfg", "/dev/ttyACM0")
	if changed, _ := AddSDCard(writeTemp(t, taz6)); changed {
		t.Error("a TAZ 6 config got the section")
	}
}
