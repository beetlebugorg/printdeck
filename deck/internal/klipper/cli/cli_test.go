package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

func TestLogFileStartsNewFile(t *testing.T) {
	name := filepath.Join(t.TempDir(), "klippy.log")
	l, err := openLog(name)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	line := []byte(strings.Repeat("x", 1023) + "\n")
	for range maxLog/len(line) + 10 {
		if _, err := l.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	cur, _ := os.Stat(name)
	old, err := os.Stat(name + ".1")
	if err != nil {
		t.Fatal("no klippy.log.1")
	}
	if cur.Size() != 10*int64(len(line)) || old.Size() > maxLog {
		t.Errorf("sizes %d and %d", cur.Size(), old.Size())
	}
}
