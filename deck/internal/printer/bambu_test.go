package printer

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"printdeck/internal/bambu"
)

func TestBambuPrint(t *testing.T) {
	hist := &History{Path: filepath.Join(t.TempDir(), "history.json")}
	b := NewBambu("192.168.1.50", "00TEST000000000", "12345678", "", "X2D", hist)
	t0 := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	report := func(r bambu.Report, at time.Duration) { b.update(r, t0.Add(at)) }

	report(bambu.Report{State: "IDLE", Nozzle: 25, Bed: 24}, 0)
	if st := b.State(); st.Printing || st.Text != "Ready" || !st.Connected || st.Tool != 25 {
		t.Fatalf("idle %+v", st)
	}
	run := bambu.Report{State: "PREPARE", Subtask: "Benchy", TaskID: "7", Stage: 2, Nozzle: 150, NozzleTarget: 220, Bed: 50, BedTarget: 60}
	report(run, time.Second)
	if st := b.State(); !st.Printing || !st.Preparing || st.Title != "Benchy" || st.Activity != "Heating the bed" {
		t.Fatalf("prepare %+v", st)
	}
	run.State, run.Stage, run.Percent, run.Remaining, run.Layer, run.Layers = "RUNNING", 0, 40, 30, 80, 200
	report(run, 10*time.Minute)
	st := b.State()
	if st.Preparing || st.Completion != 40 || st.TimeLeft != 30*time.Minute || st.Layer != 80 || st.Elapsed != 10*time.Minute-time.Second {
		t.Fatalf("running %+v", st)
	}
	run.State, run.Stage = "PAUSE", 16
	report(run, 11*time.Minute)
	if st := b.State(); !st.Paused || st.Text != "Paused" || st.Activity != "Paused by the user" {
		t.Fatalf("paused %+v", st)
	}
	run.State, run.Percent = "FINISH", 100
	report(run, 30*time.Minute)
	st = b.State()
	if st.Printing || st.Finished != "complete" || st.FinishedTitle != "Benchy" {
		t.Fatalf("finish %+v", st)
	}
	recs := hist.Records()
	if len(recs) != 1 || recs[0].Result != "complete" || recs[0].Title != "Benchy" {
		t.Fatalf("history %+v", recs)
	}
	// A stop by the user is a cancelled print.
	run.State, run.TaskID = "RUNNING", "8"
	report(run, 40*time.Minute)
	run.State, run.Error = "FAILED", bambuCancelled
	report(run, 41*time.Minute)
	if st := b.State(); st.Finished != "cancelled" {
		t.Fatalf("stop %+v", st)
	}
	// A calibration of the printer itself is no record.
	report(bambu.Report{State: "RUNNING", TaskID: "9", PrintType: "system", Subtask: "cali"}, 50*time.Minute)
	report(bambu.Report{State: "FINISH", TaskID: "9", PrintType: "system", Subtask: "cali"}, 55*time.Minute)
	if n := len(hist.Records()); n != 2 {
		t.Fatalf("records %d", n)
	}
}

func TestBambuHMS(t *testing.T) {
	b := NewBambu("192.168.1.50", "S", "C", "", "X2D", nil)
	b.update(bambu.Report{State: "IDLE", HMS: []bambu.HMS{{Attr: 0x05000600, Code: 0x00020070}}}, time.Now())
	if m := b.State().Message; m != "Printer message 0500-0600-0002-0070" {
		t.Errorf("%q", m)
	}
}

// amsReport is a report with one AMS of four trays: gray, no color, black and red PLA.
func amsReport(state string, active string) bambu.Report {
	var r bambu.Report
	b := []byte(`{"gcode_state":"` + state + `","gcode_file":"/cube.gcode.3mf","subtask_name":"cube","task_id":"5",
		"device":{"ctc":{"info":{"temp":30}}},
		"ams":{"tray_now":"` + active + `","tray_exist_bits":"f","ams":[{"id":"0","temp":"31","humidity":"1","humidity_raw":"50","tray":[
		{"id":"0","tray_type":"PLA","tray_color":"898989FF","remain":-1,"tag_uid":"0000000000000000"},
		{"id":"1","tray_type":"PLA","tray_color":"00000000","remain":-1,"tag_uid":"0000000000000000"},
		{"id":"2","tray_type":"PLA","tray_color":"161616FF","remain":-1,"tag_uid":"0000000000000000"},
		{"id":"3","tray_type":"PLA","tray_color":"F72323FF","remain":80,"tag_uid":"12AB000000000000"}]}]}}`)
	if err := json.Unmarshal(b, &r); err != nil {
		panic(err)
	}
	return r
}

