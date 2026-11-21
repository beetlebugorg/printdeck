package printer

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
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
	// A code with no text in the lists: the part of the printer, and the code in the detail.
	b.update(bambu.Report{State: "IDLE", HMS: []bambu.HMS{{Attr: 0x05000600, Code: 0x00020070}}}, time.Now())
	st := b.State()
	if st.Message != "Mainboard message" || st.ErrDetail != "Mainboard message (0500-0600-0002-0070)" {
		t.Errorf("%q %q", st.Message, st.ErrDetail)
	}
	// A code of the list of the program, and a second message.
	b.update(bambu.Report{State: "IDLE", HMS: []bambu.HMS{{Attr: 0x03009300, Code: 0x00010008}, {Attr: 0x05000600, Code: 0x00020070}}}, time.Now())
	st = b.State()
	if !strings.HasPrefix(st.Message, "Chamber temperature is abnormal") || !strings.HasSuffix(st.Message, " · 1 more") {
		t.Errorf("%q", st.Message)
	}
	if got := b.HMSMessages(); len(got) != 2 || !strings.HasSuffix(got[0], "(0300-9300-0001-0008)") {
		t.Errorf("%q", got)
	}
	// The list of the X2D has 0500-0600-0002-0070 with no text: the panel shows no message for
	// it, and the list of the messages names it.
	path := filepath.Join(t.TempDir(), "hms.json")
	if err := bambu.SaveHMS(path, &bambu.HMSTexts{HMS: map[string]string{"0500060000020070": ""}}); err != nil {
		t.Fatal(err)
	}
	b.HMS = &bambu.HMSFile{Path: path}
	b.HMS.Refresh(context.Background(), "S")
	b.update(bambu.Report{State: "IDLE", HMS: []bambu.HMS{{Attr: 0x05000600, Code: 0x00020070}}}, time.Now())
	if st := b.State(); st.Message != "" {
		t.Errorf("a code with no text: message %q", st.Message)
	}
	if got := b.HMSMessages(); len(got) != 1 || got[0] != "No text from Bambu Lab (0500-0600-0002-0070)" {
		t.Errorf("%q", got)
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
	t.Cleanup(b.trayWG.Wait)
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
	t.Cleanup(b2.trayWG.Wait)
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
	t.Cleanup(b.trayWG.Wait)
	t.Cleanup(b.useWG.Wait)
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

func TestBambuLiveUse(t *testing.T) {
	dir := t.TempDir()
	b := NewBambu("192.168.1.50", "S", "C", "", "X2D", nil)
	b.Spools = &Spools{Path: filepath.Join(dir, "spools.json")}
	b.TrayPath = filepath.Join(dir, "ams-trays.json")
	t.Cleanup(b.trayWG.Wait)
	t.Cleanup(b.useWG.Wait)
	b.update(amsReport("IDLE", "255"), time.Now())
	gray, _ := b.AddTrayRoll(0, 1000, "")
	old := fetch3MF
	t.Cleanup(func() { fetch3MF = old })
	// 10 g of filament 1 in 10 mm of E: 2 mm purge before T0, 3 mm in layer 1, 5 mm in layer 2.
	fetch3MF = func(_ *bambu.Client, file string) ([]byte, error) {
		var buf bytes.Buffer
		z := zip.NewWriter(&buf)
		w, _ := z.Create("Metadata/slice_info.config")
		w.Write([]byte(`<config><plate><metadata key="index" value="1"/>` +
			`<filament id="1" type="PLA" color="#898989" used_m="0.01" used_g="10"/></plate></config>`))
		w, _ = z.Create("Metadata/plate_1.gcode")
		w.Write([]byte("M83\nG1 E2\nT0\nM73 P0\nM73 L1\nG1 E3\nM73 P50\nM73 L2\nG1 E5\nM73 P100\n"))
		z.Close()
		return buf.Bytes(), nil
	}
	report := func(state string, layer, percent int) {
		r := amsReport(state, "0")
		r.Layer, r.Percent = bambu.Num(layer), bambu.Num(percent)
		b.update(r, time.Now())
		b.refreshLive()
	}
	left := func() float64 {
		sp, _ := b.Spools.Get(gray.ID)
		return sp.Left()
	}
	report("RUNNING", 0, 0)
	waitFor(t, "the count of the plate", func() bool { return left() == 998 })
	report("RUNNING", 1, 49)
	if l := left(); l != 998 {
		t.Errorf("layer 1 at 49 %%: left %v, want 998", l)
	}
	report("RUNNING", 1, 50)
	if l := left(); l != 995 {
		t.Errorf("layer 1 at 50 %%: left %v, want 995", l)
	}
	// The user weighs the roll during the print: 900 g. The end subtracts only the 5 g after.
	b.Spools.Weigh(gray.ID, 900)
	if l := left(); l != 900 {
		t.Errorf("after the scale: left %v, want 900", l)
	}
	report("FINISH", 2, 100)
	waitFor(t, "the end count", func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return !b.counting
	})
	sp, _ := b.Spools.Get(gray.ID)
	if sp.Left() != 895 || sp.Live != 0 {
		t.Errorf("after the print: left %v, live %v, want 895 and 0", sp.Left(), sp.Live)
	}
}

