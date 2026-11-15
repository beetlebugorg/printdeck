package bedcool

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestCoolTime(t *testing.T) {
	// 60 to 50 with tau 900: 900 * ln(38 / 28) = 274.9 s.
	if got := CoolTime(900, 60, 50).Seconds(); math.Abs(got-274.9) > 0.5 {
		t.Errorf("60 to 50: %.1f s", got)
	}
	if CoolTime(900, 50, 60) != 0 || CoolTime(900, 60, Room) != 0 {
		t.Error("no cool: not 0")
	}
}

// pla is a PLA print at 60 °C, at half of its time, with a flat part.
func pla(left time.Duration) Print {
	return Print{Busy: true, File: "a.gcode", Bed: 60, BedTarget: 60, Elapsed: time.Hour, TimeLeft: left,
		HasTimeLeft: true, Material: "PLA", Part: [3]float64{80, 60, 20}, PartOK: true}
}

func TestRunner(t *testing.T) {
	var sent []string
	r := NewRunner(DefaultSettings)
	r.Send = func(l string) error { sent = append(sent, l); return nil }
	now := time.Unix(1000, 0)
	need := CoolTime(DefaultTau, 60, 50) + margin // about 5:05
	r.Step(pla(need+time.Minute), now)
	if len(sent) != 0 {
		t.Fatalf("sent %v too early", sent)
	}
	r.Step(pla(need-time.Second), now)
	if len(sent) != 1 || sent[0] != "M140 S50" {
		t.Fatalf("sent %v, want M140 S50", sent)
	}
	if !strings.Contains(r.Info().Last, "50° 5:04 before the end") {
		t.Errorf("info %q", r.Info().Last)
	}
	r.Step(pla(time.Minute), now) // once for each print
	if len(sent) != 1 {
		t.Errorf("sent %v again", sent)
	}
	r.Step(Print{}, now) // the print ends
	// Guards: the first half, another material, a paused print, a tall part, and off.
	for name, p := range map[string]Print{
		"first half": func() Print { p := pla(time.Minute); p.Elapsed = 30 * time.Second; return p }(),
		"PETG":       func() Print { p := pla(time.Minute); p.Material = "PETG"; return p }(),
		"paused":     func() Print { p := pla(time.Minute); p.Paused = true; return p }(),
		"tall":       func() Print { p := pla(time.Minute); p.Part = [3]float64{20, 30, 60}; return p }(),
		"no time":    func() Print { p := pla(time.Minute); p.HasTimeLeft = false; return p }(),
		"bed low":    func() Print { p := pla(time.Minute); p.BedTarget = 50; return p }(),
	} {
		r := NewRunner(DefaultSettings)
		r.Send = func(l string) error { t.Errorf("%s: sent %q", name, l); return nil }
		r.Step(p, now)
	}
	r = NewRunner(DefaultSettings)
	r.Step(func() Print { p := pla(time.Minute); p.Part = [3]float64{20, 30, 60}; return p }(), now)
	if !strings.Contains(r.Info().Last, "60 mm high on a 20 mm base") {
		t.Errorf("tall info %q", r.Info().Last)
	}
	off := DefaultSettings
	off.On = false
	r = NewRunner(off)
	r.Send = func(l string) error { t.Errorf("off: sent %q", l); return nil }
	r.Step(pla(time.Minute), now)
}

// The runner learns tau from a cool of the bed that follows Newton's law.
func TestLearn(t *testing.T) {
	r := NewRunner(DefaultSettings)
	var saved Settings
	r.OnSettings = func(s Settings) { saved = s }
	const tau = 600.0
	t0 := time.Unix(1000, 0)
	for s := 0; s <= 900; s += 5 {
		bed := Room + (60-Room)*math.Exp(-float64(s)/tau)
		r.Step(Print{Bed: bed, BedTarget: 0}, t0.Add(time.Duration(s)*time.Second))
	}
	if got := r.Settings().Tau; math.Abs(got-tau) > 10 {
		t.Errorf("tau %.0f, want %.0f", got, tau)
	}
	if saved.Samples < 3 || saved.Tau != r.Settings().Tau {
		t.Errorf("saved %+v", saved)
	}
	// Heating is no cool.
	r2 := NewRunner(DefaultSettings)
	for s := 0; s <= 600; s += 5 {
		r2.Step(Print{Bed: 40 + float64(s)/30, BedTarget: 60}, t0.Add(time.Duration(s)*time.Second))
	}
	if r2.Settings().Samples != 0 {
		t.Error("learned from a heat")
	}
}

func TestParseSettings(t *testing.T) {
	s := ParseSettings(DefaultSettings.Bytes())
	if !s.On || s.Hold["PLA"] != 50 || s.Tau != DefaultTau {
		t.Errorf("round trip %+v", s)
	}
	if s := ParseSettings([]byte("{bad")); s.Hold["PLA"] != 50 {
		t.Errorf("bad file %+v", s)
	}
	if s := ParseSettings([]byte(`{"on":false,"hold":{"PETG":70}}`)); s.On || s.Hold["PLA"] != 0 || s.Hold["PETG"] != 70 || s.Tau != DefaultTau {
		t.Errorf("file %+v", s)
	}
}