func TestBambuAMS(t *testing.T) {
	dir := t.TempDir()
	b := NewBambu("192.168.1.50", "S", "C", "", "X2D", nil)
	b.Spools = &Spools{Path: filepath.Join(dir, "spools.json")}
	b.TrayPath = filepath.Join(dir, "ams-trays.json")
	b.update(amsReport("IDLE", "255"), time.Now())
	st := b.State()
	if !st.HasChamber || st.Chamber != 30 {
		t.Errorf("chamber %+v", st)
	}
	ams := b.AMS()
	if len(ams.Trays) != 4 || ams.Units[0].HumidityPct != 50 || ams.Trays[0].Color != "#898989" || ams.Trays[1].Color != "" {
		t.Fatalf("ams %+v", ams)
	}
	if ams.Trays[3].Remain != 80 || ams.Trays[0].Remain != -1 {
		t.Errorf("remain %+v", ams.Trays)
	}
	sp, err := b.AddTrayRoll(1, 1000, "#ffffff")
	if err != nil || sp.Left() != 1000 || sp.Color != "#ffffff" || sp.Material != "PLA" {
		t.Fatalf("roll %+v %v", sp, err)
	}
	l := b.Loaded()
	if len(l) != 4 || l[1].Color != "#ffffff" || l[1].Fraction != 1 || l[0].Fraction != -1 || l[3].Fraction != 0.8 {
		t.Errorf("loaded %+v", l)
	}
	// The link stays after a restart of the deck.
	b2 := NewBambu("192.168.1.50", "S", "C", "", "X2D", nil)
	b2.Spools, b2.TrayPath = b.Spools, b.TrayPath
	b2.update(amsReport("IDLE", "255"), time.Now())
	if b2.AMS().Trays[1].Roll != sp.ID {
		t.Errorf("link after restart %+v", b2.AMS().Trays[1])
	}
	// A roll is in one tray at most.
	b.LinkTray(2, sp.ID)
	if tr := b.AMS().Trays; tr[1].Roll != "" || tr[2].Roll != sp.ID {
		t.Errorf("move %+v", tr)
	}
}

func TestBambuCountUse(t *testing.T) {
	dir := t.TempDir()
	b := NewBambu("192.168.1.50", "S", "C", "", "X2D", nil)
	b.Spools = &Spools{Path: filepath.Join(dir, "spools.json")}
	b.TrayPath = filepath.Join(dir, "ams-trays.json")
	b.update(amsReport("IDLE", "255"), time.Now())
	gray, _ := b.AddTrayRoll(0, 1000, "")
	old := fetch3MF
	defer func() { fetch3MF = old }()
	fetch3MF = func(_ *bambu.Client, file string) ([]byte, error) {
		var buf bytes.Buffer
		z := zip.NewWriter(&buf)
		w, _ := z.Create("Metadata/slice_info.config")
		w.Write([]byte(`<config><plate><metadata key="index" value="1"/>` +
			`<filament id="1" type="PLA" color="#898989" used_m="3.4" used_g="10"/></plate></config>`))
		z.Close()
		return buf.Bytes(), nil
	}
	// One filament, fed from tray 1: the deck subtracts all of it at the end.
	b.update(amsReport("RUNNING", "0"), time.Now())
	b.mu.Lock()
	p := b.cur
	b.mu.Unlock()
	used := b.countUse(p, true)
	if used[gray.ID] != 10 {
		t.Fatalf("used %v (print %+v)", used, p)
	}
	if sp, _ := b.Spools.Get(gray.ID); sp.Left() != 990 {
		t.Errorf("left %v", sp.Left())
	}
	// A print that stopped at 40 % uses 40 % of the filament.
	p.percent = 40
	b.countUse(p, false)
	if sp, _ := b.Spools.Get(gray.ID); sp.Left() != 986 {
		t.Errorf("left after a stop %v", sp.Left())
	}
}
