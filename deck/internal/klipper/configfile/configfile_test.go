package configfile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// dump is the form of the JSON files of tools/klipper-reference/dumpcfg.py.
func dump(fc *FileConfig) [][]any {
	var out [][]any
	for _, s := range fc.Sections() {
		opts := []any{}
		for _, o := range fc.Options(s) {
			v, _ := fc.Get(s, o)
			opts = append(opts, []any{o, v})
		}
		out = append(out, []any{s, opts})
	}
	return out
}

// The Go reader reads the same sections, options and values as Klipper's reader.
func TestMatchesKlipper(t *testing.T) {
	for _, name := range []string{"printer.cfg", "edge.cfg"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("testdata", name)
			data, err := ReadConfigFile(path)
			if err != nil {
				t.Fatal(err)
			}
			fc, err := BuildFileconfigWithIncludes(data, path)
			if err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(path + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var want [][]any
			if err := json.Unmarshal(b, &want); err != nil {
				t.Fatal(err)
			}
			gb, _ := json.Marshal(dump(fc))
			var got [][]any
			json.Unmarshal(gb, &got)
			if len(got) != len(want) {
				t.Fatalf("%d sections, want %d", len(got), len(want))
			}
			for i := range want {
				if !reflect.DeepEqual(got[i], want[i]) {
					t.Errorf("section %d:\n got %v\nwant %v", i, got[i], want[i])
				}
			}
		})
	}
}

func writeFiles(t *testing.T, files map[string]string) string {
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// An include sets its options at the place of the include line: the options after it override it.
func TestIncludeOrder(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"printer.cfg": "[a]\nx: 1\ny: 1\n[include extra*.cfg]\n[a]\ny: 3\n",
		"extra1.cfg":  "[a]\nx: 2\ny: 2\n",
		"extra2.cfg":  "[b]\nz: 2\n",
	})
	fc, _, err := LoadMainConfig(filepath.Join(dir, "printer.cfg"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range [][3]string{{"a", "x", "2"}, {"a", "y", "3"}, {"b", "z", "2"}} {
		if v, _ := fc.Get(c[0], c[1]); v != c[2] {
			t.Errorf("%s.%s = %q, want %q", c[0], c[1], v, c[2])
		}
	}
}

func TestIncludeErrors(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"loop.cfg":    "[include loop.cfg]\n",
		"missing.cfg": "[include nothere.cfg]\n",
		"noglob.cfg":  "[a]\n[include none*.cfg]\n",
	})
	for name, want := range map[string]string{"loop.cfg": "Recursive include", "missing.cfg": "does not exist", "noglob.cfg": ""} {
		_, _, err := LoadMainConfig(filepath.Join(dir, name))
		if want == "" && err != nil || want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
			t.Errorf("%s: error %v, want %q", name, err, want)
		}
	}
}

// The SAVE_CONFIG block adds its options to the config. A field that the main file sets too keeps
// the value of the main file, and is not in the autosave config.
func TestAutosave(t *testing.T) {
	dir := writeFiles(t, map[string]string{"printer.cfg": "[extruder]\npid_kp: 1\n" + autosaveHeader +
		"#*# [extruder]\n#*# pid_kp = 22.2\n#*# pid_ki = 1.08\n#*#\n#*# [bed_mesh default]\n#*# points =\n#*# \t1, 2\n#*# \t3, 4\n"})
	fc, autosave, err := LoadMainConfig(filepath.Join(dir, "printer.cfg"))
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := fc.Get("extruder", "pid_kp"); v != "1" {
		t.Errorf("pid_kp = %q", v)
	}
	if autosave.HasOption("extruder", "pid_kp") {
		t.Error("autosave has pid_kp")
	}
	if v, _ := fc.Get("bed_mesh default", "points"); v != "\n1, 2\n3, 4" {
		t.Errorf("points = %q", v)
	}
	if v, _ := autosave.Get("extruder", "pid_ki"); v != "1.08" {
		t.Errorf("autosave pid_ki = %q", v)
	}
}

