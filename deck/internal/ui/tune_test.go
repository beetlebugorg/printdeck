package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// The Bambu Lab backend has the speed levels of the Tune page.
var _ speedLeveler = (*printer.Bambu)(nil)

// tuneX2D is an X2D with the speed levels of a Bambu Lab printer.
type tuneX2D struct {
	*x2dPrinter
	level int
	set   chan int
}

func (p *tuneX2D) SpeedLevels() ([]string, int) {
	return []string{"Silent", "Standard", "Sport", "Ludicrous"}, p.level
}

func (p *tuneX2D) SetSpeedLevel(level int) error {
	p.set <- level
	return nil
}

func newTuneX2D(level, pct int) *tuneX2D {
	p := &tuneX2D{x2dPrinter: newX2D(), level: level, set: make(chan int, 4)}
	st := &p.State_
	st.Printing, st.File, st.Completion, st.SpeedPct = true, "benchy.gcode.3mf", 40, pct
	return p
}

// Speed − and + go to the previous and the next level.
func TestTuneSpeedLevels(t *testing.T) {
	for _, tc := range []struct {
		level, pct int
		key        string
		want       int // 0: the key sends no command
		value      string
	}{
		{1, 50, "+", 2, "Silent · 50%"},
		{2, 100, "−", 1, "Standard · 100%"},
		{2, 100, "+", 3, "Standard · 100%"},
		{3, 124, "−", 2, "Sport · 124%"},
		{3, 124, "+", 4, "Sport · 124%"},
		{4, 166, "−", 3, "Ludicrous · 166%"},
		{1, 50, "−", 0, "Silent · 50%"},
		{4, 166, "+", 0, "Ludicrous · 166%"},
		{0, 100, "+", 0, "100%"},
	} {
		a := New()
		p := newTuneX2D(tc.level, tc.pct)
		a.Printer = p
		a.tune = true
		cv := gfx.New(a.Screen().W, a.Screen().H)
		if f := frameText(a); !strings.Contains(f, tc.value) {
			t.Errorf("level %d: no %q", tc.level, tc.value)
		}
		tapLabel(t, a, cv, tc.key)
		select {
		case got := <-p.set:
			if got != tc.want {
				t.Errorf("level %d %s: sent %d, want %d", tc.level, tc.key, got, tc.want)
			}
		case <-time.After(150 * time.Millisecond):
			if tc.want != 0 {
				t.Errorf("level %d %s: sent no level", tc.level, tc.key)
			}
		}
	}
}

// A printer with speed levels shows no flow and no Z offset: it has no command for them.
func TestTuneHidesFlowAndZ(t *testing.T) {
	for _, wide := range []bool{false, true} {
		a := New()
		a.Wide = wide
		a.Printer = newTuneX2D(2, 100)
		a.tune = true
		f := strings.ToUpper(frameText(a))
		for _, gone := range []string{"FLOW", "Z OFFSET", `"SAVE"`} {
			if strings.Contains(f, gone) {
				t.Errorf("wide %v: the page shows %s", wide, gone)
			}
		}
	}
}

// The ends of the levels show on the row.
func TestTuneSpeedEnds(t *testing.T) {
	for level, want := range map[int]string{1: "SLOWEST", 4: "FASTEST"} {
		a := New()
		a.Printer = newTuneX2D(level, 100)
		a.tune = true
		if f := strings.ToUpper(frameText(a)); !strings.Contains(f, want) {
			t.Errorf("level %d: no %s", level, want)
		}
	}
}

// The fan of the report has 15 steps: the 50% key lights at the step nearest to 50%.
func TestTuneFanStepLights(t *testing.T) {
	for _, fan := range []float64{7.0 / 15 * 100, 8.0 / 15 * 100} {
		if !fanKeyOn(true, fan, 50) {
			t.Errorf("fan %.1f: the 50%% key is off", fan)
		}
	}
	if fanKeyOn(false, 8.0/15*100, 50) {
		t.Error("a printer with a percent fan lit the 50% key at 53%")
	}
}

// On a tall screen, the Tune page has the aux nozzle and the chamber, and a tap opens the keypad.
func TestTuneX2DAuxAndChamber(t *testing.T) {
	for name, want := range map[string]string{"Aux": "AUX NOZZLE TEMPERATURE", "Chamber": "CHAMBER TEMPERATURE"} {
		a := New()
		a.Printer = newTuneX2D(2, 100)
		a.tune = true
		frameText(a)
		r, ok := a.KeyRect(name)
		if !ok {
			t.Fatalf("no %s card", name)
		}
		tap(a, r)
		if f := strings.ToUpper(frameText(a)); !strings.Contains(f, want) {
			t.Errorf("a tap on %s opened no keypad", name)
		}
	}
}

// The first layer of a printer with speed levels shows the print page, with no Z offset keys.
func TestFirstLayerX2DHasNoZOffset(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, large := range []bool{false, true} {
			a := New()
			a.Wide = wide
			if large {
				a.TextSize = "large"
			}
			p := newTuneX2D(2, 100)
			p.State_.Layer, p.State_.Layers = 1, 127
			a.Printer = p
			f := strings.ToUpper(frameText(a))
			for _, gone := range []string{"Z OFFSET", `"SAVE"`, `"−"`} {
				if strings.Contains(f, gone) {
					t.Errorf("wide %v large %v: the page shows %s", wide, large, gone)
				}
			}
			for _, want := range []string{"FIRST LAYER", "TUNE"} {
				if !strings.Contains(f, want) {
					t.Errorf("wide %v large %v: no %s", wide, large, want)
				}
			}
			shot(t, a, fmt.Sprintf("first-layer-x2d-wide%v-large%v", wide, large))
		}
	}
}

// A Klipper printer keeps the Z offset keys on the first layer.
func TestFirstLayerKlipperHasZOffset(t *testing.T) {
	st := paused
	st.Paused, st.Layer, st.Layers = false, 1, 20
	a, _, _ := newPrintApp(st)
	if f := strings.ToUpper(frameText(a)); !strings.Contains(f, "Z OFFSET") {
		t.Error("no Z offset on the first layer")
	}
}

// The Tune page of an X2D has no cut label on the tall and wide screens, with both text sizes.
func TestTuneX2DShots(t *testing.T) {
	var cuts []string
	gfx.OnCut = func(s string, width int) { cuts = append(cuts, s) }
	defer func() { gfx.OnCut = nil; gfx.SetFinePrint(0, 0) }()
	for _, large := range []bool{false, true} {
		for _, wide := range []bool{false, true} {
			for level, pct := range map[int]int{1: 50, 2: 100, 4: 166} {
				a := New()
				a.Wide = wide
				if large {
					a.TextSize = "large"
				}
				p := newTuneX2D(level, pct)
				p.State_.File = "cover-screw-fine-long-name_plate_1.gcode.3mf"
				a.Printer = p
				a.tune = true
				frameText(a)
				if level != 3 {
					shot(t, a, fmt.Sprintf("tune-x2d-l%d-wide%v-large%v", level, wide, large))
				}
			}
		}
	}
	if len(cuts) > 0 {
		t.Errorf("cut labels: %q", cuts)
	}
}