// waitFor waits for cond, at most 2 seconds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(2 * time.Second); !cond(); {
		if time.Now().After(end) {
			t.Fatalf("no %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestBambuSpoolChanges(t *testing.T) {
	dir := t.TempDir()
	b := NewBambu("192.168.1.50", "S", "C", "", "X2D", nil)
	b.Spools = &Spools{Path: filepath.Join(dir, "spools.json")}
	b.TrayPath = filepath.Join(dir, "ams-trays.json")
	t.Cleanup(b.trayWG.Wait)
	// The first report of a new deck: the trays with a spool and no tag ask, the tagged tray gets
	// a roll from its tag.
	r := amsReport("IDLE", "255")
	r.AMS.Units[0].Trays[3].Weight = 1000
	b.update(r, time.Now())
	waitFor(t, "roll of the RFID spool", func() bool { return b.AMS().Trays[3].Roll != "" })
	sp, _ := b.Spools.Get(b.AMS().Trays[3].Roll)
	if sp.Tag != "12AB000000000000" || sp.Left() != 800 || sp.Color != "#f72323" {
		t.Errorf("rfid roll %+v", sp)
	}
	waitFor(t, "question", func() bool { _, ok := b.TrayQuestion(); return ok })
	if q, _ := b.TrayQuestion(); q.Slot != 0 {
		t.Errorf("first question %+v", q)
	}
	// The user answers tray 1 with a new roll, and declines trays 2 and 3.
	b.AddTrayRoll(0, 1000, "")
	b.AnswerTray(0)
	b.AnswerTray(1)
	b.AnswerTray(2)
	if _, ok := b.TrayQuestion(); ok {
		t.Error("a question after the answers")
	}
	// The next start of the deck asks no question: the deck has its tray file.
	b2 := NewBambu("192.168.1.50", "S", "C", "", "X2D", nil)
	b2.Spools, b2.TrayPath = b.Spools, b.TrayPath
	t.Cleanup(b2.trayWG.Wait)
	b2.update(r, time.Now())
	time.Sleep(50 * time.Millisecond)
	if q, ok := b2.TrayQuestion(); ok {
		t.Errorf("a question after a restart %+v", q)
	}
	// The spool of tray 1 goes out: its roll goes out of the tray.
	out := amsReport("IDLE", "255")
	out.AMS.Exist = "e"
	b.update(out, time.Now())
	waitFor(t, "roll out", func() bool { return b.AMS().Trays[0].Roll == "" })
	// A new spool goes in: the screens ask.
	b.update(r, time.Now())
	waitFor(t, "question of the new spool", func() bool { q, ok := b.TrayQuestion(); return ok && q.Slot == 0 })
}

// The X2D reports the G-code in the 3MF as its file: the deck reads the 3MF by the name of the
// task.
func TestBambuPlateFileOfX2D(t *testing.T) {
	b := NewBambu("192.168.1.50", "S", "C", "", "X2D", nil)
	old := fetch3MF
	t.Cleanup(func() { fetch3MF = old })
	var asked string
	fetch3MF = func(_ *bambu.Client, file string) ([]byte, error) {
		asked = file
		return nil, errors.New("no file")
	}
	for _, c := range []struct{ file, subtask, want string }{
		{"/data/Metadata/plate_1.gcode", "cover-screw", "/cover-screw.gcode.3mf"},
		{"/cube.gcode.3mf", "cube", "/cube.gcode.3mf"},
		{"/data/Metadata/plate_1.gcode", "", "/data/Metadata/plate_1.gcode"},
	} {
		b.plateFile(bambuPrint{file: c.file, subtask: c.subtask}, func(io.ReaderAt, int64) error { return nil })
		if asked != c.want {
			t.Errorf("%s, %q: asked %q, not %q", c.file, c.subtask, asked, c.want)
		}
	}
}

// A print that fails with no error code shows the message of the printer, or a text that says
// that the printer gave no reason, not "Error 00000000".
func TestBambuFailNoCode(t *testing.T) {
	b := NewBambu("192.168.1.50", "S", "C", "", "X2D", nil)
	t0 := time.Now()
	run := bambu.Report{State: "RUNNING", TaskID: "1", Subtask: "cover"}
	b.update(run, t0)
	run.State = "FAILED"
	b.update(run, t0.Add(time.Minute))
	if st := b.State(); st.Finished != "error" || st.FinishedError != "The printer gave no reason" {
		t.Fatalf("no code, no message %+v", st)
	}
	run.State, run.TaskID = "RUNNING", "2"
	b.update(run, t0.Add(2*time.Minute))
	run.State, run.HMS = "FAILED", []bambu.HMS{{Attr: 0x05000600, Code: 0x00020070}}
	b.update(run, t0.Add(3*time.Minute))
	if st := b.State(); st.FinishedError != st.Message || st.FinishedError == "" {
		t.Fatalf("no code, a message %+v", st)
	}
}
