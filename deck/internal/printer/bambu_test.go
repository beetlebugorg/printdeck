package printer

import (
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
