package printer

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"printdeck/internal/bambu"
)

var changeT0 = time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)

// changeReport is a report of a print on an X2D: the extruder in use and the spool of each
// extruder (a tray of the AMS on the main nozzle, the external spool on the aux nozzle).
func changeReport(t *testing.T, state string, stage, layer, extruder, mainFeed, auxFeed int) bambu.Report {
	t.Helper()
	body := map[string]any{
		"gcode_state": state, "stg_cur": stage, "layer_num": layer, "task_id": "7", "subtask_name": "part",
		"device": map[string]any{"extruder": map[string]any{
			"state": 2 | extruder<<4,
			"info":  []any{map[string]any{"id": 0, "snow": mainFeed}, map[string]any{"id": 1, "snow": auxFeed}},
		}},
		"ams": map[string]any{"ams": []any{map[string]any{"id": "0", "tray": []any{
			map[string]any{"id": "0", "tray_type": "PLA"}, map[string]any{"id": "1", "tray_type": "PETG"},
		}}}},
		"vir_slot": []any{map[string]any{"id": "255", "tray_type": "TPU"}},
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var r bambu.Report
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

const (
	feedTray0 = 0x0000
	feedTray1 = 0x0001
	feedExt   = 0xFF00
	feedNone  = 0xFFFF
)

// step is one report of a sequence, at a time in seconds.
type step struct {
	at                float64
	state             string
	stage, layer, ext int
	mainFeed, auxFeed int
}

func run(t *testing.T, steps []step) []FilamentChange {
	t.Helper()
	var w changeWatch
	var out []FilamentChange
	for _, s := range steps {
		r := changeReport(t, s.state, s.stage, s.layer, s.ext, s.mainFeed, s.auxFeed)
		if c, ok := w.observe(r, changeT0.Add(time.Duration(s.at*float64(time.Second)))); ok {
			out = append(out, c)
		}
	}
	return out
}

// printing returns reports of normal printing on layer 5 from second a to second b, one each
// second, with tray 0 on the main nozzle.
func printing(a, b float64) []step {
	var out []step
	for at := a; at <= b; at++ {
		out = append(out, step{at, "RUNNING", 0, 5, 0, feedTray0, feedExt})
	}
	return out
}

// after returns normal reports with tray 1 from second a to second b.
func after(a, b float64) []step {
	var out []step
	for at := a; at <= b; at++ {
		out = append(out, step{at, "RUNNING", 0, 5, 0, feedTray1, feedExt})
	}
	return out
}

func seq(parts ...[]step) []step {
	var out []step
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// changing returns the reports of a change on the main nozzle, from second a to b.
func changing(a, b float64) []step {
	var out []step
	for at := a; at <= b; at++ {
		stage := 4
		switch {
		case at < a+(b-a)/3:
			stage = 22
		case at > a+2*(b-a)/3:
			stage = 24
		}
		out = append(out, step{at, "RUNNING", stage, 5, 0, feedNone, feedExt})
	}
	return out
}

func TestChangeNormal(t *testing.T) {
	got := run(t, seq(printing(0, 5), changing(6, 43), after(44, 50)))
	if len(got) != 1 {
		t.Fatalf("samples %+v", got)
	}
	c := got[0]
	if c.Seconds != 38 || c.FromTray != 0 || c.ToTray != 1 || c.NozzleChange || c.FromMaterial != "PLA" || c.ToMaterial != "PETG" {
		t.Errorf("sample %+v", c)
	}
	if !c.At.Equal(changeT0.Add(6 * time.Second)) {
		t.Errorf("at %v", c.At)
	}
}

// A normal stage for less than changeHold between two steps does not end the change.
func TestChangeNormalBetweenSteps(t *testing.T) {
	got := run(t, seq(printing(0, 5), changing(6, 10),
		[]step{{11, "RUNNING", 0, 5, 0, feedTray1, feedExt}},
		[]step{{12, "RUNNING", 24, 5, 0, feedTray1, feedExt}, {13, "RUNNING", 24, 5, 0, feedTray1, feedExt}},
		after(14, 20)))
	if len(got) != 1 || got[0].Seconds != 8 {
		t.Fatalf("samples %+v", got)
	}
}

func TestChangeNozzleToNozzle(t *testing.T) {
	var during, end []step
	for at := 6.0; at <= 30; at++ {
		during = append(during, step{at, "RUNNING", 4, 5, 1, feedTray0, feedExt})
	}
	for at := 31.0; at <= 36; at++ {
		end = append(end, step{at, "RUNNING", 0, 5, 1, feedTray0, feedExt})
	}
	got := run(t, seq(printing(0, 5), during, end))
	if len(got) != 1 {
		t.Fatalf("samples %+v", got)
	}
	c := got[0]
	if !c.NozzleChange || c.Seconds != 25 || c.FromTray != 0 || c.ToTray != bambu.ExternalMain || c.ToMaterial != "TPU" {
		t.Errorf("sample %+v", c)
	}
}

func TestChangePausedDuring(t *testing.T) {
	for name, pause := range map[string]step{
		"state": {20, "PAUSE", 16, 5, 0, feedNone, feedExt},
		"stage": {20, "RUNNING", 6, 5, 0, feedNone, feedExt},
	} {
		// The change goes on after the resume: its time is not a whole change.
		got := run(t, seq(printing(0, 5), changing(6, 19), []step{pause}, changing(21, 30), after(31, 40)))
		if len(got) != 0 {
			t.Errorf("%s: samples %+v", name, got)
		}
	}
}

func TestChangeCancelled(t *testing.T) {
	hist := &History{Path: filepath.Join(t.TempDir(), "history.json")}
	b := NewBambu("192.168.1.50", "00TEST000000000", "12345678", "", "X2D", hist)
	b.HeatLog = &HeatLog{}
	feed := func(at float64, r bambu.Report) { b.update(r, changeT0.Add(time.Duration(at*float64(time.Second)))) }
	for _, s := range seq(printing(0, 5), changing(6, 10)) {
		feed(s.at, changeReport(t, s.state, s.stage, s.layer, s.ext, s.mainFeed, s.auxFeed))
	}
	stop := changeReport(t, "FAILED", 0, 5, 0, feedTray1, feedExt)
	stop.Error = bambuCancelled
	feed(11, stop)
	for _, s := range after(12, 20) {
		feed(s.at, changeReport(t, "IDLE", 0, 0, 0, s.mainFeed, s.auxFeed))
	}
	if got := b.HeatLog.FilamentChanges(); len(got) != 0 {
		t.Fatalf("samples %+v", got)
	}
	b.useWG.Wait()
}

// The load in PREPARE, and the first change on layer 1, are the first load of the print. A later
// change counts.
func TestChangeFirstLoad(t *testing.T) {
	prepare := []step{
		{0, "PREPARE", 4, 0, 0, feedNone, feedExt}, {1, "PREPARE", 24, 0, 0, feedTray0, feedExt},
		{2, "RUNNING", 24, 0, 0, feedTray0, feedExt},
	}
	if got := run(t, seq(prepare, printing(3, 8))); len(got) != 0 {
		t.Errorf("load in PREPARE: %+v", got)
	}
	var one []step
	for at := 0.0; at < 4; at++ {
		one = append(one, step{at, "RUNNING", 0, 1, 0, feedTray0, feedExt})
	}
	for at := 4.0; at < 20; at++ {
		one = append(one, step{at, "RUNNING", 4, 1, 0, feedNone, feedExt})
	}
	for at := 20.0; at < 26; at++ {
		one = append(one, step{at, "RUNNING", 0, 1, 0, feedTray1, feedExt})
	}
	if got := run(t, one); len(got) != 0 {
		t.Errorf("first change on layer 1: %+v", got)
	}
	// A second change on the same layer counts.
	for at := 26.0; at < 36; at++ {
		one = append(one, step{at, "RUNNING", 4, 1, 0, feedNone, feedExt})
	}
	for at := 36.0; at < 42; at++ {
		one = append(one, step{at, "RUNNING", 0, 1, 0, feedTray0, feedExt})
	}
	if got := run(t, one); len(got) != 1 || got[0].Seconds != 10 {
		t.Errorf("second change: %+v", got)
	}
}

func TestChangeReportGap(t *testing.T) {
	got := run(t, seq(printing(0, 5), changing(6, 10), changing(41, 50), after(51, 60)))
	if len(got) != 0 {
		t.Fatalf("samples %+v", got)
	}
	// A gap of exactly 30 s keeps the change.
	got = run(t, seq(printing(0, 5), changing(6, 10), changing(40, 50), after(51, 60)))
	if len(got) != 1 || got[0].Seconds != 45 {
		t.Fatalf("at the limit: %+v", got)
	}
}

func TestChangeTooLong(t *testing.T) {
	var long []step
	for at := 6.0; at <= 6+changeLongest.Seconds()+20; at += 10 {
		long = append(long, step{at, "RUNNING", 4, 5, 0, feedNone, feedExt})
	}
	last := long[len(long)-1].at
	if got := run(t, seq(printing(0, 5), long, after(last+1, last+10))); len(got) != 0 {
		t.Fatalf("samples %+v", got)
	}
}

// A change back to the same tray is a reload, and gives no sample.
func TestChangeSameTray(t *testing.T) {
	if got := run(t, seq(printing(0, 5), changing(6, 20), printing(21, 45))); len(got) != 0 {
		t.Fatalf("samples %+v", got)
	}
}

// The update of the backend records a change with the planned flush of the print file. A change
// with no value in the file has no flush.
func TestChangePlannedFlush(t *testing.T) {
	b := NewBambu("192.168.1.50", "00TEST000000000", "12345678", "", "X2D", nil)
	b.HeatLog = &HeatLog{}
	feed := func(s step) {
		b.update(changeReport(t, s.state, s.stage, s.layer, s.ext, s.mainFeed, s.auxFeed), changeT0.Add(time.Duration(s.at*float64(time.Second))))
	}
	for _, s := range printing(0, 5) {
		feed(s)
	}
	b.mu.Lock()
	b.cur.mapping = []int{0, 1}
	b.cur.use = &bambu.PlateUse{Flush: bambu.FlushPlan{{1, 2}: 262.5}}
	b.mu.Unlock()
	for _, s := range seq(changing(6, 20), after(21, 30)) {
		feed(s)
	}
	for _, s := range seq(changing(31, 40), printing(41, 50)) {
		feed(s)
	}
	got := b.HeatLog.FilamentChanges()
	if len(got) != 2 {
		t.Fatalf("samples %+v", got)
	}
	if got[0].PlannedFlush == nil || *got[0].PlannedFlush != 262.5 {
		t.Errorf("flush %v", got[0].PlannedFlush)
	}
	if got[1].PlannedFlush != nil {
		t.Errorf("no value in the file: %v", *got[1].PlannedFlush)
	}
}

// The log keeps the last 200 samples, and a reload from the file gives the same samples.
func TestChangeKeepAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "heat-times.json")
	h := &HeatLog{Path: path}
	h.Sample(changeT0, HeatBed, 20, 0)
	for i := range 230 {
		h.AddFilamentChange(FilamentChange{At: changeT0.Add(time.Duration(i) * time.Minute), FromTray: 0, ToTray: 1, Seconds: float64(i + 1)})
	}
	got := h.FilamentChanges()
	if len(got) != 200 || got[0].Seconds != 31 || got[199].Seconds != 230 {
		t.Fatalf("%d samples, %v to %v", len(got), got[0].Seconds, got[len(got)-1].Seconds)
	}
	again := &HeatLog{Path: path}
	back := again.FilamentChanges()
	if len(back) != 200 || back[0].Seconds != 31 || !back[199].At.Equal(got[199].At) {
		t.Fatalf("after a reload: %d samples", len(back))
	}
	if r := again.Report().FilamentChanges; r.Samples != 200 {
		t.Errorf("report %+v", r)
	}
}

// A heat log file from before this change loads with no samples, and keeps its heat-ups.
func TestChangeOldFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "heat-times.json")
	old := `{"heat":{"bed":[{"at":"2026-11-20T10:00:00Z","from":22,"target":60,"s":130}]},"cool":{}}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &HeatLog{Path: path}
	if n := len(h.Heats(HeatBed)); n != 1 {
		t.Errorf("heat-ups %d", n)
	}
	if n := len(h.FilamentChanges()); n != 0 {
		t.Errorf("changes %d", n)
	}
}

func TestChangeJSON(t *testing.T) {
	b, _ := json.Marshal((&HeatLog{}).Report())
	if !strings.Contains(string(b), `"filament_changes":{"samples":0}`) {
		t.Errorf("empty %s", b)
	}
	flush := func(v float64) *float64 { return &v }
	h := &HeatLog{}
	for _, s := range []FilamentChange{
		{Seconds: 30, PlannedFlush: flush(200)}, {Seconds: 40, PlannedFlush: flush(300)}, {Seconds: 50},
		{Seconds: 100, NozzleChange: true, PlannedFlush: flush(630)},
	} {
		h.AddFilamentChange(s)
	}
	b, _ = json.Marshal(h.Report().FilamentChanges)
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got["samples"] != 4.0 || got["median_s"] != 45.0 || got["p25_s"] != 37.5 || got["p75_s"] != 62.5 {
		t.Errorf("all %s", b)
	}
	same := got["same_nozzle"].(map[string]any)
	if same["samples"] != 3.0 || same["median_s"] != 40.0 {
		t.Errorf("same nozzle %v", same)
	}
	pf := same["planned_flush"].(map[string]any)
	if pf["samples"] != 2.0 || math.Abs(pf["median_mm3"].(float64)-250) > 1e-9 {
		t.Errorf("planned flush %v", pf)
	}
	if n := got["nozzle_to_nozzle"].(map[string]any); n["samples"] != 1.0 || n["median_s"] != 100.0 {
		t.Errorf("nozzle to nozzle %v", n)
	}
}

// The captured idle report of an X2D has no change, and its values read as no spool.
func TestChangeCapturedIdle(t *testing.T) {
	data, err := os.ReadFile("../bambu/testdata/x2d-idle.json")
	if err != nil {
		t.Fatal(err)
	}
	var wrap struct {
		Print bambu.Report `json:"print"`
	}
	if err := json.Unmarshal(data, &wrap); err != nil {
		t.Fatal(err)
	}
	r := wrap.Print
	if r.State == "" {
		if err := json.Unmarshal(data, &r); err != nil {
			t.Fatal(err)
		}
	}
	var w changeWatch
	for i := range 5 {
		if c, ok := w.observe(r, changeT0.Add(time.Duration(i)*time.Second)); ok {
			t.Fatalf("sample %+v", c)
		}
	}
	if w.run != nil {
		t.Error("a change runs on an idle report")
	}
}