func testConfig(t *testing.T, data string) (*PrinterConfig, *Wrapper) {
	dir := writeFiles(t, map[string]string{"printer.cfg": data})
	pc := NewPrinterConfig(nil)
	c, err := pc.ReadMainConfig(filepath.Join(dir, "printer.cfg"))
	if err != nil {
		t.Fatal(err)
	}
	return pc, c
}

func catch(f func()) (err error) {
	defer Recover(&err)
	f()
	return nil
}

func TestGetters(t *testing.T) {
	_, c := testConfig(t, "[s]\ni: 1_000\nf: 2.5\nb: Yes\nl: 1, 2,3\nll: 1,2\n  3,4\nc: tmc\nbad: x\n")
	s := c.GetSection("s")
	if v := s.GetInt("i"); v != 1000 {
		t.Errorf("i = %d", v)
	}
	if v := s.GetFloat("f", Above(0)); v != 2.5 {
		t.Errorf("f = %v", v)
	}
	if v := s.GetFloat("missing", Default(3)); v != 3 {
		t.Errorf("missing = %v", v)
	}
	if _, ok := s.GetFloatOptional("missing", Default(nil)); ok {
		t.Error("missing with Default(nil) is there")
	}
	if !s.GetBool("b") {
		t.Error("b is false")
	}
	if v := s.GetIntList("l", ",", 0); !reflect.DeepEqual(v, []int{1, 2, 3}) {
		t.Errorf("l = %v", v)
	}
	if v := s.GetFloatLists("ll", []string{",", "\n"}, 2); !reflect.DeepEqual(v, [][]float64{{1, 2}, {3, 4}}) {
		t.Errorf("ll = %v", v)
	}
	if v := s.GetChoice("c", map[string]any{"tmc": 1, "none": 2}); v != 1 {
		t.Errorf("c = %v", v)
	}
	for f, want := range map[string]string{
		"must be specified": "Option 'nothere' in section 's' must be specified",
		"unable to parse":   "Unable to parse option 'bad' in section 's'",
		"minimum":           "Option 'f' in section 's' must have minimum of 3.0",
		"above":             "Option 'f' in section 's' must be above 2.5",
		"count":             "Option 'l' in section 's' must have 2 elements",
		"choice":            "Choice 'tmc' for option 'c' in section 's' is not a valid choice",
		"boolean":           "Unable to parse option 'i' in section 's'",
	} {
		err := catch(map[string]func(){
			"must be specified": func() { s.GetInt("nothere") },
			"unable to parse":   func() { s.GetFloat("bad") },
			"minimum":           func() { s.GetFloat("f", Min(3)) },
			"above":             func() { s.GetFloat("f", Above(2.5)) },
			"count":             func() { s.GetIntList("l", ",", 2) },
			"choice":            func() { s.GetChoice("c", map[string]any{"x": 1}) },
			"boolean":           func() { s.GetBool("i") },
		}[f])
		var ce *Error
		if !errors.As(err, &ce) || ce.Msg != want {
			t.Errorf("%s: error %v, want %q", f, err, want)
		}
	}
}

// An option that no module reads is an error, and so is a section that no module reads.
func TestCheckUnused(t *testing.T) {
	pc, c := testConfig(t, "[printer]\nkinematics: cartesian\n[fan]\npin: PH6\ntypo: 1\n[other]\nx: 1\n")
	c.Get("kinematics")
	c.GetSection("fan").Get("pin")
	err := pc.CheckUnusedOptions(c, []string{"other"})
	if err == nil || err.Error() != "Option 'typo' is not valid in section 'fan'" {
		t.Errorf("error %v", err)
	}
	pc, c = testConfig(t, "[printer]\n[fan]\n")
	if err := pc.CheckUnusedOptions(c, []string{"printer"}); err == nil || err.Error() != "Section 'fan' is not a valid config section" {
		t.Errorf("error %v", err)
	}
}
