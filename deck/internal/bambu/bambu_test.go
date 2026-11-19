package bambu

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

// loadX2D returns the "print" object of a real report of an X2D (idle, after a calibration).
func loadX2D(t *testing.T) map[string]any {
	t.Helper()
	b, err := os.ReadFile("testdata/x2d-idle.json")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m["print"].(map[string]any)
}

func TestReportX2D(t *testing.T) {
	b, _ := json.Marshal(loadX2D(t))
	var r Report
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	if r.State != "FINISH" || r.Busy() || r.PrintType != "system" {
		t.Errorf("state %q busy %v type %q", r.State, r.Busy(), r.PrintType)
	}
	if r.Name() != "new_machine_auto_cali_for_user" {
		t.Errorf("name %q", r.Name())
	}
	temp, target, ok := r.Nozzles()
	if !ok || len(temp) != 2 || temp[0] != 48 || temp[1] != 51 || target[0] != 0 {
		t.Errorf("nozzles %v %v %v", temp, target, ok)
	}
	if r.ChamberTemp() != 36 || r.Bed != 47 {
		t.Errorf("chamber %v bed %v", r.ChamberTemp(), r.Bed)
	}
	if len(r.AMS.Units) != 1 || len(r.AMS.Units[0].Trays) != 4 || r.AMS.Units[0].Trays[0].Type != "PLA" {
		t.Errorf("ams %+v", r.AMS.Units)
	}
	if len(r.HMS) != 1 || r.HMS[0].String() != "0500-0600-0002-0070" {
		t.Errorf("hms %v", r.HMS)
	}
	if r.IPCam.RTSP == "" || r.IPCam.Resolution != "1080p" {
		t.Errorf("ipcam %+v", r.IPCam)
	}
}

func TestNozzleTarget(t *testing.T) {
	var r Report
	json.Unmarshal([]byte(`{"device":{"extruder":{"info":[{"id":0,"temp":`+
		`14090460},{"id":1,"temp":25}]}}}`), &r) // 215 now, 215 target: 215<<16 | 220
	temp, target, _ := r.Nozzles()
	if temp[0] != 220 || target[0] != 215 || temp[1] != 25 || target[1] != 0 {
		t.Errorf("%v %v", temp, target)
	}
}

func TestMergeDelta(t *testing.T) {
	c := &Client{}
	c.merge([]byte(`{"print":{"gcode_state":"RUNNING","mc_percent":10,"bed_temper":60,"device":{"ctc":{"info":{"temp":30}}}}}`))
	r, ok := c.merge([]byte(`{"print":{"mc_percent":11,"device":{"bed":{"state":1}}}}`))
	if !ok || r.State != "RUNNING" || r.Percent != 11 || r.Bed != 60 || r.ChamberTemp() != 30 {
		t.Errorf("%+v", r)
	}
	if _, ok := c.merge([]byte(`{"info":{"command":"get_version"}}`)); ok {
		t.Error("a report with no print object")
	}
}

func TestClientFake(t *testing.T) {
	f, err := NewFake("00TEST000000000", "12345678")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.Send(loadX2D(t))
	c := &Client{Addr: f.Addr(), Serial: f.Serial, Code: f.Code, Roots: f.Roots}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reports := make(chan Report, 16)
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx, func(r Report) { reports <- r }) }()
	r := <-reports
	if r.State != "FINISH" {
		t.Fatalf("first report %q", r.State)
	}
	if c.Seen() != Fingerprint(f.Cert) {
		t.Errorf("seen %q", c.Seen())
	}
	f.Send(map[string]any{"gcode_state": "RUNNING", "mc_percent": 5})
	if r := <-reports; r.State != "RUNNING" || r.Percent != 5 || r.Bed != 47 {
		t.Errorf("delta %+v", r)
	}
	if err := c.Pause(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		reqs := f.Requests()
		if len(reqs) == 3 {
			p := reqs[2]["print"].(map[string]any)
			if p["command"] != "pause" || p["param"] != "" {
				t.Errorf("pause %v", reqs[2])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("requests %v", reqs)
		}
		time.Sleep(10 * time.Millisecond)
	}
	for model, fw := c.Version(); model != "Bambu Lab X2D" || fw != "01.01.00.80"; model, fw = c.Version() {
		if time.Now().After(deadline) {
			t.Fatalf("version %q %q", model, fw)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
}

func TestClientLogin(t *testing.T) {
	f, err := NewFake("00TEST000000000", "12345678")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := &Client{Addr: f.Addr(), Serial: f.Serial, Code: "wrong", Roots: f.Roots}
	if err := c.Run(ctx, nil); !errors.Is(err, ErrRefused) {
		t.Errorf("wrong code: %v", err)
	}
	// Another device: a certificate of another serial, or of a CA that is not of Bambu Lab.
	c = &Client{Addr: f.Addr(), Serial: "00OTHER00000000", Code: f.Code, Roots: f.Roots}
	if err := c.Run(ctx, nil); !errors.Is(err, ErrCert) {
		t.Errorf("another serial: %v", err)
	}
	c = &Client{Addr: f.Addr(), Serial: f.Serial, Code: f.Code}
	if err := c.Run(ctx, nil); !errors.Is(err, ErrCert) {
		t.Errorf("not a CA of Bambu Lab: %v", err)
	}
}
