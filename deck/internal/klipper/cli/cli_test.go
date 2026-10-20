package cli

import (
	"os"
	"regexp"
	"testing"
)

// KlipperVersion is the version of klipper.version, which pins the firmware of the board.
func TestKlipperVersionMatchesPin(t *testing.T) {
	data, err := os.ReadFile("../klipper.version")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^KLIPPER_GIT_VERSION = (\S+)$`).FindSubmatch(data)
	if m == nil || string(m[1]) != KlipperVersion {
		t.Fatalf("klipper.version has %q, cli.KlipperVersion is %q", m, KlipperVersion)
	}
}

func TestParseArgs(t *testing.T) {
	o, err := parseArgs([]string{"printer.cfg", "-l", "klippy.log", "-a/run/klippy.sock", "-d", "mcu2=b.dict", "-v"})
	if err != nil {
		t.Fatal(err)
	}
	if o.args[0] != "printer.cfg" || o.logfile != "klippy.log" || o.apiserver != "/run/klippy.sock" ||
		o.dictionary["dictionary_mcu2"] != "b.dict" || !o.verbose {
		t.Fatalf("%+v", o)
	}
	if _, err := parseArgs([]string{"a.cfg", "b.cfg"}); err == nil {
		t.Error("two config files must be an error")
	}
	if _, err := parseArgs([]string{"a.cfg", "-I", "/tmp/printer"}); err == nil {
		t.Error("-I must be an error")
	}
}
